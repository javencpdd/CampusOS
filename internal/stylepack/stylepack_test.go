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
name: clean-folder
version: 0.1.0
entry: templates/page.html
styles:
  - styles/theme.css
`,
		"campus/templates/page.html": `<section class="cstyle-page"><h2>Hello</h2><p>World</p></section>`,
		"campus/styles/theme.css":    `.cstyle-page { padding: 16px; color: #2563eb; }`,
	})

	pkg, result := LoadZip(bytes.NewReader(data), int64(len(data)))
	if !result.Valid {
		t.Fatalf("expected valid style pack, got %#v", result.Errors)
	}
	if pkg.Manifest.Name != "clean-folder" || pkg.HTML == "" || pkg.CSS == "" {
		t.Fatalf("unexpected package: %#v", pkg)
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
	cases := []struct {
		root   string
		target string
	}{
		{filepath.Join("..", "..", "data", "plugins", "personal-space", "style-packs", "clean-folder"), "personal-space"},
		{filepath.Join("..", "..", "data", "plugins", "homepage-customizer", "style-packs", "campus-hero"), "homepage"},
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
