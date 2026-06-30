package campusos

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHostClientGetConfigSendsPluginIdentity(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	defer server.Close()

	client := NewHostClientWithBaseURL(server.URL, "sdk-plugin")
	value, found, err := client.GetConfig(t.Context(), "entrypoint")
	if err != nil {
		t.Fatalf("get config: %v", err)
	}
	if !found || value != "handle_event" {
		t.Fatalf("unexpected config result: value=%#v found=%v", value, found)
	}
}

func TestHostClientStorageSetUsesPluginNamespace(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	defer server.Close()

	client := NewHostClientWithBaseURL(server.URL, "sdk-plugin")
	if err := client.StorageSet(t.Context(), "hello", "world"); err != nil {
		t.Fatalf("storage set: %v", err)
	}
}

func TestHostClientReturnsHTTPErrorBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"permission denied"}`))
	}))
	defer server.Close()

	client := NewHostClientWithBaseURL(server.URL, "sdk-plugin")
	if _, _, err := client.GetConfig(t.Context(), "entrypoint"); err == nil {
		t.Fatalf("expected error")
	}
}
