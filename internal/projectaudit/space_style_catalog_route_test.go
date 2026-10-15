package projectaudit

import (
	"gopkg.in/yaml.v3"
	"path/filepath"
	"testing"
)

func TestSpaceStyleCatalogActualRouteCurrentReadAndSafeResponseContract(t *testing.T) {
	root, err := FindRepositoryRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	routes, err := ParseServerRoutes(filepath.Join(root, "internal/transport/httpapi/router.go"))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, route := range routes {
		if route.OperationCode != "http.personal_space.owner_style.catalog" {
			continue
		}
		found = true
		p := profileFor(route)
		if route.Handler != "spaceHandler.ListSourceStylePacks" || route.Auth != "jwt+current-session" || route.Audit != "none" || route.Scope != "actual-owned-space+current-user" || !p.NoBody || p.ResponseSchema != "SpaceSourceStylePackList" {
			t.Fatal(route, p)
		}
	}
	if !found {
		t.Fatal("catalog route missing current read contract")
	}
	var parsed map[string]any
	if err := yaml.Unmarshal(OpenAPI(routes), &parsed); err != nil {
		t.Fatal(err)
	}
	models := parsed["components"].(map[string]any)["schemas"].(map[string]any)
	item := models["SpaceSourceStylePack"].(map[string]any)
	if item["additionalProperties"] != false || item["properties"].(map[string]any)["path"] != nil {
		t.Fatal("physical Path permitted by catalog contract", item)
	}
}
