package space

import (
	"errors"
	"net/http"
	"strconv"

	identityrepo "github.com/campusos/CampusOS/internal/core/identity/repository"
	"github.com/campusos/CampusOS/pkg/response"
	"github.com/gin-gonic/gin"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) GetByUserID(c *gin.Context) {
	userID := c.Param("user_id")
	if _, err := strconv.ParseInt(userID, 10, 64); err != nil {
		response.Error(c, http.StatusBadRequest, 10001, "invalid user_id")
		return
	}

	space, err := h.svc.GetPublicByUserID(c.Request.Context(), userID)
	if err != nil {
		writeSpaceError(c, err)
		return
	}
	response.Success(c, space)
}

func (h *Handler) GetByUsername(c *gin.Context) {
	username := c.Param("username")
	if username == "" {
		response.Error(c, http.StatusBadRequest, 10001, "invalid username")
		return
	}

	space, err := h.svc.GetPublicByUsername(c.Request.Context(), username)
	if err != nil {
		writeSpaceError(c, err)
		return
	}
	response.Success(c, space)
}

func (h *Handler) GetMe(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}

	space, err := h.svc.GetOwnSpace(c.Request.Context(), userID)
	if err != nil {
		writeSpaceError(c, err)
		return
	}
	response.Success(c, space)
}

func (h *Handler) UpdateMe(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}

	var req UpsertSpaceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, 10001, "invalid request: "+err.Error())
		return
	}

	space, err := h.svc.UpsertOwnSpace(c.Request.Context(), userID, req)
	if err != nil {
		writeSpaceError(c, err)
		return
	}
	response.Success(c, space)
}

func currentUserID(c *gin.Context) (string, bool) {
	value, ok := c.Get("user_id")
	if !ok {
		return "", false
	}
	userID, ok := value.(string)
	return userID, ok && userID != ""
}

func writeSpaceError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrInvalidVisibility):
		response.Error(c, http.StatusBadRequest, 10001, err.Error())
	case errors.Is(err, ErrSpaceNotPublic):
		response.Error(c, http.StatusForbidden, 20004, err.Error())
	case errors.Is(err, identityrepo.ErrUserNotFound), errors.Is(err, ErrSpaceNotFound):
		response.Error(c, http.StatusNotFound, 30004, err.Error())
	default:
		response.Error(c, http.StatusInternalServerError, 10006, err.Error())
	}
}
