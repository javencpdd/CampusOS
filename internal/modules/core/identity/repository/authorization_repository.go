package repository

import (
	"context"
	"errors"
	"time"
)

var ErrLastGlobalRoleAssignment = errors.New("cannot revoke the last global role assignment")

// PermissionDefinition is the stable, environment-independent permission
// catalog entry. Numeric IDs remain internal database keys; Code is used by
// routes, SDKs, audit records and documentation.
type PermissionDefinition struct {
	ID                int64      `json:"id"`
	Code              string     `json:"code"`
	Domain            string     `json:"domain"`
	Resource          string     `json:"resource"`
	Action            string     `json:"action"`
	Description       string     `json:"description"`
	RiskLevel         string     `json:"risk_level"`
	AllowedScopeTypes []string   `json:"allowed_scope_types"`
	AuditLevel        string     `json:"audit_level"`
	DeprecatedAt      *time.Time `json:"deprecated_at,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

type RolePermission struct {
	RoleID     int64                `json:"role_id"`
	RoleName   string               `json:"role_name"`
	Permission PermissionDefinition `json:"permission"`
	CreatedBy  string               `json:"created_by,omitempty"`
	CreatedAt  time.Time            `json:"created_at"`
}

// RouteOperation is runtime-discovered transport metadata. The operation code
// is stable across URL moves; legacy aliases retain older URL-derived IDs for
// logs, bookmarks and generated documentation during the compatibility window.
type RouteOperation struct {
	ID             int64     `json:"id"`
	OperationCode  string    `json:"operation_code"`
	ModuleOwner    string    `json:"module_owner"`
	Method         string    `json:"method"`
	PathTemplate   string    `json:"path_template"`
	Audience       string    `json:"audience"`
	PermissionCode string    `json:"permission_code,omitempty"`
	LegacyAliases  []string  `json:"legacy_aliases,omitempty"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type AuthorizationAudit struct {
	ID             int64     `json:"id"`
	RequestID      string    `json:"request_id,omitempty"`
	ActorID        string    `json:"actor_id,omitempty"`
	PermissionCode string    `json:"permission_code,omitempty"`
	OperationCode  string    `json:"operation_code,omitempty"`
	ScopeType      string    `json:"scope_type,omitempty"`
	ScopeID        *int64    `json:"scope_id,omitempty"`
	ResourceType   string    `json:"resource_type,omitempty"`
	ResourceID     string    `json:"resource_id,omitempty"`
	Outcome        string    `json:"outcome"`
	Reason         string    `json:"reason,omitempty"`
	IPAddress      string    `json:"ip_address,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

// AuthorizationRepository is an optional v10 extension of RoleRepository.
// PermissionService keeps the legacy RoleRepository interface so old adapters
// and tests remain compatible throughout the migration period.
type AuthorizationRepository interface {
	ListPermissionDefinitions(context.Context) ([]PermissionDefinition, error)
	ListRolePermissions(context.Context, int64) ([]RolePermission, error)
	ReplaceRolePermissions(context.Context, int64, []string, string) error
	CreateCustomRole(context.Context, Role) (*Role, error)
	UpdateCustomRole(context.Context, Role) (*Role, error)
	HasPermissionCode(context.Context, string, string) (bool, error)
	HasScopedPermissionCode(context.Context, string, string, string, int64) (bool, error)
	HasAnyScopedPermissionCode(context.Context, string, string, string) (bool, error)
	SyncRouteOperations(context.Context, []RouteOperation) error
	ListRouteOperations(context.Context) ([]RouteOperation, error)
	RecordAuthorizationAudit(context.Context, AuthorizationAudit) error
	ListAuthorizationAudits(context.Context, int) ([]AuthorizationAudit, error)
	CountGlobalRoleAssignments(context.Context, int64) (int, error)
}

// LastGlobalRoleRevoker closes the count-then-write race for protected global
// roles. PostgreSQL and Memory adapters implement the check and revocation in
// one serialization boundary; legacy adapters can continue without it during
// the compatibility period.
type LastGlobalRoleRevoker interface {
	RevokeRoleUnlessLastGlobal(context.Context, string, int64) (bool, error)
}
