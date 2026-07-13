package moderation

import (
	"errors"
	"net/http"

	communityport "github.com/campusos/CampusOS/internal/community/port"
	identityport "github.com/campusos/CampusOS/internal/core/identity/port"
	requestutil "github.com/campusos/CampusOS/pkg/request"
	"github.com/campusos/CampusOS/pkg/response"
	"github.com/gin-gonic/gin"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) Status(c *gin.Context) {
	response.Success(c, h.service.Status())
}

func (h *Handler) MyAccess(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}
	access, err := h.service.AccessForThread(c.Request.Context(), userID, c.Query("thread_id"))
	if err != nil {
		writeError(c, err)
		return
	}
	response.Success(c, access)
}

func (h *Handler) ListModerators(c *gin.Context) {
	items, err := h.service.ListModerators(c.Request.Context())
	if err != nil {
		writeError(c, err)
		return
	}
	response.Success(c, gin.H{"items": items, "total": len(items), "status": h.service.Status()})
}

func (h *Handler) GetModerator(c *gin.Context) {
	item, err := h.service.GetModerator(c.Request.Context(), c.Param("id"))
	if err != nil {
		writeError(c, err)
		return
	}
	response.Success(c, item)
}

type SetModeratorRequest struct {
	CategoryIDs []string `json:"category_ids"`
}

func (h *Handler) SetModerator(c *gin.Context) {
	actorID, ok := currentUserID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}
	var req SetModeratorRequest
	if err := requestutil.BindJSONStrict(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 10001, "请求参数错误")
		return
	}
	item, err := h.service.SetModeratorCategories(c.Request.Context(), actorID, c.Param("id"), req.CategoryIDs, operationContext(c))
	if err != nil {
		writeError(c, err)
		return
	}
	response.Success(c, gin.H{"message": "版主管理范围已保存", "assignment": item})
}

func (h *Handler) Pin(c *gin.Context) {
	h.setPinned(c, true)
}

func (h *Handler) Unpin(c *gin.Context) {
	h.setPinned(c, false)
}

func (h *Handler) setPinned(c *gin.Context, pinned bool) {
	actorID, ok := currentUserID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}
	thread, err := h.service.SetPinned(c.Request.Context(), actorID, c.Param("id"), pinned, operationContext(c))
	if err != nil {
		writeError(c, err)
		return
	}
	response.Success(c, thread)
}

func (h *Handler) Lock(c *gin.Context) {
	h.setLocked(c, true)
}

func (h *Handler) Unlock(c *gin.Context) {
	h.setLocked(c, false)
}

func (h *Handler) setLocked(c *gin.Context, locked bool) {
	actorID, ok := currentUserID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}
	thread, err := h.service.SetLocked(c.Request.Context(), actorID, c.Param("id"), locked, operationContext(c))
	if err != nil {
		writeError(c, err)
		return
	}
	response.Success(c, thread)
}

func (h *Handler) DeletePost(c *gin.Context) {
	actorID, ok := currentUserID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}
	if err := h.service.DeletePost(c.Request.Context(), actorID, c.Param("id"), c.Param("post_id"), operationContext(c)); err != nil {
		writeError(c, err)
		return
	}
	response.NoContent(c)
}

func currentUserID(c *gin.Context) (string, bool) {
	value, ok := c.Get("user_id")
	if !ok {
		return "", false
	}
	userID, ok := value.(string)
	return userID, ok && userID != ""
}

func operationContext(c *gin.Context) OperationContext {
	traceID, _ := c.Get("trace_id")
	traceIDText, _ := traceID.(string)
	return OperationContext{TraceID: traceIDText, IPAddress: c.ClientIP()}
}

func writeError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrPluginDisabled):
		response.Error(c, http.StatusServiceUnavailable, 71001, "版主插件当前未运行；系统级插件启停需要重启 API 后生效")
	case errors.Is(err, ErrActionDisabled):
		response.Error(c, http.StatusForbidden, 71002, "该版主操作已被管理员关闭")
	case errors.Is(err, ErrForbidden):
		response.Error(c, http.StatusForbidden, 20004, "你不是该板块的版主，不能执行此操作")
	case errors.Is(err, ErrInvalidScope), errors.Is(err, identityport.ErrInvalidScope):
		response.Error(c, http.StatusBadRequest, 71003, "板块范围参数无效")
	case errors.Is(err, identityport.ErrUserNotFound):
		response.Error(c, http.StatusNotFound, 30004, "目标用户不存在")
	case errors.Is(err, communityport.ErrCategoryNotFound):
		response.Error(c, http.StatusNotFound, 50004, "目标板块不存在")
	case errors.Is(err, communityport.ErrThreadNotFound):
		response.Error(c, http.StatusNotFound, 40003, "目标帖子不存在")
	case errors.Is(err, communityport.ErrPostNotFound):
		response.Error(c, http.StatusNotFound, 40004, "目标回复不存在")
	default:
		response.Error(c, http.StatusInternalServerError, 71000, "版主管理操作失败")
	}
}
