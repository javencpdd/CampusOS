package plugin

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestPluginV5CutoverHasNoLegacyTransport(t *testing.T) {
	if CurrentManifestAPIVersion != "campusos.plugin/v5" {
		t.Fatal("current wire still advertises a retired protocol")
	}
	gin.SetMode(gin.TestMode)
	for name, handler := range map[string]gin.HandlerFunc{
		"import":   NewHandler(NewManager()).ImportPluginPackage,
		"precheck": NewHandler(NewManager()).PrecheckPluginPackage,
		"rollback": NewHandler(NewManager()).RollbackVersionSnapshot,
		"export":   NewHandler(NewManager()).ExportPlugin,
	} {
		t.Run(name, func(t *testing.T) {
			router := gin.New()
			router.POST("/package", handler)
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/package", strings.NewReader("UNTRUSTED_RAW_LEGACY_PACKAGE")))
			if recorder.Code != http.StatusServiceUnavailable || strings.Contains(recorder.Body.String(), "UNTRUSTED") {
				t.Fatal("legacy transport is not closed")
			}
		})
	}
	startup, err := os.ReadFile("../server/platform_modules.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, old := range []string{".LoadV4Releases(", ".InstallV4DevelopmentSources(", ".InstallFromPluginsDir("} {
		if bytes.Contains(startup, []byte(old)) {
			t.Fatal("legacy autoload remains")
		}
	}
	if !bytes.Contains(startup, []byte(`"plugin.v5.package-catalog"`)) {
		t.Fatal("real package catalog not bound by server")
	}
	host, ext := CurrentPluginV5Catalogs()
	host[0] = ' '
	again, _ := CurrentPluginV5Catalogs()
	if bytes.Equal(host, again) {
		t.Fatal("mutable published catalog")
	}
	if bytes.Contains(again, []byte("collection-public-a")) || bytes.Contains(ext, []byte("fixture")) {
		t.Fatal("published a synthetic scope")
	}
}
