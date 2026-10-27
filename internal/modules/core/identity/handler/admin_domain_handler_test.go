package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/campusos/CampusOS/internal/modules/core/identity/handler"
	"github.com/campusos/CampusOS/internal/modules/core/identity/portadapt"
	"github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	"github.com/campusos/CampusOS/internal/modules/core/identity/service"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/campusos/CampusOS/pkg/middleware"
	"github.com/gin-gonic/gin"
)

type adminDomainHTTPFixture struct {
	router      *gin.Engine
	lifecycle   *service.AdminDomainService
	userJWT     *auth.JWTManager
	adminJWT    *auth.JWTManager
	sessions    *service.AdminSessionService
	admins      *repository.MemoryAdminDomainRepository
	security    *repository.MemoryAdminSecurityRepository
	sessionRepo *repository.MemoryAdminSessionRepository
	handler     *handler.AdminDomainHandler
}

func newAdminDomainHTTPFixture(t *testing.T) *adminDomainHTTPFixture {
	t.Helper()
	gin.SetMode(gin.TestMode)
	admins := repository.NewMemoryAdminDomainRepository()
	sessions := repository.NewMemoryAdminSessionRepository()
	security := repository.NewMemoryAdminSecurityRepository()
	managers := auth.NewDomainJWTManagers(
		auth.JWTConfig{Secret: "user-secret-material", Issuer: "campusos-user", Audience: "user-api", KeyID: "user-key"},
		auth.JWTConfig{Secret: "admin-secret-material", Issuer: "campusos-admin", Audience: "admin-api", KeyID: "admin-key"},
	)
	config := service.DefaultAdminDomainConfig()
	config.BootstrapEnabled = true
	lifecycle := service.NewAdminDomainService(admins, repository.NewMemoryRoleRepository(), config)
	domainSessions := service.NewAdminSessionService(admins, sessions, managers.Admin)
	protector, err := service.NewMFASecretProtector("k1", map[string]string{"k1": "0123456789abcdef0123456789abcdef"})
	if err != nil {
		t.Fatal(err)
	}
	mfa := service.NewAdminMFAService(admins, security, sessions, protector)
	events := reliability.NewMemoryStore()
	reliable := reliability.NewService(transaction.NewMemory(events), events)
	domainSessions.SetReliability(reliable)
	mfa.SetReliability(reliable)
	h := handler.NewAdminDomainHandler(lifecycle, domainSessions, mfa, service.NewAdminSelfService(domainSessions, admins, security))
	h.SetBootstrapSecret("test-deployment-bootstrap-proof")
	h.SetSessionHTTPConfig(handler.SessionHTTPConfig{RefreshBodyCompat: true})

	router := gin.New()
	public := router.Group("/api/v1")
	public.POST("/admin/identity/login", h.Login)
	public.POST("/admin/identity/refresh", h.Refresh)
	public.POST("/admin/identity/bootstrap", h.Bootstrap)
	secured := router.Group("/api/v1")
	secured.Use(middleware.RequireAdminDomainAccess(managers.Admin, portadapt.NewAdminDomainSessionVerifier(domainSessions), nil))
	secured.GET("/admin/identity/me", h.Me)
	secured.POST("/admin/identity/mfa/enroll", h.EnrollTOTP)
	secured.POST("/admin/identity/mfa/confirm", h.ConfirmTOTP)
	secured.POST("/admin/identity/mfa/step-up", h.StepUp)
	secured.POST("/admin/identity/logout", h.Logout)
	secured.POST("/admin/identity/accounts", h.CreateAccount)
	return &adminDomainHTTPFixture{
		router: router, lifecycle: lifecycle, userJWT: managers.User, adminJWT: managers.Admin, sessions: domainSessions, admins: admins, security: security, sessionRepo: sessions, handler: h,
	}
}

func (f *adminDomainHTTPFixture) post(t *testing.T, path string, body any, token string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	payload, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	if path == "/api/v1/admin/identity/bootstrap" {
		req.Header.Set(handler.AdminBootstrapSecretHeader, "test-deployment-bootstrap-proof")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	recorder := httptest.NewRecorder()
	f.router.ServeHTTP(recorder, req)
	var parsed map[string]any
	_ = json.Unmarshal(recorder.Body.Bytes(), &parsed)
	return recorder, parsed
}

func TestAdminDomainHTTPBootstrapRejectsMissingDeploymentProof(t *testing.T) {
	f := newAdminDomainHTTPFixture(t)
	payload := []byte(`{"username":"root","email":"root@example.test","password":"initial-password","reason":"first"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/identity/bootstrap", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	f.router.ServeHTTP(response, req)
	if response.Code != http.StatusForbidden {
		t.Fatalf("bootstrap without deployment proof: %d", response.Code)
	}
	count, err := f.admins.CountActiveSecurityAdmins(context.Background())
	if err != nil || count != 0 {
		t.Fatalf("unproven bootstrap changed accounts: count=%d err=%v", count, err)
	}
}

func TestAdminDomainHTTPBootstrapRejectsInvalidDeploymentProof(t *testing.T) {
	const secret = "test-deployment-bootstrap-proof"
	for _, test := range []struct {
		name       string
		configured string
		proofs     []string
	}{
		{"not_configured", "", []string{secret}},
		{"short_configuration", "short", []string{secret}},
		{"wrong_proof", secret, []string{"another-deployment-bootstrap-proof"}},
		{"duplicate_proof", secret, []string{secret, secret}},
		{"oversized_proof", secret, []string{strings.Repeat("x", 4097)}},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newAdminDomainHTTPFixture(t)
			f.handler.SetBootstrapSecret(test.configured)
			payload := []byte(`{"username":"root","email":"root@example.test","password":"initial-password","reason":"first"}`)
			req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/identity/bootstrap", bytes.NewReader(payload))
			req.Header.Set("Content-Type", "application/json")
			for _, proof := range test.proofs {
				req.Header.Add(handler.AdminBootstrapSecretHeader, proof)
			}
			recorder := httptest.NewRecorder()
			f.router.ServeHTTP(recorder, req)
			if recorder.Code != http.StatusForbidden || recorder.Header().Get("Cache-Control") != "private, no-store" {
				t.Fatalf("invalid proof response: %d %v", recorder.Code, recorder.Header())
			}
			if strings.Contains(recorder.Body.String(), secret) {
				t.Fatal("deployment proof leaked in response")
			}
			count, err := f.admins.CountActiveSecurityAdmins(context.Background())
			if err != nil || count != 0 {
				t.Fatalf("invalid proof changed accounts: count=%d err=%v", count, err)
			}
		})
	}
}

func TestAdminDomainHTTPBootstrapBoundsRequestBody(t *testing.T) {
	f := newAdminDomainHTTPFixture(t)
	response, _ := f.post(t, "/api/v1/admin/identity/bootstrap", map[string]any{
		"username": "root", "email": "root@example.test", "password": "initial-password", "reason": strings.Repeat("x", 5000),
	}, "")
	if response.Code != http.StatusBadRequest {
		t.Fatalf("oversized bootstrap body: %d", response.Code)
	}
	count, err := f.admins.CountActiveSecurityAdmins(context.Background())
	if err != nil || count != 0 {
		t.Fatalf("oversized body changed accounts: count=%d err=%v", count, err)
	}
}

func TestAdminDomainHTTPBootstrapLoginAndCrossDomainRejection(t *testing.T) {
	f := newAdminDomainHTTPFixture(t)
	ctx := context.Background()

	// First bootstrap through the controlled entry succeeds.
	recorder, _ := f.post(t, "/api/v1/admin/identity/bootstrap", map[string]any{
		"username": "root", "email": "root@example.test", "password": "initial-password", "reason": "first",
	}, "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("first bootstrap: %d %s", recorder.Code, recorder.Body.String())
	}
	// A second bootstrap is denied once a security administrator exists.
	recorder, _ = f.post(t, "/api/v1/admin/identity/bootstrap", map[string]any{
		"username": "second", "email": "second@example.test", "password": "password", "reason": "again",
	}, "")
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("second bootstrap: %d", recorder.Code)
	}

	// Wrong password and unknown accounts share one denial shape.
	recorder, _ = f.post(t, "/api/v1/admin/identity/login", map[string]any{"username": "nobody", "password": "x"}, "")
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("login denial shape: %d", recorder.Code)
	}

	// Login with an admin account works and issues an admin-domain token.
	recorder, body := f.post(t, "/api/v1/admin/identity/login", map[string]any{
		"username": "root", "password": "initial-password",
	}, "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("admin login: %d %s", recorder.Code, recorder.Body.String())
	}
	data := body["data"].(map[string]any)
	adminToken := data["access_token"].(string)
	refreshToken := data["refresh_token"].(string)

	// The admin token opens admin routes.
	recorder, _ = f.post(t, "/api/v1/admin/identity/accounts", map[string]any{
		"username": "ops", "email": "ops@example.test", "password": "password", "reason": "ops desk",
	}, adminToken)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("lifecycle without MFA must fail closed: %d", recorder.Code)
	}

	// A user-domain token is rejected on admin routes even when signature-valid.
	userToken, err := f.userJWT.GenerateAccessToken("1", "someone", auth.AccessTokenContext{SessionID: "user-session", AuthVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	recorder, _ = f.post(t, "/api/v1/admin/identity/logout", map[string]any{}, userToken)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("user token accepted on admin route: %d", recorder.Code)
	}
	recorder, _ = f.post(t, "/api/v1/admin/identity/logout", map[string]any{}, adminToken)
	if recorder.Code != http.StatusOK {
		t.Fatalf("admin logout: %d", recorder.Code)
	}
	// After logout the same token no longer authenticates.
	recorder, _ = f.post(t, "/api/v1/admin/identity/logout", map[string]any{}, adminToken)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("logged-out token accepted: %d", recorder.Code)
	}

	// Logout kills the session and its refresh credential.
	recorder, _ = f.post(t, "/api/v1/admin/identity/refresh", map[string]any{"refresh_token": refreshToken}, "")
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("refresh after logout: %d", recorder.Code)
	}
	// A fresh login refreshes and rotates normally.
	recorder, body = f.post(t, "/api/v1/admin/identity/login", map[string]any{"username": "root", "password": "initial-password"}, "")
	data = body["data"].(map[string]any)
	recorder, body = f.post(t, "/api/v1/admin/identity/refresh", map[string]any{"refresh_token": data["refresh_token"].(string)}, "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("fresh refresh: %d %s", recorder.Code, recorder.Body.String())
	}
	_ = ctx
	_ = f.lifecycle
}
