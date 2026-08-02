package space

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/modules/features/appearance/stylepack"
	requestutil "github.com/campusos/CampusOS/pkg/request"
	"github.com/campusos/CampusOS/pkg/response"
	"github.com/gin-gonic/gin"
)

type Handler struct {
	svc    *Service
	styles StyleApplication
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc, styles: svc}
}

func NewHandlerWithStyleApplication(svc *Service, styles StyleApplication) *Handler {
	if styles == nil {
		styles = svc
	}
	return &Handler{svc: svc, styles: styles}
}

type StyleApplication interface {
	ValidateSpaceStylePackage(context.Context, string, StylePackage) (StyleValidationResult, error)
	PreviewSpaceStylePackage(context.Context, string, StylePackage) (*StylePreview, error)
	ExportSpaceStylePackage(context.Context, string, StyleExportRequest) (*StyleExportResult, error)
	ApplySpaceStylePackage(context.Context, string, StylePackage) (*StyleApplyResult, error)
	ValidateSpaceCustomHTML(context.Context, string, string) (*StyleValidationResult, error)
	SpaceCustomHTMLExample(context.Context, string) (*StyleHTMLExampleResult, error)
	ApplySpaceCustomHTML(context.Context, string, string) (*StyleApplyResult, error)
	ValidateSpaceStylePackZip(context.Context, string, io.ReaderAt, int64) (*StylePackResult, error)
	SpaceStylePackExample(context.Context, string) (*stylepack.FileBundle, error)
	ListSpaceStylePacks(context.Context, string) (*stylepack.SourcePackList, error)
	ApplySpaceStylePackZip(context.Context, string, io.ReaderAt, int64) (*StyleApplyResult, error)
	ApplySpaceSourceStylePack(context.Context, string, string) (*StyleApplyResult, error)
	RollbackSpaceStyle(context.Context, string) (*PublicSpace, error)
	RestoreDefaultSpaceStyle(context.Context, string) (*PublicSpace, error)
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

// ListOwnContents exposes author-only profile content states. This endpoint
// is intentionally separate from public /u/:username/contents so a visitor
// cannot infer a user's drafts, private posts or moderation history.
func (h *Handler) ListOwnContents(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}
	page, pageSize, ok := pagination(c)
	if !ok {
		return
	}
	contents, total, err := h.svc.ListOwnContents(c.Request.Context(), userID, page, pageSize)
	if err != nil {
		writeSpaceError(c, err)
		return
	}
	response.List(c, contents, paginationMeta(page, pageSize, total))
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

	validation, err := h.styles.ValidateSpaceStylePackage(c.Request.Context(), userID, req)
	if err != nil {
		writeSpaceError(c, err)
		return
	}
	response.Success(c, validation)
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

	preview, err := h.styles.PreviewSpaceStylePackage(c.Request.Context(), userID, req)
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

	exported, err := h.styles.ExportSpaceStylePackage(c.Request.Context(), userID, req)
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

	applied, err := h.styles.ApplySpaceStylePackage(c.Request.Context(), userID, req)
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

	validation, err := h.styles.ValidateSpaceCustomHTML(c.Request.Context(), userID, req.HTML)
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

	example, err := h.styles.SpaceCustomHTMLExample(c.Request.Context(), userID)
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

	applied, err := h.styles.ApplySpaceCustomHTML(c.Request.Context(), userID, req.HTML)
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

	result, err := h.styles.ValidateSpaceStylePackZip(c.Request.Context(), userID, file, size)
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
	example, err := h.styles.SpaceStylePackExample(c.Request.Context(), userID)
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
	example, err := h.styles.SpaceStylePackExample(c.Request.Context(), userID)
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
	result, err := h.styles.ListSpaceStylePacks(c.Request.Context(), userID)
	if err != nil {
		writeSpaceError(c, err)
		return
	}
	response.Success(c, result)
}

func (h *Handler) ServeSourceStylePackAsset(c *gin.Context) {
	data, contentType, err := h.svc.SourceStylePackAsset(c.Request.Context(), c.Param("name"), c.Param("asset_path"))
	if err != nil {
		// Resource previews are intentionally indistinguishable from a missing
		// package or undeclared file, so this endpoint cannot reveal source paths.
		c.Status(http.StatusNotFound)
		return
	}
	c.Header("Cache-Control", "public, max-age=300")
	c.Data(http.StatusOK, contentType, data)
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

	applied, err := h.styles.ApplySpaceStylePackZip(c.Request.Context(), userID, file, size)
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
	applied, err := h.styles.ApplySpaceSourceStylePack(c.Request.Context(), userID, req.Name)
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
	space, err := h.styles.RollbackSpaceStyle(c.Request.Context(), userID)
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
	space, err := h.styles.RestoreDefaultSpaceStyle(c.Request.Context(), userID)
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
	maxBytes := h.svc.MaxAvatarBytes()
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBytes+64*1024)
	fileHeader, err := c.FormFile("file")
	if err != nil {
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			writeAvatarUploadError(c, ErrSpaceFileTooLarge, maxBytes)
			return
		}
		response.ErrorWithDetails(c, http.StatusBadRequest, 10001, "请选择要上传的头像图片。", gin.H{
			"accepted_types": []string{"image/png", "image/jpeg", "image/gif", "image/webp"},
			"max_bytes":      maxBytes,
		})
		return
	}
	if fileHeader.Size > maxBytes {
		writeAvatarUploadError(c, ErrSpaceFileTooLarge, maxBytes)
		return
	}
	file, err := fileHeader.Open()
	if err != nil {
		response.Error(c, http.StatusBadRequest, 10001, "无法读取所选头像文件，请重新选择。")
		return
	}
	defer file.Close()

	uploaded, err := h.svc.UploadAvatar(c.Request.Context(), userID, fileHeader.Filename, file)
	if err != nil {
		writeAvatarUploadError(c, err, maxBytes)
		return
	}
	response.Success(c, uploaded)
}

func writeAvatarUploadError(c *gin.Context, err error, maxBytes int64) {
	switch {
	case errors.Is(err, ErrSpaceFileTooLarge):
		response.ErrorWithDetails(c, http.StatusRequestEntityTooLarge, 10001,
			fmt.Sprintf("头像文件过大：单个文件最大 %s，请压缩或裁剪后重试。", formatSpaceBytes(maxBytes)), gin.H{
				"max_bytes":      maxBytes,
				"accepted_types": []string{"image/png", "image/jpeg", "image/gif", "image/webp"},
			})
	case errors.Is(err, ErrSpaceFileQuotaExceeded):
		response.Error(c, http.StatusRequestEntityTooLarge, 10001,
			"个人空间剩余容量不足，请删除不需要的文件，或联系管理员提高空间配额后重试。")
	case errors.Is(err, ErrSpaceFileUnsupportedType):
		response.ErrorWithDetails(c, http.StatusBadRequest, 10001,
			"头像格式不受支持，请上传 PNG、JPEG、GIF 或 WebP 图片。", gin.H{
				"accepted_types": []string{"image/png", "image/jpeg", "image/gif", "image/webp"},
				"max_bytes":      maxBytes,
			})
	default:
		writeSpaceError(c, err)
	}
}

func formatSpaceBytes(value int64) string {
	if value >= 1024*1024 && value%(1024*1024) == 0 {
		return fmt.Sprintf("%d MB", value/(1024*1024))
	}
	if value >= 1024 && value%1024 == 0 {
		return fmt.Sprintf("%d KB", value/1024)
	}
	return fmt.Sprintf("%d B", value)
}

func (h *Handler) ListAvatars(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}
	history, err := h.svc.ListAvatars(c.Request.Context(), userID)
	if err != nil {
		writeSpaceError(c, err)
		return
	}
	response.Success(c, history)
}

func (h *Handler) SelectAvatar(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}
	var req struct {
		FileName string `json:"file_name" binding:"required,min=1,max=255"`
	}
	if err := requestutil.BindJSONStrict(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 10001, "invalid request: "+err.Error())
		return
	}
	selected, err := h.svc.SelectAvatar(c.Request.Context(), userID, strings.TrimSpace(req.FileName))
	if err != nil {
		writeSpaceError(c, err)
		return
	}
	response.Success(c, selected)
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

func (h *Handler) AdminStorageStatus(c *gin.Context) {
	targetUserID := c.Param("user_id")
	if _, err := strconv.ParseInt(targetUserID, 10, 64); err != nil {
		response.Error(c, http.StatusBadRequest, 10001, "invalid user_id")
		return
	}
	status, err := h.svc.AdminStorageStatus(c.Request.Context(), targetUserID)
	if err != nil {
		writeSpaceError(c, err)
		return
	}
	response.Success(c, status)
}

func (h *Handler) SetStorageQuota(c *gin.Context) {
	targetUserID := c.Param("user_id")
	if _, err := strconv.ParseInt(targetUserID, 10, 64); err != nil {
		response.Error(c, http.StatusBadRequest, 10001, "invalid user_id")
		return
	}
	actorUserID, ok := currentUserID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}
	var req struct {
		QuotaBytes int64 `json:"quota_bytes" binding:"required,min=1048576,max=107374182400"`
	}
	if err := requestutil.BindJSONStrict(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 10001, "invalid request: "+err.Error())
		return
	}
	status, err := h.svc.SetStorageQuota(c.Request.Context(), targetUserID, actorUserID, req.QuotaBytes)
	if err != nil {
		writeSpaceError(c, err)
		return
	}
	response.Success(c, status)
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
	case errors.Is(err, ErrSpaceFileInvalidName):
		response.Error(c, http.StatusBadRequest, 10001, "文件名称或路径无效，请重新选择文件。")
	case errors.Is(err, ErrSpaceFileUnsupportedType):
		response.Error(c, http.StatusBadRequest, 10001, "文件格式不受支持，请选择系统允许的文件类型。")
	case errors.Is(err, ErrSpaceFileNotFound):
		response.Error(c, http.StatusNotFound, 30004, "文件不存在或已被删除。")
	case errors.Is(err, ErrSpaceFileTooLarge):
		response.Error(c, http.StatusRequestEntityTooLarge, 10001, "文件超过系统允许的大小，请压缩后重试。")
	case errors.Is(err, ErrSpaceFileQuotaExceeded):
		response.Error(c, http.StatusRequestEntityTooLarge, 10001, "个人空间剩余容量不足，请删除不需要的文件，或联系管理员提高空间配额后重试。")
	case errors.Is(err, ErrStorageQuotaInvalid):
		response.Error(c, http.StatusBadRequest, 10001, err.Error())
	case errors.Is(err, ErrStorageQuotaUnavailable):
		response.Error(c, http.StatusServiceUnavailable, 60006, err.Error())
	case errors.Is(err, ErrSpaceNotPublic):
		response.Error(c, http.StatusForbidden, 20004, err.Error())
	case errors.Is(err, identityport.ErrUserNotFound), errors.Is(err, ErrSpaceNotFound):
		response.Error(c, http.StatusNotFound, 30004, err.Error())
	default:
		response.Error(c, http.StatusInternalServerError, 10006, err.Error())
	}
}
