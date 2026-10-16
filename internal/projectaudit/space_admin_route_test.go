package projectaudit

import (
	"gopkg.in/yaml.v3"
	"path/filepath"
	"testing"
)

func TestSpaceAdminActualRoutesCurrentMFAAndSafeAckContracts(t *testing.T) {
	root, err := FindRepositoryRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	routes, err := ParseServerRoutes(filepath.Join(root, "internal/transport/httpapi/router.go"))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"http.personal_space.admin.disable": false, "http.personal_space.admin.enable": true}
	for _, route := range routes {
		noBody, ok := want[route.OperationCode]
		if !ok {
			continue
		}
		p := profileFor(route)
		if route.Auth != "jwt+admin-account+permission+current-mfa" || route.Ownership != "resource-policy" || route.Scope != "actual-target-user+space+current-admin-execution" || route.Audit != "required-command-audit" || p.NoBody != noBody || p.ResponseSchema != "SpaceAdminMutationAck" {
			t.Fatal(route, p)
		}
		delete(want, route.OperationCode)
	}
	if len(want) != 0 {
		t.Fatal(want)
	}
	var schema map[string]any
	if err := yaml.Unmarshal(OpenAPI(routes), &schema); err != nil {
		t.Fatal(err)
	}
	ack := schema["components"].(map[string]any)["schemas"].(map[string]any)["SpaceAdminMutationAck"].(map[string]any)
	if ack["additionalProperties"] != false || len(ack["properties"].(map[string]any)) != 3 {
		t.Fatal(ack)
	}
}
