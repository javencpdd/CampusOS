package projectaudit

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestServerRoutesHaveAuthorizationContracts(t *testing.T) {
	root, err := FindRepositoryRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	routes, err := ParseServerRoutes(filepath.Join(root, "internal/server/server.go"))
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
