package grpc

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/campusos/CampusOS/internal/plugin"
)

func TestRuntimeStopsManagedProcessWithoutMarkingItFailed(t *testing.T) {
	directory := t.TempDir()
	if runtime.GOOS == "windows" {
		source := filepath.Join(directory, "main.go")
		if err := os.WriteFile(source, []byte("package main\nimport \"time\"\nfunc main() { for { time.Sleep(time.Hour) } }\n"), 0o600); err != nil {
			t.Fatalf("write plugin fixture source: %v", err)
		}
		binary := filepath.Join(directory, "plugin.exe")
		if output, err := exec.Command("go", "build", "-o", binary, source).CombinedOutput(); err != nil {
			t.Fatalf("build plugin fixture: %v: %s", err, output)
		}
	} else {
		binary := filepath.Join(directory, "plugin")
		script := "#!/bin/sh\ntrap 'exit 0' TERM INT\nwhile :; do sleep 1; done\n"
		if err := os.WriteFile(binary, []byte(script), 0o755); err != nil {
			t.Fatalf("write plugin fixture: %v", err)
		}
	}

	runtime := NewGRPCRuntime()
	managed := &plugin.Plugin{
		ID:        "lifecycle-test",
		Directory: directory,
		Manifest:  &plugin.Manifest{Name: "lifecycle-test"},
		Status:    plugin.StatusRunning,
	}
	if err := runtime.Start(context.Background(), managed); err != nil {
		t.Fatalf("start managed process: %v", err)
	}
	if err := runtime.HealthCheck(context.Background(), managed.ID); err != nil {
		t.Fatalf("health check managed process: %v", err)
	}
	if err := runtime.Stop(context.Background(), managed.ID); err != nil {
		t.Fatalf("stop managed process: %v", err)
	}
	if runtime.IsRunning(managed.ID) {
		t.Fatal("managed process remained registered after stop")
	}
	if managed.Status == plugin.StatusError {
		t.Fatalf("intentional stop must not mark the plugin failed: %s", managed.ErrorMsg)
	}
}

func TestRuntimeSendsEventsOnlyToExplicitLoopbackEndpoint(t *testing.T) {
	received := make(chan plugin.EventMessage, 1)
	runtime := NewGRPCRuntime()
	runtime.httpClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		defer request.Body.Close()
		var event plugin.EventMessage
		if err := json.NewDecoder(request.Body).Decode(&event); err != nil {
			t.Fatal(err)
		}
		received <- event
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"allowed":true,"message":"accepted"}`)),
		}, nil
	})}

	managed := &plugin.Plugin{ID: "event-test", Manifest: &plugin.Manifest{
		Name: "event-test",
		Config: map[string]interface{}{
			"event_url": "http://127.0.0.1:19091/event",
		},
	}}
	runtime.processes[managed.ID] = &pluginProcess{plugin: managed}
	event := &plugin.EventMessage{Type: "thread.created", Subject: "42"}
	response, err := runtime.SendEvent(context.Background(), managed.ID, event)
	if err != nil {
		t.Fatalf("send event: %v", err)
	}
	if response == nil || !response.Allowed || response.Message != "accepted" {
		t.Fatalf("unexpected event response: %#v", response)
	}
	if delivered := <-received; delivered.Type != event.Type || delivered.Subject != event.Subject {
		t.Fatalf("unexpected delivered event: %#v", delivered)
	}

	managed.Manifest.Config = map[string]interface{}{"event_url": "http://example.com/event"}
	if _, err := runtime.SendEvent(context.Background(), managed.ID, event); err == nil {
		t.Fatal("non-loopback event endpoint must be rejected")
	}
	managed.Manifest.Config = nil
	response, err = runtime.SendEvent(context.Background(), managed.ID, event)
	if err != nil || response == nil || !response.Allowed {
		t.Fatalf("legacy manifest without event_url must retain log-only behavior: response=%#v err=%v", response, err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}
