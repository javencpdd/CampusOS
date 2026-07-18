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

func TestAdminRouteContractsIncludeManagementPlaneAdmission(t *testing.T) {
	root, err := FindRepositoryRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	routes, err := ParseServerRoutes(filepath.Join(root, "internal/transport/httpapi/router.go"))
	if err != nil {
		t.Fatal(err)
	}
	wanted := map[string]struct {
		auth  string
		scope string
	}{
		"GET /api/v1/roles": {
			auth: "jwt+admin-account+permission", scope: "global",
		},
		"POST /api/v1/admin/threads/:id/take-down": {
			auth: "jwt+admin-account-or-scope+permission", scope: "assigned-category-or-global-admin",
		},
	}
	for _, route := range routes {
		key := route.Method + " " + route.Path
		expected, ok := wanted[key]
		if !ok {
			continue
		}
		if route.Auth != expected.auth || route.Scope != expected.scope {
			t.Fatalf("route %s admission contract = %s/%s, want %s/%s", key, route.Auth, route.Scope, expected.auth, expected.scope)
		}
		delete(wanted, key)
	}
	if len(wanted) != 0 {
		t.Fatalf("missing management-plane route contracts: %#v", wanted)
	}
}

func TestCategoryOperationCodesMatchDatabaseContractAndKeepLegacyAliases(t *testing.T) {
	root, err := FindRepositoryRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	routes, err := ParseServerRoutes(filepath.Join(root, "internal/transport/httpapi/router.go"))
	if err != nil {
		t.Fatal(err)
	}
	type operationContract struct {
		operation string
		legacy    string
	}
	wanted := map[string]operationContract{
		"GET /api/v1/admin/categories/:id/thread-types": {
			operation: "http.community.category.thread_types",
			legacy:    "http.community.category.thread-types",
		},
		"GET /api/v1/categories/:id/archive-impact": {
			operation: "http.community.category.archive_impact",
			legacy:    "http.community.category.archive-impact",
		},
		"DELETE /api/v1/categories/:id": {
			operation: "http.community.category.archive_legacy",
			legacy:    "http.community.category.archive-legacy",
		},
	}
	for _, route := range routes {
		key := route.Method + " " + route.Path
		contract, ok := wanted[key]
		if !ok {
			continue
		}
		if route.OperationCode != contract.operation {
			t.Fatalf("operation code does not match database contract: got %s want %s", route.OperationCode, contract.operation)
		}
		if !containsRouteAlias(route.LegacyAliases, contract.legacy) {
			t.Fatalf("route %s lost legacy operation alias %s", key, contract.legacy)
		}
		delete(wanted, key)
	}
	if len(wanted) != 0 {
		t.Fatalf("missing category operation routes: %#v", wanted)
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
		"version: " + OpenAPIContractVersion,
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

func TestRegistrationChallengeOpenAPIDocumentsRateAndDependencyFailures(t *testing.T) {
	document := string(OpenAPI([]RouteContract{{
		Method: "POST", Path: "/api/v1/auth/registration/challenge", Handler: "userHandler.RequestRegistrationChallenge",
		Audience: "public", Auth: "none", Ownership: "none", Scope: "public", Stability: "experimental",
	}}))
	operation := document[:strings.Index(document, "components:")]
	for _, expected := range []string{
		"'400':\n          $ref: '#/components/responses/BadRequest'",
		"'429':\n          $ref: '#/components/responses/TooManyRequests'",
		"'503':\n          $ref: '#/components/responses/ServiceUnavailable'",
	} {
		if !strings.Contains(operation, expected) {
			t.Fatalf("registration Challenge OpenAPI is missing %q:\n%s", expected, operation)
		}
	}
}

func TestAdminLoginOpenAPIDocumentsCredentialAndAdmissionFailures(t *testing.T) {
	document := string(OpenAPI([]RouteContract{{
		Method: "POST", Path: "/api/v1/auth/admin/login", Handler: "userHandler.AdminLogin",
		Audience: "public", Auth: "none", Ownership: "none", Scope: "public", Stability: "experimental",
	}}))
	operation := document[:strings.Index(document, "components:")]
	for _, expected := range []string{
		"$ref: '#/components/schemas/LoginRequest'",
		"$ref: '#/components/schemas/LoginResponse'",
		"'401':\n          $ref: '#/components/responses/Unauthorized'",
		"'503':\n          $ref: '#/components/responses/ServiceUnavailable'",
	} {
		if !strings.Contains(operation, expected) {
			t.Fatalf("administrator login OpenAPI is missing %q:\n%s", expected, operation)
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
		Audience: "admin", Auth: "jwt+admin-account+permission", Permission: "space:manage", Ownership: "none", Scope: "global", Stability: "experimental",
	}}))
	for _, expected := range []string{"x-campusos-auth: jwt+admin-account+permission", "requestBody:", "required: false", "$ref: '#/components/schemas/DisableSpaceRequest'"} {
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
