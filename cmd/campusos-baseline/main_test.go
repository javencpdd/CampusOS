package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPercentileUsesBoundedSample(t *testing.T) {
	values := []float64{1, 2, 3, 4, 5}
	if got := percentile(values, 0.50); got != 3 {
		t.Fatalf("p50=%v", got)
	}
	if got := percentile(values, 0.99); got != 4 {
		t.Fatalf("p99=%v", got)
	}
}

func TestRequireLoopback(t *testing.T) {
	for _, value := range []string{"http://localhost:8080", "http://127.0.0.1:8080", "http://[::1]:8080"} {
		if err := requireLoopback(value); err != nil {
			t.Fatalf("requireLoopback(%q): %v", value, err)
		}
	}
	if err := requireLoopback("https://example.com"); err == nil {
		t.Fatal("remote baseline target was accepted")
	}
}

func TestCollectBundleRecognizesVitePressOutput(t *testing.T) {
	root := t.TempDir()
	assets := filepath.Join(root, "docs-site", ".vitepress", "dist", "assets")
	if err := os.MkdirAll(assets, 0o755); err != nil {
		t.Fatalf("create assets: %v", err)
	}
	if err := os.WriteFile(filepath.Join(assets, "app.js"), make([]byte, 42), 0o644); err != nil {
		t.Fatalf("write JS asset: %v", err)
	}
	if err := os.WriteFile(filepath.Join(assets, "theme.css"), make([]byte, 24), 0o644); err != nil {
		t.Fatalf("write CSS asset: %v", err)
	}

	got := collectBundle(root, "docs-site", 0, 0)
	if got.Status != "measured" || got.JSBytes != 42 || got.CSSBytes != 24 {
		t.Fatalf("vitepress bundle = %#v", got)
	}
}
