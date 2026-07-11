package stylepack

import (
	"archive/zip"
	"bytes"
	"path/filepath"
	"testing"
)

func TestLoadZipAcceptsValidStylePack(t *testing.T) {
	data := zipFiles(t, map[string]string{
		"campus/style.yaml": `schema_version: page-style-pack.v1
target: personal-space
name: clean-blog
version: 0.1.0
entry: templates/page.html
templates:
  - name: page
    path: templates/page.html
  - name: card
    path: templates/card.html
styles:
  - styles/theme.css
preview_image: preview.png
config_schema: config.schema.json
assets:
  - name: avatar-frame
    path: assets/avatar-frame.png
    type: image/png
`,
		"campus/templates/page.html":     `<section class="cstyle-page"><h2>Hello</h2><p>World</p></section>`,
		"campus/templates/card.html":     `<article class="cstyle-card"><strong>Card</strong></article>`,
		"campus/styles/theme.css":        `.public-space[data-campusos-space] .cstyle-page { padding: 16px; color: #2563eb; }`,
		"campus/preview.png":             `preview placeholder`,
		"campus/assets/avatar-frame.png": `avatar placeholder`,
		"campus/config.schema.json":      `{"type":"object","properties":{"title":{"type":"string"}}}`,
	})

	pkg, result := LoadZip(bytes.NewReader(data), int64(len(data)))
	if !result.Valid {
		t.Fatalf("expected valid style pack, got %#v", result.Errors)
	}
	if pkg.Manifest.Name != "clean-blog" || pkg.HTML == "" || pkg.CSS == "" {
		t.Fatalf("unexpected package: %#v", pkg)
	}
	if pkg.Manifest.ConfigSchema != "config.schema.json" || len(pkg.Manifest.Templates) != 2 {
		t.Fatalf("unexpected normalized manifest: %#v", pkg.Manifest)
	}
}

func TestAuroraCampusReferencePackIsValid(t *testing.T) {
	pack, result := LoadDir(filepath.Join("..", "..", "data", "plugin_data", "web-theme", "style-packs", "aurora-campus"))
	if !result.Valid {
		t.Fatalf("reference pack invalid: %#v", result.Errors)
	}
	if pack == nil || pack.Manifest.SchemaVersion != AppSchemaVersion || pack.Manifest.Layout == nil {
		t.Fatalf("unexpected reference package: %#v", pack)
	}
}

func TestLoadZipAcceptsFullViewportAppStylePackV2(t *testing.T) {
	data := zipFiles(t, map[string]string{
		"style.yaml": `schema_version: campusos.app-style-pack.v2
target: web
name: full-campus
version: 0.2.0
entry: templates/page.html
styles: [styles/theme.css]
layout:
  mode: full
  header_mode: sticky
  scroll_mode: page
  background_asset: assets/campus.png
  animation_preset: reveal
assets:
  - name: campus
    path: assets/campus.png
    type: image/png
surface_overrides:
  - surface_id: plugin.poll.page.list
    variant: reading
    region: hero
`,
		"templates/page.html": `<section class="cstyle-page">Full viewport</section>`,
		"styles/theme.css":    `.app-container[data-campusos-web] .campus-shell-body { width: 100%; }`,
		"assets/campus.png":   "image",
	})
	pkg, result := LoadZip(bytes.NewReader(data), int64(len(data)))
	if !result.Valid {
		t.Fatalf("v2 pack rejected: %#v", result.Errors)
	}
	if pkg.Manifest.Layout == nil || pkg.Manifest.Layout.Mode != "full" {
		t.Fatalf("layout missing: %#v", pkg.Manifest)
	}
}

func TestLoadZipRejectsUnsafeHTML(t *testing.T) {
	data := zipFiles(t, map[string]string{
		"style.yaml": `schema_version: page-style-pack.v1
target: homepage
name: bad-html
version: 0.1.0
entry: templates/page.html
`,
		"templates/page.html": `<img src=x onerror="alert(1)">`,
	})

	_, result := LoadZip(bytes.NewReader(data), int64(len(data)))
	if result.Valid {
		t.Fatalf("expected unsafe html to fail")
	}
}

func TestLoadZipRejectsUnsafeCSS(t *testing.T) {
	data := zipFiles(t, map[string]string{
		"style.yaml": `schema_version: page-style-pack.v1
target: homepage
name: bad-css
version: 0.1.0
entry: templates/page.html
styles:
  - styles/theme.css
`,
		"templates/page.html": `<section><h2>Hello</h2></section>`,
		"styles/theme.css":    `.x { background: url(javascript:alert(1)); }`,
	})

	_, result := LoadZip(bytes.NewReader(data), int64(len(data)))
	if result.Valid {
		t.Fatalf("expected unsafe css to fail")
	}
}

func TestLoadZipRejectsCSSOutsideTargetScope(t *testing.T) {
	data := zipFiles(t, map[string]string{
		"style.yaml": `schema_version: page-style-pack.v1
target: personal-space
name: escaped-css
version: 0.1.0
entry: templates/page.html
styles:
  - styles/theme.css
`,
		"templates/page.html": `<section class="cstyle-page"><h2>Hello</h2></section>`,
		"styles/theme.css":    `.app-header { display: none; }`,
	})

	_, result := LoadZip(bytes.NewReader(data), int64(len(data)))
	if result.Valid {
		t.Fatalf("expected CSS outside the personal-space root to fail")
	}
}

func TestLoadZipAcceptsScopedAnimationEffect(t *testing.T) {
	data := zipFiles(t, map[string]string{
		"style.yaml": `schema_version: page-style-pack.v1
target: web
name: motion-web
version: 0.1.0
entry: templates/page.html
styles:
  - styles/theme.css
effect:
  runtime: sandbox-worker.v1
  entry: effects/main.js
  source: effects/main.ts
`,
		"templates/page.html": `<section class="cstyle-page"><h2>Motion</h2></section>`,
		"styles/theme.css": `.app-container[data-campusos-web] .app-main { animation: rise 300ms ease-out; }
@keyframes rise { from { opacity: 0; } to { opacity: 1; } }
@media (max-width: 720px) { .app-container[data-campusos-web] .app-main { padding: 12px; } }`,
		"effects/main.js": `CampusEffect.register({ frame(api) { api.clear(); } });`,
		"effects/main.ts": `CampusEffect.register({ frame(api: EffectFrame) { api.clear(); } });`,
	})

	pkg, result := LoadZip(bytes.NewReader(data), int64(len(data)))
	if !result.Valid {
		t.Fatalf("expected scoped animated pack to pass, got %#v", result.Errors)
	}
	if pkg.EffectJS == "" || pkg.Manifest.Effect == nil {
		t.Fatalf("expected compiled effect, got %#v", pkg)
	}
}

func TestLoadZipRejectsEffectWithNetworkCapability(t *testing.T) {
	data := zipFiles(t, map[string]string{
		"style.yaml": `schema_version: page-style-pack.v1
target: web
name: network-effect
version: 0.1.0
entry: templates/page.html
effect:
  runtime: sandbox-worker.v1
  entry: effects/main.js
`,
		"templates/page.html": `<section><h2>Motion</h2></section>`,
		"effects/main.js":     `CampusEffect.register({ start() { fetch("https://example.com"); } });`,
	})

	_, result := LoadZip(bytes.NewReader(data), int64(len(data)))
	if result.Valid {
		t.Fatalf("expected network-capable effect to fail")
	}
}

func TestLoadZipRejectsUnsafeTemplateHTML(t *testing.T) {
	data := zipFiles(t, map[string]string{
		"style.yaml": `schema_version: page-style-pack.v1
target: homepage
name: bad-template
version: 0.1.0
entry: templates/page.html
styles:
  - styles/theme.css
`,
		"templates/page.html": `<section><h2>Hello</h2></section>`,
		"templates/card.html": `<img src=x onerror="alert(1)">`,
		"styles/theme.css":    `.x { color: #111827; }`,
	})

	_, result := LoadZip(bytes.NewReader(data), int64(len(data)))
	if result.Valid {
		t.Fatalf("expected unsafe template html to fail")
	}
}

func TestLoadZipRejectsInvalidConfigSchema(t *testing.T) {
	data := zipFiles(t, map[string]string{
		"style.yaml": `schema_version: page-style-pack.v1
target: homepage
name: bad-config
version: 0.1.0
entry: templates/page.html
config_schema: config.schema.json
`,
		"templates/page.html": `<section><h2>Hello</h2></section>`,
		"config.schema.json":  `{not-json}`,
	})

	_, result := LoadZip(bytes.NewReader(data), int64(len(data)))
	if result.Valid {
		t.Fatalf("expected invalid config schema to fail")
	}
}

func TestLoadZipRejectsAbsolutePath(t *testing.T) {
	data := zipFiles(t, map[string]string{
		"/style.yaml": `schema_version: page-style-pack.v1
target: homepage
name: absolute-path
version: 0.1.0
entry: templates/page.html
`,
		"templates/page.html": `<section><h2>Hello</h2></section>`,
	})

	_, result := LoadZip(bytes.NewReader(data), int64(len(data)))
	if result.Valid {
		t.Fatalf("expected absolute path to fail")
	}
}

func TestZipBundleCanBeLoaded(t *testing.T) {
	bundle := BuildExample("homepage", "homepage-example", "Homepage Example", "Title", "Subtitle", "", "", "")
	data, err := ZipBundle(bundle)
	if err != nil {
		t.Fatalf("zip bundle: %v", err)
	}
	pkg, result := LoadZip(bytes.NewReader(data), int64(len(data)))
	if !result.Valid {
		t.Fatalf("expected zipped example to be valid, got %#v", result.Errors)
	}
	if pkg.Manifest.Name != "homepage-example" {
		t.Fatalf("unexpected package: %#v", pkg.Manifest)
	}
}

func TestBuiltInSourceStylePacksAreValid(t *testing.T) {
	t.Chdir("../..")
	t.Setenv("PLUGIN_DATA_DIR", DefaultPluginDataDir)
	cases := []struct {
		root   string
		target string
	}{
		{SourceDir("personal-space", "clean-blog"), "personal-space"},
		{SourceDir("personal-space", "kinetic-journal"), "personal-space"},
		{SourceDir("homepage-customizer", "campus-hero"), "homepage"},
		{SourceDir("web-theme", "campus-canvas"), "web"},
	}

	for _, tc := range cases {
		pkg, result := LoadDir(tc.root)
		if !result.Valid {
			t.Fatalf("expected %s to be valid, got %#v", tc.root, result.Errors)
		}
		if pkg.Manifest.Target != tc.target {
			t.Fatalf("expected target %s, got %s", tc.target, pkg.Manifest.Target)
		}
	}
}

func TestListSourcePacksIncludesBuiltInExamples(t *testing.T) {
	t.Chdir("../..")
	t.Setenv("PLUGIN_DATA_DIR", DefaultPluginDataDir)

	items, err := ListSourcePacks("personal-space")
	if err != nil {
		t.Fatalf("list source packs: %v", err)
	}
	if len(items) == 0 {
		t.Fatalf("expected built-in source style packs")
	}
	var found bool
	for _, item := range items {
		if item.Name != "clean-blog" {
			continue
		}
		found = true
		if !item.Validation.Valid {
			t.Fatalf("expected clean-blog to be valid, got %#v", item.Validation.Errors)
		}
		if item.Target != "personal-space" || item.Manifest == nil {
			t.Fatalf("unexpected clean-blog source info: %#v", item)
		}
	}
	if !found {
		t.Fatalf("expected clean-blog source pack, got %#v", items)
	}
}

func zipFiles(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("create zip entry: %v", err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatalf("write zip entry: %v", err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return buf.Bytes()
}
