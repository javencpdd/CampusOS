package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/campusos/CampusOS/pkg/eventbus"
	"github.com/campusos/CampusOS/pkg/idgen"
	"github.com/campusos/CampusOS/pkg/observability"
	"github.com/campusos/CampusOS/pkg/response"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrEndpointNotFound = errors.New("webhook endpoint not found")

type Endpoint struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	URL        string    `json:"url"`
	Secret     string    `json:"-"`
	Events     []string  `json:"events"`
	Enabled    bool      `json:"enabled"`
	MaxRetries int       `json:"max_retries"`
	TimeoutMS  int       `json:"timeout_ms"`
	CreatedBy  string    `json:"created_by"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type Delivery struct {
	ID             string    `json:"id"`
	EndpointID     string    `json:"endpoint_id"`
	EventID        string    `json:"event_id"`
	EventType      string    `json:"event_type"`
	TargetURL      string    `json:"target_url"`
	Status         string    `json:"status"`
	Attempts       int       `json:"attempts"`
	ResponseStatus int       `json:"response_status"`
	ErrorMessage   string    `json:"error_message,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type Summary struct {
	EndpointTotal    int64 `json:"endpoint_total"`
	EnabledTotal     int64 `json:"enabled_total"`
	DeliveryTotal    int64 `json:"delivery_total"`
	SuccessTotal     int64 `json:"success_total"`
	FailedTotal      int64 `json:"failed_total"`
	LastFailureTotal int64 `json:"last_failure_total"`
}

type Store interface {
	CreateEndpoint(ctx context.Context, endpoint *Endpoint) error
	ListEndpoints(ctx context.Context) ([]*Endpoint, error)
	GetEndpoint(ctx context.Context, id string) (*Endpoint, error)
	UpdateEndpointEnabled(ctx context.Context, id string, enabled bool) error
	SaveDelivery(ctx context.Context, delivery *Delivery) error
	ListDeliveries(ctx context.Context, endpointID string, limit int) ([]*Delivery, error)
	Summary(ctx context.Context) (*Summary, error)
}

type Service struct {
	store   Store
	metrics *observability.Collector
}

func NewService(store Store, metrics *observability.Collector) *Service {
	return &Service{store: store, metrics: metrics}
}

func (s *Service) Register(bus eventbus.EventBus) error {
	if bus == nil || s == nil {
		return nil
	}
	for _, eventType := range []string{
		eventbus.EventUserCreated,
		eventbus.EventThreadCreated,
		eventbus.EventThreadUpdated,
		eventbus.EventThreadDeleted,
		eventbus.EventPostCreated,
		eventbus.EventCategoryCreated,
	} {
		if err := bus.Subscribe(eventType, func(ctx context.Context, event eventbus.Event) error {
			go s.DeliverEvent(context.Background(), event)
			return nil
		}); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) CreateEndpoint(ctx context.Context, endpoint *Endpoint) error {
	if endpoint == nil {
		return errors.New("endpoint is required")
	}
	endpoint.Name = strings.TrimSpace(endpoint.Name)
	endpoint.URL = strings.TrimSpace(endpoint.URL)
	if endpoint.Name == "" {
		return errors.New("name is required")
	}
	if err := validateURL(endpoint.URL); err != nil {
		return err
	}
	if endpoint.ID == "" {
		endpoint.ID = fmt.Sprintf("%d", idgen.New())
	}
	if endpoint.MaxRetries < 0 {
		endpoint.MaxRetries = 0
	}
	if endpoint.MaxRetries == 0 {
		endpoint.MaxRetries = 2
	}
	if endpoint.TimeoutMS <= 0 {
		endpoint.TimeoutMS = 5000
	}
	if endpoint.TimeoutMS > 30000 {
		endpoint.TimeoutMS = 30000
	}
	now := time.Now().UTC()
	if endpoint.CreatedAt.IsZero() {
		endpoint.CreatedAt = now
	}
	endpoint.UpdatedAt = now
	endpoint.Events = normalizeEvents(endpoint.Events)
	return s.store.CreateEndpoint(ctx, endpoint)
}

func (s *Service) ListEndpoints(ctx context.Context) ([]*Endpoint, error) {
	return s.store.ListEndpoints(ctx)
}

func (s *Service) Summary(ctx context.Context) (*Summary, error) {
	return s.store.Summary(ctx)
}

func (s *Service) SetEnabled(ctx context.Context, id string, enabled bool) error {
	return s.store.UpdateEndpointEnabled(ctx, id, enabled)
}

func (s *Service) ListDeliveries(ctx context.Context, endpointID string, limit int) ([]*Delivery, error) {
	return s.store.ListDeliveries(ctx, endpointID, limit)
}

func (s *Service) TestEndpoint(ctx context.Context, endpointID string) (*Delivery, error) {
	endpoint, err := s.store.GetEndpoint(ctx, endpointID)
	if err != nil {
		return nil, err
	}
	event := eventbus.NewEvent("webhook.test", "campusos.webhook", "webhook."+endpointID, map[string]interface{}{
		"message": "CampusOS webhook test event",
	})
	return s.deliverToEndpoint(ctx, endpoint, event)
}

func (s *Service) DeliverEvent(ctx context.Context, event eventbus.Event) {
	endpoints, err := s.store.ListEndpoints(ctx)
	if err != nil {
		return
	}
	for _, endpoint := range endpoints {
		if endpoint.Enabled && endpointMatches(endpoint, event.Type) {
			_, _ = s.deliverToEndpoint(ctx, endpoint, event)
		}
	}
}

func (s *Service) deliverToEndpoint(ctx context.Context, endpoint *Endpoint, event eventbus.Event) (*Delivery, error) {
	now := time.Now().UTC()
	delivery := &Delivery{
		ID:         fmt.Sprintf("%d", idgen.New()),
		EndpointID: endpoint.ID,
		EventID:    event.ID,
		EventType:  event.Type,
		TargetURL:  endpoint.URL,
		Status:     "pending",
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	body, err := json.Marshal(event)
	if err != nil {
		delivery.Status = "failed"
		delivery.ErrorMessage = err.Error()
		_ = s.store.SaveDelivery(ctx, delivery)
		return delivery, err
	}

	attempts := endpoint.MaxRetries + 1
	var lastErr error
	for i := 0; i < attempts; i++ {
		delivery.Attempts = i + 1
		statusCode, err := postSignedJSON(ctx, endpoint, body)
		delivery.ResponseStatus = statusCode
		if err == nil && statusCode >= 200 && statusCode < 300 {
			delivery.Status = "success"
			delivery.ErrorMessage = ""
			delivery.UpdatedAt = time.Now().UTC()
			_ = s.store.SaveDelivery(ctx, delivery)
			if s.metrics != nil {
				s.metrics.RecordExternal("webhook.delivery", true)
			}
			return delivery, nil
		}
		if err != nil {
			lastErr = err
		} else {
			lastErr = fmt.Errorf("unexpected status %d", statusCode)
		}
		time.Sleep(time.Duration(i+1) * 100 * time.Millisecond)
	}
	delivery.Status = "failed"
	if lastErr != nil {
		delivery.ErrorMessage = lastErr.Error()
	}
	delivery.UpdatedAt = time.Now().UTC()
	_ = s.store.SaveDelivery(ctx, delivery)
	if s.metrics != nil {
		s.metrics.RecordExternal("webhook.delivery", false)
	}
	return delivery, lastErr
}

func SignPayload(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func postSignedJSON(ctx context.Context, endpoint *Endpoint, body []byte) (int, error) {
	timeout := time.Duration(endpoint.TimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	client := &http.Client{Timeout: timeout}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.URL, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "CampusOS-Webhook/0.5")
	if endpoint.Secret != "" {
		req.Header.Set("X-CampusOS-Signature", SignPayload(endpoint.Secret, body))
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	return resp.StatusCode, nil
}

func endpointMatches(endpoint *Endpoint, eventType string) bool {
	if len(endpoint.Events) == 0 {
		return true
	}
	for _, value := range endpoint.Events {
		if value == "*" || value == eventType {
			return true
		}
	}
	return false
}

func normalizeEvents(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	events := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		events = append(events, value)
	}
	return events
}

func validateURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil {
		return err
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return errors.New("webhook url must use http or https")
	}
	if parsed.Host == "" {
		return errors.New("webhook url host is required")
	}
	return nil
}

type MemoryStore struct {
	mu         sync.RWMutex
	endpoints  map[string]*Endpoint
	deliveries []*Delivery
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{endpoints: make(map[string]*Endpoint)}
}

func (s *MemoryStore) CreateEndpoint(_ context.Context, endpoint *Endpoint) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.endpoints[endpoint.ID] = cloneEndpoint(endpoint)
	return nil
}

func (s *MemoryStore) ListEndpoints(_ context.Context) ([]*Endpoint, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]*Endpoint, 0, len(s.endpoints))
	for _, endpoint := range s.endpoints {
		items = append(items, cloneEndpoint(endpoint))
	}
	return items, nil
}

func (s *MemoryStore) GetEndpoint(_ context.Context, id string) (*Endpoint, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	endpoint, ok := s.endpoints[id]
	if !ok {
		return nil, ErrEndpointNotFound
	}
	return cloneEndpoint(endpoint), nil
}

func (s *MemoryStore) UpdateEndpointEnabled(_ context.Context, id string, enabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	endpoint, ok := s.endpoints[id]
	if !ok {
		return ErrEndpointNotFound
	}
	endpoint.Enabled = enabled
	endpoint.UpdatedAt = time.Now().UTC()
	return nil
}

func (s *MemoryStore) SaveDelivery(_ context.Context, delivery *Delivery) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deliveries = append(s.deliveries, cloneDelivery(delivery))
	return nil
}

func (s *MemoryStore) ListDeliveries(_ context.Context, endpointID string, limit int) ([]*Delivery, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if limit <= 0 {
		limit = 100
	}
	items := make([]*Delivery, 0, limit)
	for i := len(s.deliveries) - 1; i >= 0 && len(items) < limit; i-- {
		delivery := s.deliveries[i]
		if endpointID != "" && delivery.EndpointID != endpointID {
			continue
		}
		items = append(items, cloneDelivery(delivery))
	}
	return items, nil
}

func (s *MemoryStore) Summary(_ context.Context) (*Summary, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	summary := &Summary{EndpointTotal: int64(len(s.endpoints)), DeliveryTotal: int64(len(s.deliveries))}
	for _, endpoint := range s.endpoints {
		if endpoint.Enabled {
			summary.EnabledTotal++
		}
	}
	for _, delivery := range s.deliveries {
		if delivery.Status == "success" {
			summary.SuccessTotal++
		}
		if delivery.Status == "failed" {
			summary.FailedTotal++
		}
	}
	summary.LastFailureTotal = summary.FailedTotal
	return summary, nil
}

type PgStore struct {
	pool *pgxpool.Pool
}

func NewPgStore(pool *pgxpool.Pool) *PgStore {
	return &PgStore{pool: pool}
}

func (s *PgStore) CreateEndpoint(ctx context.Context, endpoint *Endpoint) error {
	query := `INSERT INTO webhook_endpoints (
			id, name, url, secret, events, enabled, max_retries, timeout_ms, created_by, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`
	_, err := s.pool.Exec(ctx, query,
		endpoint.ID, endpoint.Name, endpoint.URL, endpoint.Secret, endpoint.Events,
		endpoint.Enabled, endpoint.MaxRetries, endpoint.TimeoutMS, endpoint.CreatedBy,
		endpoint.CreatedAt, endpoint.UpdatedAt,
	)
	return err
}

func (s *PgStore) ListEndpoints(ctx context.Context) ([]*Endpoint, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, name, url, secret, events, enabled, max_retries, timeout_ms, created_by, created_at, updated_at
		FROM webhook_endpoints WHERE deleted_at IS NULL ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]*Endpoint, 0)
	for rows.Next() {
		endpoint := &Endpoint{}
		if err := rows.Scan(&endpoint.ID, &endpoint.Name, &endpoint.URL, &endpoint.Secret,
			&endpoint.Events, &endpoint.Enabled, &endpoint.MaxRetries, &endpoint.TimeoutMS,
			&endpoint.CreatedBy, &endpoint.CreatedAt, &endpoint.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, endpoint)
	}
	return items, rows.Err()
}

func (s *PgStore) GetEndpoint(ctx context.Context, id string) (*Endpoint, error) {
	endpoint := &Endpoint{}
	err := s.pool.QueryRow(ctx, `SELECT id, name, url, secret, events, enabled, max_retries, timeout_ms, created_by, created_at, updated_at
		FROM webhook_endpoints WHERE id = $1 AND deleted_at IS NULL`, id).Scan(
		&endpoint.ID, &endpoint.Name, &endpoint.URL, &endpoint.Secret, &endpoint.Events,
		&endpoint.Enabled, &endpoint.MaxRetries, &endpoint.TimeoutMS, &endpoint.CreatedBy,
		&endpoint.CreatedAt, &endpoint.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrEndpointNotFound
		}
		return nil, err
	}
	return endpoint, nil
}

func (s *PgStore) UpdateEndpointEnabled(ctx context.Context, id string, enabled bool) error {
	tag, err := s.pool.Exec(ctx, `UPDATE webhook_endpoints SET enabled = $2, updated_at = NOW()
		WHERE id = $1 AND deleted_at IS NULL`, id, enabled)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrEndpointNotFound
	}
	return nil
}

func (s *PgStore) SaveDelivery(ctx context.Context, delivery *Delivery) error {
	query := `INSERT INTO webhook_deliveries (
			id, endpoint_id, event_id, event_type, target_url, status, attempts, response_status,
			error_message, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`
	_, err := s.pool.Exec(ctx, query,
		delivery.ID, delivery.EndpointID, delivery.EventID, delivery.EventType, delivery.TargetURL,
		delivery.Status, delivery.Attempts, delivery.ResponseStatus, delivery.ErrorMessage,
		delivery.CreatedAt, delivery.UpdatedAt,
	)
	return err
}

func (s *PgStore) ListDeliveries(ctx context.Context, endpointID string, limit int) ([]*Delivery, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.pool.Query(ctx, `SELECT id, endpoint_id, event_id, event_type, target_url, status,
			attempts, response_status, error_message, created_at, updated_at
		FROM webhook_deliveries
		WHERE ($1 = '' OR endpoint_id = $1)
		ORDER BY created_at DESC
		LIMIT $2`, endpointID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]*Delivery, 0)
	for rows.Next() {
		delivery := &Delivery{}
		if err := rows.Scan(&delivery.ID, &delivery.EndpointID, &delivery.EventID, &delivery.EventType,
			&delivery.TargetURL, &delivery.Status, &delivery.Attempts, &delivery.ResponseStatus,
			&delivery.ErrorMessage, &delivery.CreatedAt, &delivery.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, delivery)
	}
	return items, rows.Err()
}

func (s *PgStore) Summary(ctx context.Context) (*Summary, error) {
	summary := &Summary{}
	err := s.pool.QueryRow(ctx, `SELECT
			(SELECT COUNT(*) FROM webhook_endpoints WHERE deleted_at IS NULL),
			(SELECT COUNT(*) FROM webhook_endpoints WHERE deleted_at IS NULL AND enabled = TRUE),
			(SELECT COUNT(*) FROM webhook_deliveries),
			(SELECT COUNT(*) FROM webhook_deliveries WHERE status = 'success'),
			(SELECT COUNT(*) FROM webhook_deliveries WHERE status = 'failed'),
			(SELECT COUNT(*) FROM webhook_deliveries WHERE status = 'failed' AND created_at > NOW() - INTERVAL '24 hours')`).Scan(
		&summary.EndpointTotal, &summary.EnabledTotal, &summary.DeliveryTotal,
		&summary.SuccessTotal, &summary.FailedTotal, &summary.LastFailureTotal,
	)
	return summary, err
}

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) ListEndpoints(c *gin.Context) {
	items, err := h.svc.ListEndpoints(c.Request.Context())
	if err != nil {
		response.Error(c, http.StatusInternalServerError, 70001, err.Error())
		return
	}
	response.Success(c, gin.H{"items": redactEndpoints(items), "total": len(items)})
}

func (h *Handler) CreateEndpoint(c *gin.Context) {
	var req struct {
		Name       string   `json:"name" binding:"required"`
		URL        string   `json:"url" binding:"required"`
		Secret     string   `json:"secret"`
		Events     []string `json:"events"`
		Enabled    *bool    `json:"enabled"`
		MaxRetries int      `json:"max_retries"`
		TimeoutMS  int      `json:"timeout_ms"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, 70002, "invalid request: "+err.Error())
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	endpoint := &Endpoint{
		Name:       req.Name,
		URL:        req.URL,
		Secret:     req.Secret,
		Events:     req.Events,
		Enabled:    enabled,
		MaxRetries: req.MaxRetries,
		TimeoutMS:  req.TimeoutMS,
		CreatedBy:  currentUserID(c),
	}
	if err := h.svc.CreateEndpoint(c.Request.Context(), endpoint); err != nil {
		response.Error(c, http.StatusBadRequest, 70002, err.Error())
		return
	}
	endpoint.Secret = ""
	response.Created(c, endpoint)
}

func (h *Handler) EnableEndpoint(c *gin.Context) {
	h.setEnabled(c, true)
}

func (h *Handler) DisableEndpoint(c *gin.Context) {
	h.setEnabled(c, false)
}

func (h *Handler) setEnabled(c *gin.Context, enabled bool) {
	if err := h.svc.SetEnabled(c.Request.Context(), c.Param("id"), enabled); err != nil {
		writeWebhookError(c, err)
		return
	}
	response.Success(c, gin.H{"enabled": enabled})
}

func (h *Handler) TestEndpoint(c *gin.Context) {
	delivery, err := h.svc.TestEndpoint(c.Request.Context(), c.Param("id"))
	if err != nil {
		writeWebhookError(c, err)
		return
	}
	response.Success(c, delivery)
}

func (h *Handler) ListDeliveries(c *gin.Context) {
	limit := 100
	if raw := c.Query("limit"); raw != "" {
		if parsed, err := fmt.Sscanf(raw, "%d", &limit); err != nil || parsed != 1 {
			limit = 100
		}
	}
	items, err := h.svc.ListDeliveries(c.Request.Context(), c.Param("id"), limit)
	if err != nil {
		writeWebhookError(c, err)
		return
	}
	response.Success(c, gin.H{"items": items, "total": len(items)})
}

func (h *Handler) Summary(c *gin.Context) {
	summary, err := h.svc.Summary(c.Request.Context())
	if err != nil {
		writeWebhookError(c, err)
		return
	}
	response.Success(c, summary)
}

func currentUserID(c *gin.Context) string {
	value, ok := c.Get("user_id")
	if !ok {
		return ""
	}
	userID, _ := value.(string)
	return userID
}

func writeWebhookError(c *gin.Context, err error) {
	if errors.Is(err, ErrEndpointNotFound) {
		response.Error(c, http.StatusNotFound, 70004, err.Error())
		return
	}
	response.Error(c, http.StatusInternalServerError, 70001, err.Error())
}

func redactEndpoints(items []*Endpoint) []*Endpoint {
	result := make([]*Endpoint, 0, len(items))
	for _, endpoint := range items {
		clone := cloneEndpoint(endpoint)
		clone.Secret = ""
		result = append(result, clone)
	}
	return result
}

func cloneEndpoint(endpoint *Endpoint) *Endpoint {
	if endpoint == nil {
		return nil
	}
	clone := *endpoint
	clone.Events = append([]string(nil), endpoint.Events...)
	return &clone
}

func cloneDelivery(delivery *Delivery) *Delivery {
	if delivery == nil {
		return nil
	}
	clone := *delivery
	return &clone
}
