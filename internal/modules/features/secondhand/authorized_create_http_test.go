package secondhand

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	identitydomain "github.com/campusos/CampusOS/internal/modules/core/identity/domain"
	identityrepo "github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	identityservice "github.com/campusos/CampusOS/internal/modules/core/identity/service"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/campusos/CampusOS/pkg/middleware"
	"github.com/gin-gonic/gin"
)

func TestSecondhandAuthorCreateHTTPCurrentUserProof(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := context.Background()
	s, threads, _, _, _, _ := typedCreateFixture(t)
	users := identityrepo.NewMemoryUserRepository()
	sessions := identityrepo.NewMemorySessionRepository()
	jwt := auth.NewDomainJWTManagers(auth.JWTConfig{Secret: "typed-create-user-test-secret", Issuer: "typed-create-user", Audience: "user", KeyID: "user", AccessTTL: time.Hour, RefreshTTL: time.Hour}, auth.JWTConfig{Secret: "typed-create-admin-test-secret", Issuer: "typed-create-admin", Audience: "admin", KeyID: "admin", AccessTTL: time.Hour})
	identity, err := identityservice.NewSessionService(sessions, users, jwt.User, identityservice.SessionConfig{IPHashSecret: "typed-create-test-ip-secret"})
	if err != nil {
		t.Fatal(err)
	}
	user := &identitydomain.User{ID: "1001", Username: "typed-owner", Nickname: "Before", Email: "typed-owner@example.test", Status: identitydomain.UserStatusActive, AuthVersion: 1}
	if err := users.Create(ctx, user); err != nil {
		t.Fatal(err)
	}
	tokens, err := identity.Issue(ctx, user, identityservice.SessionMetadata{DeviceName: "Creator Test"})
	if err != nil {
		t.Fatal(err)
	}
	user.Nickname = "Current Owner"
	if err := users.Update(ctx, user); err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(validCreateRequest())
	if err != nil {
		t.Fatal(err)
	}
	serve := func(token string, proof bool, payload string) *httptest.ResponseRecorder {
		h := NewHandler(s)
		if proof {
			h.SetSessionProof(identity, identity)
		}
		r := gin.New()
		r.POST("/create", middleware.JWTAuth(jwt.User, identity), h.Create)
		req := httptest.NewRequest(http.MethodPost, "/create?owner_id=1002&authentication_strength=mfa", strings.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("X-Actor-ID", "1002")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}
	rec := serve(tokens.AccessToken, true, string(body))
	if rec.Code != 201 || !strings.Contains(rec.Body.String(), `"author_id":"1001"`) || !strings.Contains(rec.Body.String(), `"author_name":"Current Owner"`) || !strings.Contains(rec.Body.String(), `"created_by":"1001"`) {
		t.Fatal(rec.Code, rec.Body.String())
	}
	before := threads.Snapshot()
	originalStore := s.store
	s.store = &createDriftStore{Store: originalStore, afterCreate: func(context.Context, *Detail) {
		s.SetEnabledChecker(func() bool { return false })
	}}
	if rec := serve(tokens.AccessToken, true, string(body)); rec.Code != 409 || !reflect.DeepEqual(before, threads.Snapshot()) {
		t.Fatal("Feature stopped during command must conflict and roll back", rec.Code, rec.Body.String())
	}
	s.store = originalStore
	s.SetEnabledChecker(func() bool { return true })
	if rec := serve(tokens.AccessToken, false, string(body)); rec.Code != 401 {
		t.Fatal("missing proof", rec.Code, rec.Body.String())
	}
	fake := strings.TrimSuffix(string(body), "}") + `,"created_by":"1002"}`
	if rec := serve(tokens.AccessToken, true, fake); rec.Code != 400 {
		t.Fatal("forged actor", rec.Code, rec.Body.String())
	}
	admin, err := jwt.Admin.GenerateAccessToken("1001", "Admin", auth.AccessTokenContext{SessionID: "admin-session", AuthVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	if rec := serve(admin, true, string(body)); rec.Code != 401 {
		t.Fatal("same-ID admin", rec.Code, rec.Body.String())
	}
	if _, err := sessions.RevokeByUser(ctx, "1001", "test-revoke", time.Now()); err != nil {
		t.Fatal(err)
	}
	if rec := serve(tokens.AccessToken, true, string(body)); rec.Code != 401 {
		t.Fatal("revoked", rec.Code, rec.Body.String())
	}
}
