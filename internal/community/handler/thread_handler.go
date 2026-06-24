package handler

import (
	"net/http"
	"strconv"

	"github.com/campusos/CampusOS/internal/community/domain"
	"github.com/campusos/CampusOS/internal/community/service"
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
	authorID := c.GetHeader("X-User-ID")
	if authorID == "" {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}
	authorName := c.GetHeader("X-User-Name")
	if authorName == "" {
		authorName = "Anonymous"
	}

	var req domain.CreateThreadRequest
	if err := c.ShouldBindJSON(&req); err != nil {
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

// ListThreads 获取帖子列表
// GET /api/v1/threads
func (h *ThreadHandler) ListThreads(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))

	filter := domain.ThreadListFilter{
		CategoryID: c.Query("category_id"),
		AuthorID:   c.Query("author_id"),
		Status:     c.Query("status"),
		Keyword:    c.Query("keyword"),
		Page:       page,
		PageSize:   pageSize,
	}

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
		Page:       page,
		PageSize:   pageSize,
		Total:      total,
		TotalPages: totalPages,
	})
}

// UpdateThread 更新帖子
// PUT /api/v1/threads/:id
func (h *ThreadHandler) UpdateThread(c *gin.Context) {
	id := c.Param("id")
	authorID := c.GetHeader("X-User-ID")
	if authorID == "" {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}

	var req domain.UpdateThreadRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, 10001, "invalid request: "+err.Error())
		return
	}

	thread, err := h.svc.UpdateThread(c.Request.Context(), id, authorID, req)
	if err != nil {
		response.Error(c, http.StatusForbidden, 20004, err.Error())
		return
	}

	response.Success(c, thread)
}

// DeleteThread 删除帖子
// DELETE /api/v1/threads/:id
func (h *ThreadHandler) DeleteThread(c *gin.Context) {
	id := c.Param("id")
	authorID := c.GetHeader("X-User-ID")
	if authorID == "" {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}

	if err := h.svc.DeleteThread(c.Request.Context(), id, authorID); err != nil {
		response.Error(c, http.StatusForbidden, 20004, err.Error())
		return
	}

	response.NoContent(c)
}
