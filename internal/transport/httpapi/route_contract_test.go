package httpapi

import (
	"path/filepath"
	"sort"
	"testing"

	"github.com/campusos/CampusOS/internal/projectaudit"
	"github.com/gin-gonic/gin"
)

// The source-derived contract must cover every route registered by the real
// router, including paths such as the API index with an empty relative path.
func TestRuntimeRoutesMatchGeneratedContractSource(t *testing.T) {
	gin.SetMode(gin.TestMode)
	root, err := projectaudit.FindRepositoryRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	contracts, err := projectaudit.ParseServerRoutes(filepath.Join(root, "internal/transport/httpapi/router.go"))
	if err != nil {
		t.Fatal(err)
	}
	contractRoutes := make(map[string]struct{}, len(contracts))
	for _, route := range contracts {
		contractRoutes[route.Method+" "+route.Path] = struct{}{}
	}

	router := Build(Dependencies{})
	var missing, extra []string
	for _, route := range router.Routes() {
		key := route.Method + " " + route.Path
		if _, ok := contractRoutes[key]; !ok {
			missing = append(missing, key)
		}
		delete(contractRoutes, key)
	}
	for key := range contractRoutes {
		extra = append(extra, key)
	}
	sort.Strings(missing)
	sort.Strings(extra)
	if len(missing) != 0 || len(extra) != 0 {
		t.Fatalf("runtime/contract route mismatch: missing from contract=%v, missing from runtime=%v", missing, extra)
	}
}

func TestRuntimeKnowledgePrivatePreflightKeepsUserAuthority(t *testing.T) {
	gin.SetMode(gin.TestMode)
	root, err := projectaudit.FindRepositoryRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	contracts, err := projectaudit.ParseServerRoutes(filepath.Join(root, "internal/transport/httpapi/router.go"))
	if err != nil {
		t.Fatal(err)
	}
	const operation = "http.knowledge.private.source.preflight"
	var source projectaudit.RouteContract
	for _, contract := range contracts {
		if contract.OperationCode == operation {
			source = contract
		}
	}
	router := Build(Dependencies{})
	found := false
	for _, descriptor := range router.RouteDescriptors() {
		if descriptor.OperationCode != operation {
			continue
		}
		found = true
		if string(descriptor.Audience) != source.Audience || descriptor.Auth != source.Auth || descriptor.Auth != "jwt+current-user-session+document-owner" || descriptor.Owner != "feature.knowledge" || descriptor.Audit != "none" || descriptor.PermissionCode != "" || descriptor.Permission != "" || descriptor.FeatureID != "knowledge" {
			t.Fatal("runtime preflight cannot inherit anonymous public source authority", descriptor, source)
		}
	}
	if !found {
		t.Fatal("private preflight runtime descriptor missing")
	}
}
