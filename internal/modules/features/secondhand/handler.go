package secondhand

import (
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
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
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}
	var req CreateRequest
	if err := requestutil.BindJSONStrict(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 10001, "invalid request: "+err.Error())
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
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
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
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}
	var req UpdateRequest
	if err := requestutil.BindJSONStrict(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 10001, "invalid request: "+err.Error())
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
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}
	var req UpdateStatusRequest
	if err := requestutil.BindJSONStrict(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 10001, "invalid request: "+err.Error())
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
	switch {
	case errors.Is(err, ErrFeatureDisabled):
		response.Error(c, http.StatusServiceUnavailable, 50301, err.Error())
	case errors.Is(err, ErrNotFound):
		response.Error(c, http.StatusNotFound, 40003, err.Error())
	case errors.Is(err, ErrForbidden):
		response.Error(c, http.StatusForbidden, 20003, err.Error())
	case errors.Is(err, ErrInvalidInput), errors.Is(err, ErrInvalidTransition):
		response.Error(c, http.StatusBadRequest, 10001, err.Error())
	case errors.Is(err, ErrVersionConflict), errors.Is(err, ErrThreadNotEditable):
		response.Error(c, http.StatusConflict, 40009, err.Error())
	default:
		log.Printf("secondhand unexpected error: trace_id=%s err=%v", c.GetString("trace_id"), err)
		response.Error(c, http.StatusInternalServerError, 10006, "internal server error")
	}
}
