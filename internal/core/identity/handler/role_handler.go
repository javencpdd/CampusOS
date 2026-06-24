package handler

import (
	"net/http"

	"github.com/campusos/CampusOS/internal/core/identity/service"
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
		response.Error(c, http.StatusInternalServerError, 70003, "获取用户角色失败")
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
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, 70005, "请求参数错误")
		return
	}

	if err := h.permSvc.AssignRole(c.Request.Context(), userID, req.RoleID); err != nil {
		response.Error(c, http.StatusInternalServerError, 70006, "分配角色失败")
		return
	}
	response.Success(c, gin.H{"message": "角色分配成功"})
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
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, 70008, "请求参数错误")
		return
	}

	if err := h.permSvc.RevokeRole(c.Request.Context(), userID, req.RoleID); err != nil {
		response.Error(c, http.StatusInternalServerError, 70009, "撤销角色失败")
		return
	}
	response.Success(c, gin.H{"message": "角色撤销成功"})
}
