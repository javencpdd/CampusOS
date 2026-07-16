package service

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/campusos/CampusOS/internal/core/identity/domain"
	"github.com/campusos/CampusOS/internal/core/identity/permissioncode"
	"github.com/campusos/CampusOS/internal/core/identity/repository"
)

var (
	ErrInvalidRoleAssignment  = errors.New("invalid role assignment")
	ErrRoleAssignmentNotFound = errors.New("role assignment not found")
	ErrProtectedRole          = errors.New("protected role")
	ErrRoleRequiresScope      = errors.New("role requires an explicit scope")
	ErrPermissionEscalation   = errors.New("role assignment would exceed actor permissions")
	ErrLastSystemAdmin        = errors.New("cannot remove the last effective system administrator")
	ErrAuthorizationCatalog   = errors.New("authorization catalog is unavailable")
	ErrInvalidPermissionCode  = errors.New("invalid permission code")
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

// CheckCode is the v10 authorization path. It reads the independent
// permission catalog when available and falls back to the legacy role-bound
// table only while an instance has not yet applied the additive migration.
func (s *PermissionService) CheckCode(ctx context.Context, userID, code string) (bool, error) {
	if !permissioncode.IsCode(code) {
		return false, ErrInvalidPermissionCode
	}
	if s.userRepo != nil {
		if _, err := s.userRepo.GetByID(ctx, userID); err != nil {
			return false, err
		}
	}
	if catalog, ok := s.authorizationRepository(); ok {
		allowed, err := catalog.HasPermissionCode(ctx, userID, code)
		if err == nil {
			return allowed, nil
		}
		if !isCatalogUnavailable(err) {
			return false, err
		}
	}
	resource, action, ok := permissioncode.LegacyForCode(code)
	if !ok {
		return false, nil
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

func (s *PermissionService) CheckCodeScoped(ctx context.Context, userID, code, scopeType string, scopeID int64) (bool, error) {
	if !permissioncode.IsCode(code) {
		return false, ErrInvalidPermissionCode
	}
	if scopeType == "" || scopeID <= 0 {
		return false, ErrInvalidRoleAssignment
	}
	if s.userRepo != nil {
		if _, err := s.userRepo.GetByID(ctx, userID); err != nil {
			return false, err
		}
	}
	if catalog, ok := s.authorizationRepository(); ok {
		allowed, err := catalog.HasScopedPermissionCode(ctx, userID, code, scopeType, scopeID)
		if err == nil {
			return allowed, nil
		}
		if !isCatalogUnavailable(err) {
			return false, err
		}
	}
	resource, action, ok := permissioncode.LegacyForCode(code)
	if !ok {
		return false, nil
	}
	return s.roleRepo.HasScopedPermission(ctx, userID, resource, action, scopeType, scopeID)
}

// HasAnyScopedPermissionCode is a deliberately narrow route-gate helper. A
// category-scoped moderator can reach a route whose target category is only
// known after the service loads the resource; it never replaces the final
// CheckCodeScoped call using that stored category.
func (s *PermissionService) HasAnyScopedPermissionCode(ctx context.Context, userID, code, scopeType string) (bool, error) {
	if !permissioncode.IsCode(code) {
		return false, ErrInvalidPermissionCode
	}
	if scopeType == "" {
		return false, ErrInvalidRoleAssignment
	}
	if s.userRepo != nil {
		if _, err := s.userRepo.GetByID(ctx, userID); err != nil {
			return false, err
		}
	}
	if catalog, ok := s.authorizationRepository(); ok {
		allowed, err := catalog.HasAnyScopedPermissionCode(ctx, userID, code, scopeType)
		if err == nil {
			return allowed, nil
		}
		if !isCatalogUnavailable(err) {
			return false, err
		}
	}
	if allowed, err := s.CheckCode(ctx, userID, code); err != nil || allowed {
		return allowed, err
	}
	resource, action, ok := permissioncode.LegacyForCode(code)
	if !ok {
		return false, nil
	}
	assignments, err := s.roleRepo.ListRoleAssignments(ctx, userID, 0)
	if err != nil {
		return false, err
	}
	for _, assignment := range assignments {
		if assignment.ScopeType != scopeType || assignment.ScopeID == nil {
			continue
		}
		allowed, err := s.roleRepo.HasScopedPermission(ctx, userID, resource, action, scopeType, *assignment.ScopeID)
		if err != nil {
			return false, err
		}
		if allowed {
			return true, nil
		}
	}
	return false, nil
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

// AssignRoleByActor adds service-side anti-escalation checks to the HTTP
// management flow. The legacy AssignRole method remains for compatibility with
// trusted internal callers during the migration.
func (s *PermissionService) AssignRoleByActor(ctx context.Context, actorID, userID string, roleID int64) (bool, error) {
	if actorID == "" || actorID == userID {
		return false, ErrPermissionEscalation
	}
	if err := s.requireActorCode(ctx, actorID, "identity.role.assign"); err != nil {
		return false, err
	}
	if err := s.assertActorMayAssignRole(ctx, actorID, roleID); err != nil {
		return false, err
	}
	assigned, err := s.AssignRole(ctx, userID, roleID)
	if err != nil {
		return false, err
	}
	if assigned {
		s.recordAuthorizationAudit(ctx, repository.AuthorizationAudit{
			ActorID: actorID, PermissionCode: "identity.role.assign", OperationCode: "identity.role.assign",
			ResourceType: "user_role", ResourceID: userID + ":" + strconv.FormatInt(roleID, 10), Outcome: "allow",
		})
	}
	return assigned, nil
}

func (s *PermissionService) RevokeRoleByActor(ctx context.Context, actorID, userID string, roleID int64) (bool, error) {
	if actorID == "" || actorID == userID {
		return false, ErrPermissionEscalation
	}
	if _, err := strconv.ParseInt(userID, 10, 64); err != nil || roleID <= 0 {
		return false, ErrInvalidRoleAssignment
	}
	if s.userRepo != nil {
		if _, err := s.userRepo.GetByID(ctx, userID); err != nil {
			return false, err
		}
	}
	if err := s.requireActorCode(ctx, actorID, "identity.role.revoke"); err != nil {
		return false, err
	}
	role, err := s.roleRepo.GetRoleByID(ctx, roleID)
	if err != nil {
		return false, err
	}
	if err := s.assertActorMayAssignRole(ctx, actorID, roleID); err != nil {
		return false, err
	}
	var revoked bool
	if role.Name == "admin" {
		if protectedRevoker, ok := s.roleRepo.(repository.LastGlobalRoleRevoker); ok {
			var err error
			revoked, err = protectedRevoker.RevokeRoleUnlessLastGlobal(ctx, userID, roleID)
			if errors.Is(err, repository.ErrLastGlobalRoleAssignment) {
				return false, ErrLastSystemAdmin
			}
			if err != nil {
				return false, err
			}
		} else {
			// Legacy adapters keep the compatibility check. Production
			// PostgreSQL and Memory adapters use the atomic path above.
			assignments, assignmentErr := s.roleRepo.ListRoleAssignments(ctx, "", roleID)
			if assignmentErr != nil {
				return false, assignmentErr
			}
			count := 0
			for _, assignment := range assignments {
				if assignment.ScopeType == "global" && assignment.ScopeID == nil {
					count++
				}
			}
			if count <= 1 {
				return false, ErrLastSystemAdmin
			}
			var err error
			revoked, err = s.RevokeRole(ctx, userID, roleID)
			if err != nil {
				return false, err
			}
		}
	} else {
		var err error
		revoked, err = s.RevokeRole(ctx, userID, roleID)
		if err != nil {
			return false, err
		}
	}
	if !revoked {
		return false, ErrRoleAssignmentNotFound
	}
	s.recordAuthorizationAudit(ctx, repository.AuthorizationAudit{
		ActorID: actorID, PermissionCode: "identity.role.revoke", OperationCode: "identity.role.revoke",
		ResourceType: "user_role", ResourceID: userID + ":" + strconv.FormatInt(roleID, 10), Outcome: "allow",
	})
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

// ReplaceCategoryRoleScopesByActor is the service-side administration path
// for moderator scopes. It prevents an internal caller from bypassing the
// route middleware and granting a role whose effective permissions exceed the
// actor's own permissions.
func (s *PermissionService) ReplaceCategoryRoleScopesByActor(ctx context.Context, actorID, userID, roleName string, categoryIDs []int64) (bool, error) {
	if actorID == "" || actorID == userID {
		return false, ErrPermissionEscalation
	}
	if err := s.requireActorCode(ctx, actorID, "identity.role.assign"); err != nil {
		return false, err
	}
	role, err := s.roleRepo.GetRoleByName(ctx, roleName)
	if err != nil {
		return false, err
	}
	if err := s.assertActorMayAssignRole(ctx, actorID, role.ID); err != nil {
		return false, err
	}
	changed, err := s.ReplaceCategoryRoleScopes(ctx, userID, roleName, categoryIDs)
	if err != nil {
		return false, err
	}
	if changed {
		s.recordAuthorizationAudit(ctx, repository.AuthorizationAudit{
			ActorID: actorID, PermissionCode: "identity.role.assign", OperationCode: "identity.moderator.scope.replace",
			ScopeType: "category", ResourceType: "user_role", ResourceID: userID + ":" + roleName, Outcome: "allow",
		})
	}
	return changed, nil
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

func (s *PermissionService) ListPermissionDefinitions(ctx context.Context) ([]repository.PermissionDefinition, error) {
	catalog, ok := s.authorizationRepository()
	if !ok {
		return nil, ErrAuthorizationCatalog
	}
	return catalog.ListPermissionDefinitions(ctx)
}

func (s *PermissionService) ListRolePermissions(ctx context.Context, roleID int64) ([]repository.RolePermission, error) {
	catalog, ok := s.authorizationRepository()
	if !ok {
		return nil, ErrAuthorizationCatalog
	}
	return catalog.ListRolePermissions(ctx, roleID)
}

func (s *PermissionService) CreateCustomRole(ctx context.Context, actorID, name, description string, permissionCodes []string) (*repository.Role, error) {
	catalog, ok := s.authorizationRepository()
	if !ok {
		return nil, ErrAuthorizationCatalog
	}
	if !permissioncode.IsCode("identity.role.create") {
		return nil, ErrInvalidPermissionCode
	}
	allowed, err := s.CheckCode(ctx, actorID, "identity.role.create")
	if err != nil || !allowed {
		if err != nil {
			return nil, err
		}
		return nil, ErrPermissionEscalation
	}
	if err := s.assertActorMayGrantCodes(ctx, actorID, permissionCodes); err != nil {
		return nil, err
	}
	role, err := catalog.CreateCustomRole(ctx, repository.Role{Name: name, Description: description})
	if err != nil {
		return nil, err
	}
	if err := catalog.ReplaceRolePermissions(ctx, role.ID, permissionCodes, actorID); err != nil {
		return nil, err
	}
	s.recordAuthorizationAudit(ctx, repository.AuthorizationAudit{ActorID: actorID, PermissionCode: "identity.role.create", OperationCode: "http.identity.role.create", ResourceType: "role", ResourceID: strconv.FormatInt(role.ID, 10), Outcome: "allow"})
	return role, nil
}

func (s *PermissionService) UpdateRolePermissions(ctx context.Context, actorID string, roleID int64, permissionCodes []string) error {
	catalog, ok := s.authorizationRepository()
	if !ok {
		return ErrAuthorizationCatalog
	}
	role, err := s.roleRepo.GetRoleByID(ctx, roleID)
	if err != nil {
		return err
	}
	if role.IsSystem {
		return ErrProtectedRole
	}
	allowed, err := s.CheckCode(ctx, actorID, "identity.role.update_permissions")
	if err != nil || !allowed {
		if err != nil {
			return err
		}
		return ErrPermissionEscalation
	}
	if err := s.assertActorMayGrantCodes(ctx, actorID, permissionCodes); err != nil {
		return err
	}
	if err := catalog.ReplaceRolePermissions(ctx, roleID, permissionCodes, actorID); err != nil {
		return err
	}
	s.recordAuthorizationAudit(ctx, repository.AuthorizationAudit{ActorID: actorID, PermissionCode: "identity.role.update_permissions", OperationCode: "http.identity.role.update_permissions", ResourceType: "role", ResourceID: strconv.FormatInt(roleID, 10), Outcome: "allow"})
	return nil
}

func (s *PermissionService) ListAuthorizationAudits(ctx context.Context, limit int) ([]repository.AuthorizationAudit, error) {
	catalog, ok := s.authorizationRepository()
	if !ok {
		return nil, ErrAuthorizationCatalog
	}
	return catalog.ListAuthorizationAudits(ctx, limit)
}

func (s *PermissionService) SyncRouteOperations(ctx context.Context, operations []repository.RouteOperation) error {
	catalog, ok := s.authorizationRepository()
	if !ok {
		return nil
	}
	return catalog.SyncRouteOperations(ctx, operations)
}

func (s *PermissionService) RecordRouteDecision(ctx context.Context, audit repository.AuthorizationAudit) {
	s.recordAuthorizationAudit(ctx, audit)
}

// RecordHTTPAuthorizationDecision is intentionally a small transport-facing
// adapter. It keeps Gin/middleware free of Identity repository types while
// preserving request-level allow/deny evidence for high-risk administration.
func (s *PermissionService) RecordHTTPAuthorizationDecision(ctx context.Context, actorID, permissionCode, operationCode, outcome, reason, requestID, ipAddress string) {
	s.recordAuthorizationAudit(ctx, repository.AuthorizationAudit{
		ActorID: actorID, PermissionCode: permissionCode, OperationCode: operationCode,
		Outcome: outcome, Reason: reason, RequestID: requestID, IPAddress: ipAddress,
	})
}

// RecordContentAuthorizationDecision keeps the resource-derived category
// scope alongside the decision. Community uses this optional Port without
// importing Identity repository types.
func (s *PermissionService) RecordContentAuthorizationDecision(ctx context.Context, actorID, permissionCode string, scopeID int64, outcome, reason string) {
	scope := scopeID
	s.recordAuthorizationAudit(ctx, repository.AuthorizationAudit{
		ActorID: actorID, PermissionCode: permissionCode, OperationCode: "community.content." + strings.ReplaceAll(permissionCode, ".", "_"),
		ScopeType: "category", ScopeID: &scope, ResourceType: "thread", Outcome: outcome, Reason: reason,
	})
}

func (s *PermissionService) assertActorMayAssignRole(ctx context.Context, actorID string, roleID int64) error {
	permissions, err := s.ListRolePermissions(ctx, roleID)
	if err != nil {
		return err
	}
	codes := make([]string, 0, len(permissions))
	for _, item := range permissions {
		codes = append(codes, item.Permission.Code)
	}
	return s.assertActorMayGrantCodes(ctx, actorID, codes)
}

func (s *PermissionService) assertActorMayGrantCodes(ctx context.Context, actorID string, codes []string) error {
	for _, code := range codes {
		if !permissioncode.IsCode(code) {
			return ErrInvalidPermissionCode
		}
		allowed, err := s.CheckCode(ctx, actorID, code)
		if err != nil {
			return err
		}
		if !allowed {
			return ErrPermissionEscalation
		}
	}
	return nil
}

func (s *PermissionService) requireActorCode(ctx context.Context, actorID, code string) error {
	allowed, err := s.CheckCode(ctx, actorID, code)
	if err != nil {
		return err
	}
	if !allowed {
		return ErrPermissionEscalation
	}
	return nil
}

func (s *PermissionService) authorizationRepository() (repository.AuthorizationRepository, bool) {
	catalog, ok := s.roleRepo.(repository.AuthorizationRepository)
	return catalog, ok
}

func (s *PermissionService) recordAuthorizationAudit(ctx context.Context, audit repository.AuthorizationAudit) {
	catalog, ok := s.authorizationRepository()
	if !ok {
		return
	}
	_ = catalog.RecordAuthorizationAudit(ctx, audit)
}

func isCatalogUnavailable(err error) bool {
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "does not exist") || strings.Contains(text, "undefined table")
}

func isProtectedAssignmentRole(roleName string) bool {
	return roleName == "member" || roleName == "guest"
}
