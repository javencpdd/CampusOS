package richtext

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	corestorage "github.com/campusos/CampusOS/internal/modules/core/userstorage"
	requestutil "github.com/campusos/CampusOS/pkg/request"
	"github.com/campusos/CampusOS/pkg/response"
	"github.com/gin-gonic/gin"
)

type Handler struct {
	svc *Service
}

const richTextAssetFormSlack = int64(64 * 1024)

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) Status(c *gin.Context) {
	response.Success(c, h.svc.Status())
}

func (h *Handler) CreateDraft(c *gin.Context) {
	userID, username, ok := currentUser(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}
	var req SaveArticleRequest
	if err := requestutil.BindJSONStrict(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 10001, "invalid request: "+err.Error())
		return
	}
	result, err := h.svc.CreateDraft(c.Request.Context(), userID, username, req)
	if err != nil {
		writeRichTextError(c, err)
		return
	}
	response.Created(c, result)
}

func (h *Handler) UpdateDraft(c *gin.Context) {
	userID, _, ok := currentUser(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}
	var req SaveArticleRequest
	if err := requestutil.BindJSONStrict(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 10001, "invalid request: "+err.Error())
		return
	}
	result, err := h.svc.UpdateDraft(c.Request.Context(), c.Param("id"), userID, req)
	if err != nil {
		writeRichTextError(c, err)
		return
	}
	response.Success(c, result)
}

func (h *Handler) Publish(c *gin.Context) {
	userID, _, ok := currentUser(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}
	result, err := h.svc.Publish(c.Request.Context(), c.Param("id"), userID)
	if err != nil {
		writeRichTextError(c, err)
		return
	}
	response.Success(c, result)
}

func (h *Handler) Offline(c *gin.Context) {
	userID, _, ok := currentUser(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}
	result, err := h.svc.Offline(c.Request.Context(), c.Param("id"), userID)
	if err != nil {
		writeRichTextError(c, err)
		return
	}
	response.Success(c, result)
}

func (h *Handler) Delete(c *gin.Context) {
	userID, _, ok := currentUser(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}
	if err := h.svc.Delete(c.Request.Context(), c.Param("id"), userID); err != nil {
		writeRichTextError(c, err)
		return
	}
	response.NoContent(c)
}

func (h *Handler) AdminOffline(c *gin.Context) {
	adminID, _, ok := currentUser(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}
	reason := "administrator marked richtext article offline"
	if c.Request.ContentLength > 0 {
		var req struct {
			Reason string `json:"reason"`
		}
		if err := requestutil.BindJSONStrict(c, &req); err != nil {
			response.Error(c, http.StatusBadRequest, 10001, "invalid moderation request: "+err.Error())
			return
		}
		if strings.TrimSpace(req.Reason) == "" {
			response.Error(c, http.StatusBadRequest, 10001, "moderation reason is required")
			return
		}
		reason = strings.TrimSpace(req.Reason)
	}
	result, err := h.svc.AdminOfflineWithReason(c.Request.Context(), c.Param("id"), adminID, reason)
	if err != nil {
		writeRichTextError(c, err)
		return
	}
	response.Success(c, result)
}

func (h *Handler) AdminRestore(c *gin.Context) {
	adminID, _, ok := currentUser(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}
	result, err := h.svc.AdminRestore(c.Request.Context(), c.Param("id"), adminID)
	if err != nil {
		writeRichTextError(c, err)
		return
	}
	response.Success(c, result)
}

func (h *Handler) AdminDelete(c *gin.Context) {
	adminID, _, ok := currentUser(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}
	if err := h.svc.AdminDelete(c.Request.Context(), c.Param("id"), adminID); err != nil {
		writeRichTextError(c, err)
		return
	}
	response.NoContent(c)
}

func (h *Handler) Preview(c *gin.Context) {
	var req PreviewRequest
	if err := requestutil.BindJSONStrict(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 10001, "invalid request: "+err.Error())
		return
	}
	result, err := h.svc.Preview(c.Request.Context(), req.ContentHTML)
	if err != nil {
		writeRichTextError(c, err)
		return
	}
	response.Success(c, result)
}

func (h *Handler) GetPublished(c *gin.Context) {
	article, err := h.svc.GetArticle(c.Request.Context(), c.Param("id"), "")
	if err != nil {
		writeRichTextError(c, err)
		return
	}
	response.Success(c, article)
}

func (h *Handler) GetMine(c *gin.Context) {
	userID, _, ok := currentUser(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}
	article, err := h.svc.GetArticle(c.Request.Context(), c.Param("id"), userID)
	if err != nil {
		writeRichTextError(c, err)
		return
	}
	response.Success(c, article)
}

func (h *Handler) UploadAsset(c *gin.Context) {
	userID, _, ok := currentUser(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "请先登录后再上传文章图片。")
		return
	}
	maxBytes := h.svc.MaxAssetBytes()
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBytes+richTextAssetFormSlack)
	fileHeader, err := c.FormFile("file")
	if err != nil {
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			writeRichTextAssetUploadError(c, ErrAssetTooLarge, maxBytes, 0)
			return
		}
		response.ErrorWithDetails(c, http.StatusBadRequest, 10001, "请选择要上传的文章图片。", richTextAssetUploadDetails(maxBytes, 0))
		return
	}
	if fileHeader.Size > maxBytes {
		writeRichTextAssetUploadError(c, ErrAssetTooLarge, maxBytes, fileHeader.Size)
		return
	}
	file, err := fileHeader.Open()
	if err != nil {
		response.Error(c, http.StatusBadRequest, 10001, "无法读取所选图片文件，请重新选择后再试。")
		return
	}
	defer file.Close()

	asset, err := h.svc.UploadAsset(c.Request.Context(), userID, fileHeader.Filename, file, c.PostForm("thread_id"), c.PostForm("article_content_id"))
	if err != nil {
		writeRichTextAssetUploadError(c, err, maxBytes, fileHeader.Size)
		return
	}
	response.Success(c, asset)
}

func (h *Handler) ListMyAssets(c *gin.Context) {
	userID, _, ok := currentUser(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "请先登录后查看已上传的文章图片。")
		return
	}
	items, err := h.svc.ListMyAssets(c.Request.Context(), userID)
	if err != nil {
		writeRichTextError(c, err)
		return
	}
	response.Success(c, gin.H{"items": items, "limit": 200})
}

func (h *Handler) ServeAsset(c *gin.Context) {
	path, err := h.svc.AssetPath(c.Param("user_id"), c.Param("filename"))
	if err != nil {
		writeRichTextError(c, err)
		return
	}
	c.File(path)
}

func currentUser(c *gin.Context) (string, string, bool) {
	rawID, ok := c.Get("user_id")
	if !ok {
		return "", "", false
	}
	id, ok := rawID.(string)
	if !ok || id == "" {
		return "", "", false
	}
	username := "Anonymous"
	if rawName, ok := c.Get("username"); ok {
		if name, ok := rawName.(string); ok && name != "" {
			username = name
		}
	}
	return id, username, true
}

func writeRichTextError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrPluginDisabled):
		response.Error(c, http.StatusForbidden, 73001, "图文文章功能当前未启用。")
	case errors.Is(err, ErrInvalidArticle):
		response.Error(c, http.StatusBadRequest, 73002, "图文文章请求无效，请检查标题、正文和发布状态后重试。")
	case errors.Is(err, ErrAssetInvalid):
		response.Error(c, http.StatusBadRequest, 73002, "文章图片请求无效，请重新选择图片后再试。")
	case errors.Is(err, ErrAssetTooLarge):
		response.Error(c, http.StatusRequestEntityTooLarge, 73002, "文章图片超过单文件大小限制，请压缩或裁剪后重试。")
	case errors.Is(err, ErrAssetQuotaExceeded):
		response.Error(c, http.StatusConflict, 73002, "个人空间剩余容量不足，无法保存文章图片。请清理文件或联系管理员扩容后重试。")
	case errors.Is(err, ErrAssetUnsupported):
		response.Error(c, http.StatusBadRequest, 73002, "图片格式不受支持，请上传 PNG、JPEG、GIF 或 WebP 图片。")
	case errors.Is(err, corestorage.ErrImageDimensions):
		response.Error(c, http.StatusBadRequest, 73002, richTextAssetDimensionMessage(err))
	case errors.Is(err, ErrPermissionDenied):
		response.Error(c, http.StatusForbidden, 73003, "无权操作该图文文章。")
	case errors.Is(err, ErrArticleNotFound), errors.Is(err, ErrAssetNotFound):
		response.Error(c, http.StatusNotFound, 73004, "图文文章或图片不存在，或当前无权访问。")
	default:
		response.WriteError(c, err)
	}
}

func writeRichTextAssetUploadError(c *gin.Context, err error, maxBytes, providedBytes int64) {
	switch {
	case errors.Is(err, ErrAssetTooLarge):
		response.ErrorWithDetails(c, http.StatusRequestEntityTooLarge, 73002,
			fmt.Sprintf("文章图片文件过大：单个文件最大 %s，请压缩或裁剪后重试。", formatRichTextAssetBytes(maxBytes)),
			richTextAssetUploadDetails(maxBytes, providedBytes))
	case errors.Is(err, ErrAssetUnsupported):
		response.ErrorWithDetails(c, http.StatusBadRequest, 73002,
			"图片格式不受支持，请上传 PNG、JPEG、GIF 或 WebP 图片。",
			richTextAssetUploadDetails(maxBytes, providedBytes))
	case errors.Is(err, corestorage.ErrImageDimensions):
		response.ErrorWithDetails(c, http.StatusBadRequest, 73002,
			richTextAssetDimensionMessage(err), richTextAssetDimensionDetails(err, maxBytes, providedBytes))
	case errors.Is(err, ErrAssetQuotaExceeded):
		response.ErrorWithDetails(c, http.StatusConflict, 73002,
			"个人空间剩余容量不足，无法保存文章图片。请删除不需要的文件，或联系管理员提高空间配额后重试。",
			richTextAssetUploadDetails(maxBytes, providedBytes))
	case errors.Is(err, ErrAssetInvalid):
		response.Error(c, http.StatusBadRequest, 73002, "文章图片请求无效，请重新选择图片后再试。")
	default:
		response.WriteError(c, err)
	}
}

func richTextAssetUploadDetails(maxBytes, providedBytes int64) gin.H {
	details := gin.H{
		"accepted_types":            []string{"image/png", "image/jpeg", "image/gif", "image/webp"},
		"max_bytes":                 maxBytes,
		"auto_resize_max_dimension": corestorage.DefaultImageMaxDimension,
		"max_decoded_pixels":        corestorage.MaxDecodedImagePixels,
	}
	if providedBytes > 0 {
		details["provided_bytes"] = providedBytes
	}
	return details
}

func richTextAssetDimensionDetails(err error, maxBytes, providedBytes int64) gin.H {
	details := richTextAssetUploadDetails(maxBytes, providedBytes)
	var dimensions *corestorage.ImageDimensionError
	if errors.As(err, &dimensions) && dimensions != nil {
		details["width"] = dimensions.Width
		details["height"] = dimensions.Height
		details["max_decoded_pixels"] = dimensions.MaxPixels
	}
	return details
}

func richTextAssetDimensionMessage(err error) string {
	var dimensions *corestorage.ImageDimensionError
	if errors.As(err, &dimensions) && dimensions != nil && dimensions.Width > 0 && dimensions.Height > 0 {
		return fmt.Sprintf("图片分辨率为 %d × %d，超过 %d 万像素的安全处理上限。请裁剪或缩小图片后再上传；JPEG/PNG 的最长边超过 %dpx 时会自动压缩。", dimensions.Width, dimensions.Height, dimensions.MaxPixels/10_000, corestorage.DefaultImageMaxDimension)
	}
	return fmt.Sprintf("图片分辨率超过 %d 万像素的安全处理上限。请裁剪或缩小图片后再上传；JPEG/PNG 的最长边超过 %dpx 时会自动压缩。", corestorage.MaxDecodedImagePixels/10_000, corestorage.DefaultImageMaxDimension)
}

func formatRichTextAssetBytes(value int64) string {
	if value >= 1024*1024 && value%(1024*1024) == 0 {
		return fmt.Sprintf("%d MB", value/(1024*1024))
	}
	if value >= 1024 && value%1024 == 0 {
		return fmt.Sprintf("%d KB", value/1024)
	}
	return fmt.Sprintf("%d B", value)
}
