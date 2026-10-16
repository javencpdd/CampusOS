package projectaudit

import (
	"gopkg.in/yaml.v3"
	"path/filepath"
	"strings"
	"testing"
)

func TestSpaceStyleReadActualRoutesTypedPrivateJSONAndBinaryContracts(t *testing.T) {
	root, err := FindRepositoryRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	routes, err := ParseServerRoutes(filepath.Join(root, "internal/transport/httpapi/router.go"))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"validate": "SpaceStyleReadValidation", "preview": "SpaceStyleReadPreview", "export": "SpaceStyleExport", "html_validate": "SpaceStyleReadValidation", "html_example": "SpaceStyleHTMLExample", "zip_validate": "SpaceStyleZipValidation", "pack_example": "SpaceStylePackExample", "pack_zip": ""}
	for _, r := range routes {
		if !strings.HasPrefix(r.OperationCode, "http.personal_space.owner_style_read.") {
			continue
		}
		op := strings.TrimPrefix(r.OperationCode, "http.personal_space.owner_style_read.")
		schema, ok := want[op]
		if !ok {
			t.Fatal("unexpected style read", r)
		}
		p := profileFor(r)
		if r.Auth != "jwt+current-session" || r.Ownership != "resource-policy" || r.Scope != "actual-owned-space+current-user" || r.Audit != "none" || p.ResponseSchema != schema || p.NoBody != (r.Method == "GET") {
			t.Fatal("read lost current contract", r, p)
		}
		if op == "pack_zip" && p.ResponseContentType != "application/zip" {
			t.Fatal("ZIP modeled as JSON envelope", p)
		}
		if op == "zip_validate" && p.ContentType != "multipart/form-data" {
			t.Fatal("ZIP upload transport lost", p)
		}
		delete(want, op)
	}
	if len(want) != 0 {
		t.Fatal("missing style read routes", want)
	}
	var parsed map[string]any
	if err := yaml.Unmarshal(OpenAPI(routes), &parsed); err != nil {
		t.Fatal(err)
	}
	models := parsed["components"].(map[string]any)["schemas"].(map[string]any)
	preview := models["SpaceStyleReadPreview"].(map[string]any)
	props := preview["properties"].(map[string]any)
	if preview["additionalProperties"] != false || props["space"] != nil || props["owner"] != nil {
		t.Fatal("raw preview records admitted by schema", preview)
	}
	paths := parsed["paths"].(map[string]any)
	zip := paths["/api/v1/spaces/me/styles/packs/example.zip"].(map[string]any)["get"].(map[string]any)
	content := zip["responses"].(map[string]any)["200"].(map[string]any)["content"].(map[string]any)
	if content["application/zip"] == nil || content["application/json"] != nil {
		t.Fatal("generated ZIP response is not binary", content)
	}
}
