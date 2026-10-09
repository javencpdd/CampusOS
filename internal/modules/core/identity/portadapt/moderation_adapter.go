package portadapt

import (
	"context"

	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/modules/core/identity/service"
)

var (
	ErrInvalidScope = identityport.ErrInvalidScope
	ErrUserNotFound = identityport.ErrUserNotFound
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

func (p *PermissionModerationPolicy) ListRoleAssignments(ctx context.Context, userID, roleName string) ([]identityport.RoleAssignment, error) {
	assignments, err := p.permissions.GetRoleAssignments(ctx, userID, roleName)
	if err != nil {
		return nil, err
	}
	result := make([]identityport.RoleAssignment, 0, len(assignments))
	for _, assignment := range assignments {
		result = append(result, identityport.RoleAssignment{UserID: assignment.UserID, ScopeType: assignment.ScopeType, ScopeID: assignment.ScopeID})
	}
	return result, nil
}

func (p *PermissionModerationPolicy) ReplaceCategoryRoleScopes(ctx context.Context, userID, roleName string, categoryIDs []int64) (bool, error) {
	return p.permissions.ReplaceCategoryRoleScopes(ctx, userID, roleName, categoryIDs)
}

func (p *PermissionModerationPolicy) ReplaceCategoryRoleScopesByActor(ctx context.Context, actorID, userID, roleName string, categoryIDs []int64, boards identityport.BoardFactsProvider) (bool, error) {
	return p.permissions.ReplaceCategoryRoleScopesByActor(ctx, actorID, userID, roleName, categoryIDs, boards)
}
