package projectaudit

import (
	"gopkg.in/yaml.v3"
	"path/filepath"
	"testing"
)

func TestInlineImageUploadRouteContractsUseActualHandlerFields(t *testing.T) {
	root, err := FindRepositoryRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	routes, err := ParseServerRoutes(filepath.Join(root, "internal/transport/httpapi/router.go"))
	if err != nil {
		t.Fatal(err)
	}
	wanted := map[string]struct{ handler, request, response string }{
		"http.storage.inline_image.author_upload":          {"userStorageHandler.UploadContentImage", "ContentImageUploadRequest", "ContentImage"},
		"http.feature.richtext.inline_image.author_upload": {"richTextHandler.UploadAsset", "InlineRichTextUploadRequest", "InlineRichTextAsset"},
	}
	for _, route := range routes {
		want, ok := wanted[route.OperationCode]
		if !ok {
			continue
		}
		profile := profileFor(route)
		if route.Handler != want.handler || route.Auth != "jwt+current-session" || route.Audit != "required-command-audit" || profile.RequestSchema != want.request || profile.ResponseSchema != want.response || profile.ContentType != "multipart/form-data" {
			t.Fatal("actual source handler/contract missing", route, profile, want)
		}
		delete(wanted, route.OperationCode)
	}
	if len(wanted) != 0 {
		t.Fatal("real upload routes missing", wanted)
	}
	var parsed map[string]any
	if err := yaml.Unmarshal(OpenAPI(routes), &parsed); err != nil {
		t.Fatal("real OpenAPI has duplicate/invalid fields", err)
	}
	paths := parsed["paths"].(map[string]any)
	for _, path := range []string{"/api/v1/content/assets/images", "/api/v1/richtext/assets"} {
		operation := paths[path].(map[string]any)["post"].(map[string]any)
		responses := operation["responses"].(map[string]any)
		for _, status := range []string{"200", "400", "401", "403", "409", "413", "503"} {
			if responses[status] == nil {
				t.Fatal("actual error contract missing", path, status)
			}
		}
	}
}
