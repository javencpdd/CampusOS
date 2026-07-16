package reliability

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/campusos/CampusOS/pkg/response"
	"github.com/gin-gonic/gin"
)

// Handler is deliberately restricted to platform administrators by router
// descriptors. It exposes observability and explicit replay, never arbitrary
// mutation of business data.
type Handler struct {
	service      *Service
	queryLimiter *queryLimiter
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service, queryLimiter: newQueryLimiter(120, time.Minute)}
}

// commandAuditResponse deliberately excludes Details. Command evidence is
// useful to administrators, but arbitrary command metadata must not turn the
// operations endpoint into a payload or secret inspection API.
type commandAuditResponse struct {
	ID             string    `json:"id"`
	CommandID      string    `json:"command_id"`
	CommandCode    string    `json:"command_code"`
	ActorID        string    `json:"actor_id,omitempty"`
	ActorType      string    `json:"actor_type,omitempty"`
	ResourceType   string    `json:"resource_type,omitempty"`
	ResourceID     string    `json:"resource_id,omitempty"`
	OperationCode  string    `json:"operation_code,omitempty"`
	PermissionCode string    `json:"permission_code,omitempty"`
	RequestID      string    `json:"request_id,omitempty"`
	TraceID        string    `json:"trace_id,omitempty"`
	EventID        string    `json:"event_id,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

type eventResponse struct {
	ID              string     `json:"id"`
	Type            string     `json:"type"`
	SchemaVersion   string     `json:"schema_version"`
	AggregateType   string     `json:"aggregate_type,omitempty"`
	AggregateID     string     `json:"aggregate_id,omitempty"`
	Status          string     `json:"status"`
	Attempts        int        `json:"attempts"`
	MaxAttempts     int        `json:"max_attempts"`
	AvailableAt     time.Time  `json:"available_at"`
	LeaseOwner      string     `json:"lease_owner,omitempty"`
	LeaseUntil      *time.Time `json:"lease_until,omitempty"`
	LeaseGeneration int64      `json:"lease_generation"`
	LastError       string     `json:"last_error,omitempty"`
	DeadLetteredAt  *time.Time `json:"dead_lettered_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

type operationResponse struct {
	ID          string    `json:"id"`
	Kind        string    `json:"kind"`
	SubjectType string    `json:"subject_type"`
	SubjectID   string    `json:"subject_id"`
	Status      string    `json:"status"`
	ActorID     string    `json:"actor_id,omitempty"`
	Error       string    `json:"error,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type compatibilityResponse struct {
	Key       string    `json:"key"`
	Kind      string    `json:"kind"`
	FirstSeen time.Time `json:"first_seen"`
	LastSeen  time.Time `json:"last_seen"`
	Count     int64     `json:"count"`
}

func (h *Handler) Summary(c *gin.Context) {
	if !h.allowQuery(c) {
		return
	}
	summary, err := h.service.Summary(c.Request.Context())
	if err != nil {
		response.Error(c, http.StatusInternalServerError, 89101, err.Error())
		return
	}
	response.Success(c, summary)
}

func (h *Handler) ListEvents(c *gin.Context) {
	if !h.allowQuery(c) {
		return
	}
	page, ok := parseReliabilityPage(c, 50, 100)
	if !ok {
		return
	}
	items, total, err := h.service.ListPage(c.Request.Context(), EventFilter{
		Status: strings.TrimSpace(c.Query("status")),
		Type:   strings.TrimSpace(c.Query("type")),
	}, page)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, 89102, err.Error())
		return
	}
	result := make([]eventResponse, 0, len(items))
	for _, item := range items {
		result = append(result, safeEventResponse(item))
	}
	respondReliabilityList(c, result, total, page)
}

func (h *Handler) ListAttempts(c *gin.Context) {
	if !h.allowQuery(c) {
		return
	}
	page, ok := parseReliabilityPage(c, 50, 100)
	if !ok {
		return
	}
	items, total, err := h.service.ListAttemptsPage(c.Request.Context(), strings.TrimSpace(c.Query("event_id")), page)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, 89111, err.Error())
		return
	}
	respondReliabilityList(c, items, total, page)
}

func (h *Handler) Replay(c *gin.Context) {
	actorValue, _ := c.Get("user_id")
	actorID, _ := actorValue.(string)
	traceValue, _ := c.Get("trace_id")
	requestID, _ := traceValue.(string)
	event, err := h.service.ReplayCommand(c.Request.Context(), c.Param("id"), ReplayRequest{
		ActorID: actorID, RequestID: requestID, IdempotencyKey: c.GetHeader("Idempotency-Key"),
	})
	if err != nil {
		status := http.StatusInternalServerError
		switch {
		case errors.Is(err, ErrEventNotFound):
			status = http.StatusNotFound
		case errors.Is(err, ErrEventNotReplayable), errors.Is(err, ErrReplayIdempotencyKeyRequired), errors.Is(err, ErrReplayAlreadyRequested):
			status = http.StatusConflict
		case strings.Contains(err.Error(), "replay actor is required"):
			status = http.StatusUnauthorized
		}
		response.Error(c, status, 89103, err.Error())
		return
	}
	response.Success(c, safeEventResponse(*event))
}

func (h *Handler) ListWorkers(c *gin.Context) {
	if !h.allowQuery(c) {
		return
	}
	page, ok := parseReliabilityPage(c, 25, 100)
	if !ok {
		return
	}
	items, total, err := h.service.ListWorkersPage(c.Request.Context(), page)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, 89107, err.Error())
		return
	}
	respondReliabilityList(c, items, total, page)
}

func (h *Handler) ListOperations(c *gin.Context) {
	if !h.allowQuery(c) {
		return
	}
	page, ok := parseReliabilityPage(c, 25, 100)
	if !ok {
		return
	}
	items, total, err := h.service.ListOperationsPage(c.Request.Context(), strings.TrimSpace(c.Query("kind")), page)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, 89108, err.Error())
		return
	}
	result := make([]operationResponse, 0, len(items))
	for _, item := range items {
		result = append(result, operationResponse{
			ID: item.ID, Kind: item.Kind, SubjectType: item.SubjectType, SubjectID: item.SubjectID,
			Status: item.Status, ActorID: item.ActorID, Error: item.Error,
			CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt,
		})
	}
	respondReliabilityList(c, result, total, page)
}

func (h *Handler) ListCommandAudits(c *gin.Context) {
	if !h.allowQuery(c) {
		return
	}
	page, ok := parseReliabilityPage(c, 25, 100)
	if !ok {
		return
	}
	items, total, err := h.service.ListCommandAuditsPage(c.Request.Context(), page)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, 89112, err.Error())
		return
	}
	result := make([]commandAuditResponse, 0, len(items))
	for _, item := range items {
		result = append(result, commandAuditResponse{
			ID: item.ID, CommandID: item.CommandID, CommandCode: item.CommandCode,
			ActorID: item.ActorID, ActorType: item.ActorType, ResourceType: item.ResourceType,
			ResourceID: item.ResourceID, OperationCode: item.OperationCode,
			PermissionCode: item.PermissionCode, RequestID: item.RequestID, TraceID: item.TraceID,
			EventID: item.EventID, CreatedAt: item.CreatedAt,
		})
	}
	respondReliabilityList(c, result, total, page)
}

func (h *Handler) ListCompatibility(c *gin.Context) {
	if !h.allowQuery(c) {
		return
	}
	page, ok := parseReliabilityPage(c, 25, 100)
	if !ok {
		return
	}
	items, total, err := h.service.ListCompatibilityPage(c.Request.Context(), page)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, 89105, err.Error())
		return
	}
	result := make([]compatibilityResponse, 0, len(items))
	for _, item := range items {
		result = append(result, compatibilityResponse{
			Key: item.Key, Kind: item.Kind, FirstSeen: item.FirstSeen, LastSeen: item.LastSeen, Count: item.Count,
		})
	}
	respondReliabilityList(c, result, total, page)
}

func (h *Handler) PreviewRetention(c *gin.Context) {
	if !h.allowQuery(c) {
		return
	}
	target := strings.TrimSpace(c.DefaultQuery("target", "outbox"))
	before := time.Now().UTC().Add(-30 * 24 * time.Hour)
	if raw := strings.TrimSpace(c.Query("before")); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			response.Error(c, http.StatusBadRequest, 89106, "before must use RFC3339")
			return
		}
		before = parsed
	}
	preview, err := h.service.PreviewRetention(c.Request.Context(), target, before)
	if err != nil {
		response.Error(c, http.StatusBadRequest, 89106, err.Error())
		return
	}
	response.Success(c, preview)
}

func (h *Handler) StartRetentionPreview(c *gin.Context) {
	target := strings.TrimSpace(c.DefaultQuery("target", "outbox"))
	before := time.Now().UTC().Add(-30 * 24 * time.Hour)
	if raw := strings.TrimSpace(c.Query("before")); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			response.Error(c, http.StatusBadRequest, 89109, "before must use RFC3339")
			return
		}
		before = parsed
	}
	run, err := h.service.StartRetentionPreview(c.Request.Context(), target, before)
	if err != nil {
		response.Error(c, http.StatusBadRequest, 89109, err.Error())
		return
	}
	response.Created(c, run)
}

func (h *Handler) ListRetentionRuns(c *gin.Context) {
	if !h.allowQuery(c) {
		return
	}
	page, ok := parseReliabilityPage(c, 25, 100)
	if !ok {
		return
	}
	items, total, err := h.service.ListRetentionRunsPage(c.Request.Context(), page)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, 89110, err.Error())
		return
	}
	respondReliabilityList(c, items, total, page)
}

func (h *Handler) allowQuery(c *gin.Context) bool {
	key := "ip:" + c.ClientIP()
	if value, ok := c.Get("user_id"); ok {
		if actorID, ok := value.(string); ok && strings.TrimSpace(actorID) != "" {
			key = "user:" + strings.TrimSpace(actorID)
		}
	}
	allowed, retryAfter := h.queryLimiter.Allow(key)
	if allowed {
		return true
	}
	seconds := int(retryAfter.Seconds())
	if seconds < 1 {
		seconds = 1
	}
	c.Header("Retry-After", strconv.Itoa(seconds))
	response.Error(c, http.StatusTooManyRequests, 89113, "reliability query rate limit exceeded")
	return false
}

func parseReliabilityPage(c *gin.Context, defaultSize, maximumSize int) (PageRequest, bool) {
	pageRaw := strings.TrimSpace(c.DefaultQuery("page", "1"))
	pageSizeRaw := strings.TrimSpace(c.Query("page_size"))
	if pageSizeRaw == "" {
		pageSizeRaw = strings.TrimSpace(c.Query("limit"))
	}
	if pageSizeRaw == "" {
		pageSizeRaw = strconv.Itoa(defaultSize)
	}
	page, pageErr := strconv.Atoi(pageRaw)
	pageSize, pageSizeErr := strconv.Atoi(pageSizeRaw)
	if pageErr != nil || page < 1 || pageSizeErr != nil || pageSize < 1 || pageSize > maximumSize || page > 1_000_000 {
		response.ErrorWithDetails(c, http.StatusBadRequest, 10001, "page and page_size must be bounded positive integers", gin.H{
			"page_minimum": 1, "page_maximum": 1_000_000, "page_size_minimum": 1, "page_size_maximum": maximumSize,
		})
		return PageRequest{}, false
	}
	return PageRequest{Page: page, PageSize: pageSize}, true
}

func respondReliabilityList(c *gin.Context, items any, total int64, page PageRequest) {
	totalPages := int(total) / page.PageSize
	if int(total)%page.PageSize != 0 {
		totalPages++
	}
	response.Success(c, gin.H{
		"items": items,
		"total": total,
		"pagination": response.Pagination{
			Page: page.Page, PageSize: page.PageSize, Total: total, TotalPages: totalPages,
		},
	})
}

func safeEventResponse(item Event) eventResponse {
	return eventResponse{
		ID: item.ID, Type: item.Type, SchemaVersion: item.SchemaVersion,
		AggregateType: item.AggregateType, AggregateID: item.AggregateID, Status: item.Status,
		Attempts: item.Attempts, MaxAttempts: item.MaxAttempts, AvailableAt: item.AvailableAt,
		LeaseOwner: item.LeaseOwner, LeaseUntil: item.LeaseUntil, LeaseGeneration: item.LeaseGeneration,
		LastError: item.LastError, DeadLetteredAt: item.DeadLetteredAt,
		CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt,
	}
}
