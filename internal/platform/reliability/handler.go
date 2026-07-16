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
	service *Service
}

func NewHandler(service *Service) *Handler { return &Handler{service: service} }

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

func (h *Handler) Summary(c *gin.Context) {
	summary, err := h.service.Summary(c.Request.Context())
	if err != nil {
		response.Error(c, http.StatusInternalServerError, 89101, err.Error())
		return
	}
	response.Success(c, summary)
}

func (h *Handler) ListEvents(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "100"))
	items, err := h.service.List(c.Request.Context(), EventFilter{
		Status: strings.TrimSpace(c.Query("status")),
		Type:   strings.TrimSpace(c.Query("type")),
		Limit:  limit,
	})
	if err != nil {
		response.Error(c, http.StatusInternalServerError, 89102, err.Error())
		return
	}
	response.Success(c, gin.H{"items": items, "total": len(items)})
}

func (h *Handler) ListAttempts(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "100"))
	items, err := h.service.ListAttempts(c.Request.Context(), strings.TrimSpace(c.Query("event_id")), limit)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, 89111, err.Error())
		return
	}
	response.Success(c, gin.H{"items": items, "total": len(items)})
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
	response.Success(c, event)
}

func (h *Handler) ListWorkers(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	items, err := h.service.ListWorkers(c.Request.Context(), limit)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, 89107, err.Error())
		return
	}
	response.Success(c, gin.H{"items": items, "total": len(items)})
}

func (h *Handler) ListOperations(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	items, err := h.service.ListOperations(c.Request.Context(), strings.TrimSpace(c.Query("kind")), limit)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, 89108, err.Error())
		return
	}
	response.Success(c, gin.H{"items": items, "total": len(items)})
}

func (h *Handler) ListCommandAudits(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	items, err := h.service.ListCommandAudits(c.Request.Context(), limit)
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
	response.Success(c, gin.H{"items": result, "total": len(result)})
}

func (h *Handler) ListCompatibility(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	items, err := h.service.ListCompatibility(c.Request.Context(), limit)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, 89105, err.Error())
		return
	}
	response.Success(c, gin.H{"items": items, "total": len(items)})
}

func (h *Handler) PreviewRetention(c *gin.Context) {
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
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	items, err := h.service.ListRetentionRuns(c.Request.Context(), limit)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, 89110, err.Error())
		return
	}
	response.Success(c, gin.H{"items": items, "total": len(items)})
}
