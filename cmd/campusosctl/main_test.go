package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/campusos/CampusOS/internal/plugin"
)

func TestPluginInitCreatesScaffold(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sample-plugin")
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run([]string{"plugin", "init", "sample-plugin", "--dir", dir}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "created plugin scaffold") {
		t.Fatalf("unexpected stdout: %s", stdout.String())
	}

	manifest, err := plugin.LoadManifest(filepath.Join(dir, "plugin.yaml"))
	if err != nil {
		t.Fatalf("load manifest: %v", err)
	}
	if manifest.Name != "sample-plugin" || manifest.Runtime != "wasm" {
		t.Fatalf("unexpected manifest: %#v", manifest)
	}
	if manifest.Config["entrypoint"] != "handle_event" {
		t.Fatalf("expected wasm entrypoint config, got %#v", manifest.Config)
	}
	if _, err := os.Stat(filepath.Join(dir, "README.md")); err != nil {
		t.Fatalf("expected README: %v", err)
	}
}

func TestPluginInspectPrintsManifestSummary(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "plugin.yaml"), []byte(`
name: inspected-plugin
version: "0.1.0"
runtime: wasm
events:
  subscribe:
    - thread.created
storage:
  type: none
`), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run([]string{"plugin", "inspect", dir}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d stderr=%s", code, stderr.String())
	}

	var summary map[string]interface{}
	if err := json.Unmarshal(stdout.Bytes(), &summary); err != nil {
		t.Fatalf("decode summary: %v", err)
	}
	if summary["name"] != "inspected-plugin" || summary["runtime"] != "wasm" {
		t.Fatalf("unexpected summary: %#v", summary)
	}
}

func TestPluginInitRejectsInvalidName(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run([]string{"plugin", "init", "InvalidName"}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected non-zero exit")
	}
	if !strings.Contains(stderr.String(), "can only contain") {
		t.Fatalf("unexpected stderr: %s", stderr.String())
	}
}
