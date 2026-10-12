package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	communityport "github.com/campusos/CampusOS/internal/modules/core/community/port"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/pkg/auth"
	"net/http"

	"github.com/campusos/CampusOS/pkg/response"
	"github.com/gin-gonic/gin"
)

type Handler struct {
	images   *ContentImageStore
	query    communityport.AuthenticatedThreadQuery
	verifier identityport.SessionVerifier
	strength identityport.SessionStrengthReader
}

func NewHandler(images *ContentImageStore) *Handler { return &Handler{images: images} }

func (h *Handler) SetReadProof(query communityport.AuthenticatedThreadQuery, verifier identityport.SessionVerifier, strength identityport.SessionStrengthReader) {
	h.query, h.verifier, h.strength = query, verifier, strength
}

func (h *Handler) readPrincipal(c *gin.Context) identityport.SessionPrincipalResolver {
	claims := &auth.JWTClaims{UserID: c.GetString("user_id"), SessionID: c.GetString("session_id"), AuthVersion: c.GetInt64("auth_version"), TokenType: auth.AccessTokenType}
	return func(ctx context.Context) (identityport.PrincipalContext, error) {
		return identityport.ResolveUserSessionPrincipal(ctx, h.verifier, h.strength, claims)
	}
}

func (h *Handler) UploadContentImage(c *gin.Context) {
	userID, ok := c.Get("user_id")
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "请先登录后再上传正文图片。")
		return
	}
	userIDValue, ok := userID.(string)
	if !ok || userIDValue == "" {
		response.Error(c, http.StatusUnauthorized, 20001, "请先登录后再上传正文图片。")
		return
	}
	// Enforce the upload limit before multipart parsing can spill an arbitrarily
	// large request to a temporary file. The small allowance covers form headers.
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, h.images.MaxBytes()+contentImageFormSlack)
	fileHeader, err := c.FormFile("file")
	if err != nil {
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			writeContentImageError(c, ErrImageTooLarge, h.images.MaxBytes(), 0)
			return
		}
		response.ErrorWithDetails(c, http.StatusBadRequest, 10001, "请选择要插入正文的图片文件。", contentImageUploadDetails(h.images.MaxBytes(), 0))
		return
	}
	file, err := fileHeader.Open()
	if err != nil {
		response.Error(c, http.StatusBadRequest, 10001, "无法读取所选图片文件，请重新选择后再试。")
		return
	}
	defer file.Close()
	asset, err := h.images.Save(userIDValue, fileHeader.Filename, file)
	if err != nil {
		writeContentImageError(c, err, h.images.MaxBytes(), fileHeader.Size)
		return
	}
	response.Success(c, asset)
}

// ListMyContentImages returns a read-only inventory for the current owner.
// The endpoint is intentionally authenticated even though individual image
// URLs may be publicly referenced by an already-published post.
func (h *Handler) ListMyContentImages(c *gin.Context) {
	items, err := h.images.ListForPrincipal(c.Request.Context(), h.readPrincipal(c))
	if err != nil {
		writeImageReadError(c, err)
		return
	}
	response.Success(c, gin.H{"items": items, "limit": maxListedContentImages})
}

func (h *Handler) ServeContentImage(c *gin.Context) {
	var resolve identityport.SessionPrincipalResolver
	if c.GetHeader("Authorization") != "" {
		resolve = h.readPrincipal(c)
	}
	image, err := h.images.ReadForPrincipal(c.Request.Context(), c.Param("user_id"), c.Param("filename"), c.Query("thread_id"), h.query, resolve)
	if err != nil {
		writeImageReadError(c, err)
		return
	}
	ServeCheckedImage(c, image)
}

func ServeCheckedImage(c *gin.Context, image ImageBytes) {
	c.Header("Cache-Control", "private, no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Content-Type", image.MimeType)
	http.ServeContent(c.Writer, c.Request, "image", image.ModifiedAt, bytes.NewReader(image.Data))
}

func writeImageReadError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, identityport.ErrUserSessionPrincipalUnavailable):
		response.Error(c, 401, 20002, "请重新登录后读取图片。")
	case errors.Is(err, identityport.ErrPolicyFactsChanged):
		response.Error(c, 409, 74009, "图片或访问权限已变化，请重新加载。")
	case errors.Is(err, ErrImageNotFound):
		response.Error(c, 404, 74004, "图片不存在或无权访问。")
	default:
		response.Error(c, 503, 10006, "暂时无法检查图片访问权限。")
	}
}

func writeContentImageError(c *gin.Context, err error, maxBytes, providedBytes int64) {
	switch {
	case errors.Is(err, ErrImageNotFound):
		response.Error(c, http.StatusNotFound, 74004, "正文图片不存在或已被删除。")
	case errors.Is(err, ErrImageTooLarge):
		response.ErrorWithDetails(c, http.StatusRequestEntityTooLarge, 74002,
			fmt.Sprintf("正文图片文件过大：单个文件最大 %s，请压缩或裁剪后重试。", formatContentImageBytes(maxBytes)),
			contentImageUploadDetails(maxBytes, providedBytes))
	case errors.Is(err, ErrImageUnsupported):
		response.ErrorWithDetails(c, http.StatusBadRequest, 74002,
			"图片格式不受支持，请上传 PNG、JPEG、GIF 或 WebP 图片。",
			contentImageUploadDetails(maxBytes, providedBytes))
	case errors.Is(err, ErrImageDimensions):
		response.ErrorWithDetails(c, http.StatusBadRequest, 74002,
			contentImageDimensionMessage(err), contentImageDimensionDetails(err, maxBytes, providedBytes))
	case errors.Is(err, ErrUnsafePath):
		response.Error(c, http.StatusBadRequest, 74002, "图片文件名或请求路径不合法，请重新选择文件后再试。")
	case errors.Is(err, ErrQuotaExceeded):
		response.ErrorWithDetails(c, http.StatusConflict, 74003,
			"个人空间剩余容量不足，无法保存正文图片。请删除不需要的文件，或联系管理员提高空间配额后重试。",
			contentImageUploadDetails(maxBytes, providedBytes))
	default:
		// Keep unexpected causes in server logs and return the registered safe
		// Chinese generic message instead of leaking a storage/decoder error.
		response.WriteError(c, err)
	}
}

func contentImageUploadDetails(maxBytes, providedBytes int64) gin.H {
	details := gin.H{
		"accepted_types":            []string{"image/png", "image/jpeg", "image/gif", "image/webp"},
		"max_bytes":                 maxBytes,
		"auto_resize_max_dimension": DefaultImageMaxDimension,
		"max_decoded_pixels":        MaxDecodedImagePixels,
	}
	if providedBytes > 0 {
		details["provided_bytes"] = providedBytes
	}
	return details
}

func contentImageDimensionDetails(err error, maxBytes, providedBytes int64) gin.H {
	details := contentImageUploadDetails(maxBytes, providedBytes)
	var dimensions *ImageDimensionError
	if errors.As(err, &dimensions) && dimensions != nil {
		details["width"] = dimensions.Width
		details["height"] = dimensions.Height
		details["max_decoded_pixels"] = dimensions.MaxPixels
	}
	return details
}

func contentImageDimensionMessage(err error) string {
	var dimensions *ImageDimensionError
	if errors.As(err, &dimensions) && dimensions != nil && dimensions.Width > 0 && dimensions.Height > 0 {
		return fmt.Sprintf("图片分辨率为 %d × %d，超过 %d 万像素的安全处理上限。请裁剪或缩小图片后再上传；JPEG/PNG 的最长边超过 %dpx 时会自动压缩。", dimensions.Width, dimensions.Height, dimensions.MaxPixels/10_000, DefaultImageMaxDimension)
	}
	return fmt.Sprintf("图片分辨率超过 %d 万像素的安全处理上限。请裁剪或缩小图片后再上传；JPEG/PNG 的最长边超过 %dpx 时会自动压缩。", MaxDecodedImagePixels/10_000, DefaultImageMaxDimension)
}

func formatContentImageBytes(value int64) string {
	if value >= 1024*1024 && value%(1024*1024) == 0 {
		return fmt.Sprintf("%d MB", value/(1024*1024))
	}
	if value >= 1024 && value%1024 == 0 {
		return fmt.Sprintf("%d KB", value/1024)
	}
	return fmt.Sprintf("%d B", value)
}
