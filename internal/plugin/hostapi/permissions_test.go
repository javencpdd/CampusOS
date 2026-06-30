package hostapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/campusos/CampusOS/internal/community/domain"
	"github.com/campusos/CampusOS/internal/community/repository"
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

func TestHandleHostAPIRequestForPluginReturnsConfig(t *testing.T) {
	hostAPI := NewHostAPIv2(nil, nil, nil)
	manifest := manifestWithPermissions("config-plugin", plugin.APIPermission{
		Resource: "config",
		Actions:  []string{"read"},
	})
	manifest.Config = map[string]interface{}{
		"entrypoint": "handle_event",
	}

	result, err := HandleHostAPIRequestForPlugin(hostAPI, manifest, "GetConfig", []byte(`{"key":"entrypoint"}`))
	if err != nil {
		t.Fatalf("get config: %v", err)
	}

	var response GetConfigResponse
	if err := json.Unmarshal(result, &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !response.Found || response.Value != "handle_event" {
		t.Fatalf("unexpected config response: %#v", response)
	}
}

func TestHandleHostAPIRequestForPluginReturnsThread(t *testing.T) {
	threadRepo := repository.NewMemoryThreadRepository()
	if err := threadRepo.Create(t.Context(), &domain.Thread{
		ID:         "thread-1",
		Title:      "Hello Thread",
		Content:    "body",
		AuthorID:   "user-1",
		AuthorName: "alice",
		CategoryID: "cat-1",
		Status:     domain.ThreadStatusPublished,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}); err != nil {
		t.Fatalf("create thread: %v", err)
	}

	hostAPI := NewHostAPIv2(nil, &DataAPI{threadRepo: threadRepo}, nil)
	manifest := manifestWithPermissions("thread-reader", plugin.APIPermission{
		Resource: "thread",
		Actions:  []string{"read"},
	})

	result, err := HandleHostAPIRequestForPlugin(hostAPI, manifest, "GetThread", []byte(`{"thread_id":"thread-1"}`))
	if err != nil {
		t.Fatalf("get thread: %v", err)
	}
	var response map[string]interface{}
	if err := json.Unmarshal(result, &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response["title"] != "Hello Thread" {
		t.Fatalf("unexpected thread response: %#v", response)
	}
}

func TestHandleHostAPIRequestForPluginReturnsReply(t *testing.T) {
	postRepo := repository.NewMemoryPostRepository()
	if err := postRepo.Create(t.Context(), &domain.Post{
		ID:          "post-1",
		ThreadID:    "thread-1",
		AuthorID:    "user-1",
		AuthorName:  "alice",
		Content:     "reply body",
		Status:      "published",
		FloorNumber: 1,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}); err != nil {
		t.Fatalf("create post: %v", err)
	}

	hostAPI := NewHostAPIv2(nil, &DataAPI{postRepo: postRepo}, nil)
	manifest := manifestWithPermissions("reply-reader", plugin.APIPermission{
		Resource: "reply",
		Actions:  []string{"read"},
	})

	result, err := HandleHostAPIRequestForPlugin(hostAPI, manifest, "GetReply", []byte(`{"post_id":"post-1"}`))
	if err != nil {
		t.Fatalf("get reply: %v", err)
	}
	var response map[string]interface{}
	if err := json.Unmarshal(result, &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response["content"] != "reply body" {
		t.Fatalf("unexpected reply response: %#v", response)
	}
}

func TestHandleHostAPIRequestForPluginSetsConfig(t *testing.T) {
	hostAPI := NewHostAPIv2(nil, nil, nil)
	repo := plugin.NewMemoryPluginRepository()
	hostAPI.SetPluginRepository(repo)
	manifest := manifestWithPermissions("config-writer", plugin.APIPermission{
		Resource: "config",
		Actions:  []string{"write"},
	})

	_, err := HandleHostAPIRequestForPlugin(hostAPI, manifest, "SetConfig", []byte(`{"key":"threshold","value":3}`))
	if err != nil {
		t.Fatalf("set config: %v", err)
	}
	if manifest.Config["threshold"] != float64(3) {
		t.Fatalf("expected config update, got %#v", manifest.Config)
	}
	record, err := repo.GetByName(t.Context(), "config-writer")
	if err != nil {
		t.Fatalf("get plugin record: %v", err)
	}
	var persisted map[string]interface{}
	if err := json.Unmarshal([]byte(record.Config), &persisted); err != nil {
		t.Fatalf("decode persisted config: %v", err)
	}
	if persisted["threshold"] != float64(3) {
		t.Fatalf("expected persisted config update, got %#v", persisted)
	}
}

func TestHandleHostAPIRequestForPluginChecksPermission(t *testing.T) {
	hostAPI := NewHostAPIv2(nil, nil, nil)
	hostAPI.SetPermissionChecker(fakePermissionChecker{allowed: true})
	manifest := manifestWithPermissions("permission-checker", plugin.APIPermission{
		Resource: "permission",
		Actions:  []string{"check"},
	})

	result, err := HandleHostAPIRequestForPlugin(hostAPI, manifest, "CheckPermission", []byte(`{"user_id":"1","resource":"thread","action":"read"}`))
	if err != nil {
		t.Fatalf("check permission: %v", err)
	}
	var response CheckPermissionResponse
	if err := json.Unmarshal(result, &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !response.Allowed {
		t.Fatalf("expected allowed response")
	}
}

func TestHandleHostAPIRequestForPluginWritesLog(t *testing.T) {
	hostAPI := NewHostAPIv2(nil, nil, nil)
	repo := plugin.NewMemoryPluginRepository()
	hostAPI.SetPluginLogRepository(repo)
	manifest := manifestWithPermissions("logger-plugin", plugin.APIPermission{
		Resource: "log",
		Actions:  []string{"write"},
	})

	_, err := HandleHostAPIRequestForPlugin(hostAPI, manifest, "Log", []byte(`{"level":"info","message":"hello from plugin","event_type":"thread.created"}`))
	if err != nil {
		t.Fatalf("write log: %v", err)
	}
	logs, err := repo.ListLogs(t.Context(), "logger-plugin", 10)
	if err != nil {
		t.Fatalf("list logs: %v", err)
	}
	if len(logs) != 1 || logs[0].Message != "hello from plugin" || logs[0].EventType != "thread.created" {
		t.Fatalf("unexpected logs: %#v", logs)
	}
}

func TestHandleHostAPIRequestForPluginDeniesConfigWithoutPermission(t *testing.T) {
	hostAPI := NewHostAPIv2(nil, nil, nil)
	manifest := &plugin.Manifest{
		Name: "config-denied-plugin",
		Config: map[string]interface{}{
			"entrypoint": "handle_event",
		},
	}

	_, err := HandleHostAPIRequestForPlugin(hostAPI, manifest, "GetConfig", []byte(`{"key":"entrypoint"}`))
	if !errors.Is(err, ErrHostAPIPermissionDenied) {
		t.Fatalf("expected permission denied, got %v", err)
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

type fakePermissionChecker struct {
	allowed bool
}

func (f fakePermissionChecker) Check(_ context.Context, _ string, _ string, _ string) (bool, error) {
	return f.allowed, nil
}

func manifestWithPermissions(name string, permissions ...plugin.APIPermission) *plugin.Manifest {
	return &plugin.Manifest{
		Name: name,
		Permissions: plugin.PermissionsConfig{
			API: permissions,
		},
	}
}
