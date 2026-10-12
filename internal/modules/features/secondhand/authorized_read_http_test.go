package secondhand

import (
	"context"
	identitydomain "github.com/campusos/CampusOS/internal/modules/core/identity/domain"
	identityrepo "github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	identityservice "github.com/campusos/CampusOS/internal/modules/core/identity/service"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/campusos/CampusOS/pkg/middleware"
	"github.com/gin-gonic/gin"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSecondhandAuthorizedHTTPRealSession(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := context.Background()
	s, _, _ := newTestService(t, true)
	created, err := s.Create(ctx, "1001", "Owner", validCreateRequest())
	if err != nil {
		t.Fatal(err)
	}
	users := identityrepo.NewMemoryUserRepository()
	sessions := identityrepo.NewMemorySessionRepository()
	jwt := auth.NewDomainJWTManagers(auth.JWTConfig{Secret: "typed-user-test-secret", Issuer: "typed-user", Audience: "user", KeyID: "user-key", AccessTTL: time.Hour, RefreshTTL: time.Hour}, auth.JWTConfig{Secret: "typed-admin-test-secret", Issuer: "typed-admin", Audience: "admin", KeyID: "admin-key", AccessTTL: time.Hour})
	identity, err := identityservice.NewSessionService(sessions, users, jwt.User, identityservice.SessionConfig{IPHashSecret: "typed-test-ip-secret"})
	if err != nil {
		t.Fatal(err)
	}
	tokens := map[string]string{}
	for _, id := range []string{"1001", "1002"} {
		u := &identitydomain.User{ID: id, Username: "typed-" + id, Email: "typed-" + id + "@example.test", Status: identitydomain.UserStatusActive, AuthVersion: 1}
		if err := users.Create(ctx, u); err != nil {
			t.Fatal(err)
		}
		token, err := identity.Issue(ctx, u, identityservice.SessionMetadata{DeviceName: "Typed Test"})
		if err != nil {
			t.Fatal(err)
		}
		tokens[id] = token.AccessToken
	}
	serve := func(token, path string, bound bool) *httptest.ResponseRecorder {
		h := NewHandler(s)
		if bound {
			h.SetSessionProof(identity, identity)
		}
		r := gin.New()
		r.GET("/public/:id", h.GetPublic)
		r.GET("/list", h.ListPublic)
		r.GET("/mine/:id", middleware.JWTAuth(jwt.User, identity), h.GetMine)
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("X-Actor-ID", "1002")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}
	for _, path := range []string{"/public/" + created.Thread.ID, "/list"} {
		if rec := serve("", path, true); rec.Code != 200 {
			t.Fatalf("public %d %s", rec.Code, rec.Body.String())
		}
	}
	path := "/mine/" + created.Thread.ID + "?owner_id=1002&authentication_strength=mfa"
	if rec := serve(tokens["1001"], path, true); rec.Code != 200 {
		t.Fatalf("owner %d %s", rec.Code, rec.Body.String())
	}
	if rec := serve(tokens["1002"], path, true); rec.Code != 404 {
		t.Fatalf("other %d %s", rec.Code, rec.Body.String())
	}
	if rec := serve(tokens["1001"], path, false); rec.Code != 401 {
		t.Fatalf("missing proof %d", rec.Code)
	}
	admin, err := jwt.Admin.GenerateAccessToken("1001", "Admin", auth.AccessTokenContext{SessionID: "admin-session", AuthVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	if rec := serve(admin, path, true); rec.Code != 401 {
		t.Fatalf("same-ID admin %d", rec.Code)
	}
	if _, err := sessions.RevokeByUser(ctx, "1001", "typed-test-revoke", time.Now()); err != nil {
		t.Fatal(err)
	}
	if rec := serve(tokens["1001"], path, true); rec.Code != 401 {
		t.Fatalf("revoked %d", rec.Code)
	}
}
