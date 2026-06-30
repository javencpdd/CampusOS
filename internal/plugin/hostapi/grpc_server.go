package hostapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"

	"github.com/campusos/CampusOS/internal/plugin"
)

type PluginLookup func(name string) (*plugin.Plugin, bool)

// HostAPIServer Host API 服务端（HTTP JSON-RPC 风格，兼容未来 gRPC）
type HostAPIServer struct {
	hostAPI      *HostAPIv2
	server       *http.Server
	addr         string
	pluginLookup PluginLookup
}

// NewHostAPIServer 创建 Host API 服务端
func NewHostAPIServer(hostAPI *HostAPIv2, addr string, lookup ...PluginLookup) *HostAPIServer {
	server := &HostAPIServer{
		hostAPI: hostAPI,
		addr:    addr,
	}
	if len(lookup) > 0 {
		server.pluginLookup = lookup[0]
	}
	return server
}

func (s *HostAPIServer) SetPluginLookup(lookup PluginLookup) {
	s.pluginLookup = lookup
}

// Start 启动 HTTP 服务
func (s *HostAPIServer) Start() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/host/", s.handleRequest)
	s.server = &http.Server{Addr: s.addr, Handler: mux}
	listener, err := net.Listen("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("listen host api %s: %w", s.addr, err)
	}
	log.Printf("🔌 Host API 监听 %s", s.addr)
	go func() {
		if err := s.server.Serve(listener); err != nil && err != http.ErrServerClosed {
			log.Printf("⚠️  Host API 服务错误: %v", err)
		}
	}()
	return nil
}

// Stop 停止服务
func (s *HostAPIServer) Stop() {
	if s.server != nil {
		s.server.Close()
	}
}

// handleRequest 处理 Host API 请求
func (s *HostAPIServer) handleRequest(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 从 URL 提取方法名: /api/host/GetUser -> "GetUser"
	method := r.URL.Path[len("/api/host/"):]

	// 读取请求体
	var reqBody map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
		// 空 body 也允许
		reqBody = make(map[string]interface{})
	}
	body, _ := json.Marshal(reqBody)

	manifest, err := s.resolvePluginManifest(r, reqBody)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	result, err := HandleHostAPIRequestForPlugin(s.hostAPI, manifest, method, body)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		if errors.Is(err, ErrHostAPIPermissionDenied) {
			w.WriteHeader(http.StatusForbidden)
		} else {
			w.WriteHeader(http.StatusBadRequest)
		}
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(result)
}

func (s *HostAPIServer) resolvePluginManifest(r *http.Request, reqBody map[string]interface{}) (*plugin.Manifest, error) {
	if s.pluginLookup == nil {
		return nil, fmt.Errorf("%w: plugin lookup is not configured", ErrHostAPIPermissionDenied)
	}

	pluginName := r.Header.Get("X-CampusOS-Plugin")
	if pluginName == "" {
		if raw, ok := reqBody["plugin_name"]; ok {
			if value, ok := raw.(string); ok {
				pluginName = value
			}
		}
	}
	if pluginName == "" {
		return nil, fmt.Errorf("%w: plugin identity is required", ErrHostAPIPermissionDenied)
	}

	p, ok := s.pluginLookup(pluginName)
	if !ok || p == nil || p.Manifest == nil {
		return nil, fmt.Errorf("%w: plugin %s is not registered", ErrHostAPIPermissionDenied, pluginName)
	}
	return p.Manifest, nil
}

// ─── Host API Handler 实现（供插件通过 HTTP/gRPC 调用）───

// GetUserHandler HTTP handler 封装
func (s *HostAPIServer) GetUserHandler(ctx context.Context, userID string) (map[string]interface{}, error) {
	return s.hostAPI.Identity().GetUser(ctx, userID)
}

// QueryThreadsHandler HTTP handler 封装
func (s *HostAPIServer) QueryThreadsHandler(ctx context.Context, filter map[string]interface{}) ([]map[string]interface{}, error) {
	return s.hostAPI.Data().QueryThreads(ctx, filter)
}

// PublishEventHandler HTTP handler 封装
func (s *HostAPIServer) PublishEventHandler(ctx context.Context, eventType, source, subject string, data interface{}) error {
	return s.hostAPI.Event().Publish(ctx, eventType, source, subject, data)
}

// ─── Host API Request/Response 类型 ───

type GetUserRequest struct {
	UserID string `json:"user_id"`
}

type GetThreadRequest struct {
	ThreadID string `json:"thread_id"`
}

type GetReplyRequest struct {
	ReplyID string `json:"reply_id,omitempty"`
	PostID  string `json:"post_id,omitempty"`
}

type GetUserResponse struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Nickname string `json:"nickname"`
	Email    string `json:"email"`
	Status   string `json:"status"`
}

type QueryThreadsRequest struct {
	CategoryID string `json:"category_id,omitempty"`
	AuthorID   string `json:"author_id,omitempty"`
	Keyword    string `json:"keyword,omitempty"`
	Status     string `json:"status,omitempty"`
	Page       int    `json:"page"`
	PageSize   int    `json:"page_size"`
}

type QueryThreadsResponse struct {
	Threads []map[string]interface{} `json:"threads"`
	Total   int                      `json:"total"`
}

type PublishEventRequest struct {
	EventType string      `json:"event_type"`
	Source    string      `json:"source"`
	Subject   string      `json:"subject"`
	Data      interface{} `json:"data"`
}

type SendNotificationRequest struct {
	UserID    string `json:"user_id"`
	Template  string `json:"template"`
	Title     string `json:"title"`
	Content   string `json:"content"`
	ActionURL string `json:"action_url"`
}

type GetConfigRequest struct {
	Key string `json:"key,omitempty"`
}

type GetConfigResponse struct {
	Config map[string]interface{} `json:"config,omitempty"`
	Value  interface{}            `json:"value,omitempty"`
	Found  bool                   `json:"found"`
}

type SetConfigRequest struct {
	Key   string      `json:"key"`
	Value interface{} `json:"value"`
}

type CheckPermissionRequest struct {
	UserID   string `json:"user_id"`
	Resource string `json:"resource"`
	Action   string `json:"action"`
}

type CheckPermissionResponse struct {
	Allowed bool `json:"allowed"`
}

type LogRequest struct {
	Level     string                 `json:"level"`
	Message   string                 `json:"message"`
	EventType string                 `json:"event_type,omitempty"`
	TraceID   string                 `json:"trace_id,omitempty"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

type StorageGetRequest struct {
	PluginName string `json:"plugin_name"`
	Key        string `json:"key"`
}

type StorageGetResponse struct {
	Value string `json:"value"`
	Found bool   `json:"found"`
}

type StorageSetRequest struct {
	PluginName string `json:"plugin_name"`
	Key        string `json:"key"`
	Value      string `json:"value"`
}

type StorageDeleteRequest struct {
	PluginName string `json:"plugin_name"`
	Key        string `json:"key"`
}

// ─── 内存 KV 存储（插件数据存储）───

type MemoryKVStore struct {
	data map[string]map[string]string // pluginName -> key -> value
}

func NewMemoryKVStore() *MemoryKVStore {
	return &MemoryKVStore{data: make(map[string]map[string]string)}
}

func (s *MemoryKVStore) Get(pluginName, key string) (string, bool) {
	if store, ok := s.data[pluginName]; ok {
		val, found := store[key]
		return val, found
	}
	return "", false
}

func (s *MemoryKVStore) Set(pluginName, key, value string) {
	if _, ok := s.data[pluginName]; !ok {
		s.data[pluginName] = make(map[string]string)
	}
	s.data[pluginName][key] = value
}

func (s *MemoryKVStore) Delete(pluginName, key string) {
	if store, ok := s.data[pluginName]; ok {
		delete(store, key)
	}
}

// ─── 通知服务（简化实现）───

type NotificationService struct {
	// 未来接入真实通知服务
}

func NewNotificationService() *NotificationService {
	return &NotificationService{}
}

func (s *NotificationService) Send(ctx context.Context, userID, title, content, actionURL string) error {
	// 当前仅记录日志
	data, _ := json.Marshal(map[string]string{
		"user_id":    userID,
		"title":      title,
		"content":    content,
		"action_url": actionURL,
	})
	log.Printf("📬 通知发送: %s", string(data))
	return nil
}

// ─── 更新 HostAPI 增加 Notification 和 Storage ───

// HostAPIv2 增强版 Host API（含 Notification 和 Storage）
type HostAPIv2 struct {
	*HostAPI
	notification *NotificationService
	storage      *MemoryKVStore
	logRepo      plugin.PluginLogRepository
	permission   PermissionChecker
}

type PermissionChecker interface {
	Check(ctx context.Context, userID string, resource, action string) (bool, error)
}

func NewHostAPIv2(
	identity *IdentityAPI,
	data *DataAPI,
	event *EventAPI,
) *HostAPIv2 {
	return NewHostAPIv2FromHostAPI(&HostAPI{
		identity: identity,
		data:     data,
		event:    event,
	})
}

func NewHostAPIv2FromHostAPI(base *HostAPI) *HostAPIv2 {
	if base == nil {
		base = &HostAPI{}
	}
	return &HostAPIv2{
		HostAPI:      base,
		notification: NewNotificationService(),
		storage:      NewMemoryKVStore(),
	}
}

func (h *HostAPIv2) Notification() *NotificationService { return h.notification }
func (h *HostAPIv2) Storage() *MemoryKVStore            { return h.storage }

func (h *HostAPIv2) SetPluginLogRepository(repo plugin.PluginLogRepository) {
	h.logRepo = repo
}

func (h *HostAPIv2) SetPermissionChecker(permission PermissionChecker) {
	h.permission = permission
}

// HandleHostAPIRequest 处理来自插件的 Host API 请求。
//
// Deprecated: use HandleHostAPIRequestForPlugin so Host API calls are checked
// against the calling plugin manifest.
func HandleHostAPIRequest(hostAPI *HostAPIv2, method string, body []byte) ([]byte, error) {
	return HandleHostAPIRequestForPlugin(hostAPI, nil, method, body)
}

// HandleHostAPIRequestForPlugin 处理来自指定插件的 Host API 请求。
func HandleHostAPIRequestForPlugin(hostAPI *HostAPIv2, manifest *plugin.Manifest, method string, body []byte) ([]byte, error) {
	ctx := context.Background()
	if err := CheckHostAPIPermission(manifest, method); err != nil {
		hostAPI.logPermissionDenied(ctx, manifest, method, err)
		return nil, err
	}

	switch method {
	case "GetUser":
		var req GetUserRequest
		if err := json.Unmarshal(body, &req); err != nil {
			return nil, fmt.Errorf("invalid request: %w", err)
		}
		user, err := hostAPI.Identity().GetUser(ctx, req.UserID)
		if err != nil {
			return nil, fmt.Errorf("user not found: %w", err)
		}
		return json.Marshal(user)

	case "GetThread":
		var req GetThreadRequest
		if err := json.Unmarshal(body, &req); err != nil {
			return nil, fmt.Errorf("invalid request: %w", err)
		}
		thread, err := hostAPI.Data().GetThread(ctx, req.ThreadID)
		if err != nil {
			return nil, fmt.Errorf("thread not found: %w", err)
		}
		return json.Marshal(thread)

	case "GetReply":
		var req GetReplyRequest
		if err := json.Unmarshal(body, &req); err != nil {
			return nil, fmt.Errorf("invalid request: %w", err)
		}
		replyID := req.ReplyID
		if replyID == "" {
			replyID = req.PostID
		}
		reply, err := hostAPI.Data().GetReply(ctx, replyID)
		if err != nil {
			return nil, fmt.Errorf("reply not found: %w", err)
		}
		return json.Marshal(reply)

	case "QueryThreads":
		var req QueryThreadsRequest
		if err := json.Unmarshal(body, &req); err != nil {
			return nil, fmt.Errorf("invalid request: %w", err)
		}
		filter := map[string]interface{}{
			"category_id": req.CategoryID,
			"author_id":   req.AuthorID,
			"keyword":     req.Keyword,
			"page":        req.Page,
			"page_size":   req.PageSize,
		}
		threads, err := hostAPI.Data().QueryThreads(ctx, filter)
		if err != nil {
			return nil, fmt.Errorf("query failed: %w", err)
		}
		return json.Marshal(QueryThreadsResponse{Threads: threads, Total: len(threads)})

	case "PublishEvent":
		var req PublishEventRequest
		if err := json.Unmarshal(body, &req); err != nil {
			return nil, fmt.Errorf("invalid request: %w", err)
		}
		if err := hostAPI.Event().Publish(ctx, req.EventType, req.Source, req.Subject, req.Data); err != nil {
			return nil, fmt.Errorf("publish failed: %w", err)
		}
		return json.Marshal(map[string]bool{"success": true})

	case "SendNotification":
		var req SendNotificationRequest
		if err := json.Unmarshal(body, &req); err != nil {
			return nil, fmt.Errorf("invalid request: %w", err)
		}
		if err := hostAPI.Notification().Send(ctx, req.UserID, req.Title, req.Content, req.ActionURL); err != nil {
			return nil, fmt.Errorf("notification failed: %w", err)
		}
		return json.Marshal(map[string]bool{"success": true})

	case "GetConfig":
		if manifest == nil {
			return nil, fmt.Errorf("%w: plugin manifest is required for GetConfig", ErrHostAPIPermissionDenied)
		}
		var req GetConfigRequest
		if err := json.Unmarshal(body, &req); err != nil {
			return nil, fmt.Errorf("invalid request: %w", err)
		}
		config := manifest.Config
		if config == nil {
			config = map[string]interface{}{}
		}
		if req.Key == "" {
			return json.Marshal(GetConfigResponse{Config: config, Found: true})
		}
		value, found := config[req.Key]
		return json.Marshal(GetConfigResponse{Value: value, Found: found})

	case "SetConfig":
		if manifest == nil {
			return nil, fmt.Errorf("%w: plugin manifest is required for SetConfig", ErrHostAPIPermissionDenied)
		}
		var req SetConfigRequest
		if err := json.Unmarshal(body, &req); err != nil {
			return nil, fmt.Errorf("invalid request: %w", err)
		}
		if req.Key == "" {
			return nil, errors.New("config key is required")
		}
		if manifest.Config == nil {
			manifest.Config = map[string]interface{}{}
		}
		manifest.Config[req.Key] = req.Value
		return json.Marshal(map[string]bool{"success": true})

	case "CheckPermission":
		var req CheckPermissionRequest
		if err := json.Unmarshal(body, &req); err != nil {
			return nil, fmt.Errorf("invalid request: %w", err)
		}
		if hostAPI.permission == nil {
			return nil, errors.New("permission checker is not configured")
		}
		allowed, err := hostAPI.permission.Check(ctx, req.UserID, req.Resource, req.Action)
		if err != nil {
			return nil, fmt.Errorf("check permission failed: %w", err)
		}
		return json.Marshal(CheckPermissionResponse{Allowed: allowed})

	case "Log":
		if manifest == nil {
			return nil, fmt.Errorf("%w: plugin manifest is required for Log", ErrHostAPIPermissionDenied)
		}
		var req LogRequest
		if err := json.Unmarshal(body, &req); err != nil {
			return nil, fmt.Errorf("invalid request: %w", err)
		}
		if req.Level == "" {
			req.Level = "info"
		}
		if req.Metadata == nil {
			req.Metadata = map[string]interface{}{}
		}
		if hostAPI.logRepo != nil {
			if err := hostAPI.logRepo.SaveLog(ctx, &plugin.PluginLogRecord{
				PluginName: manifest.Name,
				Level:      req.Level,
				Message:    req.Message,
				EventType:  req.EventType,
				TraceID:    req.TraceID,
				Metadata:   req.Metadata,
			}); err != nil {
				return nil, fmt.Errorf("write plugin log failed: %w", err)
			}
		}
		return json.Marshal(map[string]bool{"success": true})

	case "StorageGet":
		var req StorageGetRequest
		if err := json.Unmarshal(body, &req); err != nil {
			return nil, fmt.Errorf("invalid request: %w", err)
		}
		if err := requireStorageOwner(manifest, req.PluginName); err != nil {
			return nil, err
		}
		val, found := hostAPI.Storage().Get(req.PluginName, req.Key)
		return json.Marshal(StorageGetResponse{Value: val, Found: found})

	case "StorageSet":
		var req StorageSetRequest
		if err := json.Unmarshal(body, &req); err != nil {
			return nil, fmt.Errorf("invalid request: %w", err)
		}
		if err := requireStorageOwner(manifest, req.PluginName); err != nil {
			return nil, err
		}
		hostAPI.Storage().Set(req.PluginName, req.Key, req.Value)
		return json.Marshal(map[string]bool{"success": true})

	case "StorageDelete":
		var req StorageDeleteRequest
		if err := json.Unmarshal(body, &req); err != nil {
			return nil, fmt.Errorf("invalid request: %w", err)
		}
		if err := requireStorageOwner(manifest, req.PluginName); err != nil {
			return nil, err
		}
		hostAPI.Storage().Delete(req.PluginName, req.Key)
		return json.Marshal(map[string]bool{"success": true})

	default:
		return nil, errors.New("unknown method: " + method)
	}
}

func (h *HostAPIv2) logPermissionDenied(ctx context.Context, manifest *plugin.Manifest, method string, permissionErr error) {
	if h == nil || h.logRepo == nil || manifest == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	record := &plugin.PluginLogRecord{
		PluginName: manifest.Name,
		Level:      "warn",
		Message:    "host api permission denied",
		Metadata: map[string]interface{}{
			"method": method,
			"error":  permissionErr.Error(),
		},
	}
	if err := h.logRepo.SaveLog(ctx, record); err != nil {
		log.Printf("⚠️  Host API 权限拒绝日志写入失败: %s (%v)", manifest.Name, err)
	}
}
