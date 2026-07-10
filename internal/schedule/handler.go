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
	termYear, semester, hasTerm, err := termFromQuery(c)
	if err != nil {
		response.Error(c, http.StatusBadRequest, 10001, err.Error())
		return
	}
	var schedule *ScheduleResponse
	if hasTerm {
		schedule, err = h.svc.GetTerm(c.Request.Context(), userID, termYear, semester)
	} else {
		schedule, err = h.svc.Get(c.Request.Context(), userID)
	}
	if err != nil {
		writeError(c, err)
		return
	}
	response.Success(c, schedule)
}

// ListTerms returns all independent semester JSON files for the current user.
func (h *Handler) ListTerms(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}
	terms, err := h.svc.ListTerms(c.Request.Context(), userID)
	if err != nil {
		writeError(c, err)
		return
	}
	response.Success(c, terms)
}

// ActivateTerm selects or creates one semester JSON as the user's active schedule.
func (h *Handler) ActivateTerm(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}
	var req ActivateTermRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, 10001, "invalid request: "+err.Error())
		return
	}
	schedule, err := h.svc.ActivateTerm(c.Request.Context(), userID, req.TermYear, req.Semester)
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
	termYear, err := strconv.Atoi(c.PostForm("term_year"))
	if err != nil || termYear == 0 {
		response.Error(c, http.StatusBadRequest, 10001, "term_year is required")
		return
	}
	semester := c.PostForm("semester")
	if semester == "" {
		response.Error(c, http.StatusBadRequest, 10001, "semester is required")
		return
	}
	result, err := h.svc.Import(c.Request.Context(), userID, header.Filename, size, data, replace, termYear, semester)
	if err != nil {
		writeError(c, err)
		return
	}
	response.Success(c, result)
}

func termFromQuery(c *gin.Context) (int, string, bool, error) {
	rawYear := c.Query("term_year")
	semester := c.Query("semester")
	if rawYear == "" && semester == "" {
		return 0, "", false, nil
	}
	if rawYear == "" || semester == "" {
		return 0, "", false, errors.New("term_year and semester must be provided together")
	}
	termYear, err := strconv.Atoi(rawYear)
	if err != nil || termYear == 0 {
		return 0, "", false, errors.New("invalid term_year")
	}
	return termYear, semester, true, nil
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
