package ai

import (
	"net/http"
	"strconv"

	"github.com/campusos/CampusOS/pkg/response"
	"github.com/gin-gonic/gin"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) GetStatus(c *gin.Context) {
	response.Success(c, h.service.Status())
}

func (h *Handler) ListLogs(c *gin.Context) {
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
