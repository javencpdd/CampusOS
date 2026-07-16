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

func TestValidatePluginPackageDirRejectsCompiledModuleNames(t *testing.T) {
	for _, name := range []string{"personal-schedule", "homepage-customizer", "moderation"} {
		directory := writePackablePlugin(t, t.TempDir(), name, "0.1.0")
		if _, err := ValidatePluginPackageDir(directory); err == nil || !strings.Contains(err.Error(), "reserved") {
			t.Fatalf("expected compiled module name %q to be rejected, got %v", name, err)
		}
	}
}

func TestValidatePluginPackageDirRejectsLegacyBuiltinRuntime(t *testing.T) {
	directory := writePackablePlugin(t, t.TempDir(), "legacy-builtin", "0.1.0")
	manifestPath := filepath.Join(directory, "plugin.yaml")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	data = []byte(strings.Replace(string(data), "runtime: wasm", "runtime: builtin", 1))
	if err := os.WriteFile(manifestPath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ValidatePluginPackageDir(directory); err == nil || !strings.Contains(err.Error(), "runtime builtin") {
		t.Fatalf("expected legacy builtin runtime to be rejected, got %v", err)
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

func TestManagerImportPackageHotUpdatesLoadedUserPlugin(t *testing.T) {
	sourceV1 := writePackablePlugin(t, t.TempDir(), "hot-update", "0.1.0")
	sourceV2 := writePackablePlugin(t, t.TempDir(), "hot-update", "0.2.0")
	packageV1 := filepath.Join(t.TempDir(), "hot-update-v1.campusos-plugin.tar.gz")
	packageV2 := filepath.Join(t.TempDir(), "hot-update-v2.campusos-plugin.tar.gz")
	if _, err := PackagePlugin(sourceV1, packageV1); err != nil {
		t.Fatalf("package v1: %v", err)
	}
	if _, err := PackagePlugin(sourceV2, packageV2); err != nil {
		t.Fatalf("package v2: %v", err)
	}

	manager := NewManager()
	runtime := newFakeRuntime()
	manager.RegisterRuntime("wasm", runtime)
	installDir := t.TempDir()
	if _, err := manager.ImportPackage(packageV1, installDir, false); err != nil {
		t.Fatalf("import v1: %v", err)
	}
	if err := manager.ReloadUserPlugin("hot-update"); err != nil {
		t.Fatalf("load v1: %v", err)
	}
	installed, err := manager.ImportPackage(packageV2, installDir, true)
	if err != nil {
		t.Fatalf("hot update v2: %v", err)
	}
	if installed.Manifest.Version != "0.2.0" || installed.Status != StatusRunning {
		t.Fatalf("expected running v2 plugin, got %#v", installed)
	}
	if len(runtime.started) != 2 || len(runtime.stopped) != 1 {
		t.Fatalf("expected one runtime restart during update: starts=%#v stops=%#v", runtime.started, runtime.stopped)
	}
}

func TestPrecheckPluginPackageReportsRiskAndVersionChange(t *testing.T) {
	installDir := t.TempDir()
	installed := writePackablePlugin(t, installDir, "governed", "0.2.0")
	if err := os.Rename(installed, filepath.Join(installDir, "governed")); err != nil {
		t.Fatalf("install existing plugin dir: %v", err)
	}
	source := writeRiskyPlugin(t, t.TempDir(), "governed", "0.1.0")
	packagePath := filepath.Join(t.TempDir(), "governed.campusos-plugin.tar.gz")
	if _, err := PackagePlugin(source, packagePath); err != nil {
		t.Fatalf("package governed plugin: %v", err)
	}

	precheck, err := PrecheckPluginPackage(packagePath, installDir)
	if err != nil {
		t.Fatalf("precheck plugin package: %v", err)
	}
	if !precheck.Conflict {
		t.Fatalf("expected conflict in precheck: %#v", precheck)
	}
	if precheck.ExistingVersion != "0.2.0" || precheck.ImportVersion != "0.1.0" || precheck.VersionChange != "downgrade" {
		t.Fatalf("unexpected version comparison: %#v", precheck)
	}
	if precheck.RiskLevel != "high" || precheck.RiskScore == 0 || len(precheck.RiskReasons) == 0 {
		t.Fatalf("expected high risk package, got %#v", precheck)
	}
	if precheck.SignatureStatus != "unsigned" {
		t.Fatalf("expected unsigned package, got %q", precheck.SignatureStatus)
	}
}

func TestPrecheckPluginPackageRejectsSystemScope(t *testing.T) {
	sourceDir := writePackablePlugin(t, t.TempDir(), "system-package", "0.1.0")
	manifestPath := filepath.Join(sourceDir, "plugin.yaml")
	manifest, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	if err := os.WriteFile(manifestPath, append(manifest, []byte("\nscope: system\n")...), 0o644); err != nil {
		t.Fatalf("write system manifest: %v", err)
	}
	packagePath := filepath.Join(t.TempDir(), "system-package.campusos-plugin.tar.gz")
	if _, err := PackagePlugin(sourceDir, packagePath); err != nil {
		t.Fatalf("package system plugin: %v", err)
	}

	precheck, err := PrecheckPluginPackage(packagePath, t.TempDir())
	if err != nil {
		t.Fatalf("precheck system plugin: %v", err)
	}
	if precheck.Allowed || precheck.Manifest == nil || precheck.Manifest.Scope != ScopeSystem {
		t.Fatalf("expected system plugin package to be rejected, got %#v", precheck)
	}
}

func TestPrecheckPluginPackageReportsUserPermissionAndSchemaDiff(t *testing.T) {
	pluginsDir := t.TempDir()
	existing := writeV2ConsentPlugin(t, t.TempDir(), "consent-upgrade", "1.0.0", false, "1")
	if err := os.Rename(existing, filepath.Join(pluginsDir, "consent-upgrade")); err != nil {
		t.Fatal(err)
	}
	incoming := writeV2ConsentPlugin(t, t.TempDir(), "consent-upgrade", "1.1.0", true, "2")
	packagePath := filepath.Join(t.TempDir(), "consent-upgrade.campusos-plugin.tar.gz")
	if _, err := PackagePlugin(incoming, packagePath); err != nil {
		t.Fatal(err)
	}
	precheck, err := PrecheckPluginPackage(packagePath, pluginsDir)
	if err != nil {
		t.Fatal(err)
	}
	if !precheck.RequiresReauthorization || !containsString(precheck.AddedPermissions, "user:plugin_search:read") {
		t.Fatalf("permission diff missing: %#v", precheck)
	}
	if precheck.DataSchemaChange != "1 -> 2" {
		t.Fatalf("data schema change = %q", precheck.DataSchemaChange)
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

func TestPluginVersionSnapshotIsPortableAndTamperChecked(t *testing.T) {
	source := writePackablePlugin(t, t.TempDir(), "snapshot-plugin", "0.2.0")
	dataDir := t.TempDir()
	snapshot, err := CreatePluginSnapshot(source, dataDir, "test")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.IsAbs(snapshot.PackagePath) {
		t.Fatalf("metadata package path must be portable, got %q", snapshot.PackagePath)
	}
	items, err := ListPluginSnapshots("snapshot-plugin", dataDir)
	if err != nil || len(items) != 1 {
		t.Fatalf("expected one verified snapshot, items=%#v err=%v", items, err)
	}
	if !filepath.IsAbs(items[0].PackagePath) {
		t.Fatalf("runtime snapshot path should be resolved, got %q", items[0].PackagePath)
	}
	if err := os.WriteFile(items[0].PackagePath, []byte("tampered"), 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := ListPluginSnapshots("snapshot-plugin", dataDir); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("expected tampered snapshot rejection, got %v", err)
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

func writeRiskyPlugin(t *testing.T, root, name, version string) string {
	t.Helper()
	sourceDir := filepath.Join(root, name+"-"+version)
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatalf("create source dir: %v", err)
	}
	manifest := fmt.Sprintf(`
name: %s
version: %q
runtime: grpc
events:
  subscribe:
    - "thread.created.before"
permissions:
  api:
    - resource: "*"
      actions: ["*"]
storage:
  type: postgresql
config:
  command: "./plugin"
`, name, version)
	if err := os.WriteFile(filepath.Join(sourceDir, "plugin.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "plugin"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write plugin executable: %v", err)
	}
	return sourceDir
}

func writeV2ConsentPlugin(t *testing.T, root, name, version string, withSearch bool, schemaVersion string) string {
	t.Helper()
	dir := filepath.Join(root, name+"-"+version)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	searchPermission := ""
	if withSearch {
		searchPermission = `
    - resource: plugin_search
      actions: [read]
      purpose: Search personal notes
      revocable: true`
	}
	manifest := fmt.Sprintf(`api_version: campusos.plugin/v2
host_api_version: v2
name: %s
version: %s
runtime: wasm
scope: user
type: external
permissions:
  user:
    - resource: managed_data
      actions: [read]
      purpose: Read personal notes
      revocable: true%s
release:
  data_schema_version: %q
config:
  module: plugin.wasm
  entrypoint: handle_event
`, name, version, searchPermission, schemaVersion)
	if err := os.WriteFile(filepath.Join(dir, "plugin.yaml"), []byte(manifest), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "plugin.wasm"), []byte("wasm"), 0o640); err != nil {
		t.Fatal(err)
	}
	return dir
}
