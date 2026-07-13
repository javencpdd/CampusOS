package port

import (
	"context"

	"github.com/campusos/CampusOS/internal/core/identity/repository"
	"github.com/campusos/CampusOS/internal/core/identity/service"
)

var (
	ErrInvalidScope = service.ErrInvalidRoleAssignment
	ErrUserNotFound = repository.ErrUserNotFound
)

type PermissionModerationPolicy struct {
	permissions *service.PermissionService
}

func NewPermissionModerationPolicy(permissions *service.PermissionService) *PermissionModerationPolicy {
	return &PermissionModerationPolicy{permissions: permissions}
}

func (p *PermissionModerationPolicy) CheckScoped(ctx context.Context, userID, resource, action, scopeType string, scopeID int64) (bool, error) {
	return p.permissions.CheckScoped(ctx, userID, resource, action, scopeType, scopeID)
}

func (p *PermissionModerationPolicy) ListRoleAssignments(ctx context.Context, userID, roleName string) ([]RoleAssignment, error) {
	assignments, err := p.permissions.GetRoleAssignments(ctx, userID, roleName)
	if err != nil {
		return nil, err
	}
	result := make([]RoleAssignment, 0, len(assignments))
	for _, assignment := range assignments {
		result = append(result, RoleAssignment{UserID: assignment.UserID, ScopeType: assignment.ScopeType, ScopeID: assignment.ScopeID})
	}
	return result, nil
}

func (p *PermissionModerationPolicy) ReplaceCategoryRoleScopes(ctx context.Context, userID, roleName string, categoryIDs []int64) (bool, error) {
	return p.permissions.ReplaceCategoryRoleScopes(ctx, userID, roleName, categoryIDs)
}
