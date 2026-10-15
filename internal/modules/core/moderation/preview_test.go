package moderation

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	community "github.com/campusos/CampusOS/internal/modules/core/community"
	communityport "github.com/campusos/CampusOS/internal/modules/core/community/port"
	communityrepo "github.com/campusos/CampusOS/internal/modules/core/community/repository"
	communityservice "github.com/campusos/CampusOS/internal/modules/core/community/service"
	identityrepo "github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	identityservice "github.com/campusos/CampusOS/internal/modules/core/identity/service"
	"github.com/campusos/CampusOS/pkg/middleware"
	"github.com/gin-gonic/gin"
)

func checkedPreviewFixture(t *testing.T) (checkedThreadFlagFixture, *identityrepo.MemoryDelegationRepository) {
	t.Helper()
	e := newCheckedThreadFlag(t)
	ctx := context.Background()
	grants := identityrepo.NewMemoryDelegationRepository()
	now := time.Now()
	if err := grants.InsertDelegations(ctx, []identityrepo.Delegation{{ID: "grant.current-preview", Kind: "grant", SubjectKind: "user", SubjectID: "130104", Action: "community.post.delete", BoardID: "130103", NotBefore: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), RequiredStrength: "password", Status: "active", Version: 1}}); err != nil {
		t.Fatal(err)
	}
	posts := communityservice.NewPostService(e.posts, nil)
	posts.SetThreadRepository(e.threads)
	posts.SetThreadCommandSource(e.core)
	posts.SetGovernanceAuthority(identityservice.NewBoardGovernanceAuthorityService(grants, nil, nil))
	e.service = NewService(nil, community.NewModerationGateway(communityrepo.NewMemoryCategoryRepository(), e.threads, e.posts, e.core, posts), e.audit, Config{AllowPin: true, AllowLock: true, AllowDeletePost: true})
	return e, grants
}

func TestModerationPreviewHTTPRealCurrentUserAndStrictBatch(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, method := range []string{"GET", "POST"} {
		for _, mode := range []string{"current", "empty", "revoked", "missing_proof", "no_role", "no_grant", "disabled", "unknown_field", "duplicate", "unknown_post", "limit"} {
			if method == "GET" && (mode == "unknown_field" || mode == "duplicate" || mode == "unknown_post" || mode == "limit") {
				continue
			}
			t.Run(method+"/"+mode, func(t *testing.T) {
				e, grants := checkedPreviewFixture(t)
				ctx := context.Background()
				h := NewHandler(e.service)
				h.SetSessionStrengthReader(e.identity)
				if mode != "missing_proof" {
					h.SetUserSessionVerifier(e.identity)
				}
				body, want := `{"post_ids":["130105"]}`, 200
				switch mode {
				case "empty":
					body = `{"post_ids":[]}`
				case "revoked":
					e.sessions.RevokeByUser(ctx, "130104", "test", time.Now())
					want = 401
				case "missing_proof":
					want = 401
				case "no_role":
					e.roles.RevokeRole(ctx, "130104", 2)
				case "no_grant":
					grants.SetStatusCAS(ctx, "grant.current-preview", 1, "revoked", time.Now())
				case "disabled":
					e.service.config = Config{}
				case "unknown_field":
					body = `{"post_ids":[],"action":"fake","actor_id":"130102"}`
					want = 400
				case "duplicate":
					body = `{"post_ids":["130105","130105"]}`
					want = 400
				case "unknown_post":
					body = `{"post_ids":["unknown"]}`
					want = 404
				case "limit":
					ids := make([]string, 51)
					for i := range ids {
						ids[i] = fmt.Sprint(i + 1)
					}
					raw, _ := json.Marshal(map[string]any{"post_ids": ids})
					body = string(raw)
					want = 400
				}
				r := gin.New()
				r.GET("/moderation/me", middleware.JWTAuth(e.jwt, e.identity), h.MyAccess)
				r.POST("/moderation/threads/:id/access", middleware.JWTAuth(e.jwt, e.identity), h.PreviewBatch)
				path := "/moderation/threads/130101/access?actor_id=130102&scope_id=other"
				if method == "GET" {
					path = "/moderation/me?thread_id=130101&actor_id=130102"
					body = ""
				}
				req := httptest.NewRequest(method, path, strings.NewReader(body))
				req.Header.Set("Authorization", "Bearer "+e.token)
				req.Header.Set("Content-Type", "application/json")
				rec := httptest.NewRecorder()
				r.ServeHTTP(rec, req)
				if rec.Code != want {
					t.Fatal(rec.Code, want, rec.Body.String())
				}
				if want != 200 {
					return
				}
				if strings.Contains(rec.Body.String(), "private") || strings.Contains(rec.Body.String(), "130103") || strings.Contains(rec.Body.String(), "130102") || strings.Contains(rec.Body.String(), "credential") {
					t.Fatal("preview leaked private facts", rec.Body.String())
				}
				wantFlags := mode != "no_role" && mode != "disabled"
				if method == "GET" {
					var envelope struct {
						Data struct {
							Actions ActionSet `json:"actions"`
						}
					}
					if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil || envelope.Data.Actions.Pin != wantFlags || envelope.Data.Actions.Lock != wantFlags || envelope.Data.Actions.DeletePost {
						t.Fatal(envelope, err)
					}
				} else {
					var envelope struct {
						Data communityport.UserModerationPreview
					}
					if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil || envelope.Data.Pin != wantFlags || envelope.Data.Lock != wantFlags {
						t.Fatal(envelope, err)
					}
					if mode == "empty" {
						if len(envelope.Data.Posts) != 0 {
							t.Fatal(envelope)
						}
					} else if len(envelope.Data.Posts) != 1 || envelope.Data.Posts[0].Delete != (mode != "no_grant" && mode != "disabled") {
						t.Fatal(envelope)
					}
				}
				if len(e.audit.Records()) != 0 {
					t.Fatal("preview wrote command audit")
				}
			})
		}
	}
}
