package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	"github.com/campusos/CampusOS/internal/modules/core/community/repository"
	"github.com/campusos/CampusOS/internal/modules/core/community/service"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	identityservice "github.com/campusos/CampusOS/internal/modules/core/identity/service"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/campusos/CampusOS/pkg/middleware"
	"github.com/gin-gonic/gin"
)

func TestPostAuthorHTTPCurrentSessionThreePaths(t *testing.T) {
	for _, op := range []string{"create", "edit", "delete"} {
		for _, mode := range []string{"current", "missing", "revoked", "admin", "other", "client_facts"} {
			t.Run(op+"/"+mode, func(t *testing.T) {
				e := newOwnerHTTPEnvironment(t)
				ctx := context.Background()
				cats := repository.NewMemoryCategoryRepository()
				group := "98320"
				cats.Create(ctx, &domain.Category{ID: group, NodeKind: "group", LifecycleStatus: "active", Version: 1})
				cats.Create(ctx, &domain.Category{ID: e.thread.CategoryID, ParentID: &group, NodeKind: "board", LifecycleStatus: "active", Version: 1})
				types := repository.NewMemoryThreadTypePolicyRepository()
				types.Replace(ctx, e.thread.CategoryID, []domain.ThreadType{domain.ThreadTypeDiscussion})
				threads := service.NewThreadService(e.repo, nil)
				threads.SetCategoryRepository(cats)
				threads.SetThreadTypePolicyRepository(types)
				posts := repository.NewMemoryPostRepository()
				store := reliability.NewMemoryStore()
				s := service.NewPostService(posts, nil)
				s.SetThreadRepository(e.repo)
				s.SetCategoryRepository(cats)
				s.SetThreadCommandSource(threads)
				s.SetReliability(reliability.NewService(transaction.NewMemory(posts, e.repo, cats, types), store))
				proof := func(ctx context.Context) (identityport.PrincipalContext, error) {
					claims, err := e.jwts.User.VerifyAccessToken(e.tokens.AccessToken)
					if err != nil {
						return identityport.PrincipalContext{}, err
					}
					return identityport.ResolveUserSessionPrincipal(ctx, e.identity, e.identity, claims)
				}
				id := ""
				if op != "create" {
					p, err := s.CreatePostForPrincipal(ctx, e.thread.ID, "Owner", domain.CreatePostRequest{Content: "original reply"}, proof)
					if err != nil {
						t.Fatal(err)
					}
					id = p.ID
				}
				token := e.tokens.AccessToken
				expected := 200
				if op == "create" {
					expected = 201
				}
				if op == "delete" {
					expected = 204
				}
				switch mode {
				case "missing":
					expected = 401
				case "revoked":
					e.sessions.RevokeByUser(ctx, e.owner.ID, "post-author-test", time.Now())
					expected = 401
				case "admin":
					var err error
					token, err = e.jwts.Admin.GenerateAccessToken(e.owner.ID, "Admin", auth.AccessTokenContext{SessionID: "adms.fake", AuthVersion: 1})
					if err != nil {
						t.Fatal(err)
					}
					expected = 401
				case "other":
					u := *e.owner
					u.ID = "98302"
					u.Username = "post-other"
					u.Email = "post-other@example.test"
					if err := e.users.Create(ctx, &u); err != nil {
						t.Fatal(err)
					}
					issued, err := e.identity.Issue(ctx, &u, identityservice.SessionMetadata{DeviceName: "Other Post Author"})
					if err != nil {
						t.Fatal(err)
					}
					token = issued.AccessToken
					expected = 403
				case "client_facts":
					expected = 400
				}
				h := NewPostHandler(s)
				if mode != "missing" {
					h.SetOwnerSessionProof(e.identity, e.identity)
				}
				r := gin.New()
				r.POST("/threads/:id/posts", middleware.JWTAuth(e.jwts.User, e.identity), h.CreatePost)
				r.PUT("/posts/:post_id", middleware.JWTAuth(e.jwts.User, e.identity), h.UpdatePost)
				r.DELETE("/posts/:post_id", middleware.JWTAuth(e.jwts.User, e.identity), h.DeletePost)
				method, path, body := "POST", fmt.Sprintf("/threads/%s/posts", e.thread.ID), `{"content":"current reply"}`
				if op == "edit" {
					method, path = "PUT", "/posts/"+id
				}
				if op == "delete" {
					method, path, body = "DELETE", "/posts/"+id, ""
				}
				if mode == "client_facts" && op != "delete" {
					body = `{"content":"forged","author_id":"98301","facts_version":1}`
				}
				if mode == "client_facts" && op == "delete" {
					expected = 204
				} // There is no input DTO on DELETE; headers/query grant nothing.
				req := httptest.NewRequest(method, path+"?actor_kind=admin&authentication_strength=mfa&owner_id=98301", strings.NewReader(body))
				req.Header.Set("Authorization", "Bearer "+token)
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("X-Actor-ID", e.owner.ID)
				rec := httptest.NewRecorder()
				r.ServeHTTP(rec, req)
				if rec.Code != expected {
					t.Fatal(op, mode, rec.Code, expected, rec.Body.String())
				}
				if strings.Contains(rec.Body.String(), e.thread.Content) || strings.Contains(rec.Body.String(), "facts_version") || strings.Contains(rec.Body.String(), "deleted_actor") {
					t.Fatal("private facts leaked", rec.Body.String())
				}
				if op == "create" && expected == 201 {
					var result struct {
						Data domain.Post `json:"data"`
					}
					if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil || result.Data.AuthorID != e.owner.ID {
						t.Fatal(result, err)
					}
				}
			})
		}
	}
}
