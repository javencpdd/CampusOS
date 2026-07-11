package space

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	identityrepo "github.com/campusos/CampusOS/internal/core/identity/repository"
	"github.com/campusos/CampusOS/internal/stylepack"
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

func (h *Handler) ListContentsByUserID(c *gin.Context) {
	userID := c.Param("user_id")
	if _, err := strconv.ParseInt(userID, 10, 64); err != nil {
		response.Error(c, http.StatusBadRequest, 10001, "invalid user_id")
		return
	}

	page, pageSize, ok := pagination(c)
	if !ok {
		return
	}
	contents, total, err := h.svc.ListPublicContentsByUserID(c.Request.Context(), userID, page, pageSize)
	if err != nil {
		writeSpaceError(c, err)
		return
	}
	response.List(c, contents, paginationMeta(page, pageSize, total))
}

func (h *Handler) ListContentsByUsername(c *gin.Context) {
	username := c.Param("username")
	if username == "" {
		response.Error(c, http.StatusBadRequest, 10001, "invalid username")
		return
	}

	page, pageSize, ok := pagination(c)
	if !ok {
		return
	}
	contents, total, err := h.svc.ListPublicContentsByUsername(c.Request.Context(), username, page, pageSize)
	if err != nil {
		writeSpaceError(c, err)
		return
	}
	response.List(c, contents, paginationMeta(page, pageSize, total))
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
	if err := requestutil.BindJSONStrict(c, &req); err != nil {
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

func (h *Handler) ValidateStylePackage(c *gin.Context) {
	if _, ok := currentUserID(c); !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}
	if err := h.svc.ensureEnabled(); err != nil {
		writeSpaceError(c, err)
		return
	}

	var req StylePackage
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, 10001, "invalid request: "+err.Error())
		return
	}

	response.Success(c, ValidateStylePackage(req))
}

func (h *Handler) PreviewStylePackage(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}

	var req StylePackage
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, 10001, "invalid request: "+err.Error())
		return
	}

	preview, err := h.svc.PreviewStylePackage(c.Request.Context(), userID, req)
	if err != nil {
		writeSpaceError(c, err)
		return
	}
	response.Success(c, preview)
}

func (h *Handler) ExportStylePackage(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}

	var req StyleExportRequest
	if err := requestutil.BindJSONStrictOptional(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 10001, "invalid request: "+err.Error())
		return
	}

	exported, err := h.svc.ExportStylePackage(c.Request.Context(), userID, req)
	if err != nil {
		writeSpaceError(c, err)
		return
	}
	response.Success(c, exported)
}

func (h *Handler) ApplyStylePackage(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}

	var req StylePackage
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, 10001, "invalid request: "+err.Error())
		return
	}

	applied, err := h.svc.ApplyStylePackage(c.Request.Context(), userID, req)
	if err != nil {
		writeSpaceError(c, err)
		return
	}
	response.Success(c, applied)
}

func (h *Handler) ValidateCustomHTML(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}

	var req StyleHTMLRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, 10001, "invalid request: "+err.Error())
		return
	}

	validation, err := h.svc.ValidateCustomHTML(c.Request.Context(), userID, req.HTML)
	if err != nil {
		writeSpaceError(c, err)
		return
	}
	response.Success(c, validation)
}

func (h *Handler) CustomHTMLExample(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}

	example, err := h.svc.CustomHTMLExample(c.Request.Context(), userID)
	if err != nil {
		writeSpaceError(c, err)
		return
	}
	response.Success(c, example)
}

func (h *Handler) ApplyCustomHTML(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}

	var req StyleHTMLRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, 10001, "invalid request: "+err.Error())
		return
	}

	applied, err := h.svc.ApplyCustomHTML(c.Request.Context(), userID, req.HTML)
	if err != nil {
		writeSpaceError(c, err)
		return
	}
	response.Success(c, applied)
}

func (h *Handler) ValidateStylePackZip(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}
	file, size, ok := openUploadedStylePack(c)
	if !ok {
		return
	}
	defer file.Close()

	result, err := h.svc.ValidateStylePackZip(c.Request.Context(), userID, file, size)
	if err != nil {
		writeSpaceError(c, err)
		return
	}
	response.Success(c, result)
}

func (h *Handler) StylePackExample(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}
	example, err := h.svc.StylePackExample(c.Request.Context(), userID)
	if err != nil {
		writeSpaceError(c, err)
		return
	}
	response.Success(c, example)
}

func (h *Handler) StylePackExampleZip(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}
	example, err := h.svc.StylePackExample(c.Request.Context(), userID)
	if err != nil {
		writeSpaceError(c, err)
		return
	}
	data, err := stylepack.ZipBundle(*example)
	if err != nil {
		writeSpaceError(c, err)
		return
	}
	c.Header("Content-Disposition", `attachment; filename="`+example.Filename+`.zip"`)
	c.Data(http.StatusOK, "application/zip", data)
}

func (h *Handler) ListSourceStylePacks(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}
	result, err := h.svc.ListSourceStylePacks(c.Request.Context(), userID)
	if err != nil {
		writeSpaceError(c, err)
		return
	}
	response.Success(c, result)
}

func (h *Handler) ApplyStylePackZip(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}
	file, size, ok := openUploadedStylePack(c)
	if !ok {
		return
	}
	defer file.Close()

	applied, err := h.svc.ApplyStylePackZip(c.Request.Context(), userID, file, size)
	if err != nil {
		writeSpaceError(c, err)
		return
	}
	response.Success(c, applied)
}

func (h *Handler) ApplySourceStylePack(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}
	var req StylePackApplySourceRequest
	if err := requestutil.BindJSONStrict(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 10001, "invalid request: "+err.Error())
		return
	}
	applied, err := h.svc.ApplySourceStylePack(c.Request.Context(), userID, req.Name)
	if err != nil {
		writeSpaceError(c, err)
		return
	}
	response.Success(c, applied)
}

func (h *Handler) RollbackStyle(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}
	space, err := h.svc.RollbackStyle(c.Request.Context(), userID)
	if err != nil {
		writeSpaceError(c, err)
		return
	}
	response.Success(c, space)
}

func (h *Handler) RestoreDefaultStyle(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}
	space, err := h.svc.RestoreDefaultStyle(c.Request.Context(), userID)
	if err != nil {
		writeSpaceError(c, err)
		return
	}
	response.Success(c, space)
}

func (h *Handler) GetSyncStatus(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}
	status, err := h.svc.GetSyncStatus(c.Request.Context(), userID)
	if err != nil {
		writeSpaceError(c, err)
		return
	}
	response.Success(c, status)
}

func (h *Handler) GetStorageStatus(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}
	status, err := h.svc.StorageStatus(c.Request.Context(), userID)
	if err != nil {
		writeSpaceError(c, err)
		return
	}
	response.Success(c, status)
}

func (h *Handler) UploadAvatar(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}
	fileHeader, err := c.FormFile("file")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 10001, "avatar file is required")
		return
	}
	file, err := fileHeader.Open()
	if err != nil {
		response.Error(c, http.StatusBadRequest, 10001, err.Error())
		return
	}
	defer file.Close()

	uploaded, err := h.svc.UploadAvatar(c.Request.Context(), userID, fileHeader.Filename, file)
	if err != nil {
		writeSpaceError(c, err)
		return
	}
	response.Success(c, uploaded)
}

func (h *Handler) ServeAvatarFile(c *gin.Context) {
	userID := c.Param("user_id")
	fileName := c.Param("filename")
	if _, err := strconv.ParseInt(userID, 10, 64); err != nil {
		response.Error(c, http.StatusBadRequest, 10001, "invalid user_id")
		return
	}
	filePath, err := h.svc.AvatarFilePath(userID, fileName)
	if err != nil {
		writeSpaceError(c, err)
		return
	}
	c.File(filePath)
}

func (h *Handler) AdminSummary(c *gin.Context) {
	summary, err := h.svc.AdminSummary(c.Request.Context())
	if err != nil {
		writeSpaceError(c, err)
		return
	}
	response.Success(c, summary)
}

func (h *Handler) DisableSpace(c *gin.Context) {
	targetUserID := c.Param("user_id")
	if _, err := strconv.ParseInt(targetUserID, 10, 64); err != nil {
		response.Error(c, http.StatusBadRequest, 10001, "invalid user_id")
		return
	}
	actorUserID, _ := currentUserID(c)
	var req struct {
		Reason string `json:"reason"`
	}
	if err := requestutil.BindJSONStrictOptional(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 10001, "invalid request: "+err.Error())
		return
	}
	space, err := h.svc.DisableSpace(c.Request.Context(), targetUserID, actorUserID, strings.TrimSpace(req.Reason))
	if err != nil {
		writeSpaceError(c, err)
		return
	}
	response.Success(c, space)
}

func (h *Handler) EnableSpace(c *gin.Context) {
	targetUserID := c.Param("user_id")
	if _, err := strconv.ParseInt(targetUserID, 10, 64); err != nil {
		response.Error(c, http.StatusBadRequest, 10001, "invalid user_id")
		return
	}
	space, err := h.svc.EnableSpace(c.Request.Context(), targetUserID)
	if err != nil {
		writeSpaceError(c, err)
		return
	}
	response.Success(c, space)
}

type uploadedStylePackFile interface {
	io.ReaderAt
	io.Closer
}

func openUploadedStylePack(c *gin.Context) (uploadedStylePackFile, int64, bool) {
	fileHeader, err := c.FormFile("file")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 10001, "style pack zip file is required")
		return nil, 0, false
	}
	file, err := fileHeader.Open()
	if err != nil {
		response.Error(c, http.StatusBadRequest, 10001, err.Error())
		return nil, 0, false
	}
	reader, ok := file.(uploadedStylePackFile)
	if !ok {
		_ = file.Close()
		response.Error(c, http.StatusBadRequest, 10001, "style pack file must be seekable")
		return nil, 0, false
	}
	return reader, fileHeader.Size, true
}

func currentUserID(c *gin.Context) (string, bool) {
	value, ok := c.Get("user_id")
	if !ok {
		return "", false
	}
	userID, ok := value.(string)
	return userID, ok && userID != ""
}

func pagination(c *gin.Context) (int, int, bool) {
	return response.ParsePagination(c, 20, 100)
}

func paginationMeta(page, pageSize int, total int64) *response.Pagination {
	totalPages := int(total) / pageSize
	if int(total)%pageSize > 0 {
		totalPages++
	}
	return &response.Pagination{
		Page:       page,
		PageSize:   pageSize,
		Total:      total,
		TotalPages: totalPages,
	}
}

func writeSpaceError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrInvalidVisibility):
		response.Error(c, http.StatusBadRequest, 10001, err.Error())
	case errors.Is(err, ErrInvalidStyleExport):
		response.Error(c, http.StatusBadRequest, 10001, err.Error())
	case errors.Is(err, ErrStyleSnapshotNotFound):
		response.Error(c, http.StatusNotFound, 30004, err.Error())
	case errors.Is(err, ErrContentRepositoryUnavailable):
		response.Error(c, http.StatusInternalServerError, 10006, err.Error())
	case errors.Is(err, ErrSpacePluginDisabled):
		response.Error(c, http.StatusServiceUnavailable, 60006, err.Error())
	case errors.Is(err, ErrSpaceFileStoreUnavailable):
		response.Error(c, http.StatusInternalServerError, 10006, err.Error())
	case errors.Is(err, ErrSpaceFileInvalidName), errors.Is(err, ErrSpaceFileUnsupportedType):
		response.Error(c, http.StatusBadRequest, 10001, err.Error())
	case errors.Is(err, ErrSpaceFileNotFound):
		response.Error(c, http.StatusNotFound, 30004, err.Error())
	case errors.Is(err, ErrSpaceFileTooLarge), errors.Is(err, ErrSpaceFileQuotaExceeded):
		response.Error(c, http.StatusRequestEntityTooLarge, 10001, err.Error())
	case errors.Is(err, ErrSpaceNotPublic):
		response.Error(c, http.StatusForbidden, 20004, err.Error())
	case errors.Is(err, identityrepo.ErrUserNotFound), errors.Is(err, ErrSpaceNotFound):
		response.Error(c, http.StatusNotFound, 30004, err.Error())
	default:
		response.Error(c, http.StatusInternalServerError, 10006, err.Error())
	}
}
