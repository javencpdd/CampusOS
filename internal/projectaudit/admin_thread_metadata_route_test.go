package projectaudit

import (
	"gopkg.in/yaml.v3"
	"path/filepath"
	"testing"
)

func TestAdminThreadMetadataActualRouteScopesAndNarrowContracts(t *testing.T) {
	root, err := FindRepositoryRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	routes, err := ParseServerRoutes(filepath.Join(root, "internal/transport/httpapi/router.go"))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"http.community.thread.admin_permissions_preview": "AdminThreadPermissionsPreviewData", "http.community.thread.admin_list": "AdminThreadMetadataListData", "http.community.thread.admin_trash_list": "AdminThreadMetadataListData", "http.community.thread.admin_history": "AdminModerationMetadataListData"}
	for _, route := range routes {
		schema, ok := want[route.OperationCode]
		if !ok {
			continue
		}
		p := profileFor(route)
		expectedScope := "current-admin+actual-board+metadata-only-before-count-page"
		expectedNoBody := true
		expectedPaginated := true
		if route.OperationCode == "http.community.thread.admin_permissions_preview" {
			expectedScope = "current-admin+same-command-policies+bounded-actual-threads"
			expectedNoBody = false
			expectedPaginated = false
		}
		if route.Auth != "jwt+admin-account+permission+current-session" || route.Scope != expectedScope || route.Ownership != "resource-policy" || p.ResponseSchema != schema || p.Paginated != expectedPaginated || p.NoBody != expectedNoBody {
			t.Fatal(route, p)
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
	schemas := doc["components"].(map[string]any)["schemas"].(map[string]any)
	for name, count := range map[string]int{"AdminThreadMetadata": 12, "AdminModerationMetadata": 6, "AdminThreadPermissionFlags": 11, "AdminThreadPermissionsPreviewItem": 2} {
		m := schemas[name].(map[string]any)
		p := m["properties"].(map[string]any)
		if m["additionalProperties"] != false || len(p) != count {
			t.Fatal(name, m)
		}
		for _, key := range []string{"title", "content", "reason", "tags", "facts_version", "credential_id", "before_state", "after_state"} {
			if p[key] != nil {
				t.Fatal("private metadata contract", key)
			}
		}
	}
}
