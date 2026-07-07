package builtin

import (
	"context"
	"testing"

	"github.com/campusos/CampusOS/internal/plugin"
)

func TestBuiltinRuntimeLifecycle(t *testing.T) {
	runtime := NewRuntime()
	p := &plugin.Plugin{
		Manifest: &plugin.Manifest{
			Name:    "personal-space",
			Runtime: "builtin",
		},
	}

	if err := runtime.Start(context.Background(), p); err != nil {
		t.Fatalf("start builtin plugin: %v", err)
	}
	if !runtime.IsRunning("personal-space") {
		t.Fatalf("expected builtin plugin to be running")
	}
	response, err := runtime.SendEvent(context.Background(), "personal-space", &plugin.EventMessage{Type: "thread.created"})
	if err != nil {
		t.Fatalf("send event: %v", err)
	}
	if response == nil || !response.Allowed {
		t.Fatalf("expected builtin plugin to allow event, got %#v", response)
	}
	if err := runtime.Stop(context.Background(), "personal-space"); err != nil {
		t.Fatalf("stop builtin plugin: %v", err)
	}
	if runtime.IsRunning("personal-space") {
		t.Fatalf("expected builtin plugin to be stopped")
	}
}
