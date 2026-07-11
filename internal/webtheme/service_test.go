package webtheme

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/campusos/CampusOS/internal/plugin"
)

func TestCatalogAndPackageUseAdministratorSourceDirectory(t *testing.T) {
	root := t.TempDir()
	t.Setenv("PLUGIN_DATA_DIR", root)
	packRoot := filepath.Join(root, PluginName, "style-packs", "test-web")
	writeTestFile(t, filepath.Join(packRoot, "style.yaml"), `schema_version: page-style-pack.v1
target: web
name: test-web
version: 0.1.0
display_name: Test Web
entry: templates/page.html
styles:
  - styles/theme.css
`)
	writeTestFile(t, filepath.Join(packRoot, "templates/page.html"), `<section><h2>Test</h2></section>`)
	writeTestFile(t, filepath.Join(packRoot, "styles/theme.css"), `.app-container[data-campusos-web] .app-main { padding: 20px; }`)

	p := &plugin.Plugin{
		Status: plugin.StatusRunning,
		Manifest: &plugin.Manifest{
			Name: PluginName,
			Config: map[string]interface{}{
				"default_style_pack": "test-web",
				"allow_user_switch":  true,
			},
		},
	}
	svc := NewService(func(name string) (*plugin.Plugin, bool) { return p, name == PluginName })

	catalog, err := svc.Catalog()
	if err != nil {
		t.Fatalf("catalog: %v", err)
	}
	if !catalog.Enabled || !catalog.AllowUserSwitch || catalog.DefaultStylePack != "test-web" || len(catalog.Items) != 1 {
		t.Fatalf("unexpected catalog: %#v", catalog)
	}
	pack, err := svc.Package("test-web")
	if err != nil {
		t.Fatalf("package: %v", err)
	}
	if pack.Manifest.Target != "web" || pack.CSS == "" {
		t.Fatalf("unexpected package: %#v", pack)
	}
}

func TestPackageFailsWhenSystemPluginIsStopped(t *testing.T) {
	p := &plugin.Plugin{Status: plugin.StatusStopped, Manifest: &plugin.Manifest{Name: PluginName}}
	svc := NewService(func(name string) (*plugin.Plugin, bool) { return p, name == PluginName })

	if _, err := svc.Package("test-web"); !errors.Is(err, ErrDisabled) {
		t.Fatalf("expected disabled error, got %v", err)
	}
}

func writeTestFile(t *testing.T, name, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(name, []byte(content), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
}
