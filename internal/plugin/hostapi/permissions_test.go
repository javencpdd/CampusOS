package hostapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/campusos/CampusOS/internal/plugin"
)

func TestHandleHostAPIRequestForPluginDeniesMissingPermission(t *testing.T) {
	hostAPI := NewHostAPIv2(nil, nil, nil)
	manifest := &plugin.Manifest{Name: "no-user-permission"}

	_, err := HandleHostAPIRequestForPlugin(hostAPI, manifest, "GetUser", []byte(`{"user_id":"1"}`))
	if !errors.Is(err, ErrHostAPIPermissionDenied) {
		t.Fatalf("expected permission denied, got %v", err)
	}
}

func TestHandleHostAPIRequestForPluginWritesPermissionDeniedLog(t *testing.T) {
	hostAPI := NewHostAPIv2(nil, nil, nil)
	repo := plugin.NewMemoryPluginRepository()
	hostAPI.SetPluginLogRepository(repo)
	manifest := &plugin.Manifest{Name: "denied-plugin"}

	_, err := HandleHostAPIRequestForPlugin(hostAPI, manifest, "GetUser", []byte(`{"user_id":"1"}`))
	if !errors.Is(err, ErrHostAPIPermissionDenied) {
		t.Fatalf("expected permission denied, got %v", err)
	}

	logs, err := repo.ListLogs(t.Context(), "denied-plugin", 10)
	if err != nil {
		t.Fatalf("list logs: %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("expected one permission log, got %#v", logs)
	}
	if logs[0].Level != "warn" || logs[0].Message != "host api permission denied" {
		t.Fatalf("unexpected log record: %#v", logs[0])
	}
	if logs[0].Metadata["method"] != "GetUser" {
		t.Fatalf("expected method metadata, got %#v", logs[0].Metadata)
	}
}

func TestHandleHostAPIRequestForPluginAllowsDeclaredStoragePermission(t *testing.T) {
	hostAPI := NewHostAPIv2(nil, nil, nil)
	manifest := manifestWithPermissions("owned-storage", plugin.APIPermission{
		Resource: "storage",
		Actions:  []string{"write"},
	})

	body := []byte(`{"plugin_name":"owned-storage","key":"hello","value":"world"}`)
	result, err := HandleHostAPIRequestForPlugin(hostAPI, manifest, "StorageSet", body)
	if err != nil {
		t.Fatalf("storage set: %v", err)
	}

	var response map[string]bool
	if err := json.Unmarshal(result, &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !response["success"] {
		t.Fatalf("expected success response, got %s", string(result))
	}
}

func TestHandleHostAPIRequestForPluginAllowsWildcardPermission(t *testing.T) {
	hostAPI := NewHostAPIv2(nil, nil, nil)
	manifest := manifestWithPermissions("wildcard-storage", plugin.APIPermission{
		Resource: "*",
		Actions:  []string{"*"},
	})

	body := []byte(`{"plugin_name":"wildcard-storage","key":"hello","value":"world"}`)
	if _, err := HandleHostAPIRequestForPlugin(hostAPI, manifest, "StorageSet", body); err != nil {
		t.Fatalf("expected wildcard permission to allow call, got %v", err)
	}
}

func TestHandleHostAPIRequestForPluginDeniesForeignStorageNamespace(t *testing.T) {
	hostAPI := NewHostAPIv2(nil, nil, nil)
	manifest := manifestWithPermissions("owned-storage", plugin.APIPermission{
		Resource: "storage",
		Actions:  []string{"read"},
	})

	body := []byte(`{"plugin_name":"other-plugin","key":"hello"}`)
	_, err := HandleHostAPIRequestForPlugin(hostAPI, manifest, "StorageGet", body)
	if !errors.Is(err, ErrHostAPIPermissionDenied) {
		t.Fatalf("expected storage namespace denial, got %v", err)
	}
}

func TestHandleHostAPIRequestRequiresManifestForProtectedMethods(t *testing.T) {
	hostAPI := NewHostAPIv2(nil, nil, nil)

	_, err := HandleHostAPIRequest(hostAPI, "StorageSet", []byte(`{"plugin_name":"plugin","key":"hello","value":"world"}`))
	if !errors.Is(err, ErrHostAPIPermissionDenied) {
		t.Fatalf("expected permission denied without manifest, got %v", err)
	}
}

func TestHostAPIServerChecksPluginIdentity(t *testing.T) {
	hostAPI := NewHostAPIv2(nil, nil, nil)
	manifest := manifestWithPermissions("http-plugin", plugin.APIPermission{
		Resource: "storage",
		Actions:  []string{"write"},
	})
	server := NewHostAPIServer(hostAPI, ":0", func(name string) (*plugin.Plugin, bool) {
		if name != "http-plugin" {
			return nil, false
		}
		return &plugin.Plugin{ID: name, Manifest: manifest}, true
	})

	body := bytes.NewBufferString(`{"plugin_name":"http-plugin","key":"hello","value":"world"}`)
	request := httptest.NewRequest(http.MethodPost, "/api/host/StorageSet", body)
	request.Header.Set("X-CampusOS-Plugin", "http-plugin")
	response := httptest.NewRecorder()

	server.handleRequest(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", response.Code, response.Body.String())
	}
}

func TestHostAPIServerDeniesMissingPluginIdentity(t *testing.T) {
	hostAPI := NewHostAPIv2(nil, nil, nil)
	server := NewHostAPIServer(hostAPI, ":0", func(name string) (*plugin.Plugin, bool) {
		return nil, false
	})

	body := bytes.NewBufferString(`{"plugin_name":"missing-plugin","key":"hello","value":"world"}`)
	request := httptest.NewRequest(http.MethodPost, "/api/host/StorageSet", body)
	response := httptest.NewRecorder()

	server.handleRequest(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf("expected status 403, got %d: %s", response.Code, response.Body.String())
	}
}

func manifestWithPermissions(name string, permissions ...plugin.APIPermission) *plugin.Manifest {
	return &plugin.Manifest{
		Name: name,
		Permissions: plugin.PermissionsConfig{
			API: permissions,
		},
	}
}
