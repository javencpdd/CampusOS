package projectaudit

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestKnowledgePrivateSourcePreflightContractHasNoIndexingGrant(t *testing.T) {
	root, err := FindRepositoryRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	routes, err := ParseServerRoutes(filepath.Join(root, "internal/transport/httpapi/router.go"))
	if err != nil {
		t.Fatal(err)
	}
	var current RouteContract
	for _, route := range routes {
		if route.OperationCode == "http.knowledge.private.source.preflight" {
			current = route
		}
	}
	if current.Method != "POST" || current.Path != "/api/v1/knowledge/private/sources/preflight" || current.Handler != "knowledgeHandler.PrivateSourcePreflight" {
		t.Fatal("private preflight route missing or expanded", current)
	}
	profile := profileFor(current)
	if profile.NoBody || profile.Paginated || profile.RequestSchema != "KnowledgePrivateSourcePreflightRequest" || profile.ResponseSchema != "KnowledgePrivateSourcePreflight" {
		t.Fatal("private preflight lost narrow metadata schemas", profile)
	}
	var document map[string]any
	if err = yaml.Unmarshal(OpenAPI(routes), &document); err != nil {
		t.Fatal(err)
	}
	op := document["paths"].(map[string]any)[current.Path].(map[string]any)["post"].(map[string]any)
	if op["security"] == nil || op["x-campusos-auth"] != "jwt+current-user-session+document-owner" || op["parameters"] != nil {
		t.Fatal("preflight cannot be anonymous or accept URL queries", op)
	}
	schemas := document["components"].(map[string]any)["schemas"].(map[string]any)
	request := schemas[profile.RequestSchema].(map[string]any)
	response := schemas[profile.ResponseSchema].(map[string]any)
	for name, schema := range map[string]map[string]any{"request": request, "response": response} {
		if schema["additionalProperties"] != false {
			t.Fatal("preflight schema must be closed", name)
		}
		properties := schema["properties"].(map[string]any)
		expected := []any{"document_id", "version_id"}
		if name == "response" {
			expected = []any{"document_id", "version_id", "format", "can_index", "reason"}
		}
		if !reflect.DeepEqual(schema["required"], expected) || len(properties) != len(expected) {
			t.Fatal("preflight leaked fields or weakened required metadata", name, schema)
		}
		for _, field := range []string{"document_id", "version_id"} {
			id := properties[field].(map[string]any)
			if id["type"] != "string" || id["pattern"] != "^[1-9][0-9]{0,18}$" || id["maxLength"] != 19 {
				t.Fatal("ID must preserve int64 canonical string representation", name, field, id)
			}
		}
	}
	properties := response["properties"].(map[string]any)
	if properties["can_index"].(map[string]any)["type"] != "boolean" ||
		!reflect.DeepEqual(properties["format"].(map[string]any)["enum"], []any{"text", "markdown", "campusdoc", "pdf", "docx"}) ||
		!reflect.DeepEqual(properties["reason"].(map[string]any)["enum"], []any{"ready", "private_storage_unavailable", "unsupported_format"}) {
		t.Fatal("preflight must only report known metadata and deny indexing", properties)
	}
}

func TestKnowledgeRoutesCurrentAuthorityAndClosedDTOs(t *testing.T) {
	root, e := FindRepositoryRoot(".")
	if e != nil {
		t.Fatal(e)
	}
	routes, e := ParseServerRoutes(filepath.Join(root, "internal/transport/httpapi/router.go"))
	if e != nil {
		t.Fatal(e)
	}
	count, writes := 0, 0
	for _, r := range routes {
		if !strings.HasPrefix(r.OperationCode, "http.knowledge.") {
			continue
		}
		count++
		if r.ModuleOwner != "feature.knowledge" {
			t.Fatal("Knowledge route not owned", r)
		}
		profile := profileFor(r)
		if profile.ResponseSchema == "" || profile.ResponseSchema == "GenericObject" {
			t.Fatal("response has no narrow schema", r.OperationCode)
		}
		if strings.Contains(r.Path, "/admin/knowledge/") {
			if !strings.Contains(r.Auth, "current-admin-session") || r.Scope != "knowledge.collection_inventory/all" {
				t.Fatal("missing current domain proof", r)
			}
			expected := "knowledge.collection.read"
			if r.Method != "GET" {
				writes++
				expected = "knowledge.collection.manage"
				if !strings.Contains(r.Auth, "recent-mfa+browser-csrf") || r.Audit != "required-command+outbox" || profile.RequestSchema == "GenericObject" || profile.RequestSchema == "" {
					t.Fatal("write weakened", r)
				}
			}
			if r.PermissionCode != expected {
				t.Fatal("wrong exact permission", r)
			}
		} else if strings.HasPrefix(r.OperationCode, "http.knowledge.private.") {
			expectedAudit := "none"
			if r.OperationCode == "http.knowledge.private.select" || r.OperationCode == "http.knowledge.private.withdraw" || r.OperationCode == "http.knowledge.private.retry" || r.OperationCode == "http.knowledge.private.rag.answer" {
				expectedAudit = "required-command+outbox"
			}
			expectedAuth, expectedScope, expectedOwner := "jwt+current-user-session+source-owner", "self+current-source-version", "current-source-owner"
			if r.OperationCode == "http.knowledge.private.source.preflight" {
				expectedAuth = "jwt+current-user-session+document-owner"
				expectedScope = "self+current-document-version"
				expectedOwner = "current-document-owner"
			}
			if r.Audience != "authenticated" || r.Auth != expectedAuth || r.Scope != expectedScope || r.Ownership != expectedOwner || r.PermissionCode != "" || r.Permission != "" || r.Audit != expectedAudit {
				t.Fatal("private preflight must recheck the current User and document owner without granting a mutation", r)
			}
		} else if strings.HasPrefix(r.OperationCode, "http.knowledge.rag.") {
			if r.Audience != "authenticated" || r.Auth != "jwt+current-user-session+current-origin-policy" || r.Scope != "public-collection+current-model-binding" {
				t.Fatal("RAG current User proof missing", r)
			}
		} else if r.Auth != "none+current-origin-policy" || r.Scope != "public-collection+current-origin" {
			t.Fatal("public source must recheck origin", r)
		}
	}
	if count != 32 || writes != 9 {
		t.Fatal("Knowledge route inventory incomplete", count, writes)
	}
}

func TestKnowledgePublicSearchHTTPContractAndExactCitation(t *testing.T) {
	root, err := FindRepositoryRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	routes, err := ParseServerRoutes(filepath.Join(root, "internal/transport/httpapi/router.go"))
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err = yaml.Unmarshal(OpenAPI(routes), &document); err != nil {
		t.Fatal(err)
	}
	paths := document["paths"].(map[string]any)
	var search RouteContract
	for _, route := range routes {
		operation := paths[openAPIPath(route.Path)].(map[string]any)[strings.ToLower(route.Method)].(map[string]any)
		// Public current-origin checks describe resource authorization, not a
		// requirement to present a bearer credential. Other entry domains keep
		// their existing security scheme and must not silently become public.
		anonymous := route.Auth == "none" || route.Auth == "none+current-origin-policy"
		if (operation["security"] == nil) != anonymous {
			t.Fatal("OpenAPI authentication changed", route.OperationCode, route.Auth, operation["security"])
		}
		if route.OperationCode == "http.knowledge.search.public" {
			search = route
		}
	}
	if search.Method != "GET" || search.Path != "/api/v1/knowledge/collections/:id/search" || search.Handler != "knowledgeHandler.Search" || search.ModuleOwner != "feature.knowledge" || search.Audience != "public" || search.Auth != "none+current-origin-policy" || search.PermissionCode != "" || search.Scope != "public-collection+current-origin" || search.Ownership != "current-origin-and-collection" || search.Audit != "none" {
		t.Fatal("public search scope expanded or route missing", search)
	}
	profile := profileFor(search)
	if !profile.NoBody || profile.RequestSchema != "" || profile.ResponseSchema != "KnowledgeSearchPage" || profile.Paginated {
		t.Fatal("search uses generic body/pagination envelope", profile)
	}
	searchOperation := paths[openAPIPath(search.Path)].(map[string]any)["get"].(map[string]any)
	if searchOperation["requestBody"] != nil || searchOperation["x-campusos-auth"] != search.Auth {
		t.Fatal("search ingress contract", searchOperation)
	}
	queryParameters := map[string]map[string]any{}
	for _, raw := range searchOperation["parameters"].([]any) {
		p := raw.(map[string]any)
		if p["in"] == "query" {
			queryParameters[p["name"].(string)] = p
		}
	}
	if len(queryParameters) != 3 || queryParameters["query"]["required"] != true || queryParameters["query"]["schema"].(map[string]any)["maxLength"] != 1024 {
		t.Fatal("search query is not closed and bounded", queryParameters)
	}
	for _, test := range []struct {
		name               string
		maximum, defaultTo int
	}{{"page", 100000, 1}, {"page_size", 50, 20}} {
		schema := queryParameters[test.name]["schema"].(map[string]any)
		if schema["type"] != "integer" || schema["minimum"] != 1 || schema["maximum"] != test.maximum || schema["default"] != test.defaultTo {
			t.Fatal("search pagination diverged from bounded Port", test.name, schema)
		}
	}
	responses := searchOperation["responses"].(map[string]any)
	for _, status := range []string{"200", "400", "404", "409", "503"} {
		if responses[status] == nil {
			t.Fatal("missing search response", status)
		}
	}
	if responses["401"] != nil || responses["403"] != nil {
		t.Fatal("anonymous search incorrectly requires credential", responses)
	}
	schemas := document["components"].(map[string]any)["schemas"].(map[string]any)
	for _, name := range []string{"KnowledgeSearchPage", "KnowledgeSearchHit", "KnowledgeSearchExplanation", "KnowledgeSearchCitation"} {
		if schemas[name].(map[string]any)["additionalProperties"] != false {
			t.Fatal("search response includes unrestricted object", name)
		}
	}
	page := schemas["KnowledgeSearchPage"].(map[string]any)["properties"].(map[string]any)
	if page["generation"].(map[string]any)["type"] != "string" || page["items"].(map[string]any)["maxItems"] != 50 {
		t.Fatal("lossy generation or unbounded page", page)
	}
	hit := schemas["KnowledgeSearchHit"].(map[string]any)["properties"].(map[string]any)
	if hit["snippet"].(map[string]any)["maxLength"] != 240 {
		t.Fatal("unbounded search snippet", hit)
	}
	citation := schemas["KnowledgeSearchCitation"].(map[string]any)["properties"].(map[string]any)
	if citation["generation"].(map[string]any)["type"] != "string" || citation["visibility_revision"].(map[string]any)["pattern"] != "^[0-9a-f]{64}$" {
		t.Fatal("citation proof omitted or lossy", citation)
	}
	source := paths["/api/v1/knowledge/collections/{id}/sources/{member_id}"].(map[string]any)["get"].(map[string]any)
	foundRevision := false
	for _, raw := range source["parameters"].([]any) {
		p := raw.(map[string]any)
		if p["in"] == "query" && p["name"] == "revision" {
			foundRevision = p["schema"].(map[string]any)["pattern"] == "^[0-9a-f]{64}$"
		}
	}
	if !foundRevision {
		t.Fatal("source endpoint cannot document exact citation reopen")
	}
}

func TestKnowledgeIntegrationSourceHTTPContract(t *testing.T) {
	root, err := FindRepositoryRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	routes, err := ParseServerRoutes(filepath.Join(root, "internal/transport/httpapi/router.go"))
	if err != nil {
		t.Fatal(err)
	}
	var source RouteContract
	count := 0
	for _, route := range routes {
		if route.OperationCode != "http.integration.knowledge.source.read" {
			continue
		}
		count++
		source = route
	}
	if count != 1 || source.Method != "GET" || source.Path != "/api/v1/integration/knowledge/collections/:id/sources/:member_id/revisions/:revision" || source.Handler != "knowledgeHandler.IntegrationSource" || source.OperationCode != "http.integration.knowledge.source.read" {
		t.Fatal("unexpected Integration Knowledge entry inventory", count, source)
	}
	if source.ModuleOwner != "feature.knowledge" || source.Audience != "integration" || source.Auth != "integration-digest+current-facts+whitelist+source-acl" || source.PermissionCode != "" || source.Permission != "" || source.Scope != "endpoint+public-collection+current-origin" || source.Ownership != "current-origin-and-collection" || source.Audit != "required-command+outbox" {
		t.Fatal("Integration source authorization domain changed", source)
	}
	profile := profileFor(source)
	if !profile.NoBody || profile.RequestSchema != "" || profile.ResponseSchema != "KnowledgeSource" || profile.Paginated {
		t.Fatal("Integration source body or response expanded", profile)
	}
	var document map[string]any
	if err = yaml.Unmarshal(OpenAPI(routes), &document); err != nil {
		t.Fatal(err)
	}
	paths := document["paths"].(map[string]any)
	operation := paths[openAPIPath(source.Path)].(map[string]any)["get"].(map[string]any)
	if operation["requestBody"] != nil || operation["x-campusos-auth"] != source.Auth || operation["x-campusos-scope"] != source.Scope || operation["x-campusos-module-owner"] != source.ModuleOwner {
		t.Fatal("Integration source metadata weakened", operation)
	}
	security := operation["security"].([]any)
	if len(security) != 1 || len(security[0].(map[string]any)) != 1 || security[0].(map[string]any)["integrationCredential"] == nil {
		t.Fatal("Integration source inherited another credential domain", security)
	}
	parameters := operation["parameters"].([]any)
	if len(parameters) != 3 {
		t.Fatal("Integration source must have only three path parameters", parameters)
	}
	seen := map[string]bool{}
	for _, raw := range parameters {
		parameter := raw.(map[string]any)
		name := parameter["name"].(string)
		if parameter["in"] != "path" || parameter["required"] != true || (name != "id" && name != "member_id" && name != "revision") || seen[name] {
			t.Fatal("Integration source allows query or ambiguous parameters", parameter)
		}
		seen[name] = true
		if name == "revision" && parameter["schema"].(map[string]any)["pattern"] != "^[0-9a-f]{64}$" {
			t.Fatal("Integration source lost exact current proof", parameter)
		}
	}
	responses := operation["responses"].(map[string]any)
	for _, status := range []string{"200", "400", "401", "403", "404", "409", "503"} {
		if responses[status] == nil {
			t.Fatal("missing Integration source response", status)
		}
	}
	if !strings.Contains(string(OpenAPI([]RouteContract{source})), "$ref: '#/components/schemas/KnowledgeSource'") {
		t.Fatal("Integration source lost narrow Knowledge response")
	}
	// Issuance and scope updates must document this compiled rule too; a
	// working read endpoint with a Community-only management schema is partial.
	schemas := document["components"].(map[string]any)["schemas"].(map[string]any)
	rule := schemas["IntegrationRule"].(map[string]any)["properties"].(map[string]any)
	for field, expected := range map[string]string{"endpoint": "knowledge.source.read", "action": "knowledge.source.read", "resource_kind": "public_collection"} {
		found := false
		for _, value := range rule[field].(map[string]any)["enum"].([]any) {
			found = found || value == expected
		}
		if !found {
			t.Fatal("credential management schema cannot issue the published Knowledge scope", field)
		}
	}
}

func TestKnowledgeIntegrationSearchContractAndIssuanceScope(t *testing.T) {
	root, err := FindRepositoryRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	routes, err := ParseServerRoutes(filepath.Join(root, "internal/transport/httpapi/router.go"))
	if err != nil {
		t.Fatal(err)
	}
	var search RouteContract
	count := 0
	for _, r := range routes {
		if r.OperationCode == "http.integration.knowledge.search" {
			search = r
			count++
		}
	}
	if count != 1 || search.Method != "POST" || search.Path != "/api/v1/integration/knowledge/collections/:id/search" || search.ModuleOwner != "feature.knowledge" || search.Audience != "integration" || search.PermissionCode != "" || search.Auth != "integration-digest+current-facts+whitelist+source-acl" || search.Scope != "endpoint+public-collection+current-origin" || search.Audit != "required-command+outbox" {
		t.Fatal("search route scope/domain drift", search)
	}
	profile := profileFor(search)
	if profile.NoBody || profile.RequestSchema != "KnowledgeIntegrationSearchRequest" || profile.ResponseSchema != "KnowledgeSearchPage" || profile.Paginated {
		t.Fatal("search DTO widened", profile)
	}
	var doc map[string]any
	if err = yaml.Unmarshal(OpenAPI(routes), &doc); err != nil {
		t.Fatal(err)
	}
	op := doc["paths"].(map[string]any)[openAPIPath(search.Path)].(map[string]any)["post"].(map[string]any)
	security := op["security"].([]any)
	if len(security) != 1 || security[0].(map[string]any)["integrationCredential"] == nil {
		t.Fatal("search uses wrong credential domain")
	}
	params := op["parameters"].([]any)
	if len(params) != 1 || params[0].(map[string]any)["name"] != "id" || params[0].(map[string]any)["in"] != "path" {
		t.Fatal("machine search permits URL scope/query override")
	}
	schemas := doc["components"].(map[string]any)["schemas"].(map[string]any)
	request := schemas["KnowledgeIntegrationSearchRequest"].(map[string]any)
	if request["additionalProperties"] != false || len(request["required"].([]any)) != 3 {
		t.Fatal("search JSON is not closed")
	}
	props := request["properties"].(map[string]any)
	if len(props) != 3 || props["page"].(map[string]any)["maximum"] != 100000 || props["page_size"].(map[string]any)["maximum"] != 50 || props["query"].(map[string]any)["maxLength"] != 1024 {
		t.Fatal("search input bounds drift")
	}
	rule := schemas["IntegrationRule"].(map[string]any)["properties"].(map[string]any)
	found := false
	for _, v := range rule["endpoint"].(map[string]any)["enum"].([]any) {
		found = found || v == "knowledge.search"
	}
	if !found {
		t.Fatal("management schema cannot issue search endpoint rule")
	}
}

func TestKnowledgeStandardMCPContractUsesDirectJSONRPC(t *testing.T) {
	routes, err := ParseServerRoutes("../../internal/transport/httpapi/router.go")
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, r := range routes {
		if !strings.HasPrefix(r.OperationCode, "http.integration.knowledge.mcp") {
			continue
		}
		count++
		if r.Path != "/api/v1/integration/knowledge/collections/:id/mcp" || r.Audience != "integration" || r.PermissionCode != "" || r.Auth != "integration-digest+current-facts+whitelist+source-acl" {
			t.Fatal("MCP authority drift", r)
		}
		p, _ := knowledgeProfile(r.OperationCode)
		if r.Method == "GET" && p.SuccessStatus != "405" {
			t.Fatal("GET falsely advertises a success body")
		}
		if !p.DirectJSON || p.ResponseSchema != "KnowledgeMCPResponse" {
			t.Fatal("MCP result wrapped as platform Envelope")
		}
	}
	if count != 2 {
		t.Fatal("MCP POST/GET route contract", count)
	}
}
