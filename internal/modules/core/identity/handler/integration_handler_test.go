package handler_test

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/identity/handler"
	"github.com/campusos/CampusOS/internal/modules/core/identity/portadapt"
	"github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	"github.com/campusos/CampusOS/internal/modules/core/identity/service"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/campusos/CampusOS/pkg/middleware"
)

func TestIntegrationManagementHTTPCurrentAdminMFACSRFAndCodec(t *testing.T) {
	f := adminSPAHTTPFixture(t)
	ctx := context.Background()
	if err := f.admins.ReplaceAdminRolePermissionFacts(ctx, "security_admin", []string{"identity.integration.read", "identity.integration.manage"}, "test"); err != nil {
		t.Fatal(err)
	}
	r := repository.NewMemoryIntegrationRepository()
	store := reliability.NewMemoryStore()
	reliable := reliability.NewService(transaction.NewMemory(r, f.admins, f.sessionRepo, store), store)
	svc := service.NewIntegrationService(r, service.NewBoardGovernanceAuthorityService(nil, f.admins, f.sessions), reliable)
	h := handler.NewIntegrationHandler(svc, f.handler)
	g := f.router.Group("/api/v1")
	g.Use(middleware.RequireAdminDomainAccess(f.adminJWT, portadapt.NewAdminDomainSessionVerifier(f.sessions), nil))
	g.POST("/admin/identity/integrations", h.Create)
	g.GET("/admin/identity/integrations", h.List)
	token, cookies := adminSPABrowserLogin(t, f)
	for _, test := range []struct {
		name, body string
		csrf       bool
		want       int
	}{{"password", `{"name":"reader"}`, true, 403}, {"no_csrf", `{"name":"reader"}`, false, 403}, {"unknown", `{"name":"reader","actor_id":"admin"}`, true, 400}, {"duplicate", `{"name":"one","name":"two"}`, true, 400}, {"null", `{"name":null}`, true, 400}, {"missing", `{}`, true, 400}, {"trailing", `{"name":"r"}{}`, true, 400}, {"unicode", `{"name":"\ud800"}`, true, 400}, {"oversize", `{"name":"` + strings.Repeat("a", 32769) + `"}`, true, 400}} {
		t.Run(test.name, func(t *testing.T) {
			w, _ := adminSPARequest(t, f, "POST", "/integrations", test.body, token, cookies, test.csrf, true)
			if w.Code != test.want {
				t.Fatalf("status %d want %d: %s", w.Code, test.want, w.Body.String())
			}
		})
	}
	claims, err := f.adminJWT.VerifyAccessToken(token)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := f.sessionRepo.UpgradeSessionStrengthCAS(ctx, claims.SessionID, 1, "mfa", time.Now(), time.Now()); err != nil || !ok {
		t.Fatal(err)
	}
	w, result := adminSPARequest(t, f, "POST", "/integrations", `{"name":"reader"}`, token, cookies, true, true)
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	if result["data"].(map[string]any)["status"] != "active" {
		t.Fatal(result)
	}
	userToken, err := f.userJWT.GenerateAccessToken("adm.browser.opaque", "same id")
	if err != nil {
		t.Fatal(err)
	}
	w, _ = adminSPARequest(t, f, "POST", "/integrations", `{"name":"forged"}`, userToken, cookies, true, true)
	if w.Code != 401 {
		t.Fatal("user token crossed", w.Code)
	}
	req := httptest.NewRequest("GET", "/api/v1/admin/identity/integrations", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)
	if rec.Code != 200 || rec.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatal("list", rec.Code)
	}
	if _, _, err := f.admins.SetAccountStatusCAS(ctx, "adm.browser.opaque", 1, "suspended", "test", "test", time.Now()); err != nil {
		t.Fatal(err)
	}
	w, _ = adminSPARequest(t, f, "POST", "/integrations", `{"name":"late"}`, token, cookies, true, true)
	if w.Code != 401 {
		t.Fatal("suspended admin crossed", w.Code)
	}
}
