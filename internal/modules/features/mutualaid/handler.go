package mutualaid

import (
	"strings"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	"github.com/campusos/CampusOS/pkg/apperror"
	requestutil "github.com/campusos/CampusOS/pkg/request"
	"github.com/campusos/CampusOS/pkg/response"
	"github.com/gin-gonic/gin"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) Status(c *gin.Context) {
	response.Success(c, h.service.Status())
}

func (h *Handler) ListPublic(c *gin.Context) {
	page, pageSize, ok := response.ParsePagination(c, 20, 100)
	if !ok {
		return
	}
	items, total, err := h.service.ListPublic(c.Request.Context(), domain.ThreadListFilter{
		Page:       page,
		PageSize:   pageSize,
		CategoryID: strings.TrimSpace(c.Query("category_id")),
		Keyword:    strings.TrimSpace(c.Query("keyword")),
		Tag:        strings.TrimSpace(c.Query("tag")),
	})
	if err != nil {
		writeError(c, err)
		return
	}
	totalPages := int(total) / pageSize
	if int(total)%pageSize != 0 {
		totalPages++
	}
	response.List(c, items, &response.Pagination{Page: page, PageSize: pageSize, Total: total, TotalPages: totalPages})
}

func (h *Handler) GetPublic(c *gin.Context) {
	result, err := h.service.GetPublic(c.Request.Context(), c.Param("id"))
	if err != nil {
		writeError(c, err)
		return
	}
	response.Success(c, result)
}

func (h *Handler) Create(c *gin.Context) {
	userID, username, ok := currentUser(c)
	if !ok {
		response.ErrorDescriptor(c, apperror.AuthRequired, nil)
		return
	}
	var req CreateRequest
	if err := requestutil.BindJSONStrict(c, &req); err != nil {
		response.ErrorDescriptor(c, apperror.RequestInvalid, nil)
		return
	}
	result, err := h.service.Create(c.Request.Context(), userID, username, req)
	if err != nil {
		writeError(c, err)
		return
	}
	response.Created(c, result)
}

func (h *Handler) GetMine(c *gin.Context) {
	userID, _, ok := currentUser(c)
	if !ok {
		response.ErrorDescriptor(c, apperror.AuthRequired, nil)
		return
	}
	result, err := h.service.GetMine(c.Request.Context(), c.Param("id"), userID)
	if err != nil {
		writeError(c, err)
		return
	}
	response.Success(c, result)
}

func (h *Handler) Update(c *gin.Context) {
	userID, _, ok := currentUser(c)
	if !ok {
		response.ErrorDescriptor(c, apperror.AuthRequired, nil)
		return
	}
	var req UpdateRequest
	if err := requestutil.BindJSONStrict(c, &req); err != nil {
		response.ErrorDescriptor(c, apperror.RequestInvalid, nil)
		return
	}
	result, err := h.service.Update(c.Request.Context(), c.Param("id"), userID, req)
	if err != nil {
		writeError(c, err)
		return
	}
	response.Success(c, result)
}

func (h *Handler) UpdateStatus(c *gin.Context) {
	userID, _, ok := currentUser(c)
	if !ok {
		response.ErrorDescriptor(c, apperror.AuthRequired, nil)
		return
	}
	var req UpdateStatusRequest
	if err := requestutil.BindJSONStrict(c, &req); err != nil {
		response.ErrorDescriptor(c, apperror.RequestInvalid, nil)
		return
	}
	result, err := h.service.UpdateStatus(c.Request.Context(), c.Param("id"), userID, req)
	if err != nil {
		writeError(c, err)
		return
	}
	response.Success(c, result)
}

func currentUser(c *gin.Context) (string, string, bool) {
	rawID, ok := c.Get("user_id")
	if !ok {
		return "", "", false
	}
	userID, ok := rawID.(string)
	if !ok || strings.TrimSpace(userID) == "" {
		return "", "", false
	}
	username, _ := c.Get("username")
	name, _ := username.(string)
	return userID, name, true
}

func writeError(c *gin.Context, err error) {
	response.WriteError(c, errorTranslator.Translate(err))
}
