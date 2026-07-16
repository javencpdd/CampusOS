package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	"github.com/campusos/CampusOS/internal/modules/core/identity/service"
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

	actorID, ok := currentRoleActorID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}
	assigned, err := h.permSvc.AssignRoleByActor(c.Request.Context(), actorID, userID, req.RoleID)
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
	actorID, ok := currentRoleActorID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}
	if actorID == userID {
		response.Error(c, http.StatusForbidden, 20004, "不能撤销自己的角色")
		return
	}

	_, err := h.permSvc.RevokeRoleByActor(c.Request.Context(), actorID, userID, req.RoleID)
	if err != nil {
		writeRoleError(c, err, 70009, "撤销角色失败")
		return
	}
	response.Success(c, gin.H{"message": "角色撤销成功"})
}

type CreateCustomRoleRequest struct {
	Name            string   `json:"name" binding:"required"`
	Description     string   `json:"description"`
	PermissionCodes []string `json:"permission_codes"`
}

func (h *RoleHandler) CreateCustomRole(c *gin.Context) {
	actorID, ok := currentRoleActorID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}
	var req CreateCustomRoleRequest
	if err := requestutil.BindJSONStrict(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 70014, "请求参数错误")
		return
	}
	role, err := h.permSvc.CreateCustomRole(c.Request.Context(), actorID, req.Name, req.Description, req.PermissionCodes)
	if err != nil {
		writeRoleError(c, err, 70015, "创建自定义角色失败")
		return
	}
	response.Success(c, role)
}

type UpdateRolePermissionsRequest struct {
	PermissionCodes []string `json:"permission_codes"`
}

func (h *RoleHandler) UpdateRolePermissions(c *gin.Context) {
	actorID, ok := currentRoleActorID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}
	roleID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || roleID <= 0 {
		response.Error(c, http.StatusBadRequest, 70016, "角色 ID 无效")
		return
	}
	var req UpdateRolePermissionsRequest
	if err := requestutil.BindJSONStrict(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 70017, "请求参数错误")
		return
	}
	if err := h.permSvc.UpdateRolePermissions(c.Request.Context(), actorID, roleID, req.PermissionCodes); err != nil {
		writeRoleError(c, err, 70018, "更新角色权限失败")
		return
	}
	response.Success(c, gin.H{"message": "角色权限已更新"})
}

func (h *RoleHandler) ListPermissionDefinitions(c *gin.Context) {
	items, err := h.permSvc.ListPermissionDefinitions(c.Request.Context())
	if err != nil {
		writeRoleError(c, err, 70019, "获取权限目录失败")
		return
	}
	response.Success(c, gin.H{"items": items, "total": len(items)})
}

func (h *RoleHandler) ListRolePermissions(c *gin.Context) {
	roleID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || roleID <= 0 {
		response.Error(c, http.StatusBadRequest, 70020, "角色 ID 无效")
		return
	}
	items, err := h.permSvc.ListRolePermissions(c.Request.Context(), roleID)
	if err != nil {
		writeRoleError(c, err, 70021, "获取角色权限失败")
		return
	}
	response.Success(c, gin.H{"items": items, "total": len(items)})
}

func (h *RoleHandler) ListAuthorizationAudits(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "100"))
	items, err := h.permSvc.ListAuthorizationAudits(c.Request.Context(), limit)
	if err != nil {
		writeRoleError(c, err, 70022, "获取授权审计失败")
		return
	}
	response.Success(c, gin.H{"items": items, "total": len(items)})
}

func currentRoleActorID(c *gin.Context) (string, bool) {
	actor, ok := c.Get("user_id")
	actorID, isString := actor.(string)
	return actorID, ok && isString && actorID != ""
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
		response.Error(c, http.StatusForbidden, 70012, "系统基础角色不能通过该接口分配、撤销或修改权限")
	case errors.Is(err, service.ErrRoleRequiresScope):
		response.Error(c, http.StatusBadRequest, 70013, "版主角色必须通过版主配置选择至少一个板块")
	case errors.Is(err, service.ErrPermissionEscalation):
		response.Error(c, http.StatusForbidden, 70023, "不能授予或修改超出当前操作者权限范围的角色能力")
	case errors.Is(err, service.ErrLastSystemAdmin):
		response.Error(c, http.StatusConflict, 70024, "不能移除最后一个有效系统管理员")
	case errors.Is(err, service.ErrAuthorizationCatalog):
		response.Error(c, http.StatusServiceUnavailable, 70025, "权限目录迁移尚未完成")
	case errors.Is(err, service.ErrInvalidPermissionCode):
		response.Error(c, http.StatusBadRequest, 70026, "权限 Code 格式无效")
	default:
		response.Error(c, http.StatusInternalServerError, fallbackCode, fallbackMessage)
	}
}
