package plugin

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
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

func TestManagerPersistsLifecycleStatus(t *testing.T) {
	dir := writePluginManifest(t, `
name: persisted-lifecycle
version: "0.1.0"
runtime: wasm
`)

	manager := NewManager()
	repo := NewMemoryPluginRepository()
	manager.SetPluginRepository(repo)
	runtime := newFakeRuntime()
	manager.RegisterRuntime("wasm", runtime)

	if _, err := manager.Install(dir); err != nil {
		t.Fatalf("install plugin: %v", err)
	}
	if err := manager.Enable("persisted-lifecycle"); err != nil {
		t.Fatalf("enable plugin: %v", err)
	}
	record, err := repo.GetByName(context.Background(), "persisted-lifecycle")
	if err != nil {
		t.Fatalf("get plugin record: %v", err)
	}
	if record.Status != string(StatusRunning) {
		t.Fatalf("expected persisted running status, got %q", record.Status)
	}

	manager.StopAll()
	record, err = repo.GetByName(context.Background(), "persisted-lifecycle")
	if err != nil {
		t.Fatalf("get plugin record after stop all: %v", err)
	}
	if record.Status != string(StatusRunning) {
		t.Fatalf("stop all should not persist disabled status, got %q", record.Status)
	}

	if err := manager.Stop("persisted-lifecycle"); err != nil {
		t.Fatalf("stop plugin: %v", err)
	}
	record, err = repo.GetByName(context.Background(), "persisted-lifecycle")
	if err != nil {
		t.Fatalf("get plugin record after stop: %v", err)
	}
	if record.Status != string(StatusStopped) {
		t.Fatalf("expected persisted stopped status, got %q", record.Status)
	}
}

func TestManagerUpdateConfigUsesSchemaAndPersists(t *testing.T) {
	dir := writePluginManifest(t, `
name: configurable-plugin
version: "0.1.0"
runtime: builtin
config:
  title: Default
  enabled: true
  limit: 3
config_schema:
  fields:
    - key: title
      type: string
      default: Default
    - key: enabled
      type: boolean
      default: true
    - key: limit
      type: number
      default: 3
`)

	manager := NewManager()
	repo := NewMemoryPluginRepository()
	manager.SetPluginRepository(repo)
	manager.RegisterRuntime("builtin", newFakeRuntime())

	if _, err := manager.Install(dir); err != nil {
		t.Fatalf("install plugin: %v", err)
	}
	config, err := manager.UpdateConfig("configurable-plugin", map[string]interface{}{
		"title":   "Updated",
		"enabled": "false",
		"limit":   "5",
	})
	if err != nil {
		t.Fatalf("update config: %v", err)
	}
	if config["title"] != "Updated" || config["enabled"] != false || config["limit"] != float64(5) {
		t.Fatalf("unexpected normalized config: %#v", config)
	}
	record, err := repo.GetByName(context.Background(), "configurable-plugin")
	if err != nil {
		t.Fatalf("get plugin record: %v", err)
	}
	if record.Config == "" {
		t.Fatalf("expected persisted config")
	}
}

func TestManagerUpdateConfigRejectsUnsafeHomepageCustomHTML(t *testing.T) {
	dir := writePluginManifest(t, `
name: homepage-customizer
version: "0.1.0"
runtime: builtin
config:
  custom_html_enabled: false
  custom_html: ""
config_schema:
  fields:
    - key: custom_html_enabled
      type: boolean
      default: false
    - key: custom_html
      type: text
      default: ""
`)

	manager := NewManager()
	manager.RegisterRuntime("builtin", newFakeRuntime())
	installed, err := manager.Install(dir)
	if err != nil {
		t.Fatalf("install plugin: %v", err)
	}

	_, err = manager.UpdateConfig("homepage-customizer", map[string]interface{}{
		"custom_html_enabled": true,
		"custom_html":         `<img src=x onerror="alert(1)">`,
	})
	if err == nil {
		t.Fatalf("expected unsafe html config to fail")
	}
	if !strings.Contains(err.Error(), "custom_html") {
		t.Fatalf("expected custom_html error, got %v", err)
	}
	if installed.Manifest.Config["custom_html"] != "" {
		t.Fatalf("unsafe html should not be stored: %#v", installed.Manifest.Config)
	}
}

func TestManagerUpdateConfigAcceptsSafeHomepageCustomHTML(t *testing.T) {
	dir := writePluginManifest(t, `
name: homepage-customizer
version: "0.1.0"
runtime: builtin
config:
  custom_html_enabled: false
  custom_html: ""
config_schema:
  fields:
    - key: custom_html_enabled
      type: boolean
      default: false
    - key: custom_html
      type: text
      default: ""
`)

	manager := NewManager()
	manager.RegisterRuntime("builtin", newFakeRuntime())
	if _, err := manager.Install(dir); err != nil {
		t.Fatalf("install plugin: %v", err)
	}

	config, err := manager.UpdateConfig("homepage-customizer", map[string]interface{}{
		"custom_html_enabled": true,
		"custom_html":         `<section><h2>CampusOS</h2><a href="/threads">Threads</a></section>`,
	})
	if err != nil {
		t.Fatalf("update safe config: %v", err)
	}
	if config["custom_html"] == "" {
		t.Fatalf("expected custom_html to be stored")
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

func TestManagerListsPluginLogs(t *testing.T) {
	repo := NewMemoryPluginRepository()
	manager := NewManager()
	manager.SetPluginRepository(repo)

	if err := repo.SaveLog(context.Background(), &PluginLogRecord{
		PluginName: "listed-plugin",
		Level:      "info",
		Message:    "plugin handled event",
	}); err != nil {
		t.Fatalf("save log: %v", err)
	}

	logs, err := manager.ListPluginLogs(context.Background(), "listed-plugin", 10)
	if err != nil {
		t.Fatalf("list logs: %v", err)
	}
	if len(logs) != 1 || logs[0].Message != "plugin handled event" {
		t.Fatalf("unexpected logs: %#v", logs)
	}
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
