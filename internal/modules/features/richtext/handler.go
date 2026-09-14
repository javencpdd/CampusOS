package richtext

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
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

const richTextAttachmentFormSlack = int64(64 * 1024)

func (h *Handler) UploadAttachment(c *gin.Context) {
	userID, _, ok := currentUser(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "请先登录后再上传文章附件。 ")
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
	attachment, err := h.svc.UploadAttachment(c.Request.Context(), userID, c.Param("id"), fileHeader.Filename, fileHeader.Header.Get("Content-Type"), fileHeader.Size, file)
	if err != nil {
		if usage, usageErr := attachmentObjectUsage(h.svc.objects, userID); usageErr == nil {
			writeAttachmentError(c, err, fileHeader.Size, usage)
		} else {
			writeAttachmentError(c, err, fileHeader.Size)
		}
		return
	}
	response.Created(c, attachment)
}

func (h *Handler) BindAttachment(c *gin.Context) {
	userID, _, ok := currentUser(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "请先登录后再添加文章附件。 ")
		return
	}
	var req AttachmentBindRequest
	if err := requestutil.BindJSONStrict(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 10001, "附件请求无效，请检查资源后重试。 ")
		return
	}
	attachment, err := h.svc.AttachExisting(c.Request.Context(), userID, c.Param("id"), req.AssetID, req.DisplayName)
	if err != nil {
		writeAttachmentError(c, err, 0)
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
	items, err := h.svc.ListArticleAttachments(c.Request.Context(), userID, c.Param("id"))
	if err != nil {
		writeAttachmentError(c, err, 0)
		return
	}
	response.Success(c, gin.H{"items": items, "limit": MaxArticleAttachmentCount})
}

func (h *Handler) RemoveAttachment(c *gin.Context) {
	userID, _, ok := currentUser(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "请先登录后移除文章附件。 ")
		return
	}
	if err := h.svc.RemoveAttachment(c.Request.Context(), userID, c.Param("id"), c.Param("attachment_id")); err != nil {
		writeAttachmentError(c, err, 0)
		return
	}
	response.NoContent(c)
}

func (h *Handler) RenameAttachment(c *gin.Context) {
	userID, _, ok := currentUser(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "请先登录后修改附件名称。")
		return
	}
	var req AttachmentUpdateRequest
	if err := requestutil.BindJSONStrict(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 10001, "附件名称无效，请输入 1 至 255 个字符。")
		return
	}
	if err := h.svc.RenameAttachment(c.Request.Context(), userID, c.Param("id"), c.Param("attachment_id"), req.DisplayName); err != nil {
		writeAttachmentError(c, err, 0)
		return
	}
	response.Success(c, gin.H{"display_name": strings.TrimSpace(req.DisplayName)})
}

func (h *Handler) ReorderAttachments(c *gin.Context) {
	userID, _, ok := currentUser(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "请先登录后调整附件顺序。 ")
		return
	}
	var req AttachmentReorderRequest
	if err := requestutil.BindJSONStrict(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 10001, "附件排序请求无效。 ")
		return
	}
	if err := h.svc.ReorderAttachments(c.Request.Context(), userID, c.Param("id"), req.AttachmentIDs); err != nil {
		writeAttachmentError(c, err, 0)
		return
	}
	response.Success(c, gin.H{"attachment_ids": req.AttachmentIDs})
}

func (h *Handler) DownloadAttachment(c *gin.Context) { h.serveAttachment(c, false) }
func (h *Handler) ContentAttachment(c *gin.Context)  { h.serveAttachment(c, true) }

func (h *Handler) serveAttachment(c *gin.Context, inline bool) {
	userID, _, ok := currentUser(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "请先登录后访问文章附件。 ")
		return
	}
	opened, err := h.svc.OpenAttachment(c.Request.Context(), userID, c.Param("id"), c.Param("attachment_id"))
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
	items, err := h.svc.ListMyUserAssets(c.Request.Context(), userID, c.Query("status"))
	if err != nil {
		writeAttachmentError(c, err, 0)
		return
	}
	response.Success(c, gin.H{"items": items, "limit": 200})
}

func (h *Handler) DownloadMyUserAsset(c *gin.Context) {
	userID, _, ok := currentUser(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "请先登录后下载个人附件。")
		return
	}
	opened, err := h.svc.OpenMyUserAsset(c.Request.Context(), userID, c.Param("id"))
	if err != nil {
		writePersonalAssetError(c, err)
		return
	}
	h.serveOpenedAttachment(c, opened, false)
}

func (h *Handler) TrashUserAsset(c *gin.Context) {
	userID, _, ok := currentUser(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "请先登录后移入回收站。 ")
		return
	}
	if err := h.svc.TrashUserAsset(c.Request.Context(), userID, c.Param("id")); err != nil {
		writeAttachmentError(c, err, 0)
		return
	}
	response.Success(c, gin.H{"status": AssetStatusTrashed})
}

func (h *Handler) RestoreUserAsset(c *gin.Context) {
	userID, _, ok := currentUser(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "请先登录后恢复附件。 ")
		return
	}
	if err := h.svc.RestoreUserAsset(c.Request.Context(), userID, c.Param("id")); err != nil {
		writeAttachmentError(c, err, 0)
		return
	}
	response.Success(c, gin.H{"status": AssetStatusActive})
}

// AssetGovernanceSummary exposes only aggregate counts and bytes. It is an
// Admin operator view, never a user-file listing or live database browser.
func (h *Handler) AssetGovernanceSummary(c *gin.Context) {
	summary, err := h.svc.AssetGovernanceSummary(c.Request.Context())
	if err != nil {
		response.Error(c, http.StatusServiceUnavailable, 10006, "暂时无法读取附件治理汇总，请稍后重试。")
		return
	}
	response.Success(c, summary)
}

func (h *Handler) AdminQuarantineUserAsset(c *gin.Context) {
	adminID, _, ok := currentUser(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "请先登录管理员账号后再隔离附件。")
		return
	}
	var req AssetGovernanceActionRequest
	if err := requestutil.BindJSONStrict(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 73002, "隔离原因无效，请输入 1 至 500 个字符。")
		return
	}
	if err := h.svc.AdminQuarantineAsset(c.Request.Context(), adminID, c.Param("id"), req.Reason); err != nil {
		writeAssetGovernanceError(c, err)
		return
	}
	response.Success(c, gin.H{"status": AssetStatusQuarantined})
}

func (h *Handler) AdminRestoreQuarantinedUserAsset(c *gin.Context) {
	adminID, _, ok := currentUser(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "请先登录管理员账号后再恢复附件。")
		return
	}
	var req AssetGovernanceActionRequest
	if err := requestutil.BindJSONStrict(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 73002, "恢复原因无效，请输入 1 至 500 个字符。")
		return
	}
	if err := h.svc.AdminRestoreQuarantinedAsset(c.Request.Context(), adminID, c.Param("id"), req.Reason); err != nil {
		writeAssetGovernanceError(c, err)
		return
	}
	response.Success(c, gin.H{"status": AssetStatusActive})
}

func (h *Handler) AdminPreviewUserAssetPurge(c *gin.Context) {
	preview, err := h.svc.PreviewAssetPurge(c.Request.Context(), c.Param("id"))
	if err != nil {
		writeAssetGovernanceError(c, err)
		return
	}
	response.Success(c, preview)
}

func (h *Handler) AdminPurgeUserAsset(c *gin.Context) {
	adminID, _, ok := currentUser(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "请先登录管理员账号后再清除附件。")
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
	if err := h.svc.AdminPurgeAsset(c.Request.Context(), adminID, c.Param("id"), req.Reason); err != nil {
		writeAssetGovernanceError(c, err)
		return
	}
	response.Success(c, gin.H{"status": AssetStatusDeleted})
}

func (h *Handler) CreatePDFInvocation(c *gin.Context) {
	userID, _, ok := currentUser(c)
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
	if threadID == "" {
		response.Error(c, http.StatusBadRequest, 73002, "预览请求缺少文章标识。 ")
		return
	}
	invocation, err := h.svc.CreatePDFInvocation(c.Request.Context(), userID, threadID, req.AttachmentID, req.Presentation)
	if err != nil {
		writeAttachmentError(c, err, 0)
		return
	}
	response.Created(c, invocation)
}

func (h *Handler) CreatePersonalAssetPDFInvocation(c *gin.Context) {
	userID, _, ok := currentUser(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "请先登录后预览个人 PDF 附件。")
		return
	}
	var req PersonalAssetPDFInvocationRequest
	if err := requestutil.BindJSONStrict(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 73002, "预览请求无效，请返回个人空间后重新打开附件。")
		return
	}
	invocation, err := h.svc.CreatePersonalAssetPDFInvocation(c.Request.Context(), userID, c.Param("id"), req.Presentation)
	if err != nil {
		writePersonalAssetError(c, err)
		return
	}
	response.Created(c, invocation)
}

func (h *Handler) GetPDFInvocation(c *gin.Context) {
	userID, _, ok := currentUser(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "请先登录后打开 PDF 预览。 ")
		return
	}
	attachment, invocation, err := h.svc.DescribePDFInvocation(c.Request.Context(), userID, c.Param("id"))
	if err != nil {
		writeAttachmentError(c, err, 0)
		return
	}
	response.Success(c, gin.H{"invocation": invocation, "attachment": attachment})
}

func (h *Handler) PDFInvocationContent(c *gin.Context) {
	userID, _, ok := currentUser(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "请先登录后读取 PDF 内容。 ")
		return
	}
	opened, _, err := h.svc.OpenPDFInvocation(c.Request.Context(), userID, c.Param("id"))
	if err != nil {
		writeAttachmentError(c, err, 0)
		return
	}
	h.serveOpenedAttachment(c, opened, true)
}

func (h *Handler) serveOpenedAttachment(c *gin.Context, opened AttachmentOpen, inline bool) {
	defer opened.Reader.Close()
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
	case errors.Is(err, ErrPermissionDenied):
		response.Error(c, http.StatusForbidden, 73003, "当前无权访问该文章附件。")
	case errors.Is(err, ErrAttachmentNotFound), errors.Is(err, ErrAssetNotFound), errors.Is(err, ErrArticleNotFound):
		response.Error(c, http.StatusNotFound, 73004, "文章或个人附件不存在、已下架或当前无权访问。")
	default:
		writeRichTextError(c, err)
	}
}

func writePersonalAssetError(c *gin.Context, err error) {
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
	case errors.Is(err, ErrAssetUnavailable):
		response.Error(c, http.StatusServiceUnavailable, 10006, "个人附件存储暂不可用，请稍后重试。")
	default:
		writeRichTextError(c, err)
	}
}

func writeAssetGovernanceError(c *gin.Context, err error) {
	switch {
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
