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
