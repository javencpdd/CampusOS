package handler

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/community/repository"
	"github.com/campusos/CampusOS/internal/modules/core/community/service"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/campusos/CampusOS/pkg/middleware"
	"github.com/gin-gonic/gin"
)

func TestAuthorLifecycleHTTPCurrentSession(t *testing.T) {
	for _, op := range []string{"trash", "restore", "submit_review"} {
		t.Run(op, func(t *testing.T) {
			e := newOwnerHTTPEnvironment(t)
			ctx := context.Background()
			row, _ := e.repo.GetByID(ctx, e.thread.ID)
			if op == "restore" {
				row.DeletionStatus = "trashed"
			}
			if op == "submit_review" {
				row.ModerationStatus = "taken_down"
			}
			e.repo.Update(ctx, row)
			gov := repository.NewMemoryContentGovernanceRepository()
			store := reliability.NewMemoryStore()
			s := service.NewThreadService(e.repo, nil)
			s.SetGovernanceRepository(gov)
			s.SetReliability(reliability.NewService(transaction.NewMemory(e.repo, gov, store), store))
			admin, err := e.jwts.Admin.GenerateAccessToken(e.owner.ID, "admin", auth.AccessTokenContext{SessionID: "admin-lifecycle", AuthVersion: 1})
			if err != nil {
				t.Fatal(err)
			}
			serve := func(token string, bound bool) *httptest.ResponseRecorder {
				h := NewThreadHandler(s)
				if bound {
					h.SetOwnerSessionProof(e.identity, e.identity)
				}
				r := gin.New()
				g := r.Group("", middleware.JWTAuth(e.jwts.User, e.identity))
				method, path := "POST", "/threads/98311/restore"
				switch op {
				case "trash":
					method, path = "DELETE", "/threads/98311"
					g.DELETE("/threads/:id", h.DeleteThread)
				case "restore":
					g.POST("/threads/:id/restore", h.RestoreOwnTrash)
				case "submit_review":
					path = "/threads/98311/submit"
					g.POST("/threads/:id/submit", h.SubmitForReview)
				}
				req := httptest.NewRequest(method, path+"?owner_id=98301&action=admin_restore&authentication_strength=mfa", nil)
				req.Header.Set("Authorization", "Bearer "+token)
				rec := httptest.NewRecorder()
				r.ServeHTTP(rec, req)
				return rec
			}
			if rec := serve(admin, true); rec.Code != 401 {
				t.Fatal("admin", rec.Code)
			}
			if rec := serve(e.tokens.AccessToken, false); rec.Code != 401 {
				t.Fatal("missing proof", rec.Code, rec.Body.String())
			}
			expected := 200
			if op == "trash" {
				expected = 204
			}
			if rec := serve(e.tokens.AccessToken, true); rec.Code != expected {
				t.Fatal("owner", rec.Code, rec.Body.String())
			}
			if _, err := e.sessions.RevokeByUser(ctx, e.owner.ID, "lifecycle-test", time.Now()); err != nil {
				t.Fatal(err)
			}
			if rec := serve(e.tokens.AccessToken, true); rec.Code != 401 {
				t.Fatal("revoked", rec.Code)
			}
		})
	}
}
