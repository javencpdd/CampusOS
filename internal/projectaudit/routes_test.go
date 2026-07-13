package projectaudit

import (
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestServerRoutesHaveAuthorizationContracts(t *testing.T) {
	root, err := FindRepositoryRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	routes, err := ParseServerRoutes(filepath.Join(root, "internal/transport/httpapi/router.go"))
	if err != nil {
		t.Fatal(err)
	}
	if len(routes) < 100 {
		t.Fatalf("expected at least 100 current routes, got %d", len(routes))
	}
	seen := make(map[string]bool)
	for _, route := range routes {
		key := route.Method + " " + route.Path
		if seen[key] {
			t.Fatalf("duplicate route contract: %s", key)
		}
		seen[key] = true
		if route.Audience == "admin" && route.Permission == "" {
			t.Fatalf("admin route lacks permission: %s", key)
		}
	}
}

func TestOpenAPIIsValidYAMLWithCoreFieldContracts(t *testing.T) {
	root, err := FindRepositoryRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	routes, err := ParseServerRoutes(filepath.Join(root, "internal/transport/httpapi/router.go"))
	if err != nil {
		t.Fatal(err)
	}
	document := OpenAPI(routes)
	var parsed map[string]interface{}
	if err := yaml.Unmarshal(document, &parsed); err != nil {
		t.Fatalf("generated OpenAPI is not valid YAML: %v", err)
	}

	text := string(document)
	for _, expected := range []string{
		"version: 0.6.9-experimental",
		"$ref: '#/components/schemas/RegisterRequest'",
		"$ref: '#/components/schemas/CreateThreadRequest'",
		"$ref: '#/components/schemas/UpdateUserRequest'",
		"$ref: '#/components/schemas/ErrorEnvelope'",
		"x-campusos-schema-level: field-contract",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("generated OpenAPI is missing %q", expected)
		}
	}
	if count := strings.Count(text, "      requestBody:\n"); count < 25 {
		t.Fatalf("expected request bodies for mutating operations, got %d", count)
	}
}

func TestOpenAPICombinesPathAndPaginationParameters(t *testing.T) {
	document := string(OpenAPI([]RouteContract{{
		Method: "GET", Path: "/api/v1/threads/:id/posts", Handler: "postHandler.ListPosts",
		Audience: "public", Auth: "none", Ownership: "none", Scope: "public", Stability: "experimental",
	}}))
	operation := document[:strings.Index(document, "components:")]
	if count := strings.Count(operation, "      parameters:\n"); count != 1 {
		t.Fatalf("expected one parameters block, got %d:\n%s", count, operation)
	}
	for _, expected := range []string{"- name: id", "- name: page", "- name: page_size"} {
		if !strings.Contains(operation, expected) {
			t.Fatalf("OpenAPI is missing %q:\n%s", expected, operation)
		}
	}
}

func TestRouteDescriptorsRejectDuplicateTransportRoutes(t *testing.T) {
	routes := []RouteContract{
		{Method: "GET", Path: "/api/v1/health", Audience: "public", Auth: "none", Audit: "request-log"},
		{Method: "GET", Path: "/api/v1/health", Audience: "public", Auth: "none", Audit: "request-log"},
	}
	if err := ValidateRouteDescriptors(routes); err == nil {
		t.Fatal("expected duplicate transport route to fail")
	}
}

func TestEveryFrozenRouteHasBusinessModuleOwner(t *testing.T) {
	root, err := FindRepositoryRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	routes, err := ParseServerRoutes(filepath.Join(root, "internal/transport/httpapi/router.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, route := range routes {
		if route.ModuleOwner == "" || strings.HasPrefix(route.ModuleOwner, "unowned:") || route.ModuleOwner == "transport.httpapi" {
			t.Fatalf("route %s %s has invalid owner %q", route.Method, route.Path, route.ModuleOwner)
		}
	}
}

func TestOpenAPIRepresentsOptionalJSONBodies(t *testing.T) {
	document := string(OpenAPI([]RouteContract{{
		Method: "POST", Path: "/api/v1/spaces/:user_id/disable", Handler: "spaceHandler.DisableSpace",
		Audience: "admin", Auth: "jwt+permission", Permission: "space:manage", Ownership: "none", Scope: "global", Stability: "experimental",
	}}))
	for _, expected := range []string{"requestBody:", "required: false", "$ref: '#/components/schemas/DisableSpaceRequest'"} {
		if !strings.Contains(document, expected) {
			t.Fatalf("optional body contract is missing %q:\n%s", expected, document)
		}
	}
}

func TestOpenAPIEmitsRequiredPathParameters(t *testing.T) {
	document := string(OpenAPI([]RouteContract{{
		Method: "GET", Path: "/api/v1/threads/:id", Handler: "threadHandler.GetThread",
		Audience: "public", Auth: "none", Ownership: "none", Scope: "public", Stability: "experimental",
	}}))
	for _, expected := range []string{"/api/v1/threads/{id}:", "- name: id", "in: path", "required: true"} {
		if !strings.Contains(document, expected) {
			t.Fatalf("OpenAPI is missing %q:\n%s", expected, document)
		}
	}
}
