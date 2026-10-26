package projectaudit

import (
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestIntegrationRouteDomainSchemaAndAdminCSRF(t *testing.T) {
	root, err := FindRepositoryRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	routes, err := ParseServerRoutes(filepath.Join(root, "internal/transport/httpapi/router.go"))
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]any
	if err := yaml.Unmarshal(OpenAPI(routes), &parsed); err != nil {
		t.Fatal(err)
	}
	paths := parsed["paths"].(map[string]any)
	count := 0
	for _, r := range routes {
		if !strings.HasPrefix(r.OperationCode, "http.identity.integration.") && !strings.HasPrefix(r.OperationCode, "http.identity.integration_self.") && !strings.HasPrefix(r.OperationCode, "http.integration.public_thread.") {
			continue
		}
		count++
		profile := profileFor(r)
		if profile.ResponseSchema == "" || profile.ResponseSchema == "GenericObject" {
			t.Fatal("missing concrete response", r.Path)
		}
		op := paths[openAPIPath(r.Path)].(map[string]any)[strings.ToLower(r.Method)].(map[string]any)
		if op["x-campusos-auth"] != r.Auth {
			t.Fatal("wrong auth metadata", r.Path)
		}
		if r.Audience == "integration" {
			if r.PermissionCode != "" || !strings.HasPrefix(r.Auth, "integration-digest") {
				t.Fatal("machine inherited Admin permission", r.Path)
			}
			security := op["security"].([]any)[0].(map[string]any)
			if _, ok := security["integrationCredential"]; !ok {
				t.Fatal("machine documented as JWT", r.Path)
			}
		} else if r.Method != "GET" {
			if !strings.Contains(r.Auth, "recent-mfa") {
				t.Fatal("missing MFA", r.Path)
			}
			adminHeader, userHeader := false, false
			for _, v := range op["parameters"].([]any) {
				p := v.(map[string]any)
				adminHeader = adminHeader || p["name"] == "X-Admin-CSRF-Token"
				userHeader = userHeader || p["name"] == "X-CSRF-Token"
			}
			if !adminHeader || userHeader {
				t.Fatal("incorrect browser CSRF domain", r.Path)
			}
		}
	}
	if count != 12 {
		t.Fatal("entry inventory", count)
	}
}
