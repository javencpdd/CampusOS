package wasm

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/campusos/CampusOS/internal/plugin"
)

func TestHelloWasmExampleLifecycleAndLogging(t *testing.T) {
	pluginDir := filepath.Clean(filepath.Join("..", "..", "..", "examples", "plugins", "hello-wasm"))

	manager := plugin.NewManager()
	logRepo := plugin.NewMemoryPluginRepository()
	manager.SetPluginRepository(logRepo)
	manager.RegisterRuntime("wasm", NewRuntime())

	installed, err := manager.Install(pluginDir)
	if err != nil {
		t.Fatalf("install hello-wasm: %v", err)
	}
	if installed.Manifest.Runtime != "wasm" {
		t.Fatalf("expected wasm runtime, got %q", installed.Manifest.Runtime)
	}

	if err := manager.Enable("hello-wasm"); err != nil {
		t.Fatalf("enable hello-wasm: %v", err)
	}
	if installed.Status != plugin.StatusRunning {
		t.Fatalf("expected running status, got %q", installed.Status)
	}

	response := manager.DispatchBeforeEvent(context.Background(), &plugin.EventMessage{
		Type:    "thread.created",
		Source:  "test",
		Subject: "thread:1",
	})
	if response != nil {
		t.Fatalf("expected hello-wasm to allow event, got %#v", response)
	}

	if err := manager.Stop("hello-wasm"); err != nil {
		t.Fatalf("stop hello-wasm: %v", err)
	}
	if installed.Status != plugin.StatusStopped {
		t.Fatalf("expected stopped status, got %q", installed.Status)
	}

	logs, err := logRepo.ListLogs(context.Background(), "hello-wasm", 10)
	if err != nil {
		t.Fatalf("list hello-wasm logs: %v", err)
	}
	assertExampleLog(t, logs, "plugin started")
	assertExampleLog(t, logs, "plugin handled before-event")
	assertExampleLog(t, logs, "plugin stopped")
}

func assertExampleLog(t *testing.T, logs []*plugin.PluginLogRecord, message string) {
	t.Helper()

	for _, record := range logs {
		if record.Message == message {
			return
		}
	}
	t.Fatalf("expected log message %q, got %#v", message, logs)
}
