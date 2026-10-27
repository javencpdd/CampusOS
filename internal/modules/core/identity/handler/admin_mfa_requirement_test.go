package handler_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/campusos/CampusOS/internal/modules/core/identity/service"
)

func TestAdminMFARequirementHTTPProfileUsesOnlyDeploymentFacts(t *testing.T) {
	f := newAdminDomainHTTPFixture(t)
	ctx := context.Background()
	if _, err := f.lifecycle.BootstrapSecurityAdmin(ctx, "operator", "operator@example.test", "test-password", "test only"); err != nil {
		t.Fatal(err)
	}
	tokens, err := f.sessions.Login(ctx, "operator", "test-password", service.AdminSessionMetadata{})
	if err != nil {
		t.Fatal(err)
	}
	for _, optional := range []bool{false, true} {
		f.sessions.SetMFAOptional(optional)
		req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/identity/me?mfa_required=false&admin_mfa_optional=true", nil)
		req.Header.Set("Authorization", "Bearer "+tokens.AccessToken)
		req.Header.Set("X-Admin-MFA-Optional", "true")
		w := httptest.NewRecorder()
		f.router.ServeHTTP(w, req)
		var result struct {
			Code int `json:"code"`
			Data struct {
				Required bool   `json:"mfa_required"`
				Strength string `json:"authentication_strength"`
			} `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if w.Code != http.StatusOK || result.Code != 0 || result.Data.Required == optional || result.Data.Strength != "password" {
			t.Fatal("untrusted input changed requirement or strength", w.Code, result)
		}
		if w.Header().Get("Cache-Control") != "private, no-store" {
			t.Fatal("security profile cached")
		}
	}
}
