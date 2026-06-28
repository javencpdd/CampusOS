package plugin

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

type fakeRuntime struct {
	started      []string
	stopped      []string
	healthChecks []string
	running      map[string]bool
	sendErr      error
}

func newFakeRuntime() *fakeRuntime {
	return &fakeRuntime{running: make(map[string]bool)}
}

func (r *fakeRuntime) Start(_ context.Context, p *Plugin) error {
	r.started = append(r.started, p.Manifest.Name)
	r.running[p.Manifest.Name] = true
	return nil
}

func (r *fakeRuntime) Stop(_ context.Context, pluginName string) error {
	r.stopped = append(r.stopped, pluginName)
	r.running[pluginName] = false
	return nil
}

func (r *fakeRuntime) SendEvent(_ context.Context, _ string, _ *EventMessage) (*PluginResponse, error) {
	if r.sendErr != nil {
		return nil, r.sendErr
	}
	return &PluginResponse{Allowed: true}, nil
}

func (r *fakeRuntime) HealthCheck(_ context.Context, pluginName string) error {
	r.healthChecks = append(r.healthChecks, pluginName)
	return nil
}

func (r *fakeRuntime) IsRunning(pluginName string) bool {
	return r.running[pluginName]
}

func (r *fakeRuntime) Type() string {
	return "wasm"
}

func TestManifestAllowsWasmRuntime(t *testing.T) {
	manifest, err := ParseManifest([]byte(`
name: hello-wasm
version: "0.1.0"
runtime: wasm
events:
  subscribe:
    - thread.created
`))
	if err != nil {
		t.Fatalf("parse manifest: %v", err)
	}
	if manifest.Runtime != "wasm" {
		t.Fatalf("expected wasm runtime, got %q", manifest.Runtime)
	}
}

func TestManagerLifecycleUsesRegisteredRuntime(t *testing.T) {
	dir := writePluginManifest(t, `
name: runtime-contract
version: "0.1.0"
runtime: wasm
events:
  subscribe:
    - thread.created
`)

	manager := NewManager()
	runtime := newFakeRuntime()
	manager.RegisterRuntime("wasm", runtime)

	installed, err := manager.Install(dir)
	if err != nil {
		t.Fatalf("install plugin: %v", err)
	}
	if installed.Status != StatusInstalled {
		t.Fatalf("expected installed status, got %q", installed.Status)
	}

	if err := manager.Enable("runtime-contract"); err != nil {
		t.Fatalf("enable plugin: %v", err)
	}
	if len(runtime.started) != 1 || runtime.started[0] != "runtime-contract" {
		t.Fatalf("expected runtime start call for plugin, got %#v", runtime.started)
	}
	if installed.Status != StatusRunning {
		t.Fatalf("expected running status, got %q", installed.Status)
	}
	if !runtime.IsRunning("runtime-contract") {
		t.Fatalf("expected fake runtime to mark plugin running")
	}

	if err := manager.Stop("runtime-contract"); err != nil {
		t.Fatalf("stop plugin: %v", err)
	}
	if len(runtime.stopped) != 1 || runtime.stopped[0] != "runtime-contract" {
		t.Fatalf("expected runtime stop call for plugin, got %#v", runtime.stopped)
	}
	if installed.Status != StatusStopped {
		t.Fatalf("expected stopped status, got %q", installed.Status)
	}
}

func TestManagerMarksErrorWhenRuntimeMissing(t *testing.T) {
	dir := writePluginManifest(t, `
name: missing-runtime
version: "0.1.0"
runtime: wasm
`)

	manager := NewManager()
	installed, err := manager.Install(dir)
	if err != nil {
		t.Fatalf("install plugin: %v", err)
	}

	if err := manager.Start("missing-runtime"); err == nil {
		t.Fatalf("expected missing runtime error")
	}
	if installed.Status != StatusError {
		t.Fatalf("expected error status, got %q", installed.Status)
	}
	if installed.ErrorMsg == "" {
		t.Fatalf("expected error message")
	}
}

func TestManagerMarksErrorWhenEventDispatchFails(t *testing.T) {
	dir := writePluginManifest(t, `
name: event-error
version: "0.1.0"
runtime: wasm
events:
  subscribe:
    - thread.created
`)

	manager := NewManager()
	runtime := newFakeRuntime()
	manager.RegisterRuntime("wasm", runtime)

	installed, err := manager.Install(dir)
	if err != nil {
		t.Fatalf("install plugin: %v", err)
	}
	if err := manager.Enable("event-error"); err != nil {
		t.Fatalf("enable plugin: %v", err)
	}

	runtime.sendErr = errors.New("event dispatch failed")
	response := manager.DispatchBeforeEvent(context.Background(), &EventMessage{Type: "thread.created"})
	if response != nil {
		t.Fatalf("expected no blocking response, got %#v", response)
	}
	if installed.Status != StatusError {
		t.Fatalf("expected error status, got %q", installed.Status)
	}
	if installed.ErrorMsg != "event dispatch failed" {
		t.Fatalf("expected event error message, got %q", installed.ErrorMsg)
	}
}

func TestManagerWritesPluginLogs(t *testing.T) {
	dir := writePluginManifest(t, `
name: logged-plugin
version: "0.1.0"
runtime: wasm
events:
  subscribe:
    - thread.created
`)

	manager := NewManager()
	repo := NewMemoryPluginRepository()
	manager.SetPluginRepository(repo)
	runtime := newFakeRuntime()
	manager.RegisterRuntime("wasm", runtime)

	if _, err := manager.Install(dir); err != nil {
		t.Fatalf("install plugin: %v", err)
	}
	if err := manager.Enable("logged-plugin"); err != nil {
		t.Fatalf("enable plugin: %v", err)
	}
	if response := manager.DispatchBeforeEvent(context.Background(), &EventMessage{
		Type:    "thread.created",
		Source:  "test",
		Subject: "thread:1",
	}); response != nil {
		t.Fatalf("expected no blocking response, got %#v", response)
	}
	if err := manager.Stop("logged-plugin"); err != nil {
		t.Fatalf("stop plugin: %v", err)
	}

	logs, err := repo.ListLogs(context.Background(), "logged-plugin", 10)
	if err != nil {
		t.Fatalf("list logs: %v", err)
	}

	assertLogMessage(t, logs, "plugin started")
	assertLogMessage(t, logs, "plugin handled before-event")
	assertLogMessage(t, logs, "plugin stopped")
}

func TestManagerWritesEventErrorLog(t *testing.T) {
	dir := writePluginManifest(t, `
name: event-error-log
version: "0.1.0"
runtime: wasm
events:
  subscribe:
    - thread.created
`)

	manager := NewManager()
	repo := NewMemoryPluginRepository()
	manager.SetPluginRepository(repo)
	runtime := newFakeRuntime()
	manager.RegisterRuntime("wasm", runtime)

	if _, err := manager.Install(dir); err != nil {
		t.Fatalf("install plugin: %v", err)
	}
	if err := manager.Enable("event-error-log"); err != nil {
		t.Fatalf("enable plugin: %v", err)
	}

	runtime.sendErr = errors.New("event dispatch failed")
	manager.DispatchBeforeEvent(context.Background(), &EventMessage{Type: "thread.created"})

	logs, err := repo.ListLogs(context.Background(), "event-error-log", 10)
	if err != nil {
		t.Fatalf("list logs: %v", err)
	}
	assertLogMessage(t, logs, "plugin before-event failed")
}

func assertLogMessage(t *testing.T, logs []*PluginLogRecord, message string) {
	t.Helper()

	for _, record := range logs {
		if record.Message == message {
			return
		}
	}
	t.Fatalf("expected log message %q, got %#v", message, logs)
}

func writePluginManifest(t *testing.T, content string) string {
	t.Helper()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "plugin.yaml"), []byte(content), 0o644); err != nil {
		t.Fatalf("write plugin manifest: %v", err)
	}
	return dir
}
