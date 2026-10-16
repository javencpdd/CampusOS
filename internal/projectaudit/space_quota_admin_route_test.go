package projectaudit

import (
	"gopkg.in/yaml.v3"
	"path/filepath"
	"testing"
)

func TestSpaceQuotaAdminActualRoutesDistinctReadWriteScopesAndSafeContracts(t *testing.T) {
	root, err := FindRepositoryRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	routes, err := ParseServerRoutes(filepath.Join(root, "internal/transport/httpapi/router.go"))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]struct {
		scope, schema string
		read          bool
	}{
		"http.personal_space.admin.quota":   {"actual-target-user+current-quota+current-admin-execution", "SpaceQuotaMutationAck", false},
		"http.personal_space.admin.storage": {"actual-target-user+space+current-quota+current-admin-execution", "SpaceStorageStatus", true},
		"http.personal_space.admin.summary": {"current-space-aggregate+current-admin-execution", "SpaceAdminSummary", true},
	}
	for _, route := range routes {
		expected, ok := want[route.OperationCode]
		if !ok {
			continue
		}
		profile := profileFor(route)
		auth, audit := "jwt+admin-account+permission+current-mfa", "required-command-audit"
		if expected.read {
			auth, audit = "jwt+admin-account+permission+current-session", "none"
		}
		if route.Auth != auth || route.Audit != audit || route.Ownership != "resource-policy" || route.Scope != expected.scope || profile.ResponseSchema != expected.schema || profile.NoBody != expected.read {
			t.Fatal(route, profile)
		}
		delete(want, route.OperationCode)
	}
	if len(want) != 0 {
		t.Fatal(want)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(OpenAPI(routes), &doc); err != nil {
		t.Fatal(err)
	}
	models := doc["components"].(map[string]any)["schemas"].(map[string]any)
	for name, count := range map[string]int{"SpaceQuotaMutationAck": 2, "SpaceAdminSummary": 7, "SpaceStorageStatus": 10} {
		model := models[name].(map[string]any)
		properties := model["properties"].(map[string]any)
		if model["additionalProperties"] != false || len(properties) != count || properties["body"] != nil || properties["space"] != nil || properties["path"] != nil || properties["actor_id"] != nil {
			t.Fatal("private/generic resource leaked into metadata contract", name, model)
		}
	}
}
