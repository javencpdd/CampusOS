package handler

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	"github.com/campusos/CampusOS/internal/modules/core/community/repository"
	"github.com/campusos/CampusOS/internal/modules/core/community/service"
	identityservice "github.com/campusos/CampusOS/internal/modules/core/identity/service"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/campusos/CampusOS/pkg/middleware"
	"github.com/gin-gonic/gin"
)

func TestPostReadHTTPCurrentSessionAndSafePage(t *testing.T) {
	for _, mode := range []string{"public", "anonymous_private", "owner_private", "other_private", "other_public", "admin", "missing", "missing_public", "revoked", "forged_subject", "trashed", "empty", "no_token"} {
		t.Run(mode, func(t *testing.T) {
			e := newOwnerHTTPEnvironment(t)
			ctx := context.Background()
			posts := repository.NewMemoryPostRepository()
			p := &domain.Post{ID: "137201", ThreadID: e.thread.ID, AuthorID: e.owner.ID, AuthorName: "Owner", Content: "allowed-reply-body", Status: "published", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
			if err := posts.Create(ctx, p); err != nil {
				t.Fatal(err)
			}
			s := service.NewPostService(posts, nil)
			s.SetThreadRepository(e.repo)
			h := NewPostHandler(s)
			h.SetOwnerSessionProof(e.identity, e.identity)
			token := e.tokens.AccessToken
			expected := 200
			isPublic := false
			page := "1"
			switch mode {
			case "public":
				isPublic = true
				e.thread.PublicationStatus = "published"
				e.repo.Update(ctx, e.thread)
			case "anonymous_private":
				isPublic = true
				expected = 404
			case "other_private", "other_public":
				u := *e.owner
				u.ID = "98302"
				u.Username = "reader-other"
				u.Email = "reader-other@example.test"
				e.users.Create(ctx, &u)
				issued, err := e.identity.Issue(ctx, &u, identityservice.SessionMetadata{DeviceName: "Reader"})
				if err != nil {
					t.Fatal(err)
				}
				token = issued.AccessToken
				if mode == "other_private" {
					expected = 404
				} else {
					e.thread.PublicationStatus = "published"
					e.repo.Update(ctx, e.thread)
				}
			case "admin":
				var err error
				token, err = e.jwts.Admin.GenerateAccessToken(e.owner.ID, "Admin", auth.AccessTokenContext{SessionID: "adms.fake", AuthVersion: 1})
				if err != nil {
					t.Fatal(err)
				}
				expected = 401
			case "missing", "missing_public":
				h.SetOwnerSessionProof(nil, nil)
				expected = 401
				if mode == "missing_public" {
					e.thread.PublicationStatus = "published"
					e.repo.Update(ctx, e.thread)
				}
			case "revoked":
				e.sessions.RevokeByUser(ctx, e.owner.ID, "post-read", time.Now())
				expected = 401
			case "forged_subject":
				expected = 401
			case "trashed":
				e.thread.DeletionStatus = "trashed"
				e.repo.Update(ctx, e.thread)
				expected = 404
			case "empty":
				page = "99"
			case "no_token":
				token = ""
				expected = 401
			}
			r := gin.New()
			r.GET("/threads/:id/posts", h.ListPosts)
			middlewareChain := []gin.HandlerFunc{middleware.JWTAuth(e.jwts.User, e.identity)}
			if mode == "forged_subject" {
				middlewareChain = append(middlewareChain, func(c *gin.Context) { c.Set("user_id", "98302"); c.Next() })
			}
			middlewareChain = append(middlewareChain, h.ListPostsForCurrentUser)
			r.GET("/threads/:id/posts/me", middlewareChain...)
			path := "/threads/" + e.thread.ID + "/posts"
			if !isPublic {
				path += "/me"
			}
			req := httptest.NewRequest("GET", path+"?page="+page+"&owner_id="+e.owner.ID+"&facts_version=1&actor_kind=admin&authentication_strength=mfa", nil)
			if token != "" {
				req.Header.Set("Authorization", "Bearer "+token)
			}
			req.Header.Set("X-Actor-ID", e.owner.ID)
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			if rec.Code != expected {
				t.Fatal(mode, rec.Code, expected, rec.Body.String())
			}
			body := rec.Body.String()
			if strings.Contains(body, e.thread.Content) || strings.Contains(body, "facts_version") || strings.Contains(body, "deleted_actor") || strings.Contains(body, "metadata") {
				t.Fatal("private facts leaked", body)
			}
			if expected != 200 && strings.Contains(body, p.Content) {
				t.Fatal("denied body leaked", body)
			}
			if expected == 200 && mode != "empty" && !strings.Contains(body, p.Content) {
				t.Fatal("allowed reply missing", body)
			}
		})
	}
}
