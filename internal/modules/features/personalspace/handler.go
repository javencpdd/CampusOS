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
	corestorage "github.com/campusos/CampusOS/internal/modules/core/userstorage"
	"github.com/campusos/CampusOS/internal/modules/features/appearance/stylepack"
	"github.com/campusos/CampusOS/internal/safehtml"
	"github.com/campusos/CampusOS/pkg/apperror"
	"github.com/campusos/CampusOS/pkg/auth"
	requestutil "github.com/campusos/CampusOS/pkg/request"
	"github.com/campusos/CampusOS/pkg/response"
	"github.com/gin-gonic/gin"
)

type Handler struct {
	svc                  *Service
	styles               StyleApplication
	ownerSessionVerifier identityport.SessionVerifier
	ownerStrengthReader  identityport.SessionStrengthReader
}

// SetOwnerSessionProof binds the current Identity proof needed by the owner
// content-list route. Missing adapters leave only that authenticated entry closed.
func (h *Handler) SetOwnerSessionProof(verifier identityport.SessionVerifier, reader identityport.SessionStrengthReader) {
	h.ownerSessionVerifier, h.ownerStrengthReader = verifier, reader
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
	if body, err := io.ReadAll(io.LimitReader(c.Request.Body, 1)); err != nil || len(body) > 0 {
		response.Error(c, http.StatusBadRequest, 10001, "资料读取不接受请求正文。")
		return
	}
	_, ok := currentUserID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}

	space, err := h.svc.GetOwnSpaceForPrincipal(c.Request.Context(), h.fileReadPrincipal(c))
	if err != nil {
		writeSpaceError(c, err)
		return
	}
	c.Header("Cache-Control", "private, no-store")
	response.Success(c, space)
}

// ListOwnContents exposes author-only profile content states. This endpoint
// is intentionally separate from public /u/:username/contents so a visitor
// cannot infer a user's drafts, private posts or moderation history.
func (h *Handler) ListOwnContents(c *gin.Context) {
	_, ok := currentUserID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}
	page, pageSize, ok := pagination(c)
	if !ok {
		return
	}
	contents, total, err := h.svc.ListOwnContentsForPrincipal(c.Request.Context(), func(ctx context.Context) (identityport.PrincipalContext, error) {
		userID, _ := currentUserID(c)
		claims := &auth.JWTClaims{UserID: userID, SessionID: c.GetString("session_id"),
			AuthVersion: c.GetInt64("auth_version"), TokenType: auth.AccessTokenType}
		return identityport.ResolveUserSessionPrincipal(ctx, h.ownerSessionVerifier, h.ownerStrengthReader, claims)
	}, page, pageSize)
	if err != nil {
		writeOwnContentsReadError(c, err)
		return
	}
	response.List(c, contents, paginationMeta(page, pageSize, total))
}

func writeOwnContentsReadError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, identityport.ErrUserSessionPrincipalUnavailable):
		response.ErrorDescriptor(c, apperror.AuthInvalidToken, nil)
	case errors.Is(err, identityport.ErrPolicyFactsChanged):
		response.ErrorDescriptor(c, apperror.ResourceConflict, nil)
	case errors.Is(err, ErrSpacePluginDisabled):
		response.Error(c, http.StatusServiceUnavailable, 60006, "personal space feature is disabled")
	case errors.Is(err, identityport.ErrUserNotFound), errors.Is(err, ErrSpaceNotFound):
		response.Error(c, http.StatusNotFound, 30004, "space not found")
	default:
		response.WriteError(c, err)
	}
}

func (h *Handler) UpdateMe(c *gin.Context) {
	_, ok := currentUserID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}

	var req UpsertSpaceRequest
	if err := requestutil.BindJSONStrict(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 10001, "invalid request: "+err.Error())
		return
	}

	space, err := h.svc.UpsertOwnSpaceForPrincipal(c.Request.Context(), req, h.fileReadPrincipal(c))
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
	_, ok := currentUserID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}

	var req StylePackage
	if err := requestutil.BindJSONStrict(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 10001, "invalid request: "+err.Error())
		return
	}

	command, ok := h.styles.(AuthenticatedSpaceStyleCommands)
	if !ok || corestorage.NilImageAdapter(command) {
		writeSpaceError(c, ErrSpaceFactsUnavailable)
		return
	}
	applied, err := command.ApplyOwnSpaceStylePackage(c.Request.Context(), req, h.fileReadPrincipal(c))
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
	_, ok := currentUserID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}

	var req StyleHTMLRequest
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, safehtml.MaxHTMLLength*6+64*1024)
	if err := requestutil.BindJSONStrict(c, &req); err != nil {
		var limit *http.MaxBytesError
		if errors.As(err, &limit) {
			response.Error(c, 413, 10001, "HTML 内容超过允许的大小。")
			return
		}
		response.Error(c, http.StatusBadRequest, 10001, "invalid request: "+err.Error())
		return
	}

	commands, ok := h.styles.(AuthenticatedSpaceStyleVariants)
	if !ok || corestorage.NilImageAdapter(commands) {
		writeSpaceError(c, ErrSpaceFactsUnavailable)
		return
	}
	applied, err := commands.ApplyOwnSpaceCustomHTML(c.Request.Context(), req.HTML, h.fileReadPrincipal(c))
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
	if body, err := io.ReadAll(io.LimitReader(c.Request.Body, 1)); err != nil || len(body) > 0 {
		response.Error(c, http.StatusBadRequest, 10001, "源码包目录读取不接受请求正文。")
		return
	}
	_, ok := currentUserID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}
	catalog, ok := h.styles.(AuthenticatedSpaceStyleCatalog)
	if !ok {
		writeSpaceError(c, ErrSpaceFactsUnavailable)
		return
	}
	result, err := catalog.ListOwnSpaceSourceStylePacks(c.Request.Context(), h.fileReadPrincipal(c))
	if err != nil {
		writeSpaceError(c, err)
		return
	}
	c.Header("Cache-Control", "private, no-store")
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
	_, ok := currentUserID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}
	file, size, ok := openUploadedStylePack(c)
	if !ok {
		return
	}
	defer file.Close()

	commands, ok := h.styles.(AuthenticatedSpaceStyleVariants)
	if !ok || corestorage.NilImageAdapter(commands) {
		writeSpaceError(c, ErrSpaceFactsUnavailable)
		return
	}
	applied, err := commands.ApplyOwnSpaceStylePackZip(c.Request.Context(), file, size, h.fileReadPrincipal(c))
	if err != nil {
		writeSpaceError(c, err)
		return
	}
	response.Success(c, applied)
}

func (h *Handler) ApplySourceStylePack(c *gin.Context) {
	_, ok := currentUserID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}
	var req StylePackApplySourceRequest
	if err := requestutil.BindJSONStrict(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 10001, "invalid request: "+err.Error())
		return
	}
	commands, ok := h.styles.(AuthenticatedSpaceStyleVariants)
	if !ok || corestorage.NilImageAdapter(commands) {
		writeSpaceError(c, ErrSpaceFactsUnavailable)
		return
	}
	applied, err := commands.ApplyOwnSpaceSourceStylePack(c.Request.Context(), req.Name, h.fileReadPrincipal(c))
	if err != nil {
		writeSpaceError(c, err)
		return
	}
	response.Success(c, applied)
}

func (h *Handler) RollbackStyle(c *gin.Context) {
	_, ok := currentUserID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}
	if body, err := io.ReadAll(io.LimitReader(c.Request.Body, 1)); err != nil || len(body) > 0 {
		response.Error(c, 400, 10001, "风格回滚不接受请求正文。")
		return
	}
	command, ok := h.styles.(AuthenticatedSpaceStyleCommands)
	if !ok || corestorage.NilImageAdapter(command) {
		writeSpaceError(c, ErrSpaceFactsUnavailable)
		return
	}
	space, err := command.RollbackOwnSpaceStyle(c.Request.Context(), h.fileReadPrincipal(c))
	if err != nil {
		writeSpaceError(c, err)
		return
	}
	response.Success(c, space)
}

func (h *Handler) RestoreDefaultStyle(c *gin.Context) {
	_, ok := currentUserID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}
	if body, err := io.ReadAll(io.LimitReader(c.Request.Body, 1)); err != nil || len(body) > 0 {
		response.Error(c, 400, 10001, "恢复默认风格不接受请求正文。")
		return
	}
	command, ok := h.styles.(AuthenticatedSpaceStyleCommands)
	if !ok || corestorage.NilImageAdapter(command) {
		writeSpaceError(c, ErrSpaceFactsUnavailable)
		return
	}
	space, err := command.RestoreOwnSpaceDefaultStyle(c.Request.Context(), h.fileReadPrincipal(c))
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
	_, ok := currentUserID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}
	status, err := h.svc.StorageStatusForPrincipal(c.Request.Context(), h.fileReadPrincipal(c))
	if err != nil {
		writeCheckedFileReadError(c, err)
		return
	}
	c.Header("Cache-Control", "private, no-store")
	response.Success(c, status)
}

func (h *Handler) UploadAvatar(c *gin.Context) {
	_, ok := currentUserID(c)
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
	if form := c.Request.MultipartForm; form != nil {
		defer form.RemoveAll()
		if len(form.Value) != 0 || len(form.File) != 1 || len(form.File["file"]) != 1 {
			response.Error(c, http.StatusBadRequest, 10001, "头像请求只能包含一个文件。")
			return
		}
	}
	file, err := fileHeader.Open()
	if err != nil {
		response.Error(c, http.StatusBadRequest, 10001, "无法读取所选头像文件，请重新选择。")
		return
	}
	defer file.Close()

	uploaded, err := h.svc.UploadAvatarForPrincipal(c.Request.Context(), file, h.fileReadPrincipal(c))
	if err != nil {
		writeAvatarUploadError(c, err, maxBytes)
		return
	}
	response.Success(c, uploaded)
}

func writeAvatarUploadError(c *gin.Context, err error, maxBytes int64) {
	switch {
	case errors.Is(err, corestorage.ErrInlineImageCleanupPending):
		response.Error(c, http.StatusServiceUnavailable, 75001, "头像未保存，临时文件清理待处理，请稍后重试。")
	case errors.Is(err, identityport.ErrUserSessionPrincipalUnavailable), errors.Is(err, identityport.ErrPolicyFactsChanged), errors.Is(err, ErrSpaceFactsUnavailable), errors.Is(err, ErrSpaceOwnerDenied), errors.Is(err, ErrSpacePluginDisabled):
		writeSpaceError(c, err)
	case errors.Is(err, ErrSpaceFileTooLarge):
		response.ErrorWithDetails(c, http.StatusRequestEntityTooLarge, 10001,
			fmt.Sprintf("头像文件过大：单个文件最大 %s，请压缩或裁剪后重试。", formatSpaceBytes(maxBytes)), gin.H{
				"max_bytes":      maxBytes,
				"accepted_types": []string{"image/png", "image/jpeg", "image/gif", "image/webp"},
			})
	case errors.Is(err, ErrSpaceFileQuotaExceeded):
		response.ErrorWithDetails(c, http.StatusConflict, 10001,
			"个人空间剩余容量不足，无法保存头像。请删除不需要的文件，或联系管理员提高空间配额后重试。", gin.H{
				"max_bytes":      maxBytes,
				"accepted_types": []string{"image/png", "image/jpeg", "image/gif", "image/webp"},
			})
	case errors.Is(err, ErrSpaceFileUnsupportedType):
		response.ErrorWithDetails(c, http.StatusBadRequest, 10001,
			"头像格式不受支持，请上传 PNG、JPEG、GIF 或 WebP 图片。", gin.H{
				"accepted_types": []string{"image/png", "image/jpeg", "image/gif", "image/webp"},
				"max_bytes":      maxBytes,
			})
	case errors.Is(err, corestorage.ErrImageDimensions):
		response.ErrorWithDetails(c, http.StatusBadRequest, 10001,
			avatarImageDimensionMessage(err), avatarImageDimensionDetails(err, maxBytes))
	default:
		// Upload-specific unknown errors must not surface a decoder or storage
		// implementation detail to the user.
		response.WriteError(c, err)
	}
}

func avatarImageDimensionDetails(err error, maxBytes int64) gin.H {
	details := gin.H{
		"accepted_types":            []string{"image/png", "image/jpeg", "image/gif", "image/webp"},
		"max_bytes":                 maxBytes,
		"auto_resize_max_dimension": corestorage.DefaultImageMaxDimension,
		"max_decoded_pixels":        corestorage.MaxDecodedImagePixels,
	}
	var dimensions *corestorage.ImageDimensionError
	if errors.As(err, &dimensions) && dimensions != nil {
		details["width"] = dimensions.Width
		details["height"] = dimensions.Height
		details["max_decoded_pixels"] = dimensions.MaxPixels
	}
	return details
}

func avatarImageDimensionMessage(err error) string {
	var dimensions *corestorage.ImageDimensionError
	if errors.As(err, &dimensions) && dimensions != nil && dimensions.Width > 0 && dimensions.Height > 0 {
		return fmt.Sprintf("头像分辨率为 %d × %d，超过 %d 万像素的安全处理上限。请裁剪或缩小图片后再上传；JPEG/PNG 的最长边超过 %dpx 时会自动压缩。", dimensions.Width, dimensions.Height, dimensions.MaxPixels/10_000, corestorage.DefaultImageMaxDimension)
	}
	return fmt.Sprintf("头像分辨率超过 %d 万像素的安全处理上限。请裁剪或缩小图片后再上传；JPEG/PNG 的最长边超过 %dpx 时会自动压缩。", corestorage.MaxDecodedImagePixels/10_000, corestorage.DefaultImageMaxDimension)
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
	_, ok := currentUserID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}
	history, err := h.svc.ListAvatarsForPrincipal(c.Request.Context(), h.fileReadPrincipal(c))
	if err != nil {
		writeCheckedFileReadError(c, err)
		return
	}
	c.Header("Cache-Control", "private, no-store")
	response.Success(c, history)
}

func (h *Handler) SelectAvatar(c *gin.Context) {
	_, ok := currentUserID(c)
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
	selected, err := h.svc.SelectAvatarForPrincipal(c.Request.Context(), strings.TrimSpace(req.FileName), h.fileReadPrincipal(c))
	if err != nil {
		writeSpaceError(c, err)
		return
	}
	response.Success(c, selected)
}

func (h *Handler) CleanupAvatarHistory(c *gin.Context) {
	if _, ok := currentUserID(c); !ok {
		response.Error(c, 401, 20001, "unauthorized")
		return
	}
	if body, err := io.ReadAll(io.LimitReader(c.Request.Body, 1)); err != nil || len(body) > 0 {
		response.Error(c, 400, 10001, "头像清理不接受请求正文。")
		return
	}
	ack, err := h.svc.CleanupAvatarHistoryForPrincipal(c.Request.Context(), h.fileReadPrincipal(c))
	if err != nil {
		writeSpaceError(c, err)
		return
	}
	response.Success(c, ack)
}

func (h *Handler) ServeAvatarFile(c *gin.Context) {
	var resolve identityport.SessionPrincipalResolver
	if c.GetHeader("Authorization") != "" {
		resolve = h.fileReadPrincipal(c)
	}
	image, err := h.svc.ReadAvatarForPrincipal(c.Request.Context(), c.Param("user_id"), c.Param("filename"), resolve)
	if err != nil {
		writeCheckedFileReadError(c, err)
		return
	}
	corestorage.ServeCheckedImage(c, image)
}

func (h *Handler) fileReadPrincipal(c *gin.Context) identityport.SessionPrincipalResolver {
	claims := &auth.JWTClaims{UserID: c.GetString("user_id"), SessionID: c.GetString("session_id"), AuthVersion: c.GetInt64("auth_version"), TokenType: auth.AccessTokenType}
	return func(ctx context.Context) (identityport.PrincipalContext, error) {
		return identityport.ResolveUserSessionPrincipal(ctx, h.ownerSessionVerifier, h.ownerStrengthReader, claims)
	}
}

func writeCheckedFileReadError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, identityport.ErrUserSessionPrincipalUnavailable):
		response.Error(c, 401, 20002, "请重新登录后读取个人文件。")
	case errors.Is(err, identityport.ErrPolicyFactsChanged):
		response.Error(c, 409, 10004, "资源或访问权限已变化，请重新加载。")
	case errors.Is(err, ErrSpaceFileNotFound):
		response.Error(c, 404, 75004, "文件不存在或无权访问。")
	case errors.Is(err, ErrSpacePluginDisabled):
		response.Error(c, 403, 75001, "个人空间当前未启用。")
	default:
		response.Error(c, 503, 10006, "暂时无法检查个人文件访问权限。")
	}
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
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, stylepack.MaxPackageBytes+64*1024)
	fileHeader, err := c.FormFile("file")
	if err != nil {
		if form := c.Request.MultipartForm; form != nil {
			form.RemoveAll()
		}
		var limit *http.MaxBytesError
		if errors.As(err, &limit) {
			response.Error(c, 413, 10001, "风格包超过允许的大小。")
			return nil, 0, false
		}
		response.Error(c, http.StatusBadRequest, 10001, "style pack zip file is required")
		return nil, 0, false
	}
	form := c.Request.MultipartForm
	cleanup := func() error {
		if form != nil {
			return form.RemoveAll()
		}
		return nil
	}
	if form != nil {
		if len(form.Value) != 0 || len(form.File) != 1 || len(form.File["file"]) != 1 {
			_ = cleanup()
			response.Error(c, 400, 10001, "风格包请求只能包含一个文件。")
			return nil, 0, false
		}
	}
	if fileHeader.Size > stylepack.MaxPackageBytes {
		_ = cleanup()
		response.Error(c, 413, 10001, "风格包超过允许的大小。")
		return nil, 0, false
	}
	file, err := fileHeader.Open()
	if err != nil {
		_ = cleanup()
		response.Error(c, http.StatusBadRequest, 10001, "无法读取风格包文件。")
		return nil, 0, false
	}
	reader, ok := file.(uploadedStylePackFile)
	if !ok {
		_ = file.Close()
		_ = cleanup()
		response.Error(c, http.StatusBadRequest, 10001, "style pack file must be seekable")
		return nil, 0, false
	}
	return &stylePackUploadReader{uploadedStylePackFile: reader, cleanup: cleanup}, fileHeader.Size, true
}

type stylePackUploadReader struct {
	uploadedStylePackFile
	cleanup func() error
}

func (r *stylePackUploadReader) Close() error {
	return errors.Join(r.uploadedStylePackFile.Close(), r.cleanup())
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
	case errors.Is(err, identityport.ErrUserSessionPrincipalUnavailable):
		response.ErrorDescriptor(c, apperror.AuthInvalidToken, nil)
	case errors.Is(err, identityport.ErrPolicyFactsChanged):
		response.ErrorDescriptor(c, apperror.ResourceConflict, nil)
	case errors.Is(err, ErrSpaceFactsUnavailable):
		response.Error(c, http.StatusServiceUnavailable, 75001, "个人空间校验暂不可用，请稍后重试。")
	case errors.Is(err, ErrSpaceOwnerDenied):
		response.ErrorDescriptor(c, apperror.PermissionDenied, nil)
	case errors.Is(err, ErrSpaceOwnerInvalid):
		response.ErrorDescriptor(c, apperror.RequestInvalid, nil)
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
