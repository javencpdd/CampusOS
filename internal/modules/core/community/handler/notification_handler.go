package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/campusos/CampusOS/internal/modules/core/community/repository"
	"github.com/campusos/CampusOS/internal/modules/core/community/service"
	"github.com/campusos/CampusOS/pkg/response"
	"github.com/gin-gonic/gin"
)

type NotificationHandler struct {
	svc *service.NotificationService
}

func NewNotificationHandler(svc *service.NotificationService) *NotificationHandler {
	return &NotificationHandler{svc: svc}
}

func (h *NotificationHandler) List(c *gin.Context) {
	userID, _, ok := currentUser(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}
	page := positiveQueryInt(c, "page", 1)
	pageSize := positiveQueryInt(c, "page_size", 20)
	result, err := h.svc.List(c.Request.Context(), userID, page, pageSize)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, 10006, "load notifications failed")
		return
	}
	totalPages := 0
	if result.Total > 0 {
		totalPages = int((result.Total + int64(result.PageSize) - 1) / int64(result.PageSize))
	}
	response.Success(c, gin.H{
		"items":        result.Items,
		"unread_count": result.UnreadCount,
		"pagination": &response.Pagination{
			Page: result.Page, PageSize: result.PageSize, Total: result.Total, TotalPages: totalPages,
		},
	})
}

func (h *NotificationHandler) MarkRead(c *gin.Context) {
	userID, _, ok := currentUser(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}
	if err := h.svc.MarkRead(c.Request.Context(), userID, c.Param("id")); err != nil {
		if errors.Is(err, repository.ErrNotificationNotFound) {
			response.Error(c, http.StatusNotFound, 30004, "notification not found")
			return
		}
		response.Error(c, http.StatusInternalServerError, 10006, "mark notification read failed")
		return
	}
	response.NoContent(c)
}

func (h *NotificationHandler) MarkAllRead(c *gin.Context) {
	userID, _, ok := currentUser(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}
	updated, err := h.svc.MarkAllRead(c.Request.Context(), userID)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, 10006, "mark notifications read failed")
		return
	}
	response.Success(c, gin.H{"updated": updated})
}

func positiveQueryInt(c *gin.Context, key string, fallback int) int {
	value, err := strconv.Atoi(c.Query(key))
	if err != nil || value < 1 {
		return fallback
	}
	return value
}
