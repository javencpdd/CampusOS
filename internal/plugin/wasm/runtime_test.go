package wasm

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/campusos/CampusOS/internal/plugin"
)

func TestRuntimeLifecycle(t *testing.T) {
	dir := t.TempDir()
	modulePath := filepath.Join(dir, "plugin.wasm")
	if err := os.WriteFile(modulePath, []byte("placeholder"), 0o644); err != nil {
		t.Fatalf("write wasm module: %v", err)
	}

	runtime := NewRuntime()
	p := &plugin.Plugin{
		Directory: dir,
		Manifest: &plugin.Manifest{
			Name:    "hello-wasm",
			Runtime: "wasm",
		},
	}

	if err := runtime.Start(context.Background(), p); err != nil {
		t.Fatalf("start runtime: %v", err)
	}
	if !runtime.IsRunning("hello-wasm") {
		t.Fatalf("expected plugin to be running")
	}
	if err := runtime.HealthCheck(context.Background(), "hello-wasm"); err != nil {
		t.Fatalf("health check: %v", err)
	}

	if _, err := runtime.SendEvent(context.Background(), "hello-wasm", &plugin.EventMessage{Type: "thread.created"}); !errors.Is(err, ErrEventDispatchNotImplemented) {
		t.Fatalf("expected event dispatch placeholder error, got %v", err)
	}

	if err := runtime.Stop(context.Background(), "hello-wasm"); err != nil {
		t.Fatalf("stop runtime: %v", err)
	}
	if runtime.IsRunning("hello-wasm") {
		t.Fatalf("expected plugin to be stopped")
	}
}

func TestRuntimeUsesConfiguredModulePath(t *testing.T) {
	dir := t.TempDir()
	modulePath := filepath.Join(dir, "dist", "plugin.wasm")
	if err := os.MkdirAll(filepath.Dir(modulePath), 0o755); err != nil {
		t.Fatalf("create module dir: %v", err)
	}
	if err := os.WriteFile(modulePath, []byte("placeholder"), 0o644); err != nil {
		t.Fatalf("write wasm module: %v", err)
	}

	runtime := NewRuntime()
	p := &plugin.Plugin{
		Directory: dir,
		Manifest: &plugin.Manifest{
			Name:    "configured-wasm",
			Runtime: "wasm",
			Config: map[string]interface{}{
				"module": "dist/plugin.wasm",
			},
		},
	}

	if err := runtime.Start(context.Background(), p); err != nil {
		t.Fatalf("start runtime with configured module: %v", err)
	}
}

func TestRuntimeReturnsModuleNotFound(t *testing.T) {
	runtime := NewRuntime()
	p := &plugin.Plugin{
		Directory: t.TempDir(),
		Manifest: &plugin.Manifest{
			Name:    "missing-module",
			Runtime: "wasm",
		},
	}

	if err := runtime.Start(context.Background(), p); !errors.Is(err, ErrModuleNotFound) {
		t.Fatalf("expected module not found error, got %v", err)
	}
}
