package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	communityport "github.com/campusos/CampusOS/internal/modules/core/community/port"
	"github.com/campusos/CampusOS/pkg/idgen"
	"github.com/campusos/CampusOS/pkg/observability"
	"github.com/campusos/CampusOS/pkg/response"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrToolNotFound = errors.New("mcp tool not found")
var ErrServiceDisabled = errors.New("mcp service disabled")

type Tool struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	ReadOnly    bool     `json:"read_only"`
	Arguments   []string `json:"arguments"`
	Enabled     bool     `json:"enabled"`
}

type AuditLog struct {
	ID        string                 `json:"id"`
	UserID    string                 `json:"user_id"`
	Tool      string                 `json:"tool"`
	Arguments map[string]interface{} `json:"arguments"`
	Success   bool                   `json:"success"`
	Error     string                 `json:"error,omitempty"`
	CreatedAt time.Time              `json:"created_at"`
}

type Summary struct {
	Enabled    bool  `json:"enabled"`
	ToolTotal  int   `json:"tool_total"`
	AuditTotal int64 `json:"audit_total"`
}

type AuditStore interface {
	SaveAudit(ctx context.Context, record *AuditLog) error
	ListAudit(ctx context.Context, limit int) ([]*AuditLog, error)
	CountAudit(ctx context.Context) (int64, error)
}

type Service struct {
	mu         sync.RWMutex
	enabled    bool
	categories communityport.CategoryCatalog
	threads    communityport.ContentQuery
	audit      AuditStore
	metrics    *observability.Collector
	tools      map[string]Tool
}

func NewService(categories communityport.CategoryCatalog, threads communityport.ContentQuery, audit AuditStore, metrics *observability.Collector) *Service {
	s := &Service{
		enabled:    true,
		categories: categories,
		threads:    threads,
		audit:      audit,
		metrics:    metrics,
		tools:      make(map[string]Tool),
	}
	s.registerTool(Tool{Name: "health.check", Description: "检查 CampusOS API 基础状态", ReadOnly: true, Enabled: true})
	s.registerTool(Tool{Name: "categories.list", Description: "读取公开版块列表", ReadOnly: true, Enabled: true})
	s.registerTool(Tool{Name: "threads.list", Description: "分页读取帖子列表", ReadOnly: true, Enabled: true, Arguments: []string{"page", "page_size", "category_id", "keyword"}})
	s.registerTool(Tool{Name: "threads.get", Description: "读取单个帖子详情", ReadOnly: true, Enabled: true, Arguments: []string{"id"}})
	return s
}

func (s *Service) registerTool(tool Tool) {
	s.tools[tool.Name] = tool
}

func (s *Service) ListTools() []Tool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	tools := make([]Tool, 0, len(s.tools))
	for _, tool := range s.tools {
		tools = append(tools, tool)
	}
	return tools
}

func (s *Service) SetEnabled(enabled bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.enabled = enabled
}

func (s *Service) IsEnabled() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.enabled
}

func (s *Service) Summary(ctx context.Context) (*Summary, error) {
	total, err := s.audit.CountAudit(ctx)
	if err != nil {
		return nil, err
	}
	return &Summary{Enabled: s.IsEnabled(), ToolTotal: len(s.ListTools()), AuditTotal: total}, nil
}

func (s *Service) CallTool(ctx context.Context, userID, name string, args map[string]interface{}) (interface{}, error) {
	s.mu.RLock()
	enabled := s.enabled
	tool, ok := s.tools[name]
	s.mu.RUnlock()
	if !enabled {
		s.saveAudit(ctx, userID, name, args, false, ErrServiceDisabled.Error())
		if s.metrics != nil {
			s.metrics.RecordExternal("mcp.call", false)
		}
		return nil, ErrServiceDisabled
	}
	if !ok || !tool.Enabled {
		s.saveAudit(ctx, userID, name, args, false, ErrToolNotFound.Error())
		if s.metrics != nil {
			s.metrics.RecordExternal("mcp.call", false)
		}
		return nil, ErrToolNotFound
	}

	result, err := s.call(ctx, name, args)
	if err != nil {
		s.saveAudit(ctx, userID, name, args, false, err.Error())
		if s.metrics != nil {
			s.metrics.RecordExternal("mcp.call", false)
		}
		return nil, err
	}
	s.saveAudit(ctx, userID, name, args, true, "")
	if s.metrics != nil {
		s.metrics.RecordExternal("mcp.call", true)
	}
	return result, nil
}

func (s *Service) call(ctx context.Context, name string, args map[string]interface{}) (interface{}, error) {
	switch name {
	case "health.check":
		return gin.H{"status": "ok", "time": time.Now().UTC()}, nil
	case "categories.list":
		return s.categories.ListCategories(ctx)
	case "threads.list":
		page := intArg(args, "page", 1)
		pageSize := intArg(args, "page_size", 20)
		if pageSize > 50 {
			pageSize = 50
		}
		threads, total, err := s.threads.ListPublicThreads(ctx, domain.ThreadListFilter{
			Page:       page,
			PageSize:   pageSize,
			CategoryID: stringArg(args, "category_id"),
			Keyword:    stringArg(args, "keyword"),
			Status:     string(domain.ThreadStatusPublished),
		})
		if err != nil {
			return nil, err
		}
		return gin.H{"items": threads, "total": total, "page": page, "page_size": pageSize}, nil
	case "threads.get":
		id := stringArg(args, "id")
		if id == "" {
			return nil, errors.New("id is required")
		}
		thread, err := s.threads.GetPublicThread(ctx, id)
		if err != nil {
			return nil, err
		}
		return thread, nil
	default:
		return nil, ErrToolNotFound
	}
}

func (s *Service) ListAudit(ctx context.Context, limit int) ([]*AuditLog, error) {
	return s.audit.ListAudit(ctx, limit)
}

func (s *Service) saveAudit(ctx context.Context, userID, tool string, args map[string]interface{}, success bool, errorMessage string) {
	if s.audit == nil {
		return
	}
	record := &AuditLog{
		ID:        fmt.Sprintf("%d", idgen.New()),
		UserID:    userID,
		Tool:      tool,
		Arguments: args,
		Success:   success,
		Error:     errorMessage,
		CreatedAt: time.Now().UTC(),
	}
	_ = s.audit.SaveAudit(ctx, record)
}

func stringArg(args map[string]interface{}, key string) string {
	if args == nil {
		return ""
	}
	value, ok := args[key]
	if !ok || value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return typed
	case json.Number:
		return typed.String()
	default:
		return fmt.Sprint(typed)
	}
}

func intArg(args map[string]interface{}, key string, fallback int) int {
	raw := stringArg(args, key)
	if raw == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return parsed
}

type MemoryAuditStore struct {
	mu   sync.RWMutex
	logs []*AuditLog
}

func NewMemoryAuditStore() *MemoryAuditStore {
	return &MemoryAuditStore{}
}

func (s *MemoryAuditStore) SaveAudit(_ context.Context, record *AuditLog) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.logs = append(s.logs, cloneAudit(record))
	return nil
}

func (s *MemoryAuditStore) ListAudit(_ context.Context, limit int) ([]*AuditLog, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	items := make([]*AuditLog, 0, limit)
	for i := len(s.logs) - 1; i >= 0 && len(items) < limit; i-- {
		items = append(items, cloneAudit(s.logs[i]))
	}
	return items, nil
}

func (s *MemoryAuditStore) CountAudit(_ context.Context) (int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return int64(len(s.logs)), nil
}

type PgAuditStore struct {
	pool *pgxpool.Pool
}

func NewPgAuditStore(pool *pgxpool.Pool) *PgAuditStore {
	return &PgAuditStore{pool: pool}
}

func (s *PgAuditStore) SaveAudit(ctx context.Context, record *AuditLog) error {
	argsJSON, err := json.Marshal(record.Arguments)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `INSERT INTO mcp_audit_logs (id, user_id, tool, arguments, success, error, created_at)
		VALUES ($1, $2, $3, $4::jsonb, $5, $6, $7)`,
		record.ID, record.UserID, record.Tool, string(argsJSON), record.Success, record.Error, record.CreatedAt)
	return err
}

func (s *PgAuditStore) ListAudit(ctx context.Context, limit int) ([]*AuditLog, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.pool.Query(ctx, `SELECT id, user_id, tool, arguments, success, error, created_at
		FROM mcp_audit_logs ORDER BY created_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]*AuditLog, 0)
	for rows.Next() {
		record := &AuditLog{}
		var argsJSON []byte
		if err := rows.Scan(&record.ID, &record.UserID, &record.Tool, &argsJSON,
			&record.Success, &record.Error, &record.CreatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(argsJSON, &record.Arguments)
		if record.Arguments == nil {
			record.Arguments = map[string]interface{}{}
		}
		items = append(items, record)
	}
	return items, rows.Err()
}

func (s *PgAuditStore) CountAudit(ctx context.Context) (int64, error) {
	var total int64
	err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM mcp_audit_logs`).Scan(&total)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	return total, err
}

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) ListTools(c *gin.Context) {
	response.Success(c, gin.H{"enabled": h.svc.IsEnabled(), "items": h.svc.ListTools()})
}

func (h *Handler) CallTool(c *gin.Context) {
	var req struct {
		Arguments map[string]interface{} `json:"arguments"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, 71002, "invalid request: "+err.Error())
		return
	}
	result, err := h.svc.CallTool(c.Request.Context(), currentUserID(c), c.Param("name"), req.Arguments)
	if err != nil {
		writeMCPError(c, err)
		return
	}
	response.Success(c, gin.H{"result": result})
}

func (h *Handler) ListAudit(c *gin.Context) {
	limit := intArg(map[string]interface{}{"limit": c.DefaultQuery("limit", "100")}, "limit", 100)
	items, err := h.svc.ListAudit(c.Request.Context(), limit)
	if err != nil {
		writeMCPError(c, err)
		return
	}
	response.Success(c, gin.H{"items": items, "total": len(items)})
}

func (h *Handler) GetSettings(c *gin.Context) {
	summary, err := h.svc.Summary(c.Request.Context())
	if err != nil {
		writeMCPError(c, err)
		return
	}
	response.Success(c, summary)
}

func (h *Handler) UpdateSettings(c *gin.Context) {
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, 71002, "invalid request: "+err.Error())
		return
	}
	h.svc.SetEnabled(req.Enabled)
	response.Success(c, gin.H{"enabled": req.Enabled})
}

func currentUserID(c *gin.Context) string {
	value, ok := c.Get("user_id")
	if !ok {
		return ""
	}
	userID, _ := value.(string)
	return userID
}

func writeMCPError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrToolNotFound):
		response.Error(c, http.StatusNotFound, 71004, err.Error())
	case errors.Is(err, ErrServiceDisabled):
		response.Error(c, http.StatusForbidden, 71003, err.Error())
	default:
		response.Error(c, http.StatusInternalServerError, 71001, err.Error())
	}
}

func cloneAudit(record *AuditLog) *AuditLog {
	if record == nil {
		return nil
	}
	clone := *record
	if record.Arguments != nil {
		clone.Arguments = make(map[string]interface{}, len(record.Arguments))
		for key, value := range record.Arguments {
			clone.Arguments[key] = value
		}
	}
	return &clone
}
