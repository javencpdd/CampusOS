package mutualaid

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

func TestMutualAidAuthorWriteHTTPCurrentUserProof(t *testing.T) { runTypedAuthorHTTP(t, false) }
func TestMutualAidAuthorWriteHTTPStatusProof(t *testing.T)      { runTypedAuthorHTTP(t, true) }
func runTypedAuthorHTTP(t *testing.T, status bool) {
	gin.SetMode(gin.TestMode)
	ctx := context.Background()
	s, threads, _, _, _, _ := typedCreateFixture(t)
	created, err := s.Create(ctx, "1001", "Owner", validCreateRequest())
	if err != nil {
		t.Fatal(err)
	}
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
	other := &identitydomain.User{ID: "1002", Username: "other-author", Email: "other-author@example.test", Status: "active", AuthVersion: 1}
	if err := users.Create(ctx, other); err != nil {
		t.Fatal(err)
	}
	otherTokens, err := identity.Issue(ctx, other, identityservice.SessionMetadata{DeviceName: "Other"})
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(typedAuthorUpdateRequest())
	if status {
		body, err = json.Marshal(UpdateStatusRequest{AidStatus: AidStatusInProgress, Version: 1})
	}
	if err != nil {
		t.Fatal(err)
	}
	serve := func(token string, proof bool, payload string) *httptest.ResponseRecorder {
		h := NewHandler(s)
		if proof {
			h.SetSessionProof(identity, identity)
		}
		r := gin.New()
		handler := h.Update
		if status {
			handler = h.UpdateStatus
		}
		r.PUT("/update/:id", middleware.JWTAuth(jwt.User, identity), handler)
		req := httptest.NewRequest(http.MethodPut, "/update/"+created.Thread.ID+"?owner_id=1002&authentication_strength=mfa", strings.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("X-Actor-ID", "1002")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}
	rec := serve(tokens.AccessToken, true, string(body))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"author_id":"1001"`) || !strings.Contains(rec.Body.String(), `"created_by":"1001"`) {
		t.Fatal(rec.Code, rec.Body.String())
	}
	if row, err := threads.GetByID(ctx, created.Thread.ID); err != nil || !status && row.Title != "Updated title" {
		t.Fatal(row, err)
	}
	if rec := serve(tokens.AccessToken, true, string(body)); rec.Code != 409 {
		t.Fatal("stale detail version", rec.Code, rec.Body.String())
	}
	if rec := serve(otherTokens.AccessToken, true, string(body)); rec.Code != 403 {
		t.Fatal("other owner", rec.Code, rec.Body.String())
	}
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
