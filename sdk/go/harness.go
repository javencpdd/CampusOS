package campusos

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
)

type Harness struct {
	mu          sync.Mutex
	handler     http.Handler
	pluginName  string
	Config      map[string]interface{}
	Storage     map[string]string
	Users       map[string]map[string]interface{}
	Threads     map[string]map[string]interface{}
	Replies     map[string]map[string]interface{}
	Permissions map[string]bool
	Events      []PublishEventRequest
	Logs        []LogRequest
}

func NewHarness(pluginName string) *Harness {
	h := &Harness{
		pluginName:  pluginName,
		Config:      map[string]interface{}{},
		Storage:     map[string]string{},
		Users:       map[string]map[string]interface{}{},
		Threads:     map[string]map[string]interface{}{},
		Replies:     map[string]map[string]interface{}{},
		Permissions: map[string]bool{},
	}
	h.handler = http.HandlerFunc(h.handle)
	return h
}

func (h *Harness) Close() {
}

func (h *Harness) BaseURL() string {
	if h == nil {
		return ""
	}
	return "http://campusos-harness"
}

func (h *Harness) Client() *HostClient {
	return NewHostClientWithBaseURL(
		h.BaseURL(),
		h.pluginName,
		WithHTTPClient(&http.Client{Transport: handlerTransport{handler: h.handler}}),
	)
}

type handlerTransport struct {
	handler http.Handler
}

func (t handlerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	recorder := httptest.NewRecorder()
	t.handler.ServeHTTP(recorder, req)
	return recorder.Result(), nil
}

func (h *Harness) SetPermission(userID, resource, action string, allowed bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.Permissions[permissionKey(userID, resource, action)] = allowed
}

func (h *Harness) handle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.pluginName != "" && r.Header.Get("X-CampusOS-Plugin") != h.pluginName {
		writeHarnessError(w, http.StatusForbidden, "plugin identity mismatch")
		return
	}
	method := strings.TrimPrefix(r.URL.Path, "/api/host/")
	w.Header().Set("Content-Type", "application/json")

	switch method {
	case "GetUser":
		var req GetUserRequest
		if !decodeHarnessRequest(w, r, &req) {
			return
		}
		h.mu.Lock()
		user, ok := h.Users[req.UserID]
		h.mu.Unlock()
		if !ok {
			writeHarnessError(w, http.StatusNotFound, "user not found")
			return
		}
		_ = json.NewEncoder(w).Encode(user)
	case "GetThread":
		var req GetThreadRequest
		if !decodeHarnessRequest(w, r, &req) {
			return
		}
		h.mu.Lock()
		thread, ok := h.Threads[req.ThreadID]
		h.mu.Unlock()
		if !ok {
			writeHarnessError(w, http.StatusNotFound, "thread not found")
			return
		}
		_ = json.NewEncoder(w).Encode(thread)
	case "GetReply":
		var req GetReplyRequest
		if !decodeHarnessRequest(w, r, &req) {
			return
		}
		replyID := req.ReplyID
		if replyID == "" {
			replyID = req.PostID
		}
		h.mu.Lock()
		reply, ok := h.Replies[replyID]
		h.mu.Unlock()
		if !ok {
			writeHarnessError(w, http.StatusNotFound, "reply not found")
			return
		}
		_ = json.NewEncoder(w).Encode(reply)
	case "GetConfig":
		var req GetConfigRequest
		if !decodeHarnessRequest(w, r, &req) {
			return
		}
		h.mu.Lock()
		defer h.mu.Unlock()
		if req.Key == "" {
			_ = json.NewEncoder(w).Encode(GetConfigResponse{Config: h.Config, Found: true})
			return
		}
		value, found := h.Config[req.Key]
		_ = json.NewEncoder(w).Encode(GetConfigResponse{Value: value, Found: found})
	case "SetConfig":
		var req SetConfigRequest
		if !decodeHarnessRequest(w, r, &req) {
			return
		}
		h.mu.Lock()
		h.Config[req.Key] = req.Value
		h.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]bool{"success": true})
	case "StorageGet":
		var req StorageGetRequest
		if !decodeHarnessRequest(w, r, &req) {
			return
		}
		h.mu.Lock()
		value, found := h.Storage[req.Key]
		h.mu.Unlock()
		_ = json.NewEncoder(w).Encode(StorageGetResponse{Value: value, Found: found})
	case "StorageSet":
		var req StorageSetRequest
		if !decodeHarnessRequest(w, r, &req) {
			return
		}
		h.mu.Lock()
		h.Storage[req.Key] = req.Value
		h.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]bool{"success": true})
	case "StorageDelete":
		var req StorageDeleteRequest
		if !decodeHarnessRequest(w, r, &req) {
			return
		}
		h.mu.Lock()
		delete(h.Storage, req.Key)
		h.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]bool{"success": true})
	case "PublishEvent":
		var req PublishEventRequest
		if !decodeHarnessRequest(w, r, &req) {
			return
		}
		h.mu.Lock()
		h.Events = append(h.Events, req)
		h.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]bool{"success": true})
	case "Log":
		var req LogRequest
		if !decodeHarnessRequest(w, r, &req) {
			return
		}
		h.mu.Lock()
		h.Logs = append(h.Logs, req)
		h.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]bool{"success": true})
	case "CheckPermission":
		var req CheckPermissionRequest
		if !decodeHarnessRequest(w, r, &req) {
			return
		}
		h.mu.Lock()
		allowed := h.Permissions[permissionKey(req.UserID, req.Resource, req.Action)]
		h.mu.Unlock()
		_ = json.NewEncoder(w).Encode(CheckPermissionResponse{Allowed: allowed})
	default:
		writeHarnessError(w, http.StatusNotFound, "unknown host api method")
	}
}

func decodeHarnessRequest(w http.ResponseWriter, r *http.Request, v interface{}) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeHarnessError(w, http.StatusBadRequest, err.Error())
		return false
	}
	return true
}

func writeHarnessError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}

func permissionKey(userID, resource, action string) string {
	return userID + ":" + resource + ":" + action
}
