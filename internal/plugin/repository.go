package plugin

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"
	"time"
)

var ErrAPIKeyNotFound = errors.New("api key not found")
var ErrAPIKeyInactive = errors.New("api key is inactive")
var ErrAPIKeyExpired = errors.New("api key is expired")

// PluginRecord 插件数据库记录
type PluginRecord struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	DisplayName string    `json:"display_name"`
	Version     string    `json:"version"`
	Description string    `json:"description"`
	Author      string    `json:"author"`
	Runtime     string    `json:"runtime"`
	Status      string    `json:"status"`
	APIKey      string    `json:"api_key,omitempty"`
	Config      string    `json:"config"`
	ErrorMsg    string    `json:"error_message"`
	InstalledBy string    `json:"installed_by"`
	InstalledAt time.Time `json:"installed_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// APIKeyRecord API Key 记录
type APIKeyRecord struct {
	ID          int64      `json:"id"`
	Key         string     `json:"key"`
	Name        string     `json:"name"`
	UserID      *int64     `json:"user_id,omitempty"`
	PluginName  *string    `json:"plugin_name,omitempty"`
	Permissions []string   `json:"permissions"`
	IsActive    bool       `json:"is_active"`
	LastUsedAt  *time.Time `json:"last_used_at,omitempty"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

// PluginLogRecord 插件运行日志记录
type PluginLogRecord struct {
	ID         int64                  `json:"id"`
	PluginName string                 `json:"plugin_name"`
	Level      string                 `json:"level"`
	Message    string                 `json:"message"`
	EventType  string                 `json:"event_type"`
	TraceID    string                 `json:"trace_id"`
	Metadata   map[string]interface{} `json:"metadata"`
	CreatedAt  time.Time              `json:"created_at"`
}

// PluginRepository 插件仓储接口
type PluginRepository interface {
	Save(ctx context.Context, record *PluginRecord) error
	GetByName(ctx context.Context, name string) (*PluginRecord, error)
	List(ctx context.Context) ([]*PluginRecord, error)
	UpdateStatus(ctx context.Context, name, status, errorMsg string) error
	Delete(ctx context.Context, name string) error
}

// APIKeyRepository API Key 仓储接口
type APIKeyRepository interface {
	Create(ctx context.Context, record *APIKeyRecord) error
	GetByKey(ctx context.Context, key string) (*APIKeyRecord, error)
	ListByUser(ctx context.Context, userID int64) ([]*APIKeyRecord, error)
	Deactivate(ctx context.Context, key string) error
	UpdateLastUsed(ctx context.Context, key string) error
	Delete(ctx context.Context, key string) error
}

// PluginLogRepository 插件日志仓储接口
type PluginLogRepository interface {
	SaveLog(ctx context.Context, record *PluginLogRecord) error
	ListLogs(ctx context.Context, pluginName string, limit int) ([]*PluginLogRecord, error)
}

// MemoryPluginRepository 内存插件仓储
type MemoryPluginRepository struct {
	mu      sync.RWMutex
	plugins map[string]*PluginRecord
	logs    []*PluginLogRecord
}

func NewMemoryPluginRepository() *MemoryPluginRepository {
	return &MemoryPluginRepository{plugins: make(map[string]*PluginRecord)}
}

func (r *MemoryPluginRepository) Save(_ context.Context, record *PluginRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.plugins[record.Name] = record
	return nil
}

func (r *MemoryPluginRepository) GetByName(_ context.Context, name string) (*PluginRecord, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.plugins[name]
	if !ok {
		return nil, ErrAPIKeyNotFound
	}
	return p, nil
}

func (r *MemoryPluginRepository) List(_ context.Context) ([]*PluginRecord, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var list []*PluginRecord
	for _, p := range r.plugins {
		list = append(list, p)
	}
	return list, nil
}

func (r *MemoryPluginRepository) UpdateStatus(_ context.Context, name, status, errorMsg string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.plugins[name]
	if !ok {
		return ErrAPIKeyNotFound
	}
	p.Status = status
	p.ErrorMsg = errorMsg
	p.UpdatedAt = time.Now()
	return nil
}

func (r *MemoryPluginRepository) Delete(_ context.Context, name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.plugins, name)
	return nil
}

func (r *MemoryPluginRepository) SaveLog(_ context.Context, record *PluginLogRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if record.ID == 0 {
		record.ID = int64(len(r.logs) + 1)
	}
	if record.CreatedAt.IsZero() {
		record.CreatedAt = time.Now()
	}
	if record.Metadata == nil {
		record.Metadata = map[string]interface{}{}
	}
	copied := *record
	r.logs = append(r.logs, &copied)
	return nil
}

func (r *MemoryPluginRepository) ListLogs(_ context.Context, pluginName string, limit int) ([]*PluginLogRecord, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if limit <= 0 {
		limit = 100
	}
	logs := make([]*PluginLogRecord, 0, limit)
	for i := len(r.logs) - 1; i >= 0 && len(logs) < limit; i-- {
		record := r.logs[i]
		if pluginName != "" && record.PluginName != pluginName {
			continue
		}
		copied := *record
		logs = append(logs, &copied)
	}
	return logs, nil
}

// MemoryAPIKeyRepository 内存 API Key 仓储
type MemoryAPIKeyRepository struct {
	mu   sync.RWMutex
	keys map[string]*APIKeyRecord
}

func NewMemoryAPIKeyRepository() *MemoryAPIKeyRepository {
	return &MemoryAPIKeyRepository{keys: make(map[string]*APIKeyRecord)}
}

func (r *MemoryAPIKeyRepository) Create(_ context.Context, record *APIKeyRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.keys[record.Key] = record
	return nil
}

func (r *MemoryAPIKeyRepository) GetByKey(_ context.Context, key string) (*APIKeyRecord, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	rec, ok := r.keys[key]
	if !ok {
		return nil, ErrAPIKeyNotFound
	}
	return rec, nil
}

func (r *MemoryAPIKeyRepository) ListByUser(_ context.Context, userID int64) ([]*APIKeyRecord, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var list []*APIKeyRecord
	for _, rec := range r.keys {
		if rec.UserID != nil && *rec.UserID == userID {
			list = append(list, rec)
		}
	}
	return list, nil
}

func (r *MemoryAPIKeyRepository) Deactivate(_ context.Context, key string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	rec, ok := r.keys[key]
	if !ok {
		return ErrAPIKeyNotFound
	}
	rec.IsActive = false
	return nil
}

func (r *MemoryAPIKeyRepository) UpdateLastUsed(_ context.Context, key string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	rec, ok := r.keys[key]
	if !ok {
		return ErrAPIKeyNotFound
	}
	now := time.Now()
	rec.LastUsedAt = &now
	return nil
}

func (r *MemoryAPIKeyRepository) Delete(_ context.Context, key string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.keys, key)
	return nil
}

// GenerateAPIKey 生成随机 API Key
func GenerateAPIKey(prefix string) string {
	bytes := make([]byte, 24)
	rand.Read(bytes)
	return prefix + "_" + hex.EncodeToString(bytes)
}
