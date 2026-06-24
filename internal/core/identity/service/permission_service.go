package service

import (
	"context"

	"github.com/campusos/CampusOS/internal/core/identity/repository"
)

// PermissionService 权限检查服务
type PermissionService struct {
	roleRepo repository.RoleRepository
}

func NewPermissionService(roleRepo repository.RoleRepository) *PermissionService {
	return &PermissionService{roleRepo: roleRepo}
}

// Check 检查用户是否拥有指定权限
func (s *PermissionService) Check(ctx context.Context, userID string, resource, action string) (bool, error) {
	return s.roleRepo.HasPermission(ctx, userID, resource, action)
}

// GetUserRoles 获取用户角色列表
func (s *PermissionService) GetUserRoles(ctx context.Context, userID string) ([]*repository.Role, error) {
	return s.roleRepo.GetUserRoles(ctx, userID)
}

// AssignRole 给用户分配角色
func (s *PermissionService) AssignRole(ctx context.Context, userID string, roleID int64) error {
	return s.roleRepo.AssignRole(ctx, userID, roleID, "global", nil)
}

// RevokeRole 撤销用户角色
func (s *PermissionService) RevokeRole(ctx context.Context, userID string, roleID int64) error {
	return s.roleRepo.RevokeRole(ctx, userID, roleID)
}

// ListRoles 列出所有角色
func (s *PermissionService) ListRoles(ctx context.Context) ([]*repository.Role, error) {
	return s.roleRepo.ListRoles(ctx)
}
