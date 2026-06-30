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
	if manifest.ConfigSchema == nil || len(manifest.ConfigSchema.Fields) == 0 {
		t.Fatalf("expected config schema fields, got %#v", manifest.ConfigSchema)
	}
	if _, err := os.Stat(filepath.Join(dir, "README.md")); err != nil {
		t.Fatalf("expected README: %v", err)
	}
}

func TestPluginInitCreatesGRPCScaffold(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "grpc-plugin")
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run([]string{"plugin", "init", "grpc-plugin", "--runtime", "grpc", "--dir", dir}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d stderr=%s", code, stderr.String())
	}

	manifest, err := plugin.LoadManifest(filepath.Join(dir, "plugin.yaml"))
	if err != nil {
		t.Fatalf("load manifest: %v", err)
	}
	if manifest.Runtime != "grpc" {
		t.Fatalf("expected grpc runtime, got %q", manifest.Runtime)
	}
	if manifest.Config["command"] != "./plugin" {
		t.Fatalf("expected grpc command config, got %#v", manifest.Config)
	}
	if manifest.ConfigSchema == nil || len(manifest.ConfigSchema.Fields) == 0 {
		t.Fatalf("expected config schema fields, got %#v", manifest.ConfigSchema)
	}
	if manifest.ConfigSchema.Fields[0].Key != "command" {
		t.Fatalf("expected command schema field, got %#v", manifest.ConfigSchema.Fields[0])
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
config_schema:
  fields:
    - key: theme
      label: "Theme"
      type: select
      options:
        - label: "Light"
          value: "light"
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
	if summary["config_schema"] == nil {
		t.Fatalf("expected config_schema in summary: %#v", summary)
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

func TestPluginPackRejectsInvalidConfigSchema(t *testing.T) {
	sourceDir := filepath.Join(t.TempDir(), "invalid-schema")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatalf("create source dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "plugin.yaml"), []byte(`
name: invalid-schema
version: "0.1.0"
runtime: wasm
storage:
  type: none
config:
  module: "plugin.wasm"
config_schema:
  fields:
    - key: layout
      type: select
`), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "plugin.wasm"), []byte("wasm"), 0o644); err != nil {
		t.Fatalf("write wasm: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run([]string{"plugin", "pack", sourceDir}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected invalid schema to fail")
	}
	if !strings.Contains(stderr.String(), "requires options") {
		t.Fatalf("unexpected stderr: %s", stderr.String())
	}
}

func TestPluginPackAndInstall(t *testing.T) {
	sourceDir := filepath.Join(t.TempDir(), "packable")
	if err := os.MkdirAll(filepath.Join(sourceDir, "data"), 0o755); err != nil {
		t.Fatalf("create data dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "plugin.yaml"), []byte(`
name: packable
version: "0.1.0"
runtime: wasm
storage:
  type: none
config:
  module: "plugin.wasm"
  entrypoint: "handle_event"
`), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "plugin.wasm"), []byte("wasm"), 0o644); err != nil {
		t.Fatalf("write wasm: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "data", "runtime.db"), []byte("do not package"), 0o644); err != nil {
		t.Fatalf("write runtime data: %v", err)
	}

	packagePath := filepath.Join(t.TempDir(), "packable.campusos-plugin.tar.gz")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run([]string{"plugin", "pack", sourceDir, "--out", packagePath}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("pack exit code %d stderr=%s", code, stderr.String())
	}
	if _, err := os.Stat(packagePath); err != nil {
		t.Fatalf("expected package: %v", err)
	}

	installDir := t.TempDir()
	stdout.Reset()
	stderr.Reset()
	code = run([]string{"plugin", "install", packagePath, "--dir", installDir}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("install exit code %d stderr=%s", code, stderr.String())
	}
	if _, err := plugin.LoadManifest(filepath.Join(installDir, "packable", "plugin.yaml")); err != nil {
		t.Fatalf("expected installed manifest: %v", err)
	}
	if _, err := os.Stat(filepath.Join(installDir, "packable", "data", "runtime.db")); !os.IsNotExist(err) {
		t.Fatalf("expected runtime data to be excluded, stat err=%v", err)
	}
}

func TestPluginInstallRefusesExistingDirectory(t *testing.T) {
	sourceDir := filepath.Join(t.TempDir(), "existing-plugin")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatalf("create source dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "plugin.yaml"), []byte(`
name: existing-plugin
version: "0.1.0"
runtime: wasm
storage:
  type: none
config:
  module: "plugin.wasm"
`), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "plugin.wasm"), []byte("wasm"), 0o644); err != nil {
		t.Fatalf("write wasm: %v", err)
	}

	packagePath := filepath.Join(t.TempDir(), "existing-plugin.campusos-plugin.tar.gz")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if code := run([]string{"plugin", "pack", sourceDir, "--out", packagePath}, &stdout, &stderr); code != 0 {
		t.Fatalf("pack failed: %s", stderr.String())
	}

	installDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(installDir, "existing-plugin"), 0o755); err != nil {
		t.Fatalf("create existing plugin: %v", err)
	}
	stdout.Reset()
	stderr.Reset()
	code := run([]string{"plugin", "install", packagePath, "--dir", installDir}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected install to fail for existing directory")
	}
	if !strings.Contains(stderr.String(), "already exists") {
		t.Fatalf("unexpected stderr: %s", stderr.String())
	}
}
