package message

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/campusos/CampusOS/pkg/idgen"
	"github.com/campusos/CampusOS/pkg/observability"
	"github.com/campusos/CampusOS/pkg/response"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Sender struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name,omitempty"`
	UserID      string `json:"user_id,omitempty"`
}

type Message struct {
	ID             string                 `json:"id"`
	Platform       string                 `json:"platform"`
	ConversationID string                 `json:"conversation_id"`
	Sender         Sender                 `json:"sender"`
	Direction      string                 `json:"direction"`
	Type           string                 `json:"type"`
	Content        string                 `json:"content"`
	RawPayload     map[string]interface{} `json:"raw_payload,omitempty"`
	CreatedAt      time.Time              `json:"created_at"`
}

type Binding struct {
	ID             string    `json:"id"`
	UserID         string    `json:"user_id"`
	Platform       string    `json:"platform"`
	ExternalUserID string    `json:"external_user_id"`
	DisplayName    string    `json:"display_name"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type Adapter struct {
	Name        string `json:"name"`
	Platform    string `json:"platform"`
	Enabled     bool   `json:"enabled"`
	Description string `json:"description"`
}

type Result struct {
	Inbound  *Message `json:"inbound"`
	Outbound *Message `json:"outbound,omitempty"`
	Handled  bool     `json:"handled"`
}

type Summary struct {
	AdapterTotal int64 `json:"adapter_total"`
	MessageTotal int64 `json:"message_total"`
	BindingTotal int64 `json:"binding_total"`
}

type Store interface {
	SaveMessage(ctx context.Context, msg *Message) error
	ListMessages(ctx context.Context, limit int) ([]*Message, error)
	SaveBinding(ctx context.Context, binding *Binding) error
	CountBindings(ctx context.Context) (int64, error)
	CountMessages(ctx context.Context) (int64, error)
}

type Service struct {
	store   Store
	metrics *observability.Collector
}

func NewService(store Store, metrics *observability.Collector) *Service {
	return &Service{store: store, metrics: metrics}
}

func (s *Service) Adapters() []Adapter {
	return []Adapter{{
		Name:        "local",
		Platform:    "local",
		Enabled:     true,
		Description: "本地测试适配器，用于验证 CampusOS Message 协议收发闭环",
	}}
}

func (s *Service) Summary(ctx context.Context) (*Summary, error) {
	messageTotal, err := s.store.CountMessages(ctx)
	if err != nil {
		return nil, err
	}
	bindingTotal, err := s.store.CountBindings(ctx)
	if err != nil {
		return nil, err
	}
	return &Summary{AdapterTotal: int64(len(s.Adapters())), MessageTotal: messageTotal, BindingTotal: bindingTotal}, nil
}

func (s *Service) ReceiveLocal(ctx context.Context, msg *Message) (*Result, error) {
	if msg == nil {
		return nil, errors.New("message is required")
	}
	now := time.Now().UTC()
	if msg.ID == "" {
		msg.ID = fmt.Sprintf("%d", idgen.New())
	}
	msg.Platform = "local"
	if msg.Direction == "" {
		msg.Direction = "inbound"
	}
	if msg.Type == "" {
		msg.Type = "text"
	}
	if msg.CreatedAt.IsZero() {
		msg.CreatedAt = now
	}
	if msg.RawPayload == nil {
		msg.RawPayload = map[string]interface{}{}
	}
	if err := s.store.SaveMessage(ctx, msg); err != nil {
		if s.metrics != nil {
			s.metrics.RecordExternal("message.local", false)
		}
		return nil, err
	}

	result := &Result{Inbound: msg, Handled: true}
	if reply := localReply(msg); reply != "" {
		outbound := &Message{
			ID:             fmt.Sprintf("%d", idgen.New()),
			Platform:       "local",
			ConversationID: msg.ConversationID,
			Sender:         Sender{ID: "campusos", DisplayName: "CampusOS"},
			Direction:      "outbound",
			Type:           "text",
			Content:        reply,
			RawPayload:     map[string]interface{}{"reply_to": msg.ID},
			CreatedAt:      time.Now().UTC(),
		}
		if err := s.store.SaveMessage(ctx, outbound); err != nil {
			if s.metrics != nil {
				s.metrics.RecordExternal("message.local", false)
			}
			return nil, err
		}
		result.Outbound = outbound
	}
	if s.metrics != nil {
		s.metrics.RecordExternal("message.local", true)
	}
	return result, nil
}

func (s *Service) ListMessages(ctx context.Context, limit int) ([]*Message, error) {
	return s.store.ListMessages(ctx, limit)
}

func (s *Service) SaveBinding(ctx context.Context, binding *Binding) error {
	if binding.ID == "" {
		binding.ID = fmt.Sprintf("%d", idgen.New())
	}
	now := time.Now().UTC()
	if binding.CreatedAt.IsZero() {
		binding.CreatedAt = now
	}
	binding.UpdatedAt = now
	return s.store.SaveBinding(ctx, binding)
}

func localReply(msg *Message) string {
	switch msg.Content {
	case "/ping", "ping":
		return "pong"
	case "/help", "help":
		return "CampusOS local adapter supports ping and help."
	default:
		return ""
	}
}

type MemoryStore struct {
	mu       sync.RWMutex
	messages []*Message
	bindings map[string]*Binding
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{bindings: make(map[string]*Binding)}
}

func (s *MemoryStore) SaveMessage(_ context.Context, msg *Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages = append(s.messages, cloneMessage(msg))
	return nil
}

func (s *MemoryStore) ListMessages(_ context.Context, limit int) ([]*Message, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	items := make([]*Message, 0, limit)
	for i := len(s.messages) - 1; i >= 0 && len(items) < limit; i-- {
		items = append(items, cloneMessage(s.messages[i]))
	}
	return items, nil
}

func (s *MemoryStore) SaveBinding(_ context.Context, binding *Binding) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bindings[binding.Platform+":"+binding.ExternalUserID] = cloneBinding(binding)
	return nil
}

func (s *MemoryStore) CountBindings(_ context.Context) (int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return int64(len(s.bindings)), nil
}

func (s *MemoryStore) CountMessages(_ context.Context) (int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return int64(len(s.messages)), nil
}

type PgStore struct {
	pool *pgxpool.Pool
}

func NewPgStore(pool *pgxpool.Pool) *PgStore {
	return &PgStore{pool: pool}
}

func (s *PgStore) SaveMessage(ctx context.Context, msg *Message) error {
	rawJSON, err := json.Marshal(msg.RawPayload)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `INSERT INTO message_logs (
			id, platform, conversation_id, sender_id, direction, message_type, content, raw_payload, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb, $9)`,
		msg.ID, msg.Platform, msg.ConversationID, msg.Sender.ID, msg.Direction, msg.Type,
		msg.Content, string(rawJSON), msg.CreatedAt)
	return err
}

func (s *PgStore) ListMessages(ctx context.Context, limit int) ([]*Message, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.pool.Query(ctx, `SELECT id, platform, conversation_id, sender_id, direction, message_type, content, raw_payload, created_at
		FROM message_logs ORDER BY created_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]*Message, 0)
	for rows.Next() {
		msg := &Message{}
		var rawJSON []byte
		if err := rows.Scan(&msg.ID, &msg.Platform, &msg.ConversationID, &msg.Sender.ID,
			&msg.Direction, &msg.Type, &msg.Content, &rawJSON, &msg.CreatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(rawJSON, &msg.RawPayload)
		if msg.RawPayload == nil {
			msg.RawPayload = map[string]interface{}{}
		}
		items = append(items, msg)
	}
	return items, rows.Err()
}

func (s *PgStore) SaveBinding(ctx context.Context, binding *Binding) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO message_bindings (
			id, user_id, platform, external_user_id, display_name, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (platform, external_user_id) WHERE deleted_at IS NULL
		DO UPDATE SET user_id = EXCLUDED.user_id, display_name = EXCLUDED.display_name, updated_at = EXCLUDED.updated_at`,
		binding.ID, binding.UserID, binding.Platform, binding.ExternalUserID,
		binding.DisplayName, binding.CreatedAt, binding.UpdatedAt)
	return err
}

func (s *PgStore) CountBindings(ctx context.Context) (int64, error) {
	var total int64
	err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM message_bindings WHERE deleted_at IS NULL`).Scan(&total)
	return total, err
}

func (s *PgStore) CountMessages(ctx context.Context) (int64, error) {
	var total int64
	err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM message_logs`).Scan(&total)
	return total, err
}

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) ListAdapters(c *gin.Context) {
	response.Success(c, gin.H{"items": h.svc.Adapters(), "total": len(h.svc.Adapters())})
}

func (h *Handler) ReceiveLocal(c *gin.Context) {
	var req struct {
		ConversationID string                 `json:"conversation_id"`
		Sender         Sender                 `json:"sender"`
		Content        string                 `json:"content" binding:"required"`
		RawPayload     map[string]interface{} `json:"raw_payload"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, 72002, "invalid request: "+err.Error())
		return
	}
	result, err := h.svc.ReceiveLocal(c.Request.Context(), &Message{
		ConversationID: req.ConversationID,
		Sender:         req.Sender,
		Content:        req.Content,
		RawPayload:     req.RawPayload,
	})
	if err != nil {
		response.Error(c, http.StatusInternalServerError, 72001, err.Error())
		return
	}
	response.Success(c, result)
}

func (h *Handler) ListMessages(c *gin.Context) {
	limit := 100
	if raw := c.Query("limit"); raw != "" {
		if parsed, err := fmt.Sscanf(raw, "%d", &limit); err != nil || parsed != 1 {
			limit = 100
		}
	}
	items, err := h.svc.ListMessages(c.Request.Context(), limit)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, 72001, err.Error())
		return
	}
	response.Success(c, gin.H{"items": items, "total": len(items)})
}

func (h *Handler) CreateBinding(c *gin.Context) {
	var req Binding
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, 72002, "invalid request: "+err.Error())
		return
	}
	if err := h.svc.SaveBinding(c.Request.Context(), &req); err != nil {
		response.Error(c, http.StatusInternalServerError, 72001, err.Error())
		return
	}
	response.Created(c, req)
}

func (h *Handler) Summary(c *gin.Context) {
	summary, err := h.svc.Summary(c.Request.Context())
	if err != nil {
		response.Error(c, http.StatusInternalServerError, 72001, err.Error())
		return
	}
	response.Success(c, summary)
}

func cloneMessage(msg *Message) *Message {
	if msg == nil {
		return nil
	}
	clone := *msg
	if msg.RawPayload != nil {
		clone.RawPayload = make(map[string]interface{}, len(msg.RawPayload))
		for key, value := range msg.RawPayload {
			clone.RawPayload[key] = value
		}
	}
	return &clone
}

func cloneBinding(binding *Binding) *Binding {
	if binding == nil {
		return nil
	}
	clone := *binding
	return &clone
}
