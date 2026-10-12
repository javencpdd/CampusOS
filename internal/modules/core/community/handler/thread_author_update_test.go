package handler

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/community/repository"
	"github.com/campusos/CampusOS/internal/modules/core/community/service"
	identitydomain "github.com/campusos/CampusOS/internal/modules/core/identity/domain"
	identityservice "github.com/campusos/CampusOS/internal/modules/core/identity/service"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/campusos/CampusOS/pkg/middleware"
	"github.com/gin-gonic/gin"
)

func TestAuthorUpdateHTTPCurrentSessionAndSafeDomain(t *testing.T) {
	e := newOwnerHTTPEnvironment(t)
	ctx := context.Background()
	gov := repository.NewMemoryContentGovernanceRepository()
	store := reliability.NewMemoryStore()
	s := service.NewThreadService(e.repo, nil)
	s.SetGovernanceRepository(gov)
	s.SetReliability(reliability.NewService(transaction.NewMemory(e.repo, gov, store), store))
	other := &identitydomain.User{ID: "98302", Username: "other-editor", Email: "other-editor@example.test", Status: "active", AuthVersion: 1}
	if err := e.users.Create(ctx, other); err != nil {
		t.Fatal(err)
	}
	otherTokens, err := e.identity.Issue(ctx, other, identityservice.SessionMetadata{DeviceName: "Other"})
	if err != nil {
		t.Fatal(err)
	}
	admin, err := e.jwts.Admin.GenerateAccessToken(e.owner.ID, "admin", auth.AccessTokenContext{SessionID: "admin-author-session", AuthVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	serve := func(token string, bound bool, body string) *httptest.ResponseRecorder {
		h := NewThreadHandler(s)
		if bound {
			h.SetOwnerSessionProof(e.identity, e.identity)
		}
		r := gin.New()
		r.PUT("/threads/:id", middleware.JWTAuth(e.jwts.User, e.identity), h.UpdateThread)
		req := httptest.NewRequest("PUT", "/threads/98311?owner_id=98301&actor_kind=user&authentication_strength=mfa", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("X-Actor-ID", e.owner.ID)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}
	for _, tc := range []struct {
		token  string
		bound  bool
		status int
	}{{otherTokens.AccessToken, true, 403}, {admin, true, 401}, {e.tokens.AccessToken, false, 401}, {e.tokens.AccessToken, true, 200}} {
		rec := serve(tc.token, tc.bound, `{"title":"current owner edit"}`)
		if rec.Code != tc.status {
			t.Fatalf("status=%d expected=%d body=%s", rec.Code, tc.status, rec.Body.String())
		}
	}
	if rec := serve(e.tokens.AccessToken, true, `{"title":"forged edit","author_id":"98301"}`); rec.Code != 400 {
		t.Fatalf("client authority field admitted: %d", rec.Code)
	}
	if _, err := e.sessions.RevokeByUser(ctx, e.owner.ID, "author-test", time.Now()); err != nil {
		t.Fatal(err)
	}
	if rec := serve(e.tokens.AccessToken, true, `{"title":"revoked edit"}`); rec.Code != 401 {
		t.Fatalf("revoked %d", rec.Code)
	}
	row, _ := e.repo.GetByID(ctx, e.thread.ID)
	if row.Title != "current owner edit" {
		t.Fatal("denied update changed stored title")
	}
}
