package handler

import (
	"net/http"
	"strconv"

	"github.com/campusos/CampusOS/pkg/eventbus"
	"github.com/campusos/CampusOS/pkg/response"
	"github.com/gin-gonic/gin"
)

type EventHandler struct {
	bus *eventbus.MemoryEventBus
}

func NewEventHandler(bus *eventbus.MemoryEventBus) *EventHandler {
	return &EventHandler{bus: bus}
}

// ListEvents 获取事件历史
// GET /api/v1/events?limit=20
func (h *EventHandler) ListEvents(c *gin.Context) {
	if h.bus == nil {
		response.Error(c, http.StatusServiceUnavailable, 60001, "event history not available")
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	events := h.bus.GetHistory(limit)
	response.Success(c, gin.H{
		"items": events,
		"total": len(events),
	})
}
