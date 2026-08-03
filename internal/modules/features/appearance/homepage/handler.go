package homepage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/campusos/CampusOS/internal/modules/features/appearance/stylepack"
	requestutil "github.com/campusos/CampusOS/pkg/request"
	"github.com/campusos/CampusOS/pkg/response"
	"github.com/gin-gonic/gin"
)

type Handler struct {
	svc Application
}

type Application interface {
	PublicConfig(context.Context) (*Config, error)
	LogoAsset(context.Context) (*LogoAsset, error)
	SaveLogo(context.Context, string, io.Reader) (*LogoInfo, error)
	ResetLogo(context.Context, string) (*LogoInfo, error)
	ValidateStylePackZip(io.ReaderAt, int64) (*StylePackResult, error)
	StylePackExample(context.Context) (*stylepack.FileBundle, error)
	ListSourceStylePacks(context.Context) (*stylepack.SourcePackList, error)
	ApplyStylePackZip(context.Context, io.ReaderAt, int64) (*Config, error)
	ApplySourceStylePack(context.Context, string) (*Config, error)
	RollbackStylePack(context.Context) (*Config, error)
}

func NewHandler(svc Application) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) GetConfig(c *gin.Context) {
	cfg, err := h.svc.PublicConfig(c.Request.Context())
	if err != nil {
		response.Error(c, http.StatusInternalServerError, 10006, err.Error())
		return
	}
	response.Success(c, cfg)
}

func (h *Handler) GetLogo(c *gin.Context) {
	asset, err := h.svc.LogoAsset(c.Request.Context())
	if err != nil {
		writeHomepageError(c, err)
		return
	}
	etag := `"campusos-logo-` + asset.Version + `"`
	if c.GetHeader("If-None-Match") == etag {
		c.Status(http.StatusNotModified)
		return
	}
	c.Header("Cache-Control", "public, max-age=300")
	c.Header("ETag", etag)
	c.Header("X-Content-Type-Options", "nosniff")
	c.Data(http.StatusOK, asset.MIMEType, asset.Data)
}

func (h *Handler) UploadLogo(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, DefaultLogoMaxBytes+64*1024)
	fileHeader, err := c.FormFile("file")
	if err != nil {
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			writeHomepageError(c, ErrLogoTooLarge)
			return
		}
		response.ErrorWithDetails(c, http.StatusBadRequest, 10001, "请选择要上传的系统 Logo 图片。", gin.H{
			"accepted_types": []string{"image/png", "image/jpeg"},
			"max_bytes":      DefaultLogoMaxBytes,
		})
		return
	}
	if fileHeader.Size > DefaultLogoMaxBytes {
		writeHomepageError(c, ErrLogoTooLarge)
		return
	}
	file, err := fileHeader.Open()
	if err != nil {
		response.Error(c, http.StatusBadRequest, 10001, "无法读取所选 Logo 文件，请重新选择。")
		return
	}
	defer file.Close()

	logo, err := h.svc.SaveLogo(c.Request.Context(), currentActorID(c), file)
	if err != nil {
		writeHomepageError(c, err)
		return
	}
	response.Success(c, logo)
}

func (h *Handler) ResetLogo(c *gin.Context) {
	logo, err := h.svc.ResetLogo(c.Request.Context(), currentActorID(c))
	if err != nil {
		writeHomepageError(c, err)
		return
	}
	response.Success(c, logo)
}

func (h *Handler) ValidateStylePack(c *gin.Context) {
	file, size, ok := openUploadedStylePack(c)
	if !ok {
		return
	}
	defer file.Close()

	result, err := h.svc.ValidateStylePackZip(file, size)
	if err != nil {
		writeHomepageError(c, err)
		return
	}
	response.Success(c, result)
}

func (h *Handler) StylePackExample(c *gin.Context) {
	example, err := h.svc.StylePackExample(c.Request.Context())
	if err != nil {
		writeHomepageError(c, err)
		return
	}
	response.Success(c, example)
}

func (h *Handler) StylePackExampleZip(c *gin.Context) {
	example, err := h.svc.StylePackExample(c.Request.Context())
	if err != nil {
		writeHomepageError(c, err)
		return
	}
	data, err := stylepack.ZipBundle(*example)
	if err != nil {
		writeHomepageError(c, err)
		return
	}
	c.Header("Content-Disposition", `attachment; filename="`+example.Filename+`.zip"`)
	c.Data(http.StatusOK, "application/zip", data)
}

func (h *Handler) ListSourceStylePacks(c *gin.Context) {
	result, err := h.svc.ListSourceStylePacks(c.Request.Context())
	if err != nil {
		writeHomepageError(c, err)
		return
	}
	response.Success(c, result)
}

func (h *Handler) ApplyStylePack(c *gin.Context) {
	file, size, ok := openUploadedStylePack(c)
	if !ok {
		return
	}
	defer file.Close()

	cfg, err := h.svc.ApplyStylePackZip(c.Request.Context(), file, size)
	if err != nil {
		writeHomepageError(c, err)
		return
	}
	response.Success(c, cfg)
}

func (h *Handler) ApplySourceStylePack(c *gin.Context) {
	var req StylePackApplySourceRequest
	if err := requestutil.BindJSONStrict(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 10001, "invalid request: "+err.Error())
		return
	}
	cfg, err := h.svc.ApplySourceStylePack(c.Request.Context(), req.Name)
	if err != nil {
		writeHomepageError(c, err)
		return
	}
	response.Success(c, cfg)
}

func (h *Handler) RollbackStylePack(c *gin.Context) {
	cfg, err := h.svc.RollbackStylePack(c.Request.Context())
	if err != nil {
		writeHomepageError(c, err)
		return
	}
	response.Success(c, cfg)
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

func writeHomepageError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrLogoTooLarge):
		response.ErrorWithDetails(c, http.StatusRequestEntityTooLarge, 10001,
			fmt.Sprintf("Logo 文件过大：单个文件最大 %s，请压缩或缩小图片后重试。", formatBytes(DefaultLogoMaxBytes)), gin.H{
				"max_bytes": DefaultLogoMaxBytes, "accepted_types": []string{"image/png", "image/jpeg"},
			})
		return
	case errors.Is(err, ErrLogoUnsupported):
		response.ErrorWithDetails(c, http.StatusBadRequest, 10001,
			"Logo 格式不受支持：仅支持有效的 PNG 或 JPEG 图片。", gin.H{
				"accepted_types": []string{"image/png", "image/jpeg"}, "max_bytes": DefaultLogoMaxBytes,
			})
		return
	case errors.Is(err, ErrLogoAssetNotFound):
		response.Error(c, http.StatusNotFound, 30004, "系统 Logo 文件不存在，请联系管理员检查品牌资源。")
		return
	case errors.Is(err, ErrLogoConfigDisabled):
		response.Error(c, http.StatusServiceUnavailable, 60006, "外观功能当前不可用，暂时不能替换系统 Logo。")
		return
	case errors.Is(err, ErrLogoStoreFailed):
		response.Error(c, http.StatusInternalServerError, 10006, "Logo 保存失败，请稍后重试或检查数据目录写入权限。")
		return
	}
	if errors.Is(err, ErrStylePackInvalid) {
		response.Error(c, http.StatusBadRequest, 10001, err.Error())
		return
	}
	response.Error(c, http.StatusInternalServerError, 10006, err.Error())
}

func currentActorID(c *gin.Context) string {
	if value, ok := c.Get("user_id"); ok {
		if actorID, ok := value.(string); ok && strings.TrimSpace(actorID) != "" {
			return strings.TrimSpace(actorID)
		}
	}
	return "system"
}

func formatBytes(value int64) string {
	if value >= 1024*1024 && value%(1024*1024) == 0 {
		return fmt.Sprintf("%d MB", value/(1024*1024))
	}
	if value >= 1024 && value%1024 == 0 {
		return fmt.Sprintf("%d KB", value/1024)
	}
	return fmt.Sprintf("%d 字节", value)
}
