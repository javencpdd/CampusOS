package plugin

import (
	"context"
	"strings"
	"testing"
)

func TestPDFViewerBuiltinManifestIsManagedAndLeastPrivilege(t *testing.T) {
	manifest := NewPDFViewerBuiltinManifest()
	if err := manifest.Validate(); err != nil {
		t.Fatalf("manifest validation: %v", err)
	}
	manager := NewManager()
	manager.RegisterRuntime("builtin", NewBuiltinRuntime())
	installed, err := manager.RegisterBuiltin(manifest)
	if err != nil {
		t.Fatalf("register builtin: %v", err)
	}
	if installed.Manifest.Name != PDFViewerPluginName || installed.Manifest.Runtime != "builtin" || !installed.DesiredEnabled {
		t.Fatalf("unexpected managed builtin: %#v", installed)
	}
	if err := manager.Start(PDFViewerPluginName); err != nil {
		t.Fatalf("start builtin: %v", err)
	}
	if !manager.IsPluginRunning(PDFViewerPluginName) {
		t.Fatal("builtin PDF Viewer should expose running lifecycle state")
	}
	if _, err := manager.Packages().RegisterBuiltin(NewPDFViewerBuiltinManifest()); err == nil {
		t.Fatal("duplicate builtins must be rejected")
	}
}

func TestBuiltinPDFViewerRefreshesItsCompiledDigestAfterAPersistedVersionUpgrade(t *testing.T) {
	ctx := context.Background()
	repository := NewMemoryPluginRepository()
	if err := repository.Save(ctx, &PluginRecord{
		ID: 1, Name: PDFViewerPluginName, Version: "1.1.0-dev", Runtime: "builtin", Status: string(StatusRunning),
		Checksum: strings.Repeat("0", 64), Config: "{}",
	}); err != nil {
		t.Fatal(err)
	}
	manager := NewManager()
	manager.SetPluginRepository(repository)
	manager.RegisterRuntime("builtin", NewBuiltinRuntime())
	manifest := NewPDFViewerBuiltinManifest()
	if manifest.Version != "1.1.3-dev" {
		t.Fatalf("the authorization declaration upgrade must have a new immutable version, got %q", manifest.Version)
	}
	if _, err := manager.RegisterBuiltin(manifest); err != nil {
		t.Fatalf("register upgraded builtin: %v", err)
	}
	persisted, err := repository.GetByName(ctx, PDFViewerPluginName)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Version != manifest.Version || persisted.Checksum != digestJSON(manifest) {
		t.Fatalf("builtin upgrade retained stale persisted identity: %#v", persisted)
	}
}

func TestBuiltinPDFViewerRecoversOnlyThePreviousDeclarationUpgradeQuarantine(t *testing.T) {
	ctx := context.Background()
	repository := NewMemoryPluginRepository()
	if err := repository.Save(ctx, &PluginRecord{
		ID: 1, Name: PDFViewerPluginName, Version: "1.1.0-dev", Runtime: "builtin", Status: string(StatusError),
		Checksum: strings.Repeat("0", 64), Config: "{}", ErrorMsg: builtinDeclarationUpgradeQuarantineReason,
	}); err != nil {
		t.Fatal(err)
	}
	manager := NewManager()
	manager.SetPluginRepository(repository)
	manager.RegisterRuntime("builtin", NewBuiltinRuntime())
	if _, err := manager.RegisterBuiltin(NewPDFViewerBuiltinManifest()); err != nil {
		t.Fatal(err)
	}
	persisted, err := repository.GetByName(ctx, PDFViewerPluginName)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Status != string(StatusRunning) || persisted.ErrorMsg != "" {
		t.Fatalf("the known stale declaration quarantine should be recovered for the bumped builtin version: %#v", persisted)
	}
}
