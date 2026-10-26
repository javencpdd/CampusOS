package ai

import (
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"net/http"
	"strconv"

	"github.com/campusos/CampusOS/pkg/response"
	"github.com/gin-gonic/gin"
)

type Handler struct {
	service       *Service
	adminSessions identityport.AdminSessionAuthorityReader
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) GetStatus(c *gin.Context) {
	c.Header("Cache-Control", "private, no-store")
	if h.service != nil && h.service.managed != nil {
		if c.Request.URL.RawQuery != "" || c.Request.ContentLength > 0 {
			response.Error(c, http.StatusBadRequest, 10001, "invalid request")
			return
		}
		s, resolve := h.service.managed, h.managedResolver(c)
		a, e := s.admin(c.Request.Context(), "ai.ai.read", resolve, false)
		if e != nil {
			managedHTTPError(c, e)
			return
		}
		status := h.service.Status()
		current, e := s.admin(c.Request.Context(), "ai.ai.read", resolve, false)
		if e != nil || identityport.CheckAdminExecutionAuthority(a, current) != nil {
			managedHTTPError(c, ErrManagedDenied)
			return
		}
		response.Success(c, status)
		return
	}
	response.Success(c, h.service.Status())
}

func (h *Handler) ListLogs(c *gin.Context) {
	c.Header("Cache-Control", "private, no-store")
	if h.service != nil && h.service.managed != nil {
		h.managedLogs(c)
		return
	}
	limit := 100
	if raw := c.Query("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	logs, err := h.service.ListLogs(c.Request.Context(), limit)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, 70001, err.Error())
		return
	}
	response.Success(c, gin.H{"items": logs, "total": len(logs)})
}
