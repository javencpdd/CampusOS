package handler

import (
	"errors"
	"net/http"
	"strings"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	"github.com/campusos/CampusOS/internal/modules/core/community/repository"
	"github.com/campusos/CampusOS/internal/modules/core/community/service"
	requestutil "github.com/campusos/CampusOS/pkg/request"
	"github.com/campusos/CampusOS/pkg/response"
	"github.com/gin-gonic/gin"
)

// ThreadHandler 帖子 HTTP 处理器
type ThreadHandler struct {
	svc *service.ThreadService
}

// NewThreadHandler 创建帖子处理器
func NewThreadHandler(svc *service.ThreadService) *ThreadHandler {
	return &ThreadHandler{svc: svc}
}

// CreateThread 创建帖子
// POST /api/v1/threads
func (h *ThreadHandler) CreateThread(c *gin.Context) {
	authorID, authorName, ok := currentUser(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}

	var req domain.CreateThreadRequest
	if err := requestutil.BindJSONStrict(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 10001, "invalid request: "+err.Error())
		return
	}

	thread, err := h.svc.CreateThread(c.Request.Context(), authorID, authorName, req)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, 10006, err.Error())
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
		response.Error(c, http.StatusNotFound, 40003, err.Error())
		return
	}

	response.Success(c, thread)
}

// GetThreadForCurrentUser 获取当前用户可见的帖子详情。
// GET /api/v1/threads/:id/me
func (h *ThreadHandler) GetThreadForCurrentUser(c *gin.Context) {
	id := c.Param("id")
	userID, _, ok := currentUser(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}

	thread, err := h.svc.GetThreadForViewer(c.Request.Context(), id, userID)
	if err != nil {
		response.Error(c, http.StatusNotFound, 40003, err.Error())
		return
	}

	response.Success(c, thread)
}

// ListThreads 获取帖子列表
// GET /api/v1/threads
func (h *ThreadHandler) ListThreads(c *gin.Context) {
	filter, pageSize, ok := h.threadListFilter(c)
	if !ok {
		return
	}
	filter.Status = string(domain.ThreadStatusPublished)

	h.respondThreadList(c, filter, pageSize)
}

// AdminListThreads 获取管理端帖子列表，包含草稿、私密和归档状态。
// GET /api/v1/admin/threads
func (h *ThreadHandler) AdminListThreads(c *gin.Context) {
	filter, pageSize, ok := h.threadListFilter(c)
	if !ok {
		return
	}
	if filter.Status == "" {
		filter.Status = "all"
	}
	filter.IncludeTrashed = c.Query("include_trashed") == "1" || strings.EqualFold(c.Query("include_trashed"), "true")
	h.respondThreadList(c, filter, pageSize)
}

// AdminListTrash lists recoverable content only. It is intentionally separate
// from normal admin listing so a destructive action is never hidden in a
// generic status filter.
func (h *ThreadHandler) AdminListTrash(c *gin.Context) {
	filter, pageSize, ok := h.threadListFilter(c)
	if !ok {
		return
	}
	filter.Status = "all"
	filter.DeletionStatus = string(domain.DeletionStatusTrashed)
	filter.IncludeTrashed = true
	h.respondThreadList(c, filter, pageSize)
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

func (h *ThreadHandler) respondThreadList(c *gin.Context, filter domain.ThreadListFilter, pageSize int) {
	threads, total, err := h.svc.ListThreads(c.Request.Context(), filter)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, 10006, err.Error())
		return
	}

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
	authorID, _, ok := currentUser(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}

	var req domain.UpdateThreadRequest
	if err := requestutil.BindJSONStrict(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 10001, "invalid request: "+err.Error())
		return
	}

	thread, err := h.svc.UpdateThread(c.Request.Context(), id, authorID, req)
	if err != nil {
		writeThreadError(c, err)
		return
	}

	response.Success(c, thread)
}

// PinThread 置顶帖子
// POST /api/v1/threads/:id/pin
func (h *ThreadHandler) PinThread(c *gin.Context) {
	id := c.Param("id")
	thread, err := h.svc.PinThread(c.Request.Context(), id)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, 10006, err.Error())
		return
	}
	response.Success(c, thread)
}

// UnpinThread 取消置顶
// POST /api/v1/threads/:id/unpin
func (h *ThreadHandler) UnpinThread(c *gin.Context) {
	id := c.Param("id")
	thread, err := h.svc.UnpinThread(c.Request.Context(), id)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, 10006, err.Error())
		return
	}
	response.Success(c, thread)
}

// LockThread 锁定帖子
// POST /api/v1/threads/:id/lock
func (h *ThreadHandler) LockThread(c *gin.Context) {
	id := c.Param("id")
	thread, err := h.svc.LockThread(c.Request.Context(), id)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, 10006, err.Error())
		return
	}
	response.Success(c, thread)
}

// UnlockThread 解锁帖子
// POST /api/v1/threads/:id/unlock
func (h *ThreadHandler) UnlockThread(c *gin.Context) {
	id := c.Param("id")
	thread, err := h.svc.UnlockThread(c.Request.Context(), id)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, 10006, err.Error())
		return
	}
	response.Success(c, thread)
}

// DeleteThread 删除帖子
// DELETE /api/v1/threads/:id
func (h *ThreadHandler) DeleteThread(c *gin.Context) {
	id := c.Param("id")
	authorID, _, ok := currentUser(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}

	if err := h.svc.DeleteThread(c.Request.Context(), id, authorID); err != nil {
		writeThreadError(c, err)
		return
	}

	response.NoContent(c)
}

// AdminDeleteThread 管理员删除任意帖子。
// DELETE /api/v1/admin/threads/:id
func (h *ThreadHandler) AdminDeleteThread(c *gin.Context) {
	actorID, _, ok := currentUser(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}
	if err := h.svc.AdminDeleteThread(c.Request.Context(), c.Param("id"), actorID); err != nil {
		writeThreadError(c, err)
		return
	}
	response.NoContent(c)
}

// SubmitForReview lets an author resubmit content that was rejected or taken
// down. The service controls the actual state transition.
func (h *ThreadHandler) SubmitForReview(c *gin.Context) {
	authorID, _, ok := currentUser(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}
	thread, err := h.svc.SubmitForReview(c.Request.Context(), c.Param("id"), authorID)
	if err != nil {
		writeThreadError(c, err)
		return
	}
	response.Success(c, thread)
}

func (h *ThreadHandler) RestoreOwnTrash(c *gin.Context) {
	authorID, _, ok := currentUser(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}
	thread, err := h.svc.RestoreFromTrash(c.Request.Context(), c.Param("id"), authorID)
	if err != nil {
		writeThreadError(c, err)
		return
	}
	response.Success(c, thread)
}

func (h *ThreadHandler) AdminTakeDown(c *gin.Context) {
	h.adminTransition(c, func(actorID, reason string) (*domain.Thread, error) {
		return h.svc.TakeDown(c.Request.Context(), c.Param("id"), actorID, reason)
	})
}

func (h *ThreadHandler) AdminApprove(c *gin.Context) {
	h.adminTransition(c, func(actorID, reason string) (*domain.Thread, error) {
		return h.svc.Approve(c.Request.Context(), c.Param("id"), actorID, reason)
	})
}

func (h *ThreadHandler) AdminReject(c *gin.Context) {
	h.adminTransition(c, func(actorID, reason string) (*domain.Thread, error) {
		return h.svc.Reject(c.Request.Context(), c.Param("id"), actorID, reason)
	})
}

func (h *ThreadHandler) AdminDirectRestore(c *gin.Context) {
	h.adminTransition(c, func(actorID, reason string) (*domain.Thread, error) {
		return h.svc.DirectRestore(c.Request.Context(), c.Param("id"), actorID, reason)
	})
}

func (h *ThreadHandler) AdminRestoreTrash(c *gin.Context) {
	h.adminTransition(c, func(actorID, reason string) (*domain.Thread, error) {
		return h.svc.AdminRestoreFromTrash(c.Request.Context(), c.Param("id"), actorID, reason)
	})
}

func (h *ThreadHandler) AdminPurge(c *gin.Context) {
	actorID, _, ok := currentUser(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}
	reason, ok := requiredReason(c)
	if !ok {
		return
	}
	if err := h.svc.PurgeThread(c.Request.Context(), c.Param("id"), actorID, reason); err != nil {
		writeThreadError(c, err)
		return
	}
	response.NoContent(c)
}

func (h *ThreadHandler) AdminModerationActions(c *gin.Context) {
	items, err := h.svc.ListModerationActions(c.Request.Context(), c.Param("id"))
	if err != nil {
		writeThreadError(c, err)
		return
	}
	response.Success(c, items)
}

type moderationReasonRequest struct {
	Reason string `json:"reason"`
}

func (h *ThreadHandler) adminTransition(c *gin.Context, transition func(string, string) (*domain.Thread, error)) {
	actorID, _, ok := currentUser(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}
	reason, ok := requiredReason(c)
	if !ok {
		return
	}
	thread, err := transition(actorID, reason)
	if err != nil {
		writeThreadError(c, err)
		return
	}
	response.Success(c, thread)
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
