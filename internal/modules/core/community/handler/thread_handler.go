package handler

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/campusos/CampusOS/internal/modules/core/community/contentbody"
	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	"github.com/campusos/CampusOS/internal/modules/core/community/repository"
	"github.com/campusos/CampusOS/internal/modules/core/community/service"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/pkg/apperror"
	"github.com/campusos/CampusOS/pkg/auth"
	requestutil "github.com/campusos/CampusOS/pkg/request"
	"github.com/campusos/CampusOS/pkg/response"
	"github.com/gin-gonic/gin"
)

// ThreadHandler 帖子 HTTP 处理器
type ThreadHandler struct {
	svc                   *service.ThreadService
	ownerSessionVerifier  identityport.SessionVerifier
	ownerStrengthReader   identityport.SessionStrengthReader
	adminSessionAuthority identityport.AdminSessionAuthorityReader
}

// NewThreadHandler 创建帖子处理器
func NewThreadHandler(svc *service.ThreadService) *ThreadHandler {
	return &ThreadHandler{svc: svc}
}

// SetOwnerSessionProof supplies the current Identity session facts required by
// the authenticated owner read. Missing adapters leave this entry closed.
func (h *ThreadHandler) SetOwnerSessionProof(verifier identityport.SessionVerifier, reader identityport.SessionStrengthReader) {
	h.ownerSessionVerifier = verifier
	h.ownerStrengthReader = reader
}

func (h *ThreadHandler) SetAdminSessionAuthority(reader identityport.AdminSessionAuthorityReader) {
	h.adminSessionAuthority = reader
}

type contentPreviewRequest struct {
	Content string `json:"content" binding:"required,min=1"`
}

// PreviewContent applies the same Core sanitizer used before structured
// content is persisted. It is intentionally independent from RichText's
// feature lifecycle so Mutual Aid and Secondhand can preview safely.
func (h *ThreadHandler) PreviewContent(c *gin.Context) {
	var req contentPreviewRequest
	if err := requestutil.BindJSONStrict(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 10001, "invalid request: "+err.Error())
		return
	}
	result, err := contentbody.Sanitize(req.Content)
	if err != nil {
		response.Error(c, http.StatusBadRequest, 10001, err.Error())
		return
	}
	response.Success(c, gin.H{
		"sanitized_html": result.HTML,
		"text":           result.Text,
		"warnings":       result.Warnings,
	})
}

// CreateThread 创建帖子
// POST /api/v1/threads
func (h *ThreadHandler) CreateThread(c *gin.Context) {
	_, authorName, ok := currentUser(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}

	var req domain.CreateThreadRequest
	if err := requestutil.BindJSONStrict(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 10001, "invalid request: "+err.Error())
		return
	}

	thread, err := h.svc.CreateThreadForPrincipal(c.Request.Context(), authorName, req, func(ctx context.Context) (identityport.PrincipalContext, error) {
		return h.resolveOwnerPrincipal(ctx, c)
	})
	if err != nil {
		writeThreadError(c, err)
		return
	}

	response.Created(c, thread)
}

// GetThread 获取帖子详情
// GET /api/v1/threads/:id
func (h *ThreadHandler) GetThread(c *gin.Context) {
	id := c.Param("id")

	thread, err := h.svc.GetThread(c.Request.Context(), id)
	if err != nil {
		writePublicThreadReadError(c, err)
		return
	}

	response.Success(c, thread)
}

// GetThreadForCurrentUser 获取当前用户可见的帖子详情。
// GET /api/v1/threads/:id/me
func (h *ThreadHandler) GetThreadForCurrentUser(c *gin.Context) {
	id := c.Param("id")
	_, _, ok := currentUser(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}

	thread, err := h.svc.GetThreadForPrincipal(c.Request.Context(), id, func(ctx context.Context) (identityport.PrincipalContext, error) {
		return h.resolveOwnerPrincipal(ctx, c)
	})
	if err != nil {
		writePublicThreadReadError(c, err)
		return
	}

	response.Success(c, thread)
}

func (h *ThreadHandler) resolveOwnerPrincipal(ctx context.Context, c *gin.Context) (identityport.PrincipalContext, error) {
	userID, _, ok := currentUser(c)
	sessionID := c.GetString("session_id")
	authVersion := c.GetInt64("auth_version")
	if !ok {
		return identityport.PrincipalContext{}, service.ErrThreadOwnerPrincipalUnavailable
	}
	claims := &auth.JWTClaims{UserID: userID, SessionID: sessionID, AuthVersion: authVersion, TokenType: auth.AccessTokenType}
	principal, err := identityport.ResolveUserSessionPrincipal(ctx, h.ownerSessionVerifier, h.ownerStrengthReader, claims)
	if err != nil {
		return identityport.PrincipalContext{}, errors.Join(service.ErrThreadOwnerPrincipalUnavailable, err)
	}
	return principal, nil
}

func writePublicThreadReadError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrThreadOwnerPrincipalUnavailable):
		response.ErrorDescriptor(c, apperror.AuthInvalidToken, nil)
	case errors.Is(err, repository.ErrThreadNotFound):
		response.Error(c, http.StatusNotFound, 40003, "thread not found")
	case errors.Is(err, identityport.ErrPolicyFactsChanged):
		response.ErrorDescriptor(c, apperror.ResourceConflict, nil)
	default:
		response.WriteError(c, err)
	}
}

// ListThreads 获取帖子列表
// GET /api/v1/threads
func (h *ThreadHandler) ListThreads(c *gin.Context) {
	filter, pageSize, ok := h.threadListFilter(c)
	if !ok {
		return
	}
	threads, total, err := h.svc.ListPublicThreads(c.Request.Context(), filter)
	if err != nil {
		writePublicThreadReadError(c, err)
		return
	}
	writeThreadListResponse(c, filter, pageSize, threads, total)
}

// AdminListThreads returns checked metadata, including private publication states.
func (h *ThreadHandler) AdminListThreads(c *gin.Context) { h.adminThreadMetadataList(c, false) }
func (h *ThreadHandler) AdminListTrash(c *gin.Context)   { h.adminThreadMetadataList(c, true) }
func adminMetadataNoBody(c *gin.Context) bool {
	c.Header("Cache-Control", "private, no-store")
	if c.Request.Body != nil {
		raw, err := io.ReadAll(io.LimitReader(c.Request.Body, 1))
		if err != nil || len(raw) > 0 {
			response.Error(c, 400, 10001, "管理元数据读取不接受请求正文。")
			return false
		}
	}
	return true
}
func writeAdminMetadataError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrAdminThreadMetadataInvalid):
		response.Error(c, 400, 10001, "筛选无效；搜索仅支持帖子数字ID。")
	case errors.Is(err, service.ErrThreadGovernanceDenied):
		response.Error(c, 403, 20004, "当前管理员无权读取内容管理元数据。")
	default:
		writeThreadGovernanceError(c, err)
	}
}
func (h *ThreadHandler) adminThreadMetadataList(c *gin.Context, trash bool) {
	if !adminMetadataNoBody(c) {
		return
	}
	filter, pageSize, ok := h.threadListFilter(c)
	if !ok {
		return
	}
	rows, total, err := h.svc.ListAdminThreadMetadata(c.Request.Context(), func(ctx context.Context) (identityport.PrincipalContext, error) {
		return h.resolveAdminPrincipal(ctx, c)
	}, filter, trash)
	if err != nil {
		writeAdminMetadataError(c, err)
		return
	}
	pages := total / int64(pageSize)
	if total%int64(pageSize) > 0 {
		pages++
	}
	response.List(c, rows, &response.Pagination{Page: filter.Page, PageSize: pageSize, Total: total, TotalPages: int(pages)})
}

func (h *ThreadHandler) threadListFilter(c *gin.Context) (domain.ThreadListFilter, int, bool) {
	page, pageSize, ok := response.ParsePagination(c, 20, 100)
	if !ok {
		return domain.ThreadListFilter{}, 0, false
	}

	filter := domain.ThreadListFilter{
		CategoryID:        c.Query("category_id"),
		AuthorID:          c.Query("author_id"),
		Status:            c.Query("status"),
		ContentFormat:     c.Query("content_format"),
		ThreadType:        domain.ThreadType(c.Query("thread_type")),
		Keyword:           c.Query("keyword"),
		Tag:               c.Query("tag"),
		PublicationStatus: c.Query("publication_status"),
		ModerationStatus:  c.Query("moderation_status"),
		DeletionStatus:    c.Query("deletion_status"),
		Page:              page,
		PageSize:          pageSize,
	}
	return filter, pageSize, true
}

func writeThreadListResponse(c *gin.Context, filter domain.ThreadListFilter, pageSize int, threads []*domain.Thread, total int64) {
	totalPages := int(total) / pageSize
	if int(total)%pageSize > 0 {
		totalPages++
	}

	response.List(c, threads, &response.Pagination{
		Page:       filter.Page,
		PageSize:   pageSize,
		Total:      total,
		TotalPages: totalPages,
	})
}

// UpdateThread 更新帖子
// PUT /api/v1/threads/:id
func (h *ThreadHandler) UpdateThread(c *gin.Context) {
	id := c.Param("id")
	_, _, ok := currentUser(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}

	var req domain.UpdateThreadRequest
	if err := requestutil.BindJSONStrict(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 10001, "invalid request: "+err.Error())
		return
	}

	thread, err := h.svc.UpdateThreadForPrincipal(c.Request.Context(), id, req, func(ctx context.Context) (identityport.PrincipalContext, error) {
		return h.resolveOwnerPrincipal(ctx, c)
	})
	if err != nil {
		writeThreadError(c, err)
		return
	}

	response.Success(c, thread)
}

// PinThread 置顶帖子
// POST /api/v1/threads/:id/pin
func (h *ThreadHandler) PinThread(c *gin.Context) {
	h.adminFlag(c, "pin")
}

// UnpinThread 取消置顶
// POST /api/v1/threads/:id/unpin
func (h *ThreadHandler) UnpinThread(c *gin.Context) {
	h.adminFlag(c, "unpin")
}

// LockThread 锁定帖子
// POST /api/v1/threads/:id/lock
func (h *ThreadHandler) LockThread(c *gin.Context) {
	h.adminFlag(c, "lock")
}

// UnlockThread 解锁帖子
// POST /api/v1/threads/:id/unlock
func (h *ThreadHandler) UnlockThread(c *gin.Context) {
	h.adminFlag(c, "unlock")
}

func (h *ThreadHandler) resolveAdminPrincipal(ctx context.Context, c *gin.Context) (identityport.PrincipalContext, error) {
	id := c.GetString("admin_account_id")
	if id == "" || c.GetString("user_id") != id {
		return identityport.PrincipalContext{}, identityport.ErrAdminSessionPrincipalUnavailable
	}
	return identityport.ResolveAdminSessionPrincipal(ctx, h.adminSessionAuthority, &auth.JWTClaims{UserID: id, SessionID: c.GetString("session_id"), AuthVersion: c.GetInt64("auth_version"), TokenType: auth.AccessTokenType})
}

func (h *ThreadHandler) adminFlag(c *gin.Context, operation string) {
	result, err := h.svc.AdminSetThreadFlag(c.Request.Context(), c.Param("id"), operation, func(ctx context.Context) (identityport.PrincipalContext, error) {
		return h.resolveAdminPrincipal(ctx, c)
	})
	if err != nil {
		writeThreadGovernanceError(c, err)
		return
	}
	response.Success(c, result)
}

// DeleteThread 删除帖子
// DELETE /api/v1/threads/:id
func (h *ThreadHandler) DeleteThread(c *gin.Context) {
	id := c.Param("id")
	_, _, ok := currentUser(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}

	if _, err := h.svc.AuthorLifecycleForPrincipal(c.Request.Context(), id, "trash", func(ctx context.Context) (identityport.PrincipalContext, error) {
		return h.resolveOwnerPrincipal(ctx, c)
	}); err != nil {
		writeThreadError(c, err)
		return
	}

	response.NoContent(c)
}

// AdminDeleteThread 管理员删除任意帖子。
// DELETE /api/v1/admin/threads/:id
func (h *ThreadHandler) AdminDeleteThread(c *gin.Context) {
	h.adminLifecycle(c, "trash")
}

// SubmitForReview lets an author resubmit content that was rejected or taken
// down. The service controls the actual state transition.
func (h *ThreadHandler) SubmitForReview(c *gin.Context) {
	_, _, ok := currentUser(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}
	thread, err := h.svc.AuthorLifecycleForPrincipal(c.Request.Context(), c.Param("id"), "submit_review", func(ctx context.Context) (identityport.PrincipalContext, error) {
		return h.resolveOwnerPrincipal(ctx, c)
	})
	if err != nil {
		writeThreadError(c, err)
		return
	}
	response.Success(c, thread)
}

func (h *ThreadHandler) RestoreOwnTrash(c *gin.Context) {
	_, _, ok := currentUser(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}
	thread, err := h.svc.AuthorLifecycleForPrincipal(c.Request.Context(), c.Param("id"), "restore", func(ctx context.Context) (identityport.PrincipalContext, error) {
		return h.resolveOwnerPrincipal(ctx, c)
	})
	if err != nil {
		writeThreadError(c, err)
		return
	}
	response.Success(c, thread)
}

func (h *ThreadHandler) AdminTakeDown(c *gin.Context) {
	h.takeDown(c, true)
}

func (h *ThreadHandler) TakeDown(c *gin.Context) { h.takeDown(c, false) }

func (h *ThreadHandler) takeDown(c *gin.Context, admin bool) {
	reason, ok := requiredReason(c)
	if !ok {
		return
	}
	resolve := func(ctx context.Context) (identityport.PrincipalContext, error) {
		if !admin {
			return h.resolveOwnerPrincipal(ctx, c)
		}
		id := c.GetString("admin_account_id")
		if id == "" || c.GetString("user_id") != id {
			return identityport.PrincipalContext{}, identityport.ErrAdminSessionPrincipalUnavailable
		}
		return identityport.ResolveAdminSessionPrincipal(ctx, h.adminSessionAuthority, &auth.JWTClaims{UserID: id, SessionID: c.GetString("session_id"), AuthVersion: c.GetInt64("auth_version"), TokenType: auth.AccessTokenType})
	}
	result, err := h.svc.TakeDownForPrincipal(c.Request.Context(), c.Param("id"), reason, resolve)
	if err != nil {
		writeThreadGovernanceError(c, err)
		return
	}
	response.Success(c, result)
}

func writeThreadGovernanceError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, identityport.ErrAdminSessionPrincipalUnavailable), errors.Is(err, identityport.ErrUserSessionPrincipalUnavailable), errors.Is(err, service.ErrThreadOwnerPrincipalUnavailable):
		response.ErrorDescriptor(c, apperror.AuthInvalidToken, nil)
	case errors.Is(err, service.ErrThreadGovernanceDenied):
		response.Error(c, 403, 20004, "无权执行该内容操作；管理员请先完成当前 MFA 验证。")
	case errors.Is(err, repository.ErrModerationCaseChanged):
		response.Error(c, 409, 10004, "审核记录已变化，请重新加载。")
	case errors.Is(err, service.ErrThreadGovernanceUnavailable), errors.Is(err, identityport.ErrGovernanceAuthorityUnavailable):
		response.Error(c, 503, 10006, "暂时无法检查内容治理权限。")
	default:
		writeThreadError(c, err)
	}
}

func (h *ThreadHandler) AdminApprove(c *gin.Context) {
	h.adminReview(c, "approve")
}

func (h *ThreadHandler) AdminReject(c *gin.Context) {
	h.adminReview(c, "reject")
}

func (h *ThreadHandler) adminReview(c *gin.Context, operation string) {
	reason, ok := requiredReason(c)
	if !ok {
		return
	}
	result, err := h.svc.AdminReviewThread(c.Request.Context(), c.Param("id"), operation, reason, func(ctx context.Context) (identityport.PrincipalContext, error) {
		return h.resolveAdminPrincipal(ctx, c)
	})
	if err != nil {
		writeThreadGovernanceError(c, err)
		return
	}
	response.Success(c, result)
}

func (h *ThreadHandler) AdminDirectRestore(c *gin.Context) {
	h.adminLifecycle(c, "direct_restore")
}

func (h *ThreadHandler) AdminRestoreTrash(c *gin.Context) {
	h.adminLifecycle(c, "restore")
}

func (h *ThreadHandler) adminLifecycle(c *gin.Context, operation string) {
	reason := ""
	if operation != "trash" {
		var ok bool
		reason, ok = requiredReason(c)
		if !ok {
			return
		}
	}
	result, err := h.svc.AdminLifecycleThread(c.Request.Context(), c.Param("id"), operation, reason, func(ctx context.Context) (identityport.PrincipalContext, error) {
		return h.resolveAdminPrincipal(ctx, c)
	})
	if err != nil {
		writeThreadGovernanceError(c, err)
		return
	}
	if operation == "trash" {
		response.NoContent(c)
		return
	}
	response.Success(c, result)
}

func (h *ThreadHandler) AdminPurge(c *gin.Context) {
	reason, ok := requiredReason(c)
	if !ok {
		return
	}
	if _, err := h.svc.AdminPurgeThread(c.Request.Context(), c.Param("id"), "purge", reason, func(ctx context.Context) (identityport.PrincipalContext, error) {
		return h.resolveAdminPrincipal(ctx, c)
	}); err != nil {
		writeThreadGovernanceError(c, err)
		return
	}
	response.NoContent(c)
}

func (h *ThreadHandler) AdminModerationActions(c *gin.Context) {
	if !adminMetadataNoBody(c) {
		return
	}
	page, size, ok := response.ParsePagination(c, 20, 100)
	if !ok {
		return
	}
	rows, total, err := h.svc.AdminModerationMetadata(c.Request.Context(), c.Param("id"), func(ctx context.Context) (identityport.PrincipalContext, error) {
		return h.resolveAdminPrincipal(ctx, c)
	}, page, size)
	if err != nil {
		writeAdminMetadataError(c, err)
		return
	}
	pages := total / int64(size)
	if total%int64(size) > 0 {
		pages++
	}
	response.List(c, rows, &response.Pagination{Page: page, PageSize: size, Total: total, TotalPages: int(pages)})
}

type moderationReasonRequest struct {
	Reason string `json:"reason"`
}

func requiredReason(c *gin.Context) (string, bool) {
	var req moderationReasonRequest
	if err := requestutil.BindJSONStrict(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 10001, "invalid moderation request: "+err.Error())
		return "", false
	}
	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		response.Error(c, http.StatusBadRequest, 10001, "moderation reason is required")
		return "", false
	}
	return reason, true
}

func writeThreadError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrThreadOwnerPrincipalUnavailable), errors.Is(err, identityport.ErrUserSessionPrincipalUnavailable):
		response.Error(c, 401, 20002, "请重新登录后修改内容。")
	case errors.Is(err, service.ErrThreadAuthorWriteInvalid):
		response.Error(c, 400, 10001, "正文或发布状态无效，请检查后重试。")
	case errors.Is(err, service.ErrThreadAuthorWriteDenied):
		response.Error(c, 403, 20004, "无权修改该内容。")
	case errors.Is(err, identityport.ErrPolicyFactsChanged), errors.Is(err, repository.ErrThreadRevisionConflict):
		response.Error(c, 409, 10004, "内容或访问权限已变化，请重新加载。")
	case errors.Is(err, service.ErrThreadAuthorWriteUnavailable), errors.Is(err, identityport.ErrPolicyDecisionMismatch):
		response.Error(c, 503, 10006, "暂时无法检查内容写入权限。")
	case errors.Is(err, repository.ErrThreadNotFound):
		response.Error(c, http.StatusNotFound, 40003, "thread not found")
	case errors.Is(err, service.ErrThreadStateConflict):
		response.Error(c, http.StatusConflict, 10004, err.Error())
	case errors.Is(err, service.ErrModerationReasonRequired):
		response.Error(c, http.StatusBadRequest, 10001, err.Error())
	case strings.Contains(err.Error(), "permission denied"):
		response.Error(c, http.StatusForbidden, 20004, err.Error())
	default:
		response.Error(c, http.StatusInternalServerError, 10006, err.Error())
	}
}
