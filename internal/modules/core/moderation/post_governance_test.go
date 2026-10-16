package moderation

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	community "github.com/campusos/CampusOS/internal/modules/core/community"
	communitydomain "github.com/campusos/CampusOS/internal/modules/core/community/domain"
	communityrepo "github.com/campusos/CampusOS/internal/modules/core/community/repository"
	communityservice "github.com/campusos/CampusOS/internal/modules/core/community/service"
	identitydomain "github.com/campusos/CampusOS/internal/modules/core/identity/domain"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	identityrepo "github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	identityservice "github.com/campusos/CampusOS/internal/modules/core/identity/service"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/campusos/CampusOS/pkg/middleware"
	"github.com/gin-gonic/gin"
)

type checkedPostModerationFixture struct {
	service  *Service
	posts    *communityrepo.MemoryPostRepository
	threads  *communityrepo.MemoryThreadRepository
	audit    *MemoryAuditStore
	identity *identityservice.SessionService
	sessions *identityrepo.MemorySessionRepository
	jwt      *auth.JWTManager
	token    string
	store    *reliability.MemoryStore
}

func newCheckedPostModeration(t *testing.T) checkedPostModerationFixture {
	t.Helper()
	ctx := context.Background()
	now := time.Now()
	users := identityrepo.NewMemoryUserRepository()
	sessions := identityrepo.NewMemorySessionRepository()
	u := &identitydomain.User{ID: "130104", Username: "current-moderator", Nickname: "Current", Email: "current-moderator@example.test", Status: "active", AuthVersion: 1}
	if err := users.Create(ctx, u); err != nil {
		t.Fatal(err)
	}
	jwt := auth.NewJWTManager(auth.JWTConfig{Secret: "checked-post-moderation-test-key", Issuer: "checked-post-moderation", Audience: "user", AccessTTL: time.Hour, RefreshTTL: time.Hour})
	identity, err := identityservice.NewSessionService(sessions, users, jwt, identityservice.SessionConfig{IPHashSecret: "checked-post-test-ip-key"})
	if err != nil {
		t.Fatal(err)
	}
	tokens, err := identity.Issue(ctx, u, identityservice.SessionMetadata{DeviceName: "Moderator"})
	if err != nil {
		t.Fatal(err)
	}
	grants := identityrepo.NewMemoryDelegationRepository()
	if err := grants.InsertDelegations(ctx, []identityrepo.Delegation{{ID: "grant.checked-post", Kind: "grant", SubjectKind: "user", SubjectID: u.ID, Action: "community.post.delete", BoardID: "130103", NotBefore: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), RequiredStrength: "password", Status: "active", Version: 1}}); err != nil {
		t.Fatal(err)
	}
	threads := communityrepo.NewMemoryThreadRepository()
	if err := threads.Create(ctx, &communitydomain.Thread{ID: "130101", AuthorID: "130102", CategoryID: "130103", Title: "private title", Content: "private parent body", PublicationStatus: "private", ModerationStatus: "clear", DeletionStatus: "active", ReplyCount: 1}); err != nil {
		t.Fatal(err)
	}
	posts := communityrepo.NewMemoryPostRepository()
	if err := posts.Create(ctx, &communitydomain.Post{ID: "130105", ThreadID: "130101", AuthorID: "130102", Content: "private reply", Status: "published"}); err != nil {
		t.Fatal(err)
	}
	store := reliability.NewMemoryStore()
	postService := communityservice.NewPostService(posts, nil)
	postService.SetThreadRepository(threads)
	postService.SetGovernanceAuthority(identityservice.NewBoardGovernanceAuthorityService(grants, nil, nil))
	postService.SetReliability(reliability.NewService(transaction.NewMemory(posts, threads), store))
	audit := NewMemoryAuditStore()
	service := NewService(nil, community.NewModerationGateway(communityrepo.NewMemoryCategoryRepository(), threads, posts, communityservice.NewThreadService(threads, nil), postService), audit, Config{AllowDeletePost: true})
	return checkedPostModerationFixture{service: service, posts: posts, threads: threads, audit: audit, identity: identity, sessions: sessions, jwt: jwt, token: tokens.AccessToken, store: store}
}

func TestPostGovernanceModerationHTTPCurrentGrantAndAudit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, mode := range []string{"current", "missing", "revoked", "wrong_parent", "disabled"} {
		t.Run(mode, func(t *testing.T) {
			e := newCheckedPostModeration(t)
			h := NewHandler(e.service)
			h.SetSessionStrengthReader(e.identity)
			if mode != "missing" {
				h.SetUserSessionVerifier(e.identity)
			}
			if mode == "revoked" {
				e.sessions.RevokeByUser(context.Background(), "130104", "test", time.Now())
			}
			if mode == "disabled" {
				e.service.config.AllowDeletePost = false
			}
			path := "/moderation/threads/130101/posts/130105"
			if mode == "wrong_parent" {
				path = "/moderation/threads/other/posts/130105"
			}
			r := gin.New()
			r.DELETE("/moderation/threads/:id/posts/:post_id", middleware.JWTAuth(e.jwt, e.identity), h.DeletePost)
			req := httptest.NewRequest("DELETE", path+"?actor_id=130102&actor_kind=admin&board_id=other", nil)
			req.Header.Set("Authorization", "Bearer "+e.token)
			req.Header.Set("X-Actor-ID", "130102")
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			want := 403
			switch mode {
			case "current":
				want = 204
			case "missing", "revoked":
				want = 401
			}
			if rec.Code != want {
				t.Fatal(mode, rec.Code, want, rec.Body.String())
			}
			if strings.Contains(rec.Body.String(), "private") {
				t.Fatal("private content exposed", rec.Body.String())
			}
			parent, _ := e.threads.GetByID(context.Background(), "130101")
			rows := e.audit.Records()
			if mode == "current" {
				if parent.ReplyCount != 0 || len(rows) != 1 || rows[0].ActorID != "130104" {
					t.Fatal(parent, rows)
				}
			} else {
				if parent.ReplyCount != 1 || len(rows) != 0 {
					t.Fatal("denied module governance mutated state", parent, rows)
				}
			}
		})
	}
}

type failingPostModuleAudit struct{ *MemoryAuditStore }

func (a failingPostModuleAudit) Log(ctx context.Context, r AuditRecord) error {
	a.MemoryAuditStore.Log(ctx, r)
	return errors.New("mandatory module audit failed")
}
func TestPostGovernanceModerationAuditRollback(t *testing.T) {
	e := newCheckedPostModeration(t)
	e.service.audit = failingPostModuleAudit{e.audit}
	claims, err := e.jwt.VerifyAccessToken(e.token)
	if err != nil {
		t.Fatal(err)
	}
	proof := func(ctx context.Context) (identityport.PrincipalContext, error) {
		return identityport.ResolveUserSessionPrincipal(ctx, e.identity, e.identity, claims)
	}
	if err := e.service.DeletePostForPrincipal(context.Background(), "130101", "130105", proof, OperationContext{}); err == nil {
		t.Fatal("module audit failure accepted")
	}
	if rows := e.audit.Records(); len(rows) != 0 {
		t.Fatal("failed module audit retained records", rows)
	}
	post, err := e.posts.GetByID(context.Background(), "130105")
	parent, _ := e.threads.GetByID(context.Background(), "130101")
	if err != nil || post.DeletedAt != nil || parent.ReplyCount != 1 {
		t.Fatal("module audit failure did not roll back resource", post, parent, err)
	}
}
