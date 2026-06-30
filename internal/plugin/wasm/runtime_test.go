package wasm

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/campusos/CampusOS/internal/plugin"
)

func TestRuntimeLifecycle(t *testing.T) {
	dir := t.TempDir()
	modulePath := filepath.Join(dir, "plugin.wasm")
	if err := os.WriteFile(modulePath, wasmHandleEventReturning(1), 0o644); err != nil {
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

	response, err := runtime.SendEvent(context.Background(), "hello-wasm", &plugin.EventMessage{Type: "thread.created"})
	if err != nil {
		t.Fatalf("send event: %v", err)
	}
	if response == nil || !response.Allowed {
		t.Fatalf("expected allowed response, got %#v", response)
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
	if err := os.WriteFile(modulePath, wasmHandleEventReturning(1), 0o644); err != nil {
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

func TestRuntimeReturnsMissingHandler(t *testing.T) {
	dir := t.TempDir()
	modulePath := filepath.Join(dir, "plugin.wasm")
	if err := os.WriteFile(modulePath, wasmHandleEventReturning(1), 0o644); err != nil {
		t.Fatalf("write wasm module: %v", err)
	}

	runtime := NewRuntime()
	p := &plugin.Plugin{
		Directory: dir,
		Manifest: &plugin.Manifest{
			Name:    "missing-handler",
			Runtime: "wasm",
			Config: map[string]interface{}{
				"entrypoint": "missing_event_handler",
			},
		},
	}

	if err := runtime.Start(context.Background(), p); !errors.Is(err, ErrEventHandlerMissing) {
		t.Fatalf("expected missing event handler error, got %v", err)
	}
	if runtime.IsRunning("missing-handler") {
		t.Fatalf("expected plugin not to be running")
	}
}

func TestRuntimeRejectsEventWhenHandlerReturnsZero(t *testing.T) {
	dir := t.TempDir()
	modulePath := filepath.Join(dir, "plugin.wasm")
	if err := os.WriteFile(modulePath, wasmHandleEventReturning(0), 0o644); err != nil {
		t.Fatalf("write wasm module: %v", err)
	}

	runtime := NewRuntime()
	p := &plugin.Plugin{
		Directory: dir,
		Manifest: &plugin.Manifest{
			Name:    "rejecting-wasm",
			Runtime: "wasm",
		},
	}

	if err := runtime.Start(context.Background(), p); err != nil {
		t.Fatalf("start runtime: %v", err)
	}

	response, err := runtime.SendEvent(context.Background(), "rejecting-wasm", &plugin.EventMessage{Type: "thread.created"})
	if err != nil {
		t.Fatalf("send event: %v", err)
	}
	if response == nil || response.Allowed || response.Message == "" {
		t.Fatalf("expected rejected response with message, got %#v", response)
	}
}

func TestRuntimePassesEventPayloadToHandler(t *testing.T) {
	dir := t.TempDir()
	modulePath := filepath.Join(dir, "plugin.wasm")
	if err := os.WriteFile(modulePath, wasmHandleEventPayloadLenNonZero(), 0o644); err != nil {
		t.Fatalf("write wasm module: %v", err)
	}

	runtime := NewRuntime()
	p := &plugin.Plugin{
		Directory: dir,
		Manifest: &plugin.Manifest{
			Name:    "payload-wasm",
			Runtime: "wasm",
		},
	}

	if err := runtime.Start(context.Background(), p); err != nil {
		t.Fatalf("start runtime: %v", err)
	}

	response, err := runtime.SendEvent(context.Background(), "payload-wasm", &plugin.EventMessage{
		Type:    "thread.created",
		Source:  "test",
		Subject: "thread:1",
		Data: map[string]interface{}{
			"title": "Hello",
		},
	})
	if err != nil {
		t.Fatalf("send event: %v", err)
	}
	if response == nil || !response.Allowed {
		t.Fatalf("expected payload handler to allow event, got %#v", response)
	}
}

func TestRuntimeUsesHeapBaseForEventPayload(t *testing.T) {
	dir := t.TempDir()
	modulePath := filepath.Join(dir, "plugin.wasm")
	if err := os.WriteFile(modulePath, wasmHandleEventPayloadAtHeapBase(), 0o644); err != nil {
		t.Fatalf("write wasm module: %v", err)
	}

	runtime := NewRuntime()
	p := &plugin.Plugin{
		Directory: dir,
		Manifest: &plugin.Manifest{
			Name:    "payload-heap-base-wasm",
			Runtime: "wasm",
		},
	}

	if err := runtime.Start(context.Background(), p); err != nil {
		t.Fatalf("start runtime: %v", err)
	}

	response, err := runtime.SendEvent(context.Background(), "payload-heap-base-wasm", &plugin.EventMessage{
		Type:   "thread.created",
		Source: "test",
	})
	if err != nil {
		t.Fatalf("send event: %v", err)
	}
	if response == nil || !response.Allowed {
		t.Fatalf("expected heap-base payload handler to allow event, got %#v", response)
	}
}

func TestRuntimeRejectsPayloadHandlerWithoutExportedMemory(t *testing.T) {
	dir := t.TempDir()
	modulePath := filepath.Join(dir, "plugin.wasm")
	if err := os.WriteFile(modulePath, wasmHandleEventPayloadWithoutMemory(), 0o644); err != nil {
		t.Fatalf("write wasm module: %v", err)
	}

	runtime := NewRuntime()
	p := &plugin.Plugin{
		Directory: dir,
		Manifest: &plugin.Manifest{
			Name:    "payload-without-memory",
			Runtime: "wasm",
		},
	}

	if err := runtime.Start(context.Background(), p); !errors.Is(err, ErrEventPayloadABI) {
		t.Fatalf("expected payload ABI error, got %v", err)
	}
}

func TestRuntimeRejectsUnsupportedEventHandlerSignature(t *testing.T) {
	dir := t.TempDir()
	modulePath := filepath.Join(dir, "plugin.wasm")
	if err := os.WriteFile(modulePath, wasmHandleEventWithUnsupportedSignature(), 0o644); err != nil {
		t.Fatalf("write wasm module: %v", err)
	}

	runtime := NewRuntime()
	p := &plugin.Plugin{
		Directory: dir,
		Manifest: &plugin.Manifest{
			Name:    "unsupported-signature-wasm",
			Runtime: "wasm",
		},
	}

	if err := runtime.Start(context.Background(), p); !errors.Is(err, ErrEventPayloadABI) {
		t.Fatalf("expected payload ABI error, got %v", err)
	}
}

func TestRuntimeTimesOutLongRunningEvent(t *testing.T) {
	dir := t.TempDir()
	modulePath := filepath.Join(dir, "plugin.wasm")
	if err := os.WriteFile(modulePath, wasmHandleEventLooping(), 0o644); err != nil {
		t.Fatalf("write wasm module: %v", err)
	}

	runtime := NewRuntime()
	p := &plugin.Plugin{
		Directory: dir,
		Manifest: &plugin.Manifest{
			Name:    "looping-wasm",
			Runtime: "wasm",
			Config: map[string]interface{}{
				"event_timeout_ms": 10,
			},
		},
	}

	if err := runtime.Start(context.Background(), p); err != nil {
		t.Fatalf("start runtime: %v", err)
	}

	startedAt := time.Now()
	response, err := runtime.SendEvent(context.Background(), "looping-wasm", &plugin.EventMessage{Type: "thread.created"})
	elapsed := time.Since(startedAt)

	if response != nil {
		t.Fatalf("expected nil response, got %#v", response)
	}
	if !errors.Is(err, ErrEventCallTimeout) {
		t.Fatalf("expected timeout error, got %v", err)
	}
	if elapsed > time.Second {
		t.Fatalf("expected timeout to stop quickly, took %s", elapsed)
	}
	if runtime.IsRunning("looping-wasm") {
		t.Fatalf("expected timed out module to be closed")
	}
}

func TestRuntimeIsolatesTrappedEvent(t *testing.T) {
	dir := t.TempDir()
	modulePath := filepath.Join(dir, "plugin.wasm")
	if err := os.WriteFile(modulePath, wasmHandleEventTrapping(), 0o644); err != nil {
		t.Fatalf("write wasm module: %v", err)
	}

	runtime := NewRuntime()
	p := &plugin.Plugin{
		Directory: dir,
		Manifest: &plugin.Manifest{
			Name:    "trapping-wasm",
			Runtime: "wasm",
		},
	}

	if err := runtime.Start(context.Background(), p); err != nil {
		t.Fatalf("start runtime: %v", err)
	}

	response, err := runtime.SendEvent(context.Background(), "trapping-wasm", &plugin.EventMessage{Type: "thread.created"})
	if response != nil {
		t.Fatalf("expected nil response, got %#v", response)
	}
	if !errors.Is(err, ErrEventCallFailed) {
		t.Fatalf("expected event call failed error, got %v", err)
	}
	if runtime.IsRunning("trapping-wasm") {
		t.Fatalf("expected trapped module to be isolated")
	}
	if err := runtime.HealthCheck(context.Background(), "trapping-wasm"); !errors.Is(err, ErrPluginNotRunning) {
		t.Fatalf("expected plugin not running after trap, got %v", err)
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

func wasmHandleEventTrapping() []byte {
	return []byte{
		0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00,
		0x01, 0x05, 0x01, 0x60, 0x00, 0x01, 0x7f,
		0x03, 0x02, 0x01, 0x00,
		0x07, 0x10, 0x01, 0x0c,
		0x68, 0x61, 0x6e, 0x64, 0x6c, 0x65, 0x5f,
		0x65, 0x76, 0x65, 0x6e, 0x74,
		0x00, 0x00,
		0x0a, 0x05, 0x01, 0x03, 0x00, 0x00, 0x0b,
	}
}

func wasmHandleEventReturning(value byte) []byte {
	return []byte{
		0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00,
		0x01, 0x05, 0x01, 0x60, 0x00, 0x01, 0x7f,
		0x03, 0x02, 0x01, 0x00,
		0x07, 0x10, 0x01, 0x0c,
		0x68, 0x61, 0x6e, 0x64, 0x6c, 0x65, 0x5f,
		0x65, 0x76, 0x65, 0x6e, 0x74,
		0x00, 0x00,
		0x0a, 0x06, 0x01, 0x04, 0x00, 0x41, value, 0x0b,
	}
}

func wasmHandleEventPayloadLenNonZero() []byte {
	return []byte{
		0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00,
		0x01, 0x07, 0x01, 0x60, 0x02, 0x7f, 0x7f, 0x01, 0x7f,
		0x03, 0x02, 0x01, 0x00,
		0x05, 0x03, 0x01, 0x00, 0x01,
		0x07, 0x19, 0x02,
		0x06, 0x6d, 0x65, 0x6d, 0x6f, 0x72, 0x79, 0x02, 0x00,
		0x0c, 0x68, 0x61, 0x6e, 0x64, 0x6c, 0x65, 0x5f,
		0x65, 0x76, 0x65, 0x6e, 0x74, 0x00, 0x00,
		0x0a, 0x13, 0x01, 0x11, 0x00, 0x20, 0x01, 0x41,
		0x00, 0x4b, 0x20, 0x00, 0x2d, 0x00, 0x00, 0x41,
		0xfb, 0x00, 0x46, 0x71, 0x0b,
	}
}

func wasmHandleEventPayloadAtHeapBase() []byte {
	return []byte{
		0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00,
		0x01, 0x07, 0x01, 0x60, 0x02, 0x7f, 0x7f, 0x01, 0x7f,
		0x03, 0x02, 0x01, 0x00,
		0x05, 0x03, 0x01, 0x00, 0x01,
		0x06, 0x06, 0x01, 0x7f, 0x00, 0x41, 0x10, 0x0b,
		0x07, 0x27, 0x03,
		0x06, 0x6d, 0x65, 0x6d, 0x6f, 0x72, 0x79, 0x02, 0x00,
		0x0c, 0x68, 0x61, 0x6e, 0x64, 0x6c, 0x65, 0x5f,
		0x65, 0x76, 0x65, 0x6e, 0x74, 0x00, 0x00,
		0x0b, 0x5f, 0x5f, 0x68, 0x65, 0x61, 0x70, 0x5f,
		0x62, 0x61, 0x73, 0x65, 0x03, 0x00,
		0x0a, 0x19, 0x01, 0x17, 0x00, 0x20, 0x00, 0x41,
		0x10, 0x46, 0x20, 0x01, 0x41, 0x00, 0x4b, 0x71,
		0x20, 0x00, 0x2d, 0x00, 0x00, 0x41, 0xfb, 0x00,
		0x46, 0x71, 0x0b,
	}
}

func wasmHandleEventPayloadWithoutMemory() []byte {
	return []byte{
		0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00,
		0x01, 0x07, 0x01, 0x60, 0x02, 0x7f, 0x7f, 0x01, 0x7f,
		0x03, 0x02, 0x01, 0x00,
		0x07, 0x10, 0x01, 0x0c,
		0x68, 0x61, 0x6e, 0x64, 0x6c, 0x65, 0x5f,
		0x65, 0x76, 0x65, 0x6e, 0x74,
		0x00, 0x00,
		0x0a, 0x06, 0x01, 0x04, 0x00, 0x41, 0x01, 0x0b,
	}
}

func wasmHandleEventWithUnsupportedSignature() []byte {
	return []byte{
		0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00,
		0x01, 0x06, 0x01, 0x60, 0x01, 0x7f, 0x01, 0x7f,
		0x03, 0x02, 0x01, 0x00,
		0x07, 0x10, 0x01, 0x0c,
		0x68, 0x61, 0x6e, 0x64, 0x6c, 0x65, 0x5f,
		0x65, 0x76, 0x65, 0x6e, 0x74,
		0x00, 0x00,
		0x0a, 0x06, 0x01, 0x04, 0x00, 0x41, 0x01, 0x0b,
	}
}

func wasmHandleEventLooping() []byte {
	return []byte{
		0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00,
		0x01, 0x05, 0x01, 0x60, 0x00, 0x01, 0x7f,
		0x03, 0x02, 0x01, 0x00,
		0x07, 0x10, 0x01, 0x0c,
		0x68, 0x61, 0x6e, 0x64, 0x6c, 0x65, 0x5f,
		0x65, 0x76, 0x65, 0x6e, 0x74,
		0x00, 0x00,
		0x0a, 0x0b, 0x01, 0x09, 0x00, 0x03, 0x40, 0x0c,
		0x00, 0x0b, 0x41, 0x01, 0x0b,
	}
}
