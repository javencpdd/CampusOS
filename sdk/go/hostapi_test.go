package campusos

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestHostClientGetConfigSendsPluginIdentity(t *testing.T) {
	client := newTestHostClient("sdk-plugin", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/host/GetConfig" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("X-CampusOS-Plugin"); got != "sdk-plugin" {
			t.Fatalf("expected plugin identity header, got %q", got)
		}
		var request GetConfigRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if request.Key != "entrypoint" {
			t.Fatalf("unexpected key: %q", request.Key)
		}
		_ = json.NewEncoder(w).Encode(GetConfigResponse{Value: "handle_event", Found: true})
	}))

	value, found, err := client.GetConfig(t.Context(), "entrypoint")
	if err != nil {
		t.Fatalf("get config: %v", err)
	}
	if !found || value != "handle_event" {
		t.Fatalf("unexpected config result: value=%#v found=%v", value, found)
	}
}

func TestHostClientStorageSetUsesPluginNamespace(t *testing.T) {
	client := newTestHostClient("sdk-plugin", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/host/StorageSet" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		var request StorageSetRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if request.PluginName != "sdk-plugin" || request.Key != "hello" || request.Value != "world" {
			t.Fatalf("unexpected storage request: %#v", request)
		}
		_ = json.NewEncoder(w).Encode(map[string]bool{"success": true})
	}))

	if err := client.StorageSet(t.Context(), "hello", "world"); err != nil {
		t.Fatalf("storage set: %v", err)
	}
}

func TestHostClientGetThread(t *testing.T) {
	client := newTestHostClient("sdk-plugin", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/host/GetThread" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		var request GetThreadRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if request.ThreadID != "thread-1" {
			t.Fatalf("unexpected request: %#v", request)
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": "thread-1", "title": "Hello"})
	}))

	thread, err := client.GetThread(t.Context(), "thread-1")
	if err != nil {
		t.Fatalf("get thread: %v", err)
	}
	if thread["title"] != "Hello" {
		t.Fatalf("unexpected thread: %#v", thread)
	}
}

func TestHostClientCheckPermission(t *testing.T) {
	client := newTestHostClient("sdk-plugin", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/host/CheckPermission" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		var request CheckPermissionRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if request.UserID != "user-1" || request.Resource != "thread" || request.Action != "read" {
			t.Fatalf("unexpected request: %#v", request)
		}
		_ = json.NewEncoder(w).Encode(CheckPermissionResponse{Allowed: true})
	}))

	allowed, err := client.CheckPermission(t.Context(), "user-1", "thread", "read")
	if err != nil {
		t.Fatalf("check permission: %v", err)
	}
	if !allowed {
		t.Fatalf("expected allowed")
	}
}

func TestHostClientLog(t *testing.T) {
	client := newTestHostClient("sdk-plugin", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/host/Log" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		var request LogRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if request.Message != "handled event" || request.EventType != "thread.created" {
			t.Fatalf("unexpected request: %#v", request)
		}
		_ = json.NewEncoder(w).Encode(map[string]bool{"success": true})
	}))

	if err := client.Log(t.Context(), LogRequest{
		Level:     "info",
		Message:   "handled event",
		EventType: "thread.created",
	}); err != nil {
		t.Fatalf("log: %v", err)
	}
}

func TestHostClientReturnsHTTPErrorBody(t *testing.T) {
	client := newTestHostClient("sdk-plugin", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"permission denied"}`))
	}))

	if _, _, err := client.GetConfig(t.Context(), "entrypoint"); err == nil {
		t.Fatalf("expected error")
	}
}

func newTestHostClient(pluginName string, handler http.Handler) *HostClient {
	return NewHostClientWithBaseURL(
		"http://campusos-host-test",
		pluginName,
		WithHTTPClient(&http.Client{Transport: handlerTransport{handler: handler}}),
	)
}
