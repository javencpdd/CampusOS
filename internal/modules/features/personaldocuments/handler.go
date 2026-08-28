package personaldocuments

import (
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/campusos/CampusOS/pkg/apperror"
	requestutil "github.com/campusos/CampusOS/pkg/request"
	"github.com/campusos/CampusOS/pkg/response"
	"github.com/gin-gonic/gin"
)

type Handler struct{ service *Service }

func NewHandler(service *Service) *Handler { return &Handler{service: service} }
func (h *Handler) List(c *gin.Context) {
	owner, ok := documentOwner(c)
	if !ok {
		return
	}
	items, e := h.service.List(c.Request.Context(), owner, strings.TrimSpace(c.Query("status")))
	if e != nil {
		response.WriteError(c, e)
		return
	}
	response.Success(c, gin.H{"items": items})
}
func (h *Handler) Create(c *gin.Context) {
	owner, ok := documentOwner(c)
	if !ok {
		return
	}
	var req CreateRequest
	if e := requestutil.BindJSONStrict(c, &req); e != nil {
		response.ErrorDescriptor(c, apperror.PersonalDocumentInvalid, gin.H{"field": "request", "reason": "请求体格式不正确"})
		return
	}
	item, e := h.service.CreateText(c.Request.Context(), owner, req)
	if e != nil {
		response.WriteError(c, e)
		return
	}
	response.Created(c, item)
}
func (h *Handler) Upload(c *gin.Context) {
	owner, ok := documentOwner(c)
	if !ok {
		return
	}
	file, e := c.FormFile("file")
	if e != nil {
		response.ErrorDescriptor(c, apperror.PersonalDocumentInvalid, gin.H{"field": "file", "reason": "请选择要上传的文件"})
		return
	}
	format := formatFromName(file.Filename)
	if format == "" {
		response.ErrorDescriptor(c, apperror.PersonalDocumentInvalid, gin.H{"field": "file", "reason": "仅支持 TXT、Markdown、CampusDoc、PDF 或 DOCX 文件"})
		return
	}
	reader, e := file.Open()
	if e != nil {
		response.ErrorDescriptor(c, apperror.PersonalDocumentInvalid, gin.H{"field": "file", "reason": "文件无法读取"})
		return
	}
	defer reader.Close()
	name := strings.TrimSpace(c.PostForm("name"))
	if name == "" {
		name = file.Filename
	}
	item, e := h.service.Upload(c.Request.Context(), owner, name, format, file.Header.Get("Content-Type"), file.Size, reader)
	if e != nil {
		response.WriteError(c, e)
		return
	}
	response.Created(c, item)
}
func (h *Handler) Get(c *gin.Context) {
	owner, ok := documentOwner(c)
	if !ok {
		return
	}
	item, e := h.service.Get(c.Request.Context(), owner, c.Param("id"))
	if e != nil {
		response.WriteError(c, e)
		return
	}
	response.Success(c, item)
}
func (h *Handler) Content(c *gin.Context) {
	owner, ok := documentOwner(c)
	if !ok {
		return
	}
	item, content, e := h.service.TextContent(c.Request.Context(), owner, c.Param("id"))
	if e != nil {
		response.WriteError(c, e)
		return
	}
	response.Success(c, gin.H{"document": item, "content": content})
}
func (h *Handler) Preview(c *gin.Context) {
	owner, ok := documentOwner(c)
	if !ok {
		return
	}
	item, e := h.service.Preview(c.Request.Context(), owner, c.Param("id"))
	if e != nil {
		response.WriteError(c, e)
		return
	}
	response.Success(c, item)
}
func (h *Handler) Save(c *gin.Context) {
	owner, ok := documentOwner(c)
	if !ok {
		return
	}
	var req SaveRequest
	if e := requestutil.BindJSONStrict(c, &req); e != nil {
		response.ErrorDescriptor(c, apperror.PersonalDocumentInvalid, gin.H{"field": "request"})
		return
	}
	item, e := h.service.Save(c.Request.Context(), owner, c.Param("id"), req)
	if e != nil {
		response.WriteError(c, e)
		return
	}
	response.Success(c, item)
}
func (h *Handler) Versions(c *gin.Context) {
	owner, ok := documentOwner(c)
	if !ok {
		return
	}
	items, e := h.service.Versions(c.Request.Context(), owner, c.Param("id"))
	if e != nil {
		response.WriteError(c, e)
		return
	}
	response.Success(c, gin.H{"items": items})
}
func (h *Handler) RestoreVersion(c *gin.Context) {
	owner, ok := documentOwner(c)
	if !ok {
		return
	}
	var req VersionRequest
	if e := requestutil.BindJSONStrict(c, &req); e != nil {
		response.ErrorDescriptor(c, apperror.PersonalDocumentInvalid, gin.H{"field": "request"})
		return
	}
	item, e := h.service.RestoreVersion(c.Request.Context(), owner, c.Param("id"), c.Param("version_id"), req.ExpectedVersion)
	if e != nil {
		response.WriteError(c, e)
		return
	}
	response.Success(c, item)
}
func (h *Handler) Trash(c *gin.Context)   { h.status(c, StatusTrashed) }
func (h *Handler) Restore(c *gin.Context) { h.status(c, StatusActive) }
func (h *Handler) status(c *gin.Context, status string) {
	owner, ok := documentOwner(c)
	if !ok {
		return
	}
	var req VersionRequest
	if e := requestutil.BindJSONStrict(c, &req); e != nil {
		response.ErrorDescriptor(c, apperror.PersonalDocumentInvalid, gin.H{"field": "request"})
		return
	}
	item, e := h.service.SetStatus(c.Request.Context(), owner, c.Param("id"), req.ExpectedVersion, status)
	if e != nil {
		response.WriteError(c, e)
		return
	}
	response.Success(c, item)
}
func (h *Handler) Download(c *gin.Context) {
	owner, ok := documentOwner(c)
	if !ok {
		return
	}
	item, object, e := h.service.OpenCurrent(c.Request.Context(), owner, c.Param("id"))
	if e != nil {
		response.WriteError(c, e)
		return
	}
	defer object.Reader.Close()
	name := strings.ReplaceAll(item.Name, "\"", "_")
	c.Header("Content-Type", object.Object.MimeType)
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Cache-Control", "private, no-store")
	c.Header("Content-Disposition", `attachment; filename="`+name+`"`)
	c.Header("Content-Length", strconv.FormatInt(object.Object.SizeBytes, 10))
	c.Status(http.StatusOK)
	_, _ = io.Copy(c.Writer, object.Reader)
}
func documentOwner(c *gin.Context) (string, bool) {
	raw, ok := c.Get("user_id")
	id, _ := raw.(string)
	if !ok || strings.TrimSpace(id) == "" {
		response.ErrorDescriptor(c, apperror.AuthRequired, nil)
		return "", false
	}
	return id, true
}
func formatFromName(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".txt":
		return FormatText
	case ".md", ".markdown":
		return FormatMarkdown
	case ".campusdoc", ".json":
		return FormatCampusDoc
	case ".pdf":
		return FormatPDF
	case ".docx":
		return FormatDOCX
	}
	return ""
}
