package richtext

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	communityport "github.com/campusos/CampusOS/internal/modules/core/community/port"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	corestorage "github.com/campusos/CampusOS/internal/modules/core/userstorage"
	"github.com/campusos/CampusOS/pkg/apperror"
	"github.com/campusos/CampusOS/pkg/auth"
	requestutil "github.com/campusos/CampusOS/pkg/request"
	"github.com/campusos/CampusOS/pkg/response"
	"github.com/gin-gonic/gin"
)

type Handler struct {
	svc              *Service
	contentAdmission contentAdmission
	verifier         identityport.SessionVerifier
	strength         identityport.SessionStrengthReader
	adminAuthority   identityport.AdminSessionAuthorityReader
}

func (h *Handler) SetSessionProof(verifier identityport.SessionVerifier, strength identityport.SessionStrengthReader) {
	h.verifier, h.strength = verifier, strength
}

func (h *Handler) SetAdminSessionAuthority(reader identityport.AdminSessionAuthorityReader) {
	h.adminAuthority = reader
}

func (h *Handler) adminPrincipal(c *gin.Context) identityport.SessionPrincipalResolver {
	id := c.GetString("admin_account_id")
	claims := &auth.JWTClaims{UserID: id, SessionID: c.GetString("session_id"), AuthVersion: c.GetInt64("auth_version"), TokenType: auth.AccessTokenType}
	return func(ctx context.Context) (identityport.PrincipalContext, error) {
		if id == "" || c.GetString("user_id") != id {
			return identityport.PrincipalContext{}, identityport.ErrAdminSessionPrincipalUnavailable
		}
		return identityport.ResolveAdminSessionPrincipal(ctx, h.adminAuthority, claims)
	}
}

func (h *Handler) readPrincipal(c *gin.Context) identityport.SessionPrincipalResolver {
	claims := &auth.JWTClaims{UserID: c.GetString("user_id"), SessionID: c.GetString("session_id"), AuthVersion: c.GetInt64("auth_version"), TokenType: auth.AccessTokenType}
	return func(ctx context.Context) (identityport.PrincipalContext, error) {
		return identityport.ResolveUserSessionPrincipal(ctx, h.verifier, h.strength, claims)
	}
}

const richTextAssetFormSlack = int64(64 * 1024)

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) Status(c *gin.Context) {
	response.Success(c, h.svc.Status())
}

func (h *Handler) CreateDraft(c *gin.Context) {
	userID, displayName, ok := currentUser(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}
	var req SaveArticleRequest
	if err := requestutil.BindJSONStrict(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 10001, "invalid request: "+err.Error())
		return
	}
	_ = userID
	result, err := h.svc.CreateDraftForPrincipal(c.Request.Context(), displayName, req, h.readPrincipal(c))
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
	_ = userID
	result, err := h.svc.UpdateDraftForPrincipal(c.Request.Context(), c.Param("id"), req, h.readPrincipal(c))
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
	_ = userID
	result, err := h.svc.PublishForPrincipal(c.Request.Context(), c.Param("id"), h.readPrincipal(c))
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
	_ = userID
	result, err := h.svc.OfflineForPrincipal(c.Request.Context(), c.Param("id"), h.readPrincipal(c))
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
	_ = userID
	if err := h.svc.DeleteForPrincipal(c.Request.Context(), c.Param("id"), h.readPrincipal(c)); err != nil {
		writeRichTextError(c, err)
		return
	}
	response.NoContent(c)
}

func articleGovernanceReason(c *gin.Context, fallback string) (string, bool) {
	reason := fallback
	if c.Request.ContentLength != 0 {
		var req struct {
			Reason string `json:"reason"`
		}
		if err := requestutil.BindJSONStrict(c, &req); err != nil {
			response.Error(c, http.StatusBadRequest, 10001, "invalid moderation request: "+err.Error())
			return "", false
		}
		if strings.TrimSpace(req.Reason) == "" {
			response.Error(c, http.StatusBadRequest, 10001, "moderation reason is required")
			return "", false
		}
		reason = strings.TrimSpace(req.Reason)
	}
	return reason, true
}

func (h *Handler) AdminOffline(c *gin.Context) {
	reason, ok := articleGovernanceReason(c, "administrator marked richtext article offline")
	if !ok {
		return
	}
	result, err := h.svc.AdminGovernanceForPrincipal(c.Request.Context(), c.Param("id"), "take_down", reason, h.adminPrincipal(c))
	if err != nil {
		writeRichTextError(c, err)
		return
	}
	response.Success(c, result)
}

func (h *Handler) AdminRestore(c *gin.Context) {
	reason, ok := articleGovernanceReason(c, "administrator restored richtext article")
	if !ok {
		return
	}
	result, err := h.svc.AdminGovernanceForPrincipal(c.Request.Context(), c.Param("id"), "direct_restore", reason, h.adminPrincipal(c))
	if err != nil {
		writeRichTextError(c, err)
		return
	}
	response.Success(c, result)
}

func (h *Handler) AdminDelete(c *gin.Context) {
	reason, ok := articleGovernanceReason(c, "administrator moved richtext article to trash")
	if !ok {
		return
	}
	if _, err := h.svc.AdminGovernanceForPrincipal(c.Request.Context(), c.Param("id"), "trash", reason, h.adminPrincipal(c)); err != nil {
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
	article, err := h.svc.GetArticleForPrincipal(c.Request.Context(), c.Param("id"), nil)
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
	_ = userID
	article, err := h.svc.GetArticleForPrincipal(c.Request.Context(), c.Param("id"), h.readPrincipal(c))
	if err != nil {
		writeRichTextError(c, err)
		return
	}
	response.Success(c, article)
}

func (h *Handler) UploadAsset(c *gin.Context) {
	_, _, ok := currentUser(c)
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
	if form := c.Request.MultipartForm; form != nil {
		defer form.RemoveAll()
		valid := len(form.File) == 1 && len(form.File["file"]) == 1
		for key, values := range form.Value {
			valid = valid && (key == "thread_id" || key == "article_content_id") && len(values) == 1
		}
		if !valid {
			response.Error(c, http.StatusBadRequest, 10001, "图片请求包含无效字段，请重新选择后重试。")
			return
		}
	}
	file, err := fileHeader.Open()
	if err != nil {
		response.Error(c, http.StatusBadRequest, 10001, "无法读取所选图片文件，请重新选择后再试。")
		return
	}
	defer file.Close()

	asset, err := h.svc.UploadAssetForPrincipal(c.Request.Context(), file, c.PostForm("thread_id"), c.PostForm("article_content_id"), h.readPrincipal(c))
	if err != nil {
		writeRichTextAssetUploadError(c, err, maxBytes, fileHeader.Size)
		return
	}
	response.Success(c, asset)
}

func (h *Handler) ListMyAssets(c *gin.Context) {
	_, _, ok := currentUser(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "请先登录后查看已上传的文章图片。")
		return
	}
	items, err := h.svc.ListInlineImagesForPrincipal(c.Request.Context(), h.readPrincipal(c))
	if err != nil {
		writeRichTextError(c, err)
		return
	}
	response.Success(c, gin.H{"items": items, "limit": 200})
}

func (h *Handler) ServeAsset(c *gin.Context) {
	var resolve identityport.SessionPrincipalResolver
	if c.GetHeader("Authorization") != "" {
		resolve = h.readPrincipal(c)
	}
	image, err := h.svc.ReadInlineImageForPrincipal(c.Request.Context(), c.Param("user_id"), c.Param("filename"), c.Query("thread_id"), c.Query("store"), resolve)
	if err != nil {
		writeRichTextError(c, err)
		return
	}
	corestorage.ServeCheckedImage(c, image)
}

const richTextAttachmentFormSlack = int64(64 * 1024)

func (h *Handler) UploadAttachment(c *gin.Context) {
	_, _, ok := currentUser(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "请先登录后再上传文章附件。 ")
		return
	}
	resolve := h.readPrincipal(c)
	if resolve == nil {
		writeAttachmentError(c, identityport.ErrUserSessionPrincipalUnavailable, 0)
		return
	}
	principal, err := resolve(c.Request.Context())
	if err != nil || principal.Validate() != nil || principal.Actor.Kind != "user" || principal.Audience != "user" {
		writeAttachmentError(c, identityport.ErrUserSessionPrincipalUnavailable, 0)
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, MaxArticleAttachmentBytes+richTextAttachmentFormSlack)
	fileHeader, err := c.FormFile("file")
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeAttachmentError(c, ErrAssetTooLarge, 0)
			return
		}
		response.ErrorWithDetails(c, http.StatusBadRequest, 73002, "请选择要上传的文章附件。", attachmentLimits())
		return
	}
	if form := c.Request.MultipartForm; form != nil {
		defer form.RemoveAll()
		if len(form.Value) != 0 || len(form.File) != 1 || len(form.File["file"]) != 1 {
			response.Error(c, http.StatusBadRequest, 10001, "附件请求只能包含一个文件，请重新选择后重试。")
			return
		}
	}
	if fileHeader.Size <= 0 || fileHeader.Size > MaxArticleAttachmentBytes {
		writeAttachmentError(c, ErrAssetTooLarge, fileHeader.Size)
		return
	}
	file, err := fileHeader.Open()
	if err != nil {
		response.Error(c, http.StatusBadRequest, 73002, "无法读取所选附件文件，请重新选择后再试。 ")
		return
	}
	defer file.Close()
	attachment, err := h.svc.UploadAttachmentForPrincipal(c.Request.Context(), c.Param("id"), fileHeader.Filename, fileHeader.Header.Get("Content-Type"), fileHeader.Size, file, resolve)
	if err != nil {
		writeAttachmentError(c, err, fileHeader.Size)
		return
	}
	response.Created(c, attachment)
}

func (h *Handler) BindAttachment(c *gin.Context) {
	_, _, ok := currentUser(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "请先登录后再添加文章附件。 ")
		return
	}
	var req AttachmentBindRequest
	if err := requestutil.BindJSONStrict(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 10001, "附件请求无效，请检查资源后重试。 ")
		return
	}
	attachment, err := h.svc.AttachExistingForPrincipal(c.Request.Context(), c.Param("id"), req.AssetID, req.DisplayName, h.readPrincipal(c))
	if err != nil {
		writeAttachmentError(c, err, 0)
		return
	}
	response.Created(c, attachment)
}

func (h *Handler) AttachPersonalDocument(c *gin.Context) {
	_, _, ok := currentUser(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "请先登录后再从个人文档添加文章附件。")
		return
	}
	var req PersonalDocumentAttachmentRequest
	if err := requestutil.BindJSONStrict(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 10001, "个人文档附件请求无效，请重新选择文件后重试。")
		return
	}
	attachment, err := h.svc.AttachPersonalDocumentForPrincipal(c.Request.Context(), c.Param("id"), req.DocumentID, req.DisplayName, h.readPrincipal(c))
	if err != nil {
		writePersonalDocumentAttachmentError(c, err)
		return
	}
	response.Created(c, attachment)
}

func (h *Handler) ListAttachments(c *gin.Context) {
	userID, _, ok := currentUser(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "请先登录后查看文章附件。 ")
		return
	}
	_ = userID
	items, err := h.svc.ListAttachmentsForPrincipal(c.Request.Context(), c.Param("id"), h.readPrincipal(c))
	if err != nil {
		writeAttachmentError(c, err, 0)
		return
	}
	response.Success(c, gin.H{"items": items, "limit": MaxArticleAttachmentCount})
}

func (h *Handler) RemoveAttachment(c *gin.Context) {
	_, _, ok := currentUser(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "请先登录后移除文章附件。 ")
		return
	}
	if c.Request.Body != nil {
		var extra [1]byte
		if n, err := c.Request.Body.Read(extra[:]); n != 0 || err != io.EOF {
			response.Error(c, http.StatusBadRequest, 10001, "移除附件请求不能包含正文。")
			return
		}
	}
	if err := h.svc.RemoveAttachmentForPrincipal(c.Request.Context(), c.Param("id"), c.Param("attachment_id"), h.readPrincipal(c)); err != nil {
		writeAttachmentError(c, err, 0)
		return
	}
	response.NoContent(c)
}

func (h *Handler) RenameAttachment(c *gin.Context) {
	_, _, ok := currentUser(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "请先登录后修改附件名称。")
		return
	}
	var req AttachmentUpdateRequest
	if err := requestutil.BindJSONStrict(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 10001, "附件名称无效，请输入 1 至 255 个字符。")
		return
	}
	ack, err := h.svc.RenameAttachmentForPrincipal(c.Request.Context(), c.Param("id"), c.Param("attachment_id"), req.DisplayName, h.readPrincipal(c))
	if err != nil {
		writeAttachmentError(c, err, 0)
		return
	}
	response.Success(c, ack)
}

func (h *Handler) ReorderAttachments(c *gin.Context) {
	_, _, ok := currentUser(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "请先登录后调整附件顺序。 ")
		return
	}
	var req AttachmentReorderRequest
	if err := requestutil.BindJSONStrict(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 10001, "附件排序请求无效。 ")
		return
	}
	ack, err := h.svc.ReorderAttachmentsForPrincipal(c.Request.Context(), c.Param("id"), req.AttachmentIDs, h.readPrincipal(c))
	if err != nil {
		writeAttachmentError(c, err, 0)
		return
	}
	response.Success(c, ack)
}

func (h *Handler) DownloadAttachment(c *gin.Context) { h.serveAttachment(c, false) }
func (h *Handler) ContentAttachment(c *gin.Context)  { h.serveAttachment(c, true) }

func (h *Handler) serveAttachment(c *gin.Context, inline bool) {
	userID, _, ok := currentUser(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "请先登录后访问文章附件。 ")
		return
	}
	_ = userID
	opened, err := h.svc.OpenAttachmentForPrincipal(c.Request.Context(), c.Param("id"), c.Param("attachment_id"), h.readPrincipal(c))
	if err != nil {
		writeAttachmentError(c, err, 0)
		return
	}
	h.serveOpenedAttachment(c, opened, inline)
}

func (h *Handler) ListUserAssets(c *gin.Context) {
	userID, _, ok := currentUser(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "请先登录后查看个人附件。 ")
		return
	}
	limit := 0
	if raw := c.Query("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 200 {
			response.Error(c, http.StatusBadRequest, 73002, "每页附件数量须为 1–200 的整数，请修改 limit 参数后重试。")
			return
		}
		limit = parsed
	}
	_ = userID
	page, err := h.svc.UserAssetPageForPrincipal(c.Request.Context(), c.Query("status"), c.Query("cursor"), limit, h.readPrincipal(c))
	if err != nil {
		writeAttachmentError(c, err, 0)
		return
	}
	response.Success(c, page)
}

func (h *Handler) DownloadMyUserAsset(c *gin.Context) {
	userID, _, ok := currentUser(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "请先登录后下载个人附件。")
		return
	}
	_ = userID
	opened, err := h.svc.OpenUserAssetForPrincipal(c.Request.Context(), c.Param("id"), h.readPrincipal(c))
	if err != nil {
		writePersonalAssetError(c, err)
		return
	}
	h.serveOpenedAttachment(c, opened, false)
}

func (h *Handler) TrashUserAsset(c *gin.Context) {
	_, _, ok := currentUser(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "请先登录后移入回收站。 ")
		return
	}
	if !assetMutationNoBody(c) {
		return
	}
	ack, err := h.svc.TrashUserAssetForPrincipal(c.Request.Context(), c.Param("id"), h.readPrincipal(c))
	if err != nil {
		writeAttachmentError(c, err, 0)
		return
	}
	response.Success(c, ack)
}

func (h *Handler) RestoreUserAsset(c *gin.Context) {
	_, _, ok := currentUser(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "请先登录后恢复附件。 ")
		return
	}
	if !assetMutationNoBody(c) {
		return
	}
	ack, err := h.svc.RestoreUserAssetForPrincipal(c.Request.Context(), c.Param("id"), h.readPrincipal(c))
	if err != nil {
		writeAttachmentError(c, err, 0)
		return
	}
	response.Success(c, ack)
}

func assetMutationNoBody(c *gin.Context) bool {
	if c.Request.Body == nil {
		return true
	}
	var first [1]byte
	n, err := c.Request.Body.Read(first[:])
	if n != 0 || err != io.EOF {
		response.Error(c, http.StatusBadRequest, 10001, "附件状态请求无需请求体，请刷新后重试。")
		return false
	}
	return true
}

// AssetGovernanceSummary exposes only aggregate counts and bytes. It is an
// Admin operator view, never a user-file listing or live database browser.
func (h *Handler) AssetGovernanceSummary(c *gin.Context) {
	if !assetMutationNoBody(c) {
		return
	}
	summary, err := h.svc.AssetGovernanceSummaryForPrincipal(c.Request.Context(), h.adminPrincipal(c))
	if err != nil {
		writeAssetGovernanceError(c, err)
		return
	}
	response.Success(c, summary)
}

func (h *Handler) AdminQuarantineUserAsset(c *gin.Context) {
	h.assetAdminLifecycle(c, "quarantine")
}

func (h *Handler) AdminRestoreQuarantinedUserAsset(c *gin.Context) {
	h.assetAdminLifecycle(c, "restore")
}

func (h *Handler) assetAdminLifecycle(c *gin.Context, operation string) {
	proof := h.adminPrincipal(c)
	if _, err := proof(c.Request.Context()); err != nil {
		writeAssetGovernanceError(c, err)
		return
	}
	var req AssetAdminLifecycleRequest
	if err := requestutil.BindJSONStrict(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 73002, "资产治理请求无效，请只填写 1 至 500 个字符的原因。")
		return
	}
	ack, err := h.svc.AdminAssetLifecycleForPrincipal(c.Request.Context(), c.Param("id"), operation, req.Reason, proof)
	if err != nil {
		writeAssetGovernanceError(c, err)
		return
	}
	response.Success(c, ack)
}

func (h *Handler) AdminPreviewUserAssetPurge(c *gin.Context) {
	if !assetMutationNoBody(c) {
		return
	}
	preview, err := h.svc.PreviewAssetPurgeForPrincipal(c.Request.Context(), c.Param("id"), h.adminPrincipal(c))
	if err != nil {
		writeAssetGovernanceError(c, err)
		return
	}
	response.Success(c, preview)
}

func (h *Handler) AdminPurgeUserAsset(c *gin.Context) {
	proof := h.adminPrincipal(c)
	if _, err := proof(c.Request.Context()); err != nil {
		writeAssetGovernanceError(c, err)
		return
	}
	var req AssetGovernanceActionRequest
	if err := requestutil.BindJSONStrict(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 73002, "清除请求无效，请填写原因并确认附件标识。")
		return
	}
	if strings.TrimSpace(req.ConfirmAssetID) != c.Param("id") {
		response.Error(c, http.StatusBadRequest, 73002, "二次确认失败：确认的附件标识必须与待清除附件一致。")
		return
	}
	ack, err := h.svc.AdminPurgeAssetForPrincipal(c.Request.Context(), c.Param("id"), req.Reason, proof)
	if err != nil {
		writeAssetGovernanceError(c, err)
		return
	}
	response.Success(c, ack)
}

func (h *Handler) CreatePDFInvocation(c *gin.Context) {
	_, _, ok := currentUser(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "请先登录后预览 PDF 附件。 ")
		return
	}
	var req PluginUIInvocationRequest
	if err := requestutil.BindJSONStrict(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 10001, "预览请求无效，请重新打开附件。 ")
		return
	}
	threadID := strings.TrimSpace(c.Query("thread_id"))
	if req.PluginKey != "" || req.ResourceType != "" || req.SurfaceID != "" {
		invocation, err := h.svc.CreateInvocationForPrincipal(c.Request.Context(), req, h.readPrincipal(c))
		if err != nil {
			writeAttachmentError(c, err, 0)
			return
		}
		response.Created(c, invocation)
		return
	}
	if threadID == "" {
		response.Error(c, http.StatusBadRequest, 73002, "预览请求缺少文章标识。 ")
		return
	}
	invocation, err := h.svc.CreateInvocationForPrincipal(c.Request.Context(), PluginUIInvocationRequest{PluginKey: PDFViewerPluginKey, SurfaceID: PDFViewerSurfaceID, ResourceType: InvocationContextArticleAttachment, ThreadID: threadID, ResourceID: req.AttachmentID, Presentation: req.Presentation}, h.readPrincipal(c))
	if err != nil {
		writeAttachmentError(c, err, 0)
		return
	}
	response.Created(c, invocation)
}

func (h *Handler) CreatePersonalAssetPDFInvocation(c *gin.Context) {
	_, _, ok := currentUser(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "请先登录后预览个人 PDF 附件。")
		return
	}
	var req PersonalAssetPDFInvocationRequest
	if err := requestutil.BindJSONStrict(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 73002, "预览请求无效，请返回个人空间后重新打开附件。")
		return
	}
	invocation, err := h.svc.CreateInvocationForPrincipal(c.Request.Context(), PluginUIInvocationRequest{PluginKey: PDFViewerPluginKey, SurfaceID: PDFViewerSurfaceID, ResourceType: InvocationContextPersonalAsset, ResourceID: c.Param("id"), Presentation: req.Presentation}, h.readPrincipal(c))
	if err != nil {
		writePersonalAssetError(c, err)
		return
	}
	response.Created(c, invocation)
}

func (h *Handler) GetPDFInvocation(c *gin.Context) {
	_, _, ok := currentUser(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "请先登录后打开 PDF 预览。 ")
		return
	}
	opened, invocation, err := h.svc.OpenInvocationForPrincipal(c.Request.Context(), c.Param("id"), false, h.readPrincipal(c))
	attachment := &opened.Attachment
	if opened.Reader != nil {
		_ = opened.Reader.Close()
	}
	if err != nil {
		writeAttachmentError(c, err, 0)
		return
	}
	resourceID := invocation.AssetID
	if invocation.ContextKind == InvocationContextPersonalDocument {
		resourceID = invocation.PersonalDocumentID
	}
	resource := gin.H{"resource_type": invocation.ContextKind, "resource_id": resourceID}
	if invocation.ContextKind == InvocationContextArticleAttachment {
		article, err := h.svc.store.GetArticleByContentID(c.Request.Context(), invocation.ArticleContentID)
		if err != nil {
			writeAttachmentError(c, err, 0)
			return
		}
		resource["thread_id"], resource["resource_id"] = article.ThreadID, invocation.AttachmentID
	}
	response.Success(c, gin.H{"invocation": invocation, "attachment": attachment, "resource_context": resource})
}

func (h *Handler) DownloadPDFInvocation(c *gin.Context) {
	_, _, ok := currentUser(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "请先登录后下载文件。")
		return
	}
	opened, _, err := h.svc.OpenInvocationForPrincipal(c.Request.Context(), c.Param("id"), true, h.readPrincipal(c))
	if err != nil {
		writeAttachmentError(c, err, 0)
		return
	}
	h.serveOpenedAttachment(c, opened, false)
}

func (h *Handler) PDFInvocationContent(c *gin.Context) {
	_, _, ok := currentUser(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "请先登录后读取 PDF 内容。 ")
		return
	}
	opened, _, err := h.svc.OpenInvocationForPrincipal(c.Request.Context(), c.Param("id"), false, h.readPrincipal(c))
	if err != nil {
		writeAttachmentError(c, err, 0)
		return
	}
	h.serveOpenedAttachment(c, opened, true)
}

func (h *Handler) serveOpenedAttachment(c *gin.Context, opened AttachmentOpen, inline bool) {
	defer opened.Reader.Close()
	userID, _, ok := currentUser(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "请先登录后读取附件。")
		return
	}
	release, admitted := h.admitContent(c, userID)
	if !admitted {
		return
	}
	defer release()
	// Bound slow content writers without imposing a global deadline on SSE.
	// Gin exposes its underlying writer through Unwrap; recorders may not.
	controller := http.NewResponseController(c.Writer)
	if err := controller.SetWriteDeadline(time.Now().Add(2 * time.Minute)); err == nil {
		defer controller.SetWriteDeadline(time.Time{})
	}
	reader, ok := opened.Reader.(io.ReadSeeker)
	if !ok {
		response.Error(c, http.StatusServiceUnavailable, 10006, "附件存储暂不支持安全范围读取，请下载文件后查看。 ")
		return
	}
	if inline && opened.Attachment.Asset.MimeType != "application/pdf" {
		response.Error(c, http.StatusBadRequest, 73002, "该附件格式不支持在线预览，请下载后使用本地软件打开。 ")
		return
	}
	c.Header("Content-Type", opened.Attachment.Asset.MimeType)
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Cache-Control", "private, no-store")
	if opened.Object.SHA256 != "" {
		c.Header("ETag", strconv.Quote(opened.Object.SHA256))
	}
	disposition := "attachment"
	if inline {
		disposition = "inline"
	}
	c.Header("Content-Disposition", disposition+`; filename="`+safeDownloadName(opened.Attachment.DisplayName)+`"`)
	http.ServeContent(c.Writer, c.Request, safeDownloadName(opened.Attachment.DisplayName), opened.Object.UpdatedAt, reader)
}

func safeDownloadName(value string) string {
	value = strings.ReplaceAll(strings.TrimSpace(value), `"`, "'")
	value = strings.ReplaceAll(value, "\r", "")
	value = strings.ReplaceAll(value, "\n", "")
	if value == "" {
		return "attachment"
	}
	return value
}

func attachmentLimits() gin.H {
	return gin.H{"accepted_extensions": []string{".pdf", ".mp3", ".mp4", ".doc", ".docx", ".xls", ".xlsx", ".zip", ".rar"},
		"max_file_bytes": MaxArticleAttachmentBytes, "max_attachment_count": MaxArticleAttachmentCount, "max_total_bytes": MaxArticleAttachmentTotalBytes}
}

type attachmentUsageReader interface {
	Usage(context.Context, string) (corestorage.ObjectUsage, error)
}

func attachmentObjectUsage(objects corestorage.ObjectPort, owner string) (corestorage.ObjectUsage, error) {
	reader, ok := objects.(attachmentUsageReader)
	if !ok {
		return corestorage.ObjectUsage{}, errors.New("attachment object usage is unavailable")
	}
	return reader.Usage(context.Background(), owner)
}

func writeAttachmentError(c *gin.Context, err error, providedBytes int64, usages ...corestorage.ObjectUsage) {
	if errors.Is(err, ErrAttachmentCompensationPending) || errors.Is(err, corestorage.ErrObjectCompensationPending) {
		response.Error(c, http.StatusServiceUnavailable, 73001, "附件未保存成功，临时文件清理尚未完成，请稍后检查容量再重试。")
		return
	}
	if writeReadProofError(c, err) {
		return
	}
	details := attachmentLimits()
	if providedBytes > 0 {
		details["provided_bytes"] = providedBytes
	}
	var usage *corestorage.ObjectUsage
	if len(usages) > 0 {
		usage = &usages[0]
		details["used_bytes"] = usage.UsedBytes
		details["quota_bytes"] = usage.QuotaBytes
		details["remaining_bytes"] = usage.RemainingBytes
	}
	switch {
	case errors.Is(err, ErrAttachmentLimit), errors.Is(err, ErrAssetTooLarge):
		response.ErrorWithDetails(c, http.StatusRequestEntityTooLarge, 73002, fmt.Sprintf("附件超过限制：单个文件最大 %s，每篇文章最多 %d 个附件。", formatRichTextAssetBytes(MaxArticleAttachmentBytes), MaxArticleAttachmentCount), details)
	case errors.Is(err, ErrAttachmentAlreadyBound):
		response.Error(c, http.StatusConflict, 73002, "该附件已经添加到当前文章，无需重复添加。")
	case errors.Is(err, ErrAttachmentTotalSize):
		response.ErrorWithDetails(c, http.StatusConflict, 73002, fmt.Sprintf("文章附件总大小不能超过 %s，请移除部分附件后重试。", formatRichTextAssetBytes(MaxArticleAttachmentTotalBytes)), details)
	case errors.Is(err, ErrAttachmentType):
		response.ErrorWithDetails(c, http.StatusBadRequest, 73002, "附件格式不受支持。允许 PDF、MP3、MP4、DOC/DOCX、XLS/XLSX、ZIP、RAR；可执行文件和脚本不允许上传。", details)
	case errors.Is(err, ErrAttachmentContent):
		response.ErrorWithDetails(c, http.StatusBadRequest, 73002, "附件内容与扩展名或声明类型不一致，可能已损坏或存在安全风险。请重新导出后上传。", details)
	case errors.Is(err, ErrAssetQuotaExceeded):
		message := "个人空间剩余容量不足，无法保存附件。请清理文件或联系管理员扩容后重试。"
		if usage != nil {
			message = fmt.Sprintf("个人空间剩余容量不足：已用 %s / 配额 %s，剩余 %s。请清理文件或联系管理员扩容后重试。", formatRichTextAssetBytes(usage.UsedBytes), formatRichTextAssetBytes(usage.QuotaBytes), formatRichTextAssetBytes(usage.RemainingBytes))
		}
		response.ErrorWithDetails(c, http.StatusConflict, 73002, message, details)
	case errors.Is(err, ErrAssetReferenced):
		response.Error(c, http.StatusConflict, 73002, "该附件仍被图文文章引用，不能移入回收站。请先从所有文章中移除它。")
	case errors.Is(err, ErrInvocationExpired), errors.Is(err, ErrInvocationNotFound):
		response.Error(c, http.StatusGone, 73004, "PDF 预览已过期、被撤销或当前无权访问。请返回文章或个人空间后重新打开。")
	case errors.Is(err, ErrInvocationPresentation):
		response.Error(c, http.StatusBadRequest, 73002, "预览展示方式无效，请使用弹窗、抽屉、全屏或新标签页重新打开。")
	case errors.Is(err, ErrPluginDisabled):
		response.Error(c, http.StatusServiceUnavailable, 73001, "PDF 预览插件当前未启用。你仍可下载有权访问的附件后使用本地阅读器打开。")
	case errors.Is(err, ErrPluginAuthorization):
		message := "PDF 预览尚未获得所需授权。请在插件中心确认个人数据用途，或联系管理员授予 PDF Viewer 权限。"
		var authorizationErr *PDFViewerAuthorizationError
		if errors.As(err, &authorizationErr) && strings.TrimSpace(authorizationErr.Message) != "" {
			message = authorizationErr.Message
		}
		response.Error(c, http.StatusForbidden, 73003, message)
	case errors.Is(err, ErrPermissionDenied):
		response.Error(c, http.StatusForbidden, 73003, "当前无权访问该文章附件。")
	case errors.Is(err, ErrAttachmentNotFound), errors.Is(err, ErrAssetNotFound), errors.Is(err, ErrArticleNotFound), errors.Is(err, communityport.ErrThreadNotFound):
		response.Error(c, http.StatusNotFound, 73004, "文章或个人附件不存在、已下架或当前无权访问。")
	default:
		writeRichTextError(c, err)
	}
}

func writePersonalDocumentAttachmentError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrAssetNotFound), errors.Is(err, ErrPermissionDenied):
		response.Error(c, http.StatusNotFound, 73004, "个人文档不存在、已移入回收站，或当前无权添加。")
	case errors.Is(err, ErrAttachmentType):
		response.Error(c, http.StatusBadRequest, 73002, "该个人文档不能作为文章附件。目前仅支持 PDF 或 DOCX。")
	case errors.Is(err, ErrAssetUnavailable):
		response.Error(c, http.StatusServiceUnavailable, 10006, "个人文档存储暂不可用，请稍后重试。")
	default:
		writeAttachmentError(c, err, 0)
	}
}

func writePersonalAssetError(c *gin.Context, err error) {
	if writeReadProofError(c, err) {
		return
	}
	switch {
	case errors.Is(err, ErrAssetNotFound), errors.Is(err, ErrPermissionDenied):
		// Do not reveal whether an asset ID belongs to another user.
		response.Error(c, http.StatusNotFound, 73004, "个人附件不存在、已删除或当前无权访问。")
	case errors.Is(err, ErrAttachmentType):
		response.Error(c, http.StatusBadRequest, 73002, "该个人附件不是 PDF，暂不支持在线预览。")
	case errors.Is(err, ErrInvocationPresentation):
		response.Error(c, http.StatusBadRequest, 73002, "预览展示方式无效，请使用弹窗、抽屉、全屏或新标签页重新打开。")
	case errors.Is(err, ErrPluginDisabled):
		response.Error(c, http.StatusServiceUnavailable, 73001, "PDF 预览插件当前未启用。你仍可下载自己的个人附件后使用本地阅读器打开。")
	case errors.Is(err, ErrPluginAuthorization):
		message := "PDF 预览尚未获得读取你个人空间文件的授权。请在插件中心确认授权，或联系管理员授予 PDF Viewer 权限。"
		var authorizationErr *PDFViewerAuthorizationError
		if errors.As(err, &authorizationErr) && strings.TrimSpace(authorizationErr.Message) != "" {
			message = authorizationErr.Message
		}
		response.Error(c, http.StatusForbidden, 73003, message)
	case errors.Is(err, ErrAssetUnavailable):
		response.Error(c, http.StatusServiceUnavailable, 10006, "个人附件存储暂不可用，请稍后重试。")
	default:
		writeRichTextError(c, err)
	}
}

func writeAssetGovernanceError(c *gin.Context, err error) {
	if errors.Is(err, ErrAssetPurgePending) {
		response.Error(c, http.StatusServiceUnavailable, 73001, "清除尚未完成，附件保持不可用；请稍后重新确认并重试。")
		return
	}
	if writeReadProofError(c, err) {
		return
	}
	switch {
	case errors.Is(err, ErrPermissionDenied):
		response.Error(c, http.StatusForbidden, 73003, "无权执行资产治理；请确认当前权限并完成 MFA 验证。")
	case errors.Is(err, ErrAssetPurgeIneligible), errors.Is(err, ErrAssetReferenced):
		response.Error(c, http.StatusConflict, 73002, "该附件当前不满足清除条件：请确认它已在回收站或隔离状态，且不存在文章引用。")
	case errors.Is(err, ErrAssetNotFound):
		response.Error(c, http.StatusNotFound, 73004, "附件不存在，已删除，或当前无法执行治理操作。")
	case errors.Is(err, ErrAssetInvalid):
		response.Error(c, http.StatusBadRequest, 73002, "附件治理请求无效，请检查原因和附件标识后重试。")
	default:
		response.Error(c, http.StatusServiceUnavailable, 10006, "附件治理操作暂未完成。附件保持不可公开访问，请检查存储服务后重试。")
	}
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
	displayName := "Anonymous"
	if rawName, ok := c.Get("nickname"); ok {
		if name, ok := rawName.(string); ok && name != "" {
			displayName = name
		}
	}
	if displayName == "Anonymous" {
		if rawName, ok := c.Get("username"); ok {
			if name, ok := rawName.(string); ok && name != "" {
				displayName = name
			}
		}
	}
	return id, displayName, true
}

func writeRichTextError(c *gin.Context, err error) {
	if writeReadProofError(c, err) {
		return
	}
	switch {
	case errors.Is(err, ErrArticleFactsConflict), errors.Is(err, communityport.ErrGovernanceCommandConflict):
		response.ErrorDescriptor(c, apperror.ResourceConflict, nil)
	case errors.Is(err, ErrArticleFactsUnavailable), errors.Is(err, communityport.ErrGovernanceCommandUnavailable), errors.Is(err, identityport.ErrGovernanceAuthorityUnavailable):
		response.Error(c, 503, 50001, "当前文章治理校验暂不可用，请稍后重试。")
	case errors.Is(err, communityport.ErrGovernanceCommandDenied):
		response.Error(c, 403, 73003, "无权执行该文章操作；管理员请先完成当前 MFA 验证。")
	case errors.Is(err, identityport.ErrResourcePolicyUnknownAction):
		response.ErrorDescriptor(c, apperror.RequestInvalid, nil)
	case errors.Is(err, communityport.ErrAuthorCommandUnavailable), errors.Is(err, identityport.ErrPolicyDecisionMismatch):
		response.Error(c, 503, 50001, "当前文章操作校验暂不可用，请稍后重试")
	case errors.Is(err, communityport.ErrAuthorCommandDenied):
		response.ErrorDescriptor(c, apperror.PermissionDenied, nil)
	case errors.Is(err, communityport.ErrAuthorCommandInvalid):
		response.ErrorDescriptor(c, apperror.RequestInvalid, nil)
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

func writeReadProofError(c *gin.Context, err error) bool {
	switch {
	case errors.Is(err, ErrAssetBindingFactsUnavailable):
		response.Error(c, http.StatusServiceUnavailable, 73001, "附件校验暂不可用，请稍后重试。")
		return true
	case errors.Is(err, identityport.ErrUserSessionPrincipalUnavailable), errors.Is(err, identityport.ErrAdminSessionPrincipalUnavailable):
		response.ErrorDescriptor(c, apperror.AuthInvalidToken, nil)
		return true
	case errors.Is(err, identityport.ErrPolicyFactsChanged), errors.Is(err, identityport.ErrResourcePolicyFactsChanged):
		response.ErrorDescriptor(c, apperror.ResourceConflict, nil)
		return true
	}
	return false
}

func writeRichTextAssetUploadError(c *gin.Context, err error, maxBytes, providedBytes int64) {
	if errors.Is(err, corestorage.ErrInlineImageCleanupPending) {
		response.Error(c, http.StatusServiceUnavailable, 73001, "图片清理尚未完成，请稍后重试。")
		return
	}
	if writeReadProofError(c, err) {
		return
	}
	switch {
	case errors.Is(err, ErrPermissionDenied), errors.Is(err, communityport.ErrAuthorCommandDenied):
		response.ErrorDescriptor(c, apperror.PermissionDenied, nil)
	case errors.Is(err, ErrAssetUnavailable), errors.Is(err, communityport.ErrAuthorCommandUnavailable):
		response.Error(c, http.StatusServiceUnavailable, 73001, "图片上传校验暂不可用，请稍后重试。")
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
