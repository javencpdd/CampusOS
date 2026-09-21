package plugin

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	pluginv4 "github.com/campusos/CampusOS/internal/plugin/v4"
	"github.com/gin-gonic/gin"
)

func testV4RuntimeManifest(t *testing.T) *pluginv4.Manifest {
	t.Helper()
	manifest := &pluginv4.Manifest{
		APIVersion: pluginv4.APIVersion, Key: "campusos.pdf-viewer", Version: "2.0.0-dev.1",
		Publisher: pluginv4.Publisher{ID: "campusos", Name: "CampusOS"}, DisplayName: "PDF 文档预览", Description: "isolated test viewer",
		Compatibility: pluginv4.Compatibility{Host: ">=1.1", UI: pluginv4.UIContractVersion, Bridge: pluginv4.BridgeContractVersion},
		Backend:       pluginv4.Backend{Runtime: "none"},
		Artifacts:     pluginv4.Artifacts{UserUI: &pluginv4.UIArtifact{Source: "frontend/user", Entry: "ui/user/index.html"}},
		UI:            pluginv4.UI{User: &pluginv4.Audience{Surfaces: []pluginv4.Surface{{ID: "preview", Route: "preview", Title: "PDF 预览", Presentations: []string{"modal", "new-tab"}}}}},
		Capabilities:  []pluginv4.Capability{{Code: "article_attachment.self.preview", Required: true, Purpose: "预览文章附件", Scope: "self"}, {Code: "plugin_ui.surface.open", Required: true, Purpose: "打开受控界面", Scope: "self"}},
	}
	if err := manifest.Validate(); err != nil {
		t.Fatal(err)
	}
	return manifest
}

func TestV4ReleaseUsesSeparateCatalogAndProjectsAnIsolatedFrame(t *testing.T) {
	gin.SetMode(gin.TestMode)
	manager := NewManager()
	manager.RegisterRuntime("none", NewNoneRuntime())
	manifest := testV4RuntimeManifest(t)
	installed, err := manager.RegisterV4Release(V4Release{Release: pluginv4.InstalledRelease{Manifest: manifest, Path: t.TempDir(), Digest: "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}, PublicPath: "plugins/campusos.pdf-viewer/2.0.0-dev.1-test", UIOrigin: "http://localhost:3003"})
	if err != nil {
		t.Fatal(err)
	}
	if !installed.Manifest.UI.Empty() || installed.Manifest.Runtime != "none" {
		t.Fatalf("v4 authorization adapter leaked UI into legacy catalog: %#v", installed.Manifest)
	}
	if err := manager.Start(manifest.Key); err != nil {
		t.Fatal(err)
	}

	handler := NewRuntimeHTTPHandler(manager, nil)
	router := gin.New()
	router.GET("/runtime", handler.RuntimeManifest)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/runtime", nil))
	if recorder.Code != http.StatusOK {
		t.Fatal(recorder.Body.String())
	}
	body := recorder.Body.String()
	for _, expected := range []string{"campusos.ui/v3", "isolated-iframe", "http://localhost:3003/plugins/campusos.pdf-viewer/2.0.0-dev.1-test/ui/user/index.html", "campusos.pdf-viewer.preview"} {
		if !strings.Contains(body, expected) {
			t.Fatalf("runtime manifest missing %q: %s", expected, body)
		}
	}
}
