package projectaudit

import (
	"gopkg.in/yaml.v3"
	"path/filepath"
	"strings"
	"testing"
)

func TestAdminSPAActualSelfRoutesCookieCSRFAndSafeCurrentContracts(t *testing.T) {
	root, err := FindRepositoryRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	routes, err := ParseServerRoutes(filepath.Join(root, "internal/transport/httpapi/router.go"))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"read": "AdminCurrentSelf", "logout": "AdminOwnLogoutReceipt", "mfa.enroll": "AdminTOTPEnrollment", "mfa.confirm": "AdminTOTPConfirmation", "mfa.step_up": "AdminStepUpReceipt"}
	for _, r := range routes {
		op := strings.TrimPrefix(r.OperationCode, "http.identity.admin_self.")
		schema, ok := want[op]
		if !ok {
			continue
		}
		p := profileFor(r)
		auth := "jwt+current-admin-session+browser-csrf"
		if op == "read" {
			auth = "jwt+current-admin-session"
			if !p.NoBody || r.Audit != "none" {
				t.Fatal(r)
			}
		} else if r.Audit != "required-command+outbox" {
			t.Fatal(r)
		}
		if r.Audience != "authenticated" || r.Auth != auth || r.Scope != "current-admin-self" || r.Permission != "" || r.PermissionCode != "" || p.ResponseSchema != schema {
			t.Fatal("current self profile incorrectly inherited management authority", r, p)
		}
		delete(want, op)
	}
	if len(want) != 0 {
		t.Fatal("missing actual self route", want)
	}
	var api map[string]any
	if err := yaml.Unmarshal(OpenAPI(routes), &api); err != nil {
		t.Fatal(err)
	}
	components := api["components"].(map[string]any)
	schemas := components["schemas"].(map[string]any)
	for _, name := range []string{"AdminCurrentSelf", "AdminSelfAccount", "AdminSelfRole", "AdminStepUpReceipt"} {
		model := schemas[name].(map[string]any)
		if model["additionalProperties"] != false {
			t.Fatal(name)
		}
		props := model["properties"].(map[string]any)
		for _, private := range []string{"email", "refresh_token", "session_id", "credential_hash", "nonce", "ciphertext", "granted_by", "facts_version"} {
			if props[private] != nil {
				t.Fatal("private self field", name, private)
			}
		}
	}
	security := components["securitySchemes"].(map[string]any)
	if security["adminRefreshCookie"].(map[string]any)["name"] != "campusos_admin_refresh" {
		t.Fatal("User cookie alias")
	}
	path := api["paths"].(map[string]any)["/api/v1/admin/identity/refresh"].(map[string]any)["post"].(map[string]any)
	if !strings.Contains(string(OpenAPI(routes)), "X-Admin-CSRF-Token") || path["x-campusos-auth"] != "admin-refresh-cookie+csrf" {
		t.Fatal("missing Admin refresh CSRF contract")
	}
}
