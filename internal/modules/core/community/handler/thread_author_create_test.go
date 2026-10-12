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
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/campusos/CampusOS/pkg/middleware"
	"github.com/gin-gonic/gin"
)

func TestAuthorCreateHTTPCurrentSessionAndClientAuthority(t *testing.T) {
	e := newOwnerHTTPEnvironment(t)
	ctx := context.Background()
	cats := repository.NewMemoryCategoryRepository()
	cats.Create(ctx, &domain.Category{ID: "98321", NodeKind: domain.CategoryNodeBoard, LifecycleStatus: "active", Version: 1})
	types := repository.NewMemoryThreadTypePolicyRepository()
	types.Replace(ctx, "98321", []domain.ThreadType{domain.ThreadTypeDiscussion})
	gov := repository.NewMemoryContentGovernanceRepository()
	store := reliability.NewMemoryStore()
	s := service.NewThreadService(e.repo, nil)
	s.SetCategoryRepository(cats)
	s.SetThreadTypePolicyRepository(types)
	s.SetGovernanceRepository(gov)
	s.SetReliability(reliability.NewService(transaction.NewMemory(e.repo, cats, types, gov, store), store))
	admin, err := e.jwts.Admin.GenerateAccessToken(e.owner.ID, "admin", auth.AccessTokenContext{SessionID: "admin-create", AuthVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	serve := func(token string, bound bool, body string) *httptest.ResponseRecorder {
		h := NewThreadHandler(s)
		if bound {
			h.SetOwnerSessionProof(e.identity, e.identity)
		}
		r := gin.New()
		r.POST("/threads", middleware.JWTAuth(e.jwts.User, e.identity), h.CreateThread)
		req := httptest.NewRequest("POST", "/threads?owner_id=98302&authentication_strength=mfa", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("X-Actor-ID", "98302")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}
	body := `{"title":"actual owner create","content":"body","category_id":"98321"}`
	for _, tc := range []struct {
		token  string
		bound  bool
		status int
	}{{admin, true, 401}, {e.tokens.AccessToken, false, 401}, {e.tokens.AccessToken, true, 201}} {
		rec := serve(tc.token, tc.bound, body)
		if rec.Code != tc.status {
			t.Fatal(rec.Code, tc.status, rec.Body.String())
		}
	}
	if rec := serve(e.tokens.AccessToken, true, `{"title":"client authority","content":"body","category_id":"98321","author_id":"98302"}`); rec.Code != 400 {
		t.Fatal(rec.Code, rec.Body.String())
	}
	if _, err := e.sessions.RevokeByUser(ctx, e.owner.ID, "create-test", time.Now()); err != nil {
		t.Fatal(err)
	}
	if rec := serve(e.tokens.AccessToken, true, body); rec.Code != 401 {
		t.Fatal(rec.Code)
	}
}
