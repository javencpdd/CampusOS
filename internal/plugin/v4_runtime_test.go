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
	installed, err := registerHistoricalV4Release(t, manager, V4Release{Release: pluginv4.InstalledRelease{Manifest: manifest, Path: t.TempDir(), Digest: "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}, PublicPath: "plugins/campusos.pdf-viewer/2.0.0-dev.1-test", UIOrigin: "http://localhost:3003"})
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

func TestV4RuntimeManifestTemplateOriginUsesBrowserFacingHost(t *testing.T) {
	gin.SetMode(gin.TestMode)
	manager := NewManager()
	manager.RegisterRuntime("none", NewNoneRuntime())
	manifest := testV4RuntimeManifest(t)
	if _, err := registerHistoricalV4Release(t, manager, V4Release{
		Release:    pluginv4.InstalledRelease{Manifest: manifest, Path: t.TempDir(), Digest: "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"},
		PublicPath: "plugins/campusos.pdf-viewer/2.0.0-dev.1-test",
		UIOrigin:   "http://{host}:3003",
	}); err != nil {
		t.Fatal(err)
	}
	if err := manager.Start(manifest.Key); err != nil {
		t.Fatal(err)
	}

	handler := NewRuntimeHTTPHandler(manager, nil)
	router := gin.New()
	router.GET("/runtime", handler.RuntimeManifest)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/runtime", nil)
	// Vite's Docker proxy must preserve this browser-visible host instead of
	// replacing it with the internal API service name.
	request.Host = "192.0.2.42:3000"
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatal(recorder.Body.String())
	}
	want := "http://192.0.2.42:3003/plugins/campusos.pdf-viewer/2.0.0-dev.1-test/ui/user/index.html"
	if !strings.Contains(recorder.Body.String(), want) {
		t.Fatalf("runtime manifest did not retain the browser-facing host: %s", recorder.Body.String())
	}
}

// This retired DTO fixture only exercises isolated rendering. Production has
// no v4-to-Manager adapter or loader after 03a.
func registerHistoricalV4Release(t *testing.T, m *Manager, release V4Release) (*Plugin, error) {
	t.Helper()
	if err := m.v4.put(release); err != nil {
		return nil, err
	}
	manifest := &Manifest{APIVersion: ManifestAPIVersionV3, HostAPIVersion: HostAPIVersionV3, Name: release.Release.Manifest.Key, Version: release.Release.Manifest.Version, Runtime: "none", Scope: ScopeSystem}
	for _, c := range release.Release.Manifest.Capabilities {
		manifest.CapabilityDeclarations = append(manifest.CapabilityDeclarations, CapabilityRequest{Code: c.Code, Required: c.Required, Purpose: c.Purpose, Scope: c.Scope})
	}
	if err := manifest.Validate(); err != nil {
		return nil, err
	}
	p := &Plugin{ID: manifest.Name, Manifest: manifest, Status: StatusInstalled, BackendState: BackendInstalled, FrontendState: FrontendLoaded, Health: HealthUnknown, DesiredEnabled: true, Directory: release.Release.Path, Checksum: strings.TrimPrefix(release.Release.Digest, "sha256:"), IsolatedUI: true}
	m.catalog.plugins[p.ID] = p
	return clonePlugin(p), nil
}
