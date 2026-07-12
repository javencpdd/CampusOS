package resource

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestRepositoryPrefersNewAndReadsLegacy(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "themes", "clean")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "theme.css"), []byte("body{}"), 0644); err != nil {
		t.Fatal(err)
	}
	checksum, err := DirectoryChecksum(dir)
	if err != nil {
		t.Fatal(err)
	}
	m := Manifest{Schema: "campusos.resource/v1", ID: "clean", Type: Theme, Version: "1.0.0", Compatibility: ">=0.7", Entry: "theme.css", Checksum: checksum, Source: "test"}
	data, _ := json.Marshal(m)
	if err := os.WriteFile(filepath.Join(dir, "resource.json"), data, 0644); err != nil {
		t.Fatal(err)
	}
	legacy := filepath.Join(t.TempDir(), "legacy")
	if err := os.MkdirAll(filepath.Join(legacy, "old"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacy, "old", "style.yaml"), []byte("name: old"), 0644); err != nil {
		t.Fatal(err)
	}
	items, err := NewFileRepository(root, map[Type][]string{Theme: {legacy}}).List(Theme)
	if err != nil || len(items) != 2 {
		t.Fatalf("items=%#v err=%v", items, err)
	}
}

func TestValidateRejectsRuntimeArtifactsAndChecksumMismatch(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "theme.css"), []byte("body{}"), 0644); err != nil {
		t.Fatal(err)
	}
	manifest := Manifest{Schema: "campusos.resource/v1", ID: "safe", Type: Theme, Version: "1", Compatibility: ">=0.7", Entry: "theme.css", Checksum: "wrong", Source: "test"}
	if err := Validate(dir, manifest); err == nil {
		t.Fatal("expected checksum rejection")
	}
	if err := os.WriteFile(filepath.Join(dir, "plugin.yaml"), []byte("runtime: wasm"), 0644); err != nil {
		t.Fatal(err)
	}
	manifest.Checksum, _ = DirectoryChecksum(dir)
	if err := Validate(dir, manifest); err == nil {
		t.Fatal("expected runtime artifact rejection")
	}
}
