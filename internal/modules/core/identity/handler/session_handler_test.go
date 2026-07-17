package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/identity/domain"
	"github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	"github.com/campusos/CampusOS/internal/modules/core/identity/service"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/campusos/CampusOS/pkg/middleware"
	"github.com/gin-gonic/gin"
)

func TestSessionHTTPFlowUsesCookieCSRFAndRejectsReuse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	users := repository.NewMemoryUserRepository()
	user := &domain.User{ID: "33001", Username: "http_session", Nickname: "HTTP Session", Email: "http-session@example.test", Status: domain.UserStatusActive, AuthVersion: 1, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err := users.Create(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	hash, err := auth.HashPassword("Secret123")
	if err != nil {
		t.Fatal(err)
	}
	if err := users.CreateVerifiedAccount(context.Background(), user.ID, user.Email, hash); err != nil {
		t.Fatal(err)
	}
	jwtManager := auth.NewJWTManager(auth.JWTConfig{Secret: "session-handler-secret", AccessTTL: time.Hour, RefreshTTL: 24 * time.Hour, Issuer: "test"})
	userService := service.NewUserService(users, jwtManager, users, nil)
	sessions, err := service.NewSessionService(repository.NewMemorySessionRepository(), users, jwtManager, service.SessionConfig{IPHashSecret: "session-handler-ip-hash-secret"})
	if err != nil {
		t.Fatal(err)
	}
	sessions.SetReliability(reliability.NewService(transaction.NewMemory(), reliability.NewMemoryStore()))
	handler := NewUserHandler(userService)
	handler.SetSessionService(sessions, SessionHTTPConfig{})

	router := gin.New()
	router.POST("/auth/login", handler.Login)
	router.POST("/auth/refresh", handler.Refresh)
	authenticated := router.Group("")
	authenticated.Use(middleware.JWTAuth(jwtManager, sessions))
	authenticated.GET("/auth/me", handler.GetMe)
	authenticated.POST("/auth/logout", handler.Logout)

	login := httptest.NewRecorder()
	loginRequest := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewBufferString(`{"email":"http-session@example.test","password":"Secret123"}`))
	loginRequest.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(login, loginRequest)
	if login.Code != http.StatusOK {
		t.Fatalf("login status=%d body=%s", login.Code, login.Body.String())
	}
	var loginPayload struct {
		Data domain.LoginResponse `json:"data"`
	}
	if err := json.Unmarshal(login.Body.Bytes(), &loginPayload); err != nil {
		t.Fatal(err)
	}
	if loginPayload.Data.AccessToken == "" || loginPayload.Data.RefreshToken != "" || loginPayload.Data.ExpiresIn != 3600 {
		t.Fatalf("unexpected v12 login response: %#v", loginPayload.Data)
	}
	loginCookies := login.Result().Cookies()
	refreshCookie := cookieByName(t, loginCookies, refreshCookieName)
	csrfCookie := cookieByName(t, loginCookies, csrfCookieName)
	if !refreshCookie.HttpOnly || csrfCookie.HttpOnly || refreshCookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("unexpected session cookies: refresh=%#v csrf=%#v", refreshCookie, csrfCookie)
	}

	me := httptest.NewRecorder()
	meRequest := httptest.NewRequest(http.MethodGet, "/auth/me", nil)
	meRequest.Header.Set("Authorization", "Bearer "+loginPayload.Data.AccessToken)
	router.ServeHTTP(me, meRequest)
	if me.Code != http.StatusOK {
		t.Fatalf("new access token not accepted: %d %s", me.Code, me.Body.String())
	}

	withoutCSRF := httptest.NewRecorder()
	withoutCSRFRequest := httptest.NewRequest(http.MethodPost, "/auth/refresh", nil)
	withoutCSRFRequest.AddCookie(refreshCookie)
	withoutCSRFRequest.AddCookie(csrfCookie)
	router.ServeHTTP(withoutCSRF, withoutCSRFRequest)
	if withoutCSRF.Code != http.StatusForbidden {
		t.Fatalf("refresh without CSRF status=%d body=%s", withoutCSRF.Code, withoutCSRF.Body.String())
	}

	refreshed := httptest.NewRecorder()
	refreshRequest := httptest.NewRequest(http.MethodPost, "/auth/refresh", nil)
	refreshRequest.AddCookie(refreshCookie)
	refreshRequest.AddCookie(csrfCookie)
	refreshRequest.Header.Set(csrfHeaderName, csrfCookie.Value)
	router.ServeHTTP(refreshed, refreshRequest)
	if refreshed.Code != http.StatusOK {
		t.Fatalf("refresh status=%d body=%s", refreshed.Code, refreshed.Body.String())
	}
	newRefreshCookie := cookieByName(t, refreshed.Result().Cookies(), refreshCookieName)
	newCSRFCookie := cookieByName(t, refreshed.Result().Cookies(), csrfCookieName)

	reused := httptest.NewRecorder()
	reusedRequest := httptest.NewRequest(http.MethodPost, "/auth/refresh", nil)
	reusedRequest.AddCookie(refreshCookie)
	reusedRequest.AddCookie(csrfCookie)
	reusedRequest.Header.Set(csrfHeaderName, csrfCookie.Value)
	router.ServeHTTP(reused, reusedRequest)
	if reused.Code != http.StatusUnauthorized {
		t.Fatalf("old refresh reuse status=%d body=%s", reused.Code, reused.Body.String())
	}

	familyRevoked := httptest.NewRecorder()
	familyRevokedRequest := httptest.NewRequest(http.MethodPost, "/auth/refresh", nil)
	familyRevokedRequest.AddCookie(newRefreshCookie)
	familyRevokedRequest.AddCookie(newCSRFCookie)
	familyRevokedRequest.Header.Set(csrfHeaderName, newCSRFCookie.Value)
	router.ServeHTTP(familyRevoked, familyRevokedRequest)
	if familyRevoked.Code != http.StatusUnauthorized {
		t.Fatalf("refresh from reused family status=%d body=%s", familyRevoked.Code, familyRevoked.Body.String())
	}
}

func cookieByName(t *testing.T, cookies []*http.Cookie, name string) *http.Cookie {
	t.Helper()
	for _, cookie := range cookies {
		if cookie.Name == name {
			return cookie
		}
	}
	t.Fatalf("cookie %q missing from %#v", name, cookies)
	return nil
}
