package projectaudit

import (
	"gopkg.in/yaml.v3"
	"path/filepath"
	"strings"
	"testing"
)

func TestSpaceCurrentReadActualSixRoutesExplicitSafeCurrentContracts(t *testing.T) {
	root, err := FindRepositoryRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	routes, err := ParseServerRoutes(filepath.Join(root, "internal/transport/httpapi/router.go"))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"public_profile.by_id": "SpacePublicProfile", "public_profile.by_username": "SpacePublicProfile", "public_contents.by_id": "SpacePublicContentListData", "public_contents.by_username": "SpacePublicContentListData", "owner_contents.read": "SpaceOwnerContentListData", "owner_sync_status.read": "SpaceCurrentSyncStatus"}
	for _, r := range routes {
		op := strings.TrimPrefix(r.OperationCode, "http.personal_space.")
		schema, ok := want[op]
		if !ok {
			continue
		}
		p := profileFor(r)
		auth := "public"
		if strings.HasPrefix(op, "owner_") {
			auth = "jwt+current-session"
		}
		if r.Method != "GET" || r.Auth != auth || r.Ownership != "resource-policy" || r.Audit != "none" || p.ResponseSchema != schema || !p.NoBody {
			t.Fatal(r, p)
		}
		delete(want, op)
	}
	if len(want) != 0 {
		t.Fatal("missing actual current route", want)
	}
	var parsed map[string]any
	if err := yaml.Unmarshal(OpenAPI(routes), &parsed); err != nil {
		t.Fatal(err)
	}
	models := parsed["components"].(map[string]any)["schemas"].(map[string]any)
	for _, name := range []string{"SpacePublicOwner", "SpacePublicAppearance", "SpacePublicContent", "SpaceOwnerContent", "SpaceCurrentSyncStatus"} {
		model := models[name].(map[string]any)
		props := model["properties"].(map[string]any)
		if model["additionalProperties"] != false {
			t.Fatal(name, model)
		}
		for _, hidden := range []string{"password_hash", "email", "facts_version", "sync_categories", "sync_tags", "disabled_actor_id", "disabled_reason"} {
			if props[hidden] != nil {
				t.Fatal(name, hidden)
			}
		}
	}
	props := models["SpacePublicContent"].(map[string]any)["properties"].(map[string]any)
	if props["moderation_reason"] != nil {
		t.Fatal("public schema admits governance reason")
	}
}
