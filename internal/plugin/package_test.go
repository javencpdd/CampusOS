package plugin

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPackagePluginAndInstallPluginPackage(t *testing.T) {
	sourceDir := writePackablePlugin(t, t.TempDir(), "packable", "0.1.0")
	if err := os.MkdirAll(filepath.Join(sourceDir, "data"), 0o755); err != nil {
		t.Fatalf("create data dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "data", "runtime.db"), []byte("runtime data"), 0o644); err != nil {
		t.Fatalf("write runtime data: %v", err)
	}

	packagePath := filepath.Join(t.TempDir(), "packable.campusos-plugin.tar.gz")
	info, err := PackagePlugin(sourceDir, packagePath)
	if err != nil {
		t.Fatalf("package plugin: %v", err)
	}
	if info.Manifest.Name != "packable" || info.PackagePath != packagePath {
		t.Fatalf("unexpected package info: %#v", info)
	}

	installDir := t.TempDir()
	installed, err := InstallPluginPackage(packagePath, installDir, false)
	if err != nil {
		t.Fatalf("install plugin package: %v", err)
	}
	if installed.Manifest.Name != "packable" {
		t.Fatalf("unexpected installed manifest: %#v", installed.Manifest)
	}
	if _, err := LoadManifest(filepath.Join(installDir, "packable", "plugin.yaml")); err != nil {
		t.Fatalf("expected installed manifest: %v", err)
	}
	if _, err := os.Stat(filepath.Join(installDir, "packable", "data", "runtime.db")); !os.IsNotExist(err) {
		t.Fatalf("expected runtime data to be excluded, stat err=%v", err)
	}

	inspected, err := InspectPluginPackage(packagePath)
	if err != nil {
		t.Fatalf("inspect package: %v", err)
	}
	if inspected.Name != "packable" {
		t.Fatalf("unexpected inspected manifest: %#v", inspected)
	}
}

func TestManagerImportPackageReplaceAndExport(t *testing.T) {
	sourceV1 := writePackablePlugin(t, t.TempDir(), "replaceable", "0.1.0")
	sourceV2 := writePackablePlugin(t, t.TempDir(), "replaceable", "0.2.0")
	packageV1 := filepath.Join(t.TempDir(), "replaceable-v1.campusos-plugin.tar.gz")
	packageV2 := filepath.Join(t.TempDir(), "replaceable-v2.campusos-plugin.tar.gz")
	if _, err := PackagePlugin(sourceV1, packageV1); err != nil {
		t.Fatalf("package v1: %v", err)
	}
	if _, err := PackagePlugin(sourceV2, packageV2); err != nil {
		t.Fatalf("package v2: %v", err)
	}

	manager := NewManager()
	installDir := t.TempDir()
	installed, err := manager.ImportPackage(packageV1, installDir, false)
	if err != nil {
		t.Fatalf("import v1: %v", err)
	}
	if installed.Manifest.Version != "0.1.0" {
		t.Fatalf("unexpected v1: %#v", installed.Manifest)
	}

	if _, err := manager.ImportPackage(packageV2, installDir, false); err == nil {
		t.Fatalf("expected duplicate import to fail")
	}

	installed, err = manager.ImportPackage(packageV2, installDir, true)
	if err != nil {
		t.Fatalf("replace import: %v", err)
	}
	if installed.Manifest.Version != "0.2.0" {
		t.Fatalf("unexpected replaced manifest: %#v", installed.Manifest)
	}

	exportPath := filepath.Join(t.TempDir(), "replaceable-export.campusos-plugin.tar.gz")
	exported, err := manager.ExportPackage("replaceable", exportPath)
	if err != nil {
		t.Fatalf("export package: %v", err)
	}
	if exported.PackagePath != exportPath {
		t.Fatalf("unexpected exported info: %#v", exported)
	}
	manifest, err := InspectPluginPackage(exportPath)
	if err != nil {
		t.Fatalf("inspect exported package: %v", err)
	}
	if manifest.Version != "0.2.0" {
		t.Fatalf("unexpected exported version: %#v", manifest)
	}
}

func TestExtractPluginPackageRejectsUnsafeArchivePath(t *testing.T) {
	packagePath := filepath.Join(t.TempDir(), "unsafe.campusos-plugin.tar.gz")
	file, err := os.Create(packagePath)
	if err != nil {
		t.Fatalf("create archive: %v", err)
	}
	gz := gzip.NewWriter(file)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: "../plugin.yaml", Mode: 0o644, Size: 4}); err != nil {
		t.Fatalf("write header: %v", err)
	}
	if _, err := tw.Write([]byte("test")); err != nil {
		t.Fatalf("write body: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close file: %v", err)
	}

	err = ExtractPluginPackage(packagePath, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "unsafe archive path") {
		t.Fatalf("expected unsafe path error, got %v", err)
	}
}

func writePackablePlugin(t *testing.T, root, name, version string) string {
	t.Helper()
	sourceDir := filepath.Join(root, name+"-"+version)
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatalf("create source dir: %v", err)
	}
	manifest := fmt.Sprintf(`
name: %s
version: %q
runtime: wasm
storage:
  type: none
config:
  module: "plugin.wasm"
  entrypoint: "handle_event"
`, name, version)
	if err := os.WriteFile(filepath.Join(sourceDir, "plugin.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "plugin.wasm"), []byte("wasm"), 0o644); err != nil {
		t.Fatalf("write wasm: %v", err)
	}
	return sourceDir
}
