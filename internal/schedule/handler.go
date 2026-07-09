package schedule

import (
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/campusos/CampusOS/pkg/response"
	"github.com/gin-gonic/gin"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) Status(c *gin.Context) {
	response.Success(c, h.svc.Status())
}

func (h *Handler) GetMe(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}
	schedule, err := h.svc.Get(c.Request.Context(), userID)
	if err != nil {
		writeError(c, err)
		return
	}
	response.Success(c, schedule)
}

func (h *Handler) SaveMe(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}
	var req UpsertRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, 10001, "invalid request: "+err.Error())
		return
	}
	schedule, err := h.svc.Save(c.Request.Context(), userID, req)
	if err != nil {
		writeError(c, err)
		return
	}
	response.Success(c, schedule)
}

func (h *Handler) ImportMe(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 10001, "schedule file is required")
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, h.svc.cfg.MaxImportBytes+1))
	if err != nil {
		response.Error(c, http.StatusBadRequest, 10001, "read file failed: "+err.Error())
		return
	}
	size := header.Size
	if size <= 0 {
		size = int64(len(data))
	}
	replace := boolForm(c.PostForm("replace"))
	result, err := h.svc.Import(c.Request.Context(), userID, header.Filename, size, data, replace)
	if err != nil {
		writeError(c, err)
		return
	}
	response.Success(c, result)
}

func currentUserID(c *gin.Context) (string, bool) {
	value, ok := c.Get("user_id")
	if !ok {
		return "", false
	}
	userID, ok := value.(string)
	return userID, ok && userID != ""
}

func boolForm(value string) bool {
	parsed, err := strconv.ParseBool(value)
	return err == nil && parsed
}

func writeError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrPluginDisabled):
		response.Error(c, http.StatusServiceUnavailable, 70001, err.Error())
	case errors.Is(err, ErrInvalidInput), errors.Is(err, ErrUnsupported):
		response.Error(c, http.StatusBadRequest, 10001, err.Error())
	case errors.Is(err, ErrQuotaExceeded):
		response.Error(c, http.StatusBadRequest, 70002, err.Error())
	default:
		response.Error(c, http.StatusInternalServerError, 10006, err.Error())
	}
}
