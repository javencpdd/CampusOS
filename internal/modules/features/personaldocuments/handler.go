package personaldocuments

import (
	"context"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/pkg/apperror"
	"github.com/campusos/CampusOS/pkg/auth"
	requestutil "github.com/campusos/CampusOS/pkg/request"
	"github.com/campusos/CampusOS/pkg/response"
	"github.com/gin-gonic/gin"
)

type Handler struct {
	service  *Service
	verifier identityport.SessionVerifier
	strength identityport.SessionStrengthReader
}

func (h *Handler) SetSessionProof(verifier identityport.SessionVerifier, strength identityport.SessionStrengthReader) {
	h.verifier, h.strength = verifier, strength
}

func (h *Handler) principal(c *gin.Context) DocumentPrincipalResolver {
	claims := &auth.JWTClaims{UserID: c.GetString("user_id"), SessionID: c.GetString("session_id"), AuthVersion: c.GetInt64("auth_version"), TokenType: auth.AccessTokenType}
	return func(ctx context.Context) (identityport.PrincipalContext, error) {
		return identityport.ResolveUserSessionPrincipal(ctx, h.verifier, h.strength, claims)
	}
}

func writeDocumentReadError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, identityport.ErrUserSessionPrincipalUnavailable):
		response.ErrorDescriptor(c, apperror.AuthInvalidToken, nil)
	case errors.Is(err, identityport.ErrPolicyFactsChanged):
		response.ErrorDescriptor(c, apperror.ResourceConflict, nil)
	case errors.Is(err, ErrDocumentCommandUnavailable), errors.Is(err, ErrDocumentCompensationPending):
		response.Error(c, http.StatusServiceUnavailable, 73001, "文档写入暂不可用，请稍后重试。")
	default:
		response.WriteError(c, err)
	}
}

const maxDocumentMultipartBytes = maxBinaryBytes + 1024*1024

func NewHandler(service *Service) *Handler { return &Handler{service: service} }
func (h *Handler) List(c *gin.Context) {
	owner, ok := documentOwner(c)
	if !ok {
		return
	}
	_ = owner
	items, e := h.service.ListForPrincipal(c.Request.Context(), h.principal(c), strings.TrimSpace(c.Query("status")))
	if e != nil {
		writeDocumentReadError(c, e)
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
	_ = owner
	item, e := h.service.CreateTextForPrincipal(c.Request.Context(), h.principal(c), req)
	if e != nil {
		writeDocumentReadError(c, e)
		return
	}
	response.Created(c, item)
}
func (h *Handler) Upload(c *gin.Context) {
	owner, ok := documentOwner(c)
	if !ok {
		return
	}
	// FormFile otherwise lets multipart parsing consume an unbounded request
	// before Service can enforce the per-format limit.
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxDocumentMultipartBytes)
	file, e := c.FormFile("file")
	if e != nil {
		var maxErr *http.MaxBytesError
		if errors.As(e, &maxErr) {
			response.WriteError(c, h.service.tooLargeError("document", maxBinaryBytes, maxDocumentMultipartBytes))
			return
		}
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
	_ = owner
	item, e := h.service.UploadForPrincipal(c.Request.Context(), h.principal(c), name, format, file.Header.Get("Content-Type"), file.Size, reader)
	if e != nil {
		writeDocumentReadError(c, e)
		return
	}
	response.Created(c, item)
}
func (h *Handler) Get(c *gin.Context) {
	owner, ok := documentOwner(c)
	if !ok {
		return
	}
	_ = owner
	item, e := h.service.GetForPrincipal(c.Request.Context(), h.principal(c), c.Param("id"))
	if e != nil {
		writeDocumentReadError(c, e)
		return
	}
	response.Success(c, item)
}
func (h *Handler) Content(c *gin.Context) {
	owner, ok := documentOwner(c)
	if !ok {
		return
	}
	_ = owner
	item, content, e := h.service.TextContentForPrincipal(c.Request.Context(), h.principal(c), c.Param("id"), strings.TrimSpace(c.Query("version_id")))
	if e != nil {
		writeDocumentReadError(c, e)
		return
	}
	response.Success(c, gin.H{"document": item, "content": content})
}
func (h *Handler) Preview(c *gin.Context) {
	owner, ok := documentOwner(c)
	if !ok {
		return
	}
	_ = owner
	item, e := h.service.PreviewForPrincipal(c.Request.Context(), h.principal(c), c.Param("id"), strings.TrimSpace(c.Query("version_id")))
	if e != nil {
		writeDocumentReadError(c, e)
		return
	}
	response.Success(c, item)
}
func (h *Handler) CreatePDFInvocation(c *gin.Context) {
	owner, ok := documentOwner(c)
	if !ok {
		return
	}
	var req PDFInvocationRequest
	if e := requestutil.BindJSONStrict(c, &req); e != nil {
		response.ErrorDescriptor(c, apperror.PersonalDocumentInvalid, gin.H{"field": "presentation", "reason": "请选择弹窗、侧边栏、全屏或新标签页预览方式"})
		return
	}
	_ = owner
	invocation, e := h.service.PDFInvocationForPrincipal(c.Request.Context(), h.principal(c), c.Param("id"), strings.TrimSpace(req.Presentation))
	if e != nil {
		if errors.Is(e, identityport.ErrUserSessionPrincipalUnavailable) || errors.Is(e, identityport.ErrPolicyFactsChanged) {
			writeDocumentReadError(c, e)
		} else {
			writePDFPreviewError(c, e)
		}
		return
	}
	response.Created(c, invocation)
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
	_ = owner
	item, e := h.service.SaveForPrincipal(c.Request.Context(), h.principal(c), c.Param("id"), req)
	if e != nil {
		writeDocumentReadError(c, e)
		return
	}
	response.Success(c, item)
}
func (h *Handler) Versions(c *gin.Context) {
	owner, ok := documentOwner(c)
	if !ok {
		return
	}
	_ = owner
	items, e := h.service.VersionsForPrincipal(c.Request.Context(), h.principal(c), c.Param("id"))
	if e != nil {
		writeDocumentReadError(c, e)
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
	_ = owner
	item, e := h.service.RestoreVersionForPrincipal(c.Request.Context(), h.principal(c), c.Param("id"), c.Param("version_id"), req.ExpectedVersion)
	if e != nil {
		writeDocumentReadError(c, e)
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
	_ = owner
	item, e := h.service.SetStatusForPrincipal(c.Request.Context(), h.principal(c), c.Param("id"), req.ExpectedVersion, status)
	if e != nil {
		writeDocumentReadError(c, e)
		return
	}
	response.Success(c, item)
}
func (h *Handler) Download(c *gin.Context) {
	owner, ok := documentOwner(c)
	if !ok {
		return
	}
	_ = owner
	item, object, e := h.service.OpenVersionForPrincipal(c.Request.Context(), h.principal(c), c.Param("id"), strings.TrimSpace(c.Query("version_id")))
	if e != nil {
		writeDocumentReadError(c, e)
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

func writePDFPreviewError(c *gin.Context, err error) {
	var previewErr *PDFPreviewError
	if errors.As(err, &previewErr) {
		status := previewErr.Status
		if status == 0 {
			status = http.StatusServiceUnavailable
		}
		code := previewErr.Code
		if code == 0 {
			code = 73001
		}
		response.Error(c, status, code, previewErr.Message)
		return
	}
	if errors.Is(err, ErrPDFPreviewUnavailable) {
		response.Error(c, http.StatusServiceUnavailable, 73001, "PDF 预览插件暂不可用，请稍后重试或下载后使用本地阅读器打开。")
		return
	}
	response.WriteError(c, err)
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
