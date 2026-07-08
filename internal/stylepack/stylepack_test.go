package stylepack

import (
	"archive/zip"
	"bytes"
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
		"campus/styles/theme.css":        `.cstyle-page { padding: 16px; color: #2563eb; }`,
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
		{SourceDir("homepage-customizer", "campus-hero"), "homepage"},
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
