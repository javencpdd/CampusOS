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

func TestHealthCheckDoesNotChurnUIRevisionWhenHealthIsStable(t *testing.T) {
	dir := writePluginManifest(t, `name: stable-health
version: 0.1.0
runtime: wasm
`)
	manager := NewManager()
	runtime := newFakeRuntime()
	manager.RegisterRuntime("wasm", runtime)
	if _, err := manager.Install(dir); err != nil {
		t.Fatal(err)
	}
	if err := manager.RequestEnable("stable-health"); err != nil {
		t.Fatal(err)
	}
	revision := manager.UIRevision()
	if err := manager.HealthCheck("stable-health"); err != nil {
		t.Fatal(err)
	}
	if manager.UIRevision() != revision {
		t.Fatalf("stable health check changed revision: %d -> %d", revision, manager.UIRevision())
	}
}

func TestHotDisableIsIdempotent(t *testing.T) {
	dir := writePluginManifest(t, `name: idempotent-disable
version: 0.1.0
runtime: wasm
`)
	manager := NewManager()
	runtime := newFakeRuntime()
	manager.RegisterRuntime("wasm", runtime)
	if _, err := manager.Install(dir); err != nil {
		t.Fatal(err)
	}
	if err := manager.RequestEnable("idempotent-disable"); err != nil {
		t.Fatal(err)
	}
	if err := manager.RequestDisable("idempotent-disable"); err != nil {
		t.Fatal(err)
	}
	if err := manager.RequestDisable("idempotent-disable"); err != nil {
		t.Fatal(err)
	}
	if len(runtime.stopped) != 1 {
		t.Fatalf("disable called runtime stop %d times", len(runtime.stopped))
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

func TestSystemPluginLifecycleStagesChangesUntilRestart(t *testing.T) {
	dir := writePluginManifest(t, `
name: system-contract
version: "0.1.0"
runtime: wasm
scope: system
lifecycle:
  backend:
    activation_mode: restart
`)

	manager := NewManager()
	repo := NewMemoryPluginRepository()
	manager.SetPluginRepository(repo)
	runtime := newFakeRuntime()
	manager.RegisterRuntime("wasm", runtime)
	if _, err := manager.Install(dir); err != nil {
		t.Fatalf("install system plugin: %v", err)
	}
	manager.StartDesiredPlugins(ScopeSystem)
	if len(runtime.started) != 1 {
		t.Fatalf("expected startup to start system plugin, got %#v", runtime.started)
	}

	if err := manager.RequestDisable("system-contract"); err != nil {
		t.Fatalf("stage system plugin disable: %v", err)
	}
	if len(runtime.stopped) != 0 {
		t.Fatalf("system plugin should keep running until restart, got stops %#v", runtime.stopped)
	}
	state, ok := manager.LifecycleState("system-contract")
	if !ok || state.DesiredEnabled || !state.PendingRestart || state.ActivationMode != "restart" {
		t.Fatalf("unexpected staged disable state: %#v", state)
	}
	record, err := repo.GetByName(context.Background(), "system-contract")
	if err != nil {
		t.Fatalf("get staged system record: %v", err)
	}
	if record.Status != string(StatusStopped) {
		t.Fatalf("expected disabled target state to persist, got %q", record.Status)
	}

	nextManager := NewManager()
	nextRuntime := newFakeRuntime()
	nextManager.SetPluginRepository(repo)
	nextManager.RegisterRuntime("wasm", nextRuntime)
	if _, err := nextManager.Install(dir); err != nil {
		t.Fatalf("install after staged disable: %v", err)
	}
	nextManager.StartDesiredPlugins(ScopeSystem)
	if len(nextRuntime.started) != 0 {
		t.Fatalf("disabled system plugin should stay stopped after restart, got %#v", nextRuntime.started)
	}
	if err := nextManager.RequestEnable("system-contract"); err != nil {
		t.Fatalf("stage system plugin enable: %v", err)
	}
	state, ok = nextManager.LifecycleState("system-contract")
	if !ok || !state.DesiredEnabled || !state.PendingRestart {
		t.Fatalf("unexpected staged enable state: %#v", state)
	}

	finalManager := NewManager()
	finalRuntime := newFakeRuntime()
	finalManager.SetPluginRepository(repo)
	finalManager.RegisterRuntime("wasm", finalRuntime)
	if _, err := finalManager.Install(dir); err != nil {
		t.Fatalf("install after staged enable: %v", err)
	}
	finalManager.StartDesiredPlugins(ScopeSystem)
	if len(finalRuntime.started) != 1 {
		t.Fatalf("enabled system plugin should start after restart, got %#v", finalRuntime.started)
	}
}

func TestUserPluginReloadsWithoutRestart(t *testing.T) {
	dir := writePluginManifest(t, `
name: user-contract
version: "0.1.0"
runtime: wasm
scope: user
`)

	manager := NewManager()
	runtime := newFakeRuntime()
	manager.RegisterRuntime("wasm", runtime)
	if _, err := manager.Install(dir); err != nil {
		t.Fatalf("install user plugin: %v", err)
	}
	if err := manager.ReloadUserPlugin("user-contract"); err != nil {
		t.Fatalf("load user plugin: %v", err)
	}
	if len(runtime.started) != 1 || len(runtime.stopped) != 0 {
		t.Fatalf("unexpected first user load: starts=%#v stops=%#v", runtime.started, runtime.stopped)
	}
	if err := manager.ReloadUserPlugin("user-contract"); err != nil {
		t.Fatalf("reload user plugin: %v", err)
	}
	if len(runtime.started) != 2 || len(runtime.stopped) != 1 {
		t.Fatalf("expected runtime restart for user reload: starts=%#v stops=%#v", runtime.started, runtime.stopped)
	}
	state, ok := manager.LifecycleState("user-contract")
	if !ok || !state.DesiredEnabled || state.PendingRestart || state.ActivationMode != "hot" {
		t.Fatalf("unexpected user lifecycle state: %#v", state)
	}
}

func TestManagerUpdateConfigUsesSchemaAndPersists(t *testing.T) {
	dir := writePluginManifest(t, `
name: configurable-plugin
version: "0.1.0"
runtime: wasm
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
	manager.RegisterRuntime("wasm", newFakeRuntime())

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
	snapshot, ok := manager.GetPluginConfig("configurable-plugin")
	if !ok || snapshot["title"] != "Updated" || snapshot["enabled"] != false {
		t.Fatalf("unexpected config snapshot: %#v", snapshot)
	}
	snapshot["title"] = "mutated outside manager"
	current, ok := manager.GetPluginConfig("configurable-plugin")
	if !ok || current["title"] != "Updated" {
		t.Fatalf("config snapshot must not expose manager state: %#v", current)
	}
	record, err := repo.GetByName(context.Background(), "configurable-plugin")
	if err != nil {
		t.Fatalf("get plugin record: %v", err)
	}
	if record.Config == "" {
		t.Fatalf("expected persisted config")
	}
}

func TestPluginManagerRejectsCompiledAppearanceModuleAlias(t *testing.T) {
	dir := writePluginManifest(t, `
name: homepage-customizer
version: "0.1.0"
runtime: wasm
`)

	manager := NewManager()
	if _, err := manager.Install(dir); err == nil || !strings.Contains(err.Error(), "reserved") {
		t.Fatalf("expected compiled module alias rejection, got %v", err)
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
