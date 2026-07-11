package service

import (
	"context"
	"errors"
	"strconv"

	"github.com/campusos/CampusOS/internal/core/identity/domain"
	"github.com/campusos/CampusOS/internal/core/identity/repository"
)

var (
	ErrInvalidRoleAssignment  = errors.New("invalid role assignment")
	ErrRoleAssignmentNotFound = errors.New("role assignment not found")
	ErrProtectedRole          = errors.New("protected role")
	ErrRoleRequiresScope      = errors.New("role requires an explicit scope")
)

type UserLookup interface {
	GetByID(ctx context.Context, id string) (*domain.User, error)
}

// PermissionService manages role permission checks and global role assignments.
type PermissionService struct {
	roleRepo repository.RoleRepository
	userRepo UserLookup
}

func NewPermissionService(roleRepo repository.RoleRepository, userRepo UserLookup) *PermissionService {
	return &PermissionService{roleRepo: roleRepo, userRepo: userRepo}
}

// Check checks whether a user has the requested resource action.
func (s *PermissionService) Check(ctx context.Context, userID string, resource, action string) (bool, error) {
	if s.userRepo != nil {
		if _, err := s.userRepo.GetByID(ctx, userID); err != nil {
			return false, err
		}
	}
	return s.roleRepo.HasPermission(ctx, userID, resource, action)
}

// CheckScoped checks a permission against a server-derived data scope. Global
// grants remain valid, while category grants only match their category ID.
func (s *PermissionService) CheckScoped(ctx context.Context, userID string, resource, action, scopeType string, scopeID int64) (bool, error) {
	if scopeType == "" || scopeID <= 0 {
		return false, ErrInvalidRoleAssignment
	}
	if s.userRepo != nil {
		if _, err := s.userRepo.GetByID(ctx, userID); err != nil {
			return false, err
		}
	}
	return s.roleRepo.HasScopedPermission(ctx, userID, resource, action, scopeType, scopeID)
}

// GetUserRoles returns active roles assigned to one user.
func (s *PermissionService) GetUserRoles(ctx context.Context, userID string) ([]*repository.Role, error) {
	if _, err := strconv.ParseInt(userID, 10, 64); err != nil {
		return nil, ErrInvalidRoleAssignment
	}
	if s.userRepo != nil {
		if _, err := s.userRepo.GetByID(ctx, userID); err != nil {
			return nil, err
		}
	}
	return s.roleRepo.GetUserRoles(ctx, userID)
}

// AssignRole assigns a global role. It validates both target entities before
// writing and reports whether a new assignment was created.
func (s *PermissionService) AssignRole(ctx context.Context, userID string, roleID int64) (bool, error) {
	if _, err := strconv.ParseInt(userID, 10, 64); err != nil || roleID <= 0 {
		return false, ErrInvalidRoleAssignment
	}
	if s.userRepo != nil {
		if _, err := s.userRepo.GetByID(ctx, userID); err != nil {
			return false, err
		}
	}
	role, err := s.roleRepo.GetRoleByID(ctx, roleID)
	if err != nil {
		return false, err
	}
	if isProtectedAssignmentRole(role.Name) {
		return false, ErrProtectedRole
	}
	if role.Name == "moderator" {
		return false, ErrRoleRequiresScope
	}
	return s.roleRepo.AssignRole(ctx, userID, roleID, "global", nil)
}

// RevokeRole revokes every active explicit assignment for the role from a user.
func (s *PermissionService) RevokeRole(ctx context.Context, userID string, roleID int64) (bool, error) {
	if _, err := strconv.ParseInt(userID, 10, 64); err != nil || roleID <= 0 {
		return false, ErrInvalidRoleAssignment
	}
	if s.userRepo != nil {
		if _, err := s.userRepo.GetByID(ctx, userID); err != nil {
			return false, err
		}
	}
	role, err := s.roleRepo.GetRoleByID(ctx, roleID)
	if err != nil {
		return false, err
	}
	if isProtectedAssignmentRole(role.Name) {
		return false, ErrProtectedRole
	}
	revoked, err := s.roleRepo.RevokeRole(ctx, userID, roleID)
	if err != nil {
		return false, err
	}
	if !revoked {
		return false, ErrRoleAssignmentNotFound
	}
	return true, nil
}

// ReplaceCategoryRoleScopes atomically replaces one user's category grants for
// a role. Callers must validate that every category exists before invoking it.
func (s *PermissionService) ReplaceCategoryRoleScopes(ctx context.Context, userID, roleName string, categoryIDs []int64) (bool, error) {
	if _, err := strconv.ParseInt(userID, 10, 64); err != nil {
		return false, ErrInvalidRoleAssignment
	}
	if s.userRepo != nil {
		if _, err := s.userRepo.GetByID(ctx, userID); err != nil {
			return false, err
		}
	}
	role, err := s.roleRepo.GetRoleByName(ctx, roleName)
	if err != nil {
		return false, err
	}
	if isProtectedAssignmentRole(role.Name) {
		return false, ErrProtectedRole
	}
	for _, categoryID := range categoryIDs {
		if categoryID <= 0 {
			return false, ErrInvalidRoleAssignment
		}
	}
	return s.roleRepo.ReplaceRoleScopes(ctx, userID, role.ID, "category", categoryIDs)
}

func (s *PermissionService) GetRoleAssignments(ctx context.Context, userID, roleName string) ([]*repository.UserRoleAssignment, error) {
	if userID != "" {
		if _, err := strconv.ParseInt(userID, 10, 64); err != nil {
			return nil, ErrInvalidRoleAssignment
		}
		if s.userRepo != nil {
			if _, err := s.userRepo.GetByID(ctx, userID); err != nil {
				return nil, err
			}
		}
	}
	role, err := s.roleRepo.GetRoleByName(ctx, roleName)
	if err != nil {
		return nil, err
	}
	return s.roleRepo.ListRoleAssignments(ctx, userID, role.ID)
}

// ListRoles lists active role definitions.
func (s *PermissionService) ListRoles(ctx context.Context) ([]*repository.Role, error) {
	return s.roleRepo.ListRoles(ctx)
}

func isProtectedAssignmentRole(roleName string) bool {
	return roleName == "member" || roleName == "guest"
}
