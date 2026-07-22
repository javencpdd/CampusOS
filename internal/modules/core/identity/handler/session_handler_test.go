package handler

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base32"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestAdminLoginRequiresIndependentAdminAccount(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := context.Background()
	users := repository.NewMemoryUserRepository()
	user := &domain.User{ID: "33002", Username: "admin_login", Nickname: "Admin Login", Email: "admin-login@example.test", Status: domain.UserStatusActive, AuthVersion: 1, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err := users.Create(ctx, user); err != nil {
		t.Fatal(err)
	}
	hash, err := auth.HashPassword("Secret123")
	if err != nil {
		t.Fatal(err)
	}
	if err := users.CreateVerifiedAccount(ctx, user.ID, user.Email, hash); err != nil {
		t.Fatal(err)
	}
	roles := repository.NewMemoryRoleRepository()
	adminAccounts := repository.NewMemoryAdminAccountRepository()
	jwtManager := auth.NewJWTManager(auth.JWTConfig{Secret: "admin-session-handler-secret", AccessTTL: time.Hour, RefreshTTL: 24 * time.Hour, Issuer: "test"})
	userService := service.NewUserService(users, jwtManager, users, nil)
	userService.SetRoleRepository(roles)
	sessions, err := service.NewSessionService(repository.NewMemorySessionRepository(), users, jwtManager, service.SessionConfig{IPHashSecret: "admin-session-handler-ip-hash-secret"})
	if err != nil {
		t.Fatal(err)
	}
	handler := NewUserHandler(userService)
	handler.SetSessionService(sessions, SessionHTTPConfig{})
	handler.SetAdminAccessService(service.NewAdminAccessService(adminAccounts))
	router := gin.New()
	router.POST("/auth/admin/login", handler.AdminLogin)

	requestLogin := func() *httptest.ResponseRecorder {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/auth/admin/login", bytes.NewBufferString(`{"email":"admin-login@example.test","password":"Secret123"}`))
		request.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(response, request)
		return response
	}

	if response := requestLogin(); response.Code != http.StatusUnauthorized {
		t.Fatalf("role-free user entered Admin: status=%d body=%s", response.Code, response.Body.String())
	}
	if assigned, err := roles.AssignRole(ctx, user.ID, 1, "global", nil); err != nil || !assigned {
		t.Fatalf("assign role fixture: assigned=%v err=%v", assigned, err)
	}
	if active, err := adminAccounts.EnsureActive(ctx, user.ID, "test"); err != nil || !active {
		t.Fatalf("provision admin fixture: active=%v err=%v", active, err)
	}
	if response := requestLogin(); response.Code != http.StatusOK {
		t.Fatalf("active administrator login status=%d body=%s", response.Code, response.Body.String())
	}
	if revoked, err := adminAccounts.Revoke(ctx, user.ID); err != nil || !revoked {
		t.Fatalf("revoke admin fixture: revoked=%v err=%v", revoked, err)
	}
	if response := requestLogin(); response.Code != http.StatusUnauthorized {
		t.Fatalf("revoked administrator entered Admin: status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestMFAHTTPLoginUsesOneTimeTicketBeforeIssuingSession(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := context.Background()
	now := time.Date(2026, time.July, 22, 11, 0, 0, 0, time.UTC)
	users := repository.NewMemoryUserRepository()
	user := &domain.User{ID: "33003", Username: "mfa_http", Nickname: "MFA HTTP", Email: "mfa-http@example.test", Status: domain.UserStatusActive, AuthVersion: 1, CreatedAt: now, UpdatedAt: now}
	if err := users.Create(ctx, user); err != nil {
		t.Fatal(err)
	}
	hash, err := auth.HashPassword("Secret123")
	if err != nil {
		t.Fatal(err)
	}
	if err := users.CreateVerifiedAccount(ctx, user.ID, user.Email, hash); err != nil {
		t.Fatal(err)
	}
	jwtManager := auth.NewJWTManager(auth.JWTConfig{Secret: "mfa-http-session-secret", AccessTTL: time.Hour, RefreshTTL: 24 * time.Hour, Issuer: "test"})
	userService := service.NewUserService(users, jwtManager, users, nil)
	sessions, err := service.NewSessionService(repository.NewMemorySessionRepository(), users, jwtManager, service.SessionConfig{IPHashSecret: "mfa-http-ip-hash-secret", Clock: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	reliable := reliability.NewService(transaction.NewMemory(), reliability.NewMemoryStore())
	sessions.SetReliability(reliable)
	roles := repository.NewMemoryRoleRepository()
	permissions := service.NewPermissionService(roles, users)
	mfa, err := service.NewMFAService(repository.NewMemoryMFARepository(), users, permissions, roles, service.MFAConfig{
		ActiveKeyID: "v1", EncryptionKeys: map[string]string{"v1": "mfa-http-encryption-key-material"}, Issuer: "CampusOS Test", Clock: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	mfa.SetReliability(reliable)
	mfa.SetSessionRevoker(sessions)
	enrollment, err := mfa.StartEnrollment(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	secret, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(enrollment.ManualKey)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mfa.ConfirmEnrollment(ctx, user.ID, handlerTOTPCode(secret, now.Unix()/30)); err != nil {
		t.Fatal(err)
	}
	now = now.Add(30 * time.Second)

	handler := NewUserHandler(userService)
	handler.SetSessionService(sessions, SessionHTTPConfig{})
	handler.SetMFAService(mfa)
	router := gin.New()
	router.POST("/auth/login", handler.Login)
	router.POST("/auth/mfa/login/complete", handler.CompleteMFALogin)

	login := httptest.NewRecorder()
	loginRequest := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewBufferString(`{"email":"mfa-http@example.test","password":"Secret123"}`))
	loginRequest.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(login, loginRequest)
	if login.Code != http.StatusOK || strings.Contains(login.Body.String(), "access_token") || strings.Contains(login.Body.String(), "refresh_token") {
		t.Fatalf("first-factor response issued a session: status=%d body=%s", login.Code, login.Body.String())
	}
	var challenge struct {
		Data service.MFARequirement `json:"data"`
	}
	if err := json.Unmarshal(login.Body.Bytes(), &challenge); err != nil {
		t.Fatal(err)
	}
	if !challenge.Data.Required || challenge.Data.Ticket == "" {
		t.Fatalf("missing MFA challenge ticket: %#v", challenge.Data)
	}

	complete := httptest.NewRecorder()
	completeRequest := httptest.NewRequest(http.MethodPost, "/auth/mfa/login/complete", bytes.NewBufferString(`{"mfa_ticket":"`+challenge.Data.Ticket+`","code":"`+handlerTOTPCode(secret, now.Unix()/30)+`"}`))
	completeRequest.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(complete, completeRequest)
	if complete.Code != http.StatusOK || !strings.Contains(complete.Body.String(), "access_token") {
		t.Fatalf("MFA completion did not issue session: status=%d body=%s", complete.Code, complete.Body.String())
	}

	replay := httptest.NewRecorder()
	replayRequest := httptest.NewRequest(http.MethodPost, "/auth/mfa/login/complete", bytes.NewBufferString(`{"mfa_ticket":"`+challenge.Data.Ticket+`","code":"`+handlerTOTPCode(secret, now.Unix()/30)+`"}`))
	replayRequest.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(replay, replayRequest)
	if replay.Code != http.StatusUnauthorized || !strings.Contains(replay.Body.String(), "identity.mfa.ticket_invalid") {
		t.Fatalf("MFA ticket replay response=%d body=%s", replay.Code, replay.Body.String())
	}
}

func handlerTOTPCode(secret []byte, step int64) string {
	message := make([]byte, 8)
	for index := 7; index >= 0; index-- {
		message[index] = byte(step)
		step >>= 8
	}
	mac := hmac.New(sha1.New, secret)
	_, _ = mac.Write(message)
	digest := mac.Sum(nil)
	offset := int(digest[len(digest)-1] & 0x0f)
	value := (int(digest[offset])&0x7f)<<24 | int(digest[offset+1])<<16 | int(digest[offset+2])<<8 | int(digest[offset+3])
	return fmt.Sprintf("%06d", value%1000000)
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
