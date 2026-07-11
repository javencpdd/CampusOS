package campusos

import (
	"context"
	"errors"
	"net/http"
	"testing"
)

func TestHarnessSupportsConfigStorageEventsAndLogs(t *testing.T) {
	harness := NewHarness("sdk-plugin")
	defer harness.Close()
	harness.Config["entrypoint"] = "handle_event"

	client := harness.Client()
	value, found, err := client.GetConfig(t.Context(), "entrypoint")
	if err != nil {
		t.Fatalf("get config: %v", err)
	}
	if !found || value != "handle_event" {
		t.Fatalf("unexpected config: found=%v value=%#v", found, value)
	}

	if err := client.SetConfig(t.Context(), "threshold", 3); err != nil {
		t.Fatalf("set config: %v", err)
	}
	if harness.Config["threshold"] != float64(3) {
		t.Fatalf("expected harness config update, got %#v", harness.Config)
	}

	if err := client.StorageSet(t.Context(), "hello", "world"); err != nil {
		t.Fatalf("storage set: %v", err)
	}
	stored, found, err := client.StorageGet(t.Context(), "hello")
	if err != nil {
		t.Fatalf("storage get: %v", err)
	}
	if !found || stored != "world" {
		t.Fatalf("unexpected storage: found=%v value=%q", found, stored)
	}

	if err := client.PublishEvent(t.Context(), PublishEventRequest{
		EventType: "plugin.tested",
		Source:    "sdk-test",
		Subject:   "thread:1",
		Data:      map[string]interface{}{"ok": true},
	}); err != nil {
		t.Fatalf("publish event: %v", err)
	}
	if len(harness.Events) != 1 || harness.Events[0].EventType != "plugin.tested" {
		t.Fatalf("unexpected events: %#v", harness.Events)
	}

	if err := client.Log(t.Context(), LogRequest{Level: "info", Message: "handled"}); err != nil {
		t.Fatalf("log: %v", err)
	}
	if len(harness.Logs) != 1 || harness.Logs[0].Message != "handled" {
		t.Fatalf("unexpected logs: %#v", harness.Logs)
	}
}

func TestHarnessSupportsFailureInjectionAndTypedPermissionError(t *testing.T) {
	harness := NewHarness("sdk-plugin")
	harness.SetFailure("GetConfig", http.StatusForbidden, "permission denied")
	_, _, err := harness.Client().GetConfig(t.Context(), "secret")
	if !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("expected typed permission error, got %v", err)
	}
	harness.ClearFailure("GetConfig")
	harness.Config["secret"] = "visible"
	if _, found, err := harness.Client().GetConfig(t.Context(), "secret"); err != nil || !found {
		t.Fatalf("expected cleared failure, found=%v err=%v", found, err)
	}
}

func TestHarnessSimulatesIncomingEvent(t *testing.T) {
	harness := NewHarness("sdk-plugin")
	called := false
	err := harness.SimulateEvent(t.Context(), Event{Type: "thread.created"}, func(_ context.Context, event Event) error {
		called = event.Type == "thread.created"
		return nil
	})
	if err != nil || !called {
		t.Fatalf("expected event handler call, called=%v err=%v", called, err)
	}
}

func TestHarnessSupportsPermissionChecks(t *testing.T) {
	harness := NewHarness("sdk-plugin")
	defer harness.Close()
	harness.SetPermission("user-1", "thread", "read", true)

	allowed, err := harness.Client().CheckPermission(t.Context(), "user-1", "thread", "read")
	if err != nil {
		t.Fatalf("check permission: %v", err)
	}
	if !allowed {
		t.Fatalf("expected permission to be allowed")
	}
}
