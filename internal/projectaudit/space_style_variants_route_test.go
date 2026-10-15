package projectaudit

import (
	"gopkg.in/yaml.v3"
	"path/filepath"
	"testing"
)

func TestSpaceStyleVariantsActualRouteContractsCarryCurrentProofAndSafeAck(t *testing.T) {
	root, err := FindRepositoryRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	routes, err := ParseServerRoutes(filepath.Join(root, "internal/transport/httpapi/router.go"))
	if err != nil {
		t.Fatal(err)
	}
	wanted := map[string]string{"http.personal_space.owner_style.html": "spaceHandler.ApplyCustomHTML", "http.personal_space.owner_style.zip": "spaceHandler.ApplyStylePackZip", "http.personal_space.owner_style.source": "spaceHandler.ApplySourceStylePack"}
	for _, route := range routes {
		handler, ok := wanted[route.OperationCode]
		if !ok {
			continue
		}
		profile := profileFor(route)
		if route.Handler != handler || route.Auth != "jwt+current-session" || route.Audit != "required-command-audit" || route.Scope != "actual-owned-space+current-user" || profile.ResponseSchema != "SpaceMutationAck" {
			t.Fatal("actual variant route lost contract", route, profile)
		}
		if route.OperationCode == "http.personal_space.owner_style.zip" && profile.ContentType != "multipart/form-data" {
			t.Fatal("ZIP body contract lost", profile)
		}
		delete(wanted, route.OperationCode)
	}
	if len(wanted) != 0 {
		t.Fatal("missing actual variant routes", wanted)
	}
	var parsed map[string]any
	if err := yaml.Unmarshal(OpenAPI(routes), &parsed); err != nil {
		t.Fatal("invalid/duplicate current OpenAPI", err)
	}
}
