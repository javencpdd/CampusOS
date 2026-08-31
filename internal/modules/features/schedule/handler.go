package schedule

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	academicterm "github.com/campusos/CampusOS/internal/modules/core/academicterm"
	corestorage "github.com/campusos/CampusOS/internal/modules/core/userstorage"
	"github.com/campusos/CampusOS/pkg/apperror"
	requestutil "github.com/campusos/CampusOS/pkg/request"
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
	academicTermID := strings.TrimSpace(c.Query("academic_term_id"))
	termYear, semester, hasTerm, err := termFromQuery(c)
	if err != nil {
		response.ErrorDescriptor(c, apperror.AcademicTermInvalid, map[string]any{
			"field":       "academic_term_id_or_term",
			"reason":      "学期参数不正确，请使用 academic_term_id，或同时提供 term_year 与 spring/fall semester",
			"next_action": "请从系统开放的学期列表中重新选择",
		})
		return
	}
	var schedule *ScheduleResponse
	if academicTermID != "" && hasTerm {
		response.ErrorDescriptor(c, apperror.AcademicTermInvalid, map[string]any{
			"field":       "academic_term_id_or_term",
			"reason":      "academic_term_id 不能与 term_year、semester 同时使用",
			"next_action": "请仅保留一种学期参数后重试",
		})
		return
	}
	if academicTermID != "" {
		schedule, err = h.svc.GetTermByID(c.Request.Context(), userID, academicTermID)
	} else if hasTerm {
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
	if err := requestutil.BindJSONStrict(c, &req); err != nil {
		response.ErrorDescriptor(c, apperror.AcademicTermInvalid, map[string]any{
			"field":       "request",
			"reason":      "课表学期请求格式不正确",
			"next_action": "请从系统提供的开放学期中选择后重试",
		})
		return
	}
	var schedule *ScheduleResponse
	var err error
	if strings.TrimSpace(req.AcademicTermID) != "" {
		schedule, err = h.svc.ActivateTermByID(c.Request.Context(), userID, req.AcademicTermID)
	} else {
		schedule, err = h.svc.ActivateTerm(c.Request.Context(), userID, req.TermYear, req.Semester)
	}
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
	if err := requestutil.BindJSONStrict(c, &req); err != nil {
		response.ErrorDescriptor(c, apperror.AcademicTermInvalid, map[string]any{
			"field":       "request",
			"reason":      "课表保存请求格式不正确",
			"next_action": "请刷新课表后检查学期、第一周日期和课程信息",
		})
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
		response.ErrorDescriptor(c, apperror.AcademicTermInvalid, map[string]any{
			"field":       "file",
			"reason":      "请选择要导入的课表文件",
			"next_action": "请选择系统支持的课表文件后重试",
		})
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, h.svc.cfg.MaxImportBytes+1))
	if err != nil {
		response.ErrorDescriptor(c, apperror.AcademicTermInvalid, map[string]any{
			"field":       "file",
			"reason":      "课表文件无法读取",
			"next_action": "请确认文件未损坏后重新选择",
		})
		return
	}
	size := header.Size
	if size <= 0 {
		size = int64(len(data))
	}
	if size > h.svc.cfg.MaxImportBytes || int64(len(data)) > h.svc.cfg.MaxImportBytes {
		response.ErrorDescriptor(c, apperror.AcademicTermInvalid, map[string]any{
			"field":        "file",
			"reason":       "课表导入文件超过大小限制",
			"max_bytes":    h.svc.cfg.MaxImportBytes,
			"actual_bytes": size,
			"next_action":  "请压缩或拆分文件后重新导入",
		})
		return
	}
	replace := boolForm(c.PostForm("replace"))
	termYear, err := strconv.Atoi(c.PostForm("term_year"))
	if err != nil || termYear == 0 {
		response.ErrorDescriptor(c, apperror.AcademicTermInvalid, map[string]any{
			"field":       "term_year",
			"reason":      "导入课表必须指定有效学年",
			"next_action": "请从系统开放的学期中选择后重新导入",
		})
		return
	}
	semester := c.PostForm("semester")
	if semester == "" {
		response.ErrorDescriptor(c, apperror.AcademicTermInvalid, map[string]any{
			"field":       "semester",
			"reason":      "导入课表必须指定春季或秋季学期",
			"next_action": "请从系统开放的学期中选择后重新导入",
		})
		return
	}
	expectedVersion := int64(0)
	if rawExpected := strings.TrimSpace(c.PostForm("expected_version")); rawExpected != "" {
		expectedVersion, err = strconv.ParseInt(rawExpected, 10, 64)
		if err != nil || expectedVersion < 0 {
			response.ErrorDescriptor(c, apperror.AcademicTermInvalid, map[string]any{
				"field":       "expected_version",
				"reason":      "课表版本号必须是非负整数",
				"next_action": "请刷新课表后重新导入",
			})
			return
		}
	}
	result, err := h.svc.Import(c.Request.Context(), userID, header.Filename, size, data, replace, termYear, semester, expectedVersion)
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
	var public *apperror.AppError
	if errors.As(err, &public) {
		response.WriteError(c, err)
		return
	}
	switch {
	case errors.Is(err, ErrPluginDisabled):
		response.ErrorDescriptor(c, apperror.PersonalScheduleDisabled, map[string]any{"feature": "personal-schedule"})
	case errors.Is(err, ErrTermReferenceConflict):
		response.ErrorDescriptor(c, apperror.AcademicTermVersionConflict, map[string]any{"next_action": "请刷新课表后重新保存"})
	case errors.Is(err, ErrObjectUnavailable), errors.Is(err, corestorage.ErrObjectUnavailable):
		response.ErrorDescriptor(c, apperror.ScheduleObjectUnavailable, map[string]any{"next_action": "请稍后重试；若持续失败请联系管理员执行对象对账"})
	case errors.Is(err, ErrInvalidInput), errors.Is(err, ErrUnsupported):
		response.ErrorDescriptor(c, apperror.AcademicTermInvalid, map[string]any{"reason": "课表请求不符合要求，请检查学期、日期、课程和导入文件后重试"})
	case errors.Is(err, ErrQuotaExceeded), errors.Is(err, corestorage.ErrObjectQuota):
		response.ErrorDescriptor(c, apperror.UserStorageQuotaExceeded, map[string]any{"next_action": "请清理个人文件，或联系管理员调整个人空间配额"})
	case errors.Is(err, academicterm.ErrClosed):
		response.ErrorDescriptor(c, apperror.AcademicTermClosed, map[string]any{"next_action": "请选择开放学期，关闭学期仅可查看"})
	case errors.Is(err, academicterm.ErrNotFound):
		response.ErrorDescriptor(c, apperror.AcademicTermNotAvailable, map[string]any{"next_action": "请从管理员开放的学期列表中选择"})
	default:
		response.ErrorDescriptor(c, apperror.InternalError, nil)
	}
}
