package emaildelivery

import (
	"github.com/campusos/CampusOS/pkg/response"
	"github.com/gin-gonic/gin"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler { return &Handler{service: service} }

// Status intentionally returns no SMTP host, username, recipient, message
// content, challenge ID, or provider-native error detail.
func (h *Handler) Status(c *gin.Context) {
	response.Success(c, h.service.Status())
}
