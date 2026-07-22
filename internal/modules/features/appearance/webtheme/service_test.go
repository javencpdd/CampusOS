package webtheme

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

type fakeConfigSource struct {
	enabled bool
	config  map[string]interface{}
}

func (s fakeConfigSource) Enabled() bool                  { return s.enabled }
func (s fakeConfigSource) Config() map[string]interface{} { return s.config }

func TestCatalogAndPackageUseAdministratorSourceDirectory(t *testing.T) {
	root := t.TempDir()
	t.Setenv("RESOURCE_DIR", root)
	packRoot := filepath.Join(root, "themes", "test-web")
	writeTestFile(t, filepath.Join(packRoot, "style.yaml"), `schema_version: page-style-pack.v1
target: web
name: test-web
version: 0.1.0
display_name: Test Web
delivery_contract: campusos.appearance-delivery/v1
viewport_support:
  desktop: true
  mobile: true
  mobile_breakpoint: 720px
entry: templates/page.html
styles:
  - styles/theme.css
preview_images:
  desktop: preview-desktop.png
  mobile: preview-mobile.png
`)
	writeTestFile(t, filepath.Join(packRoot, "templates/page.html"), `<section><h2>Test</h2></section>`)
	writeTestFile(t, filepath.Join(packRoot, "styles/theme.css"), `.app-container[data-campusos-web] .app-main { padding: 20px; } @media (max-width: 720px) { .app-container[data-campusos-web] .app-main { padding: 12px; } }`)
	writeTestFile(t, filepath.Join(packRoot, "preview-desktop.png"), "desktop preview")
	writeTestFile(t, filepath.Join(packRoot, "preview-mobile.png"), "mobile preview")

	source := fakeConfigSource{enabled: true, config: map[string]interface{}{
		"default_style_pack": "test-web",
		"allow_user_switch":  true,
	}}
	svc := NewService(source)

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

func TestPackageFailsWhenAppearanceFeatureIsDisabled(t *testing.T) {
	svc := NewService(fakeConfigSource{enabled: false})

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
