package handler

import (
	"net/http"
	"strconv"

	"github.com/campusos/CampusOS/internal/core/identity/domain"
	"github.com/campusos/CampusOS/internal/core/identity/service"
	"github.com/campusos/CampusOS/pkg/response"
	"github.com/gin-gonic/gin"
)

// UserHandler 用户 HTTP 处理器
type UserHandler struct {
	svc *service.UserService
}

// NewUserHandler 创建用户处理器
func NewUserHandler(svc *service.UserService) *UserHandler {
	return &UserHandler{svc: svc}
}

// Register 用户注册
// POST /api/v1/auth/register
func (h *UserHandler) Register(c *gin.Context) {
	var req domain.CreateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, 10001, "invalid request: "+err.Error())
		return
	}

	user, err := h.svc.Register(c.Request.Context(), req)
	if err != nil {
		response.Error(c, http.StatusConflict, 10004, err.Error())
		return
	}

	response.Created(c, user)
}

// Login 用户登录
// POST /api/v1/auth/login
func (h *UserHandler) Login(c *gin.Context) {
	var req domain.LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, 10001, "invalid request: "+err.Error())
		return
	}

	user, accessToken, refreshToken, err := h.svc.Login(c.Request.Context(), req)
	if err != nil {
		response.Error(c, http.StatusUnauthorized, 20001, err.Error())
		return
	}

	// 获取用户角色
	roles, _ := h.svc.GetUserRoles(c.Request.Context(), user.ID)

	response.Success(c, domain.LoginResponse{
		User:         user,
		Roles:        roles,
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		TokenType:    "Bearer",
		ExpiresIn:    7200,
	})
}

// GetMe 获取当前用户信息
// GET /api/v1/auth/me
func (h *UserHandler) GetMe(c *gin.Context) {
	// 从 JWT 中间件注入的 context 获取用户 ID
	userID, _ := c.Get("user_id")
	if userID == nil || userID == "" {
		response.Error(c, http.StatusUnauthorized, 20001, "unauthorized")
		return
	}

	user, err := h.svc.GetByID(c.Request.Context(), userID.(string))
	if err != nil {
		response.Error(c, http.StatusNotFound, 30004, err.Error())
		return
	}

	response.Success(c, user)
}

// GetUser 获取用户详情
// GET /api/v1/users/:id
func (h *UserHandler) GetUser(c *gin.Context) {
	id := c.Param("id")

	user, err := h.svc.GetByID(c.Request.Context(), id)
	if err != nil {
		response.Error(c, http.StatusNotFound, 30004, err.Error())
		return
	}

	response.Success(c, user)
}

// ListUsers 获取用户列表
// GET /api/v1/users
func (h *UserHandler) ListUsers(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))

	users, total, err := h.svc.ListUsers(c.Request.Context(), page, pageSize)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, 10006, err.Error())
		return
	}

	totalPages := int(total) / pageSize
	if int(total)%pageSize > 0 {
		totalPages++
	}

	response.List(c, users, &response.Pagination{
		Page:       page,
		PageSize:   pageSize,
		Total:      total,
		TotalPages: totalPages,
	})
}

// UpdateUser 更新用户信息
// PUT /api/v1/users/:id
func (h *UserHandler) UpdateUser(c *gin.Context) {
	id := c.Param("id")

	var req domain.UpdateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, 10001, "invalid request: "+err.Error())
		return
	}

	user, err := h.svc.UpdateUser(c.Request.Context(), id, req)
	if err != nil {
		response.Error(c, http.StatusNotFound, 30004, err.Error())
		return
	}

	response.Success(c, user)
}

// SuspendUser 封禁用户
// POST /api/v1/users/:id/suspend
func (h *UserHandler) SuspendUser(c *gin.Context) {
	id := c.Param("id")
	user, err := h.svc.SuspendUser(c.Request.Context(), id)
	if err != nil {
		response.Error(c, http.StatusNotFound, 30004, err.Error())
		return
	}
	response.Success(c, user)
}

// ActivateUser 解封用户
// POST /api/v1/users/:id/activate
func (h *UserHandler) ActivateUser(c *gin.Context) {
	id := c.Param("id")
	user, err := h.svc.ActivateUser(c.Request.Context(), id)
	if err != nil {
		response.Error(c, http.StatusNotFound, 30004, err.Error())
		return
	}
	response.Success(c, user)
}

// ListRoles 获取角色列表
// GET /api/v1/roles
func (h *UserHandler) ListRoles(c *gin.Context) {
	response.Success(c, gin.H{"message": "roles list - use permission handler"})
}

// HealthCheck 健康检查
// GET /api/v1/health
func (h *UserHandler) HealthCheck(c *gin.Context) {
	response.Success(c, gin.H{
		"status":  "ok",
		"service": "CampusOS",
		"version": "0.1.0-dev",
	})
}
