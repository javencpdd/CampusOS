package handler

import (
	"errors"
	"net/http"

	"github.com/campusos/CampusOS/internal/core/identity/repository"
	"github.com/campusos/CampusOS/internal/core/identity/service"
	requestutil "github.com/campusos/CampusOS/pkg/request"
	"github.com/campusos/CampusOS/pkg/response"
	"github.com/gin-gonic/gin"
)

type RoleHandler struct {
	permSvc *service.PermissionService
}

func NewRoleHandler(permSvc *service.PermissionService) *RoleHandler {
	return &RoleHandler{permSvc: permSvc}
}

// ListRoles 列出所有角色
func (h *RoleHandler) ListRoles(c *gin.Context) {
	roles, err := h.permSvc.ListRoles(c.Request.Context())
	if err != nil {
		response.Error(c, http.StatusInternalServerError, 70001, "获取角色列表失败")
		return
	}
	response.Success(c, roles)
}

// GetUserRoles 获取用户的角色列表
func (h *RoleHandler) GetUserRoles(c *gin.Context) {
	userID := c.Param("id")
	if userID == "" {
		response.Error(c, http.StatusBadRequest, 70002, "用户 ID 不能为空")
		return
	}

	roles, err := h.permSvc.GetUserRoles(c.Request.Context(), userID)
	if err != nil {
		writeRoleError(c, err, 70003, "获取用户角色失败")
		return
	}
	response.Success(c, roles)
}

// AssignRole 给用户分配角色
type AssignRoleRequest struct {
	RoleID int64 `json:"role_id" binding:"required"`
}

func (h *RoleHandler) AssignRole(c *gin.Context) {
	userID := c.Param("id")
	if userID == "" {
		response.Error(c, http.StatusBadRequest, 70004, "用户 ID 不能为空")
		return
	}

	var req AssignRoleRequest
	if err := requestutil.BindJSONStrict(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 70005, "请求参数错误")
		return
	}

	assigned, err := h.permSvc.AssignRole(c.Request.Context(), userID, req.RoleID)
	if err != nil {
		writeRoleError(c, err, 70006, "分配角色失败")
		return
	}
	message := "角色分配成功"
	if !assigned {
		message = "用户已拥有该角色"
	}
	response.Success(c, gin.H{"message": message, "assigned": assigned})
}

// RevokeRole 撤销用户角色
type RevokeRoleRequest struct {
	RoleID int64 `json:"role_id" binding:"required"`
}

func (h *RoleHandler) RevokeRole(c *gin.Context) {
	userID := c.Param("id")
	if userID == "" {
		response.Error(c, http.StatusBadRequest, 70007, "用户 ID 不能为空")
		return
	}

	var req RevokeRoleRequest
	if err := requestutil.BindJSONStrict(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 70008, "请求参数错误")
		return
	}
	if actorID, ok := c.Get("user_id"); ok {
		if actorIDText, ok := actorID.(string); ok && actorIDText == userID {
			response.Error(c, http.StatusForbidden, 20004, "不能撤销自己的角色")
			return
		}
	}

	if _, err := h.permSvc.RevokeRole(c.Request.Context(), userID, req.RoleID); err != nil {
		writeRoleError(c, err, 70009, "撤销角色失败")
		return
	}
	response.Success(c, gin.H{"message": "角色撤销成功"})
}

func writeRoleError(c *gin.Context, err error, fallbackCode int, fallbackMessage string) {
	switch {
	case errors.Is(err, service.ErrInvalidRoleAssignment):
		response.Error(c, http.StatusBadRequest, 10001, "用户或角色参数无效")
	case errors.Is(err, repository.ErrUserNotFound):
		response.Error(c, http.StatusNotFound, 30004, "目标用户不存在")
	case errors.Is(err, repository.ErrRoleNotFound):
		response.Error(c, http.StatusNotFound, 70010, "目标角色不存在")
	case errors.Is(err, service.ErrRoleAssignmentNotFound):
		response.Error(c, http.StatusNotFound, 70011, "用户未拥有该角色")
	case errors.Is(err, service.ErrProtectedRole):
		response.Error(c, http.StatusForbidden, 70012, "member 和 guest 是系统基础角色，不能通过角色管理接口分配或撤销")
	case errors.Is(err, service.ErrRoleRequiresScope):
		response.Error(c, http.StatusBadRequest, 70013, "版主角色必须通过版主配置选择至少一个板块")
	default:
		response.Error(c, http.StatusInternalServerError, fallbackCode, fallbackMessage)
	}
}
