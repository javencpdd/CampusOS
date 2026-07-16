package homepage

import (
	"context"
	"errors"
	"io"
	"net/http"

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
	if errors.Is(err, ErrStylePackInvalid) {
		response.Error(c, http.StatusBadRequest, 10001, err.Error())
		return
	}
	response.Error(c, http.StatusInternalServerError, 10006, err.Error())
}
