package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/identity/permissioncode"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/campusos/CampusOS/pkg/idgen"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Role 角色实体
type Role struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	IsSystem    bool   `json:"is_system"`
}

// UserRole 用户角色关联
type UserRole struct {
	ID        int64  `json:"id"`
	UserID    int64  `json:"user_id"`
	RoleID    int64  `json:"role_id"`
	ScopeType string `json:"scope_type"`
	ScopeID   *int64 `json:"scope_id,omitempty"`
}

// UserRoleAssignment exposes one explicit role grant and its data scope.
type UserRoleAssignment struct {
	ID        int64  `json:"id"`
	UserID    string `json:"user_id"`
	RoleID    int64  `json:"role_id"`
	RoleName  string `json:"role_name"`
	ScopeType string `json:"scope_type"`
	ScopeID   *int64 `json:"scope_id,omitempty"`
}

// Permission 权限实体
type Permission struct {
	ID       int64  `json:"id"`
	RoleID   int64  `json:"role_id"`
	Resource string `json:"resource"`
	Action   string `json:"action"`
}

var ErrRoleNotFound = errors.New("role not found")

// RoleRepository 角色仓储接口
type RoleRepository interface {
	GetRoleByName(ctx context.Context, name string) (*Role, error)
	GetRoleByID(ctx context.Context, id int64) (*Role, error)
	GetUserRoles(ctx context.Context, userID string) ([]*Role, error)
	ListRoleAssignments(ctx context.Context, userID string, roleID int64) ([]*UserRoleAssignment, error)
	AssignRole(ctx context.Context, userID string, roleID int64, scopeType string, scopeID *int64) (bool, error)
	ReplaceRoleScopes(ctx context.Context, userID string, roleID int64, scopeType string, scopeIDs []int64) (bool, error)
	RevokeRole(ctx context.Context, userID string, roleID int64) (bool, error)
	HasPermission(ctx context.Context, userID string, resource, action string) (bool, error)
	HasScopedPermission(ctx context.Context, userID string, resource, action, scopeType string, scopeID int64) (bool, error)
	ListRoles(ctx context.Context) ([]*Role, error)
}

// PgRoleRepository PostgreSQL 角色仓储
type PgRoleRepository struct {
	pool *pgxpool.Pool
}

func NewPgRoleRepository(pool *pgxpool.Pool) *PgRoleRepository {
	return &PgRoleRepository{pool: pool}
}

func (r *PgRoleRepository) db(ctx context.Context) transaction.Executor {
	return transaction.ExecutorFor(ctx, r.pool)
}

// withTransaction participates in a Core command transaction when one is
// present and otherwise preserves the repository's historical atomic helper.
func (r *PgRoleRepository) withTransaction(ctx context.Context, action func(transaction.Executor) error) error {
	if tx, ok := transaction.FromContext(ctx); ok {
		return action(tx)
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := action(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *PgRoleRepository) GetRoleByName(ctx context.Context, name string) (*Role, error) {
	query := `SELECT id, name, description, is_system FROM roles WHERE name = $1 AND deleted_at IS NULL`
	role := &Role{}
	err := r.db(ctx).QueryRow(ctx, query, name).Scan(&role.ID, &role.Name, &role.Description, &role.IsSystem)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrRoleNotFound
		}
		return nil, err
	}
	return role, nil
}

func (r *PgRoleRepository) GetRoleByID(ctx context.Context, id int64) (*Role, error) {
	query := `SELECT id, name, description, is_system FROM roles WHERE id = $1 AND deleted_at IS NULL`
	role := &Role{}
	err := r.db(ctx).QueryRow(ctx, query, id).Scan(&role.ID, &role.Name, &role.Description, &role.IsSystem)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrRoleNotFound
		}
		return nil, err
	}
	return role, nil
}

func (r *PgRoleRepository) GetUserRoles(ctx context.Context, userID string) ([]*Role, error) {
	query := `SELECT r.id, r.name, r.description, r.is_system
		FROM roles r
		WHERE r.deleted_at IS NULL
		  AND (
				r.name = 'member'
				AND EXISTS (
					SELECT 1 FROM users u
					WHERE u.id = $1 AND u.deleted_at IS NULL
				)
					OR EXISTS (
						SELECT 1 FROM user_roles ur
						WHERE ur.user_id = $1 AND ur.role_id = r.id AND ur.deleted_at IS NULL
						  AND (r.name <> 'admin' OR EXISTS (
							SELECT 1 FROM identity_admin_accounts aa
							WHERE aa.user_id=ur.user_id AND aa.status='active'
						  ))
					)
			  )
		ORDER BY r.id ASC`
	rows, err := r.db(ctx).Query(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var roles []*Role
	for rows.Next() {
		role := &Role{}
		if err := rows.Scan(&role.ID, &role.Name, &role.Description, &role.IsSystem); err != nil {
			return nil, err
		}
		roles = append(roles, role)
	}
	return roles, nil
}

func (r *PgRoleRepository) ListRoleAssignments(ctx context.Context, userID string, roleID int64) ([]*UserRoleAssignment, error) {
	query := `SELECT ur.id, ur.user_id, ur.role_id, r.name, ur.scope_type, ur.scope_id
		FROM user_roles ur
		INNER JOIN roles r ON r.id = ur.role_id AND r.deleted_at IS NULL
		WHERE ur.deleted_at IS NULL
		  AND ($1 = '' OR ur.user_id::text = $1)
		  AND ($2 = 0 OR ur.role_id = $2)
		ORDER BY ur.user_id ASC, ur.role_id ASC, ur.scope_type ASC, ur.scope_id ASC NULLS FIRST`
	rows, err := r.db(ctx).Query(ctx, query, userID, roleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	assignments := make([]*UserRoleAssignment, 0)
	for rows.Next() {
		assignment := &UserRoleAssignment{}
		if err := rows.Scan(
			&assignment.ID,
			&assignment.UserID,
			&assignment.RoleID,
			&assignment.RoleName,
			&assignment.ScopeType,
			&assignment.ScopeID,
		); err != nil {
			return nil, err
		}
		assignments = append(assignments, assignment)
	}
	return assignments, rows.Err()
}

func (r *PgRoleRepository) AssignRole(ctx context.Context, userID string, roleID int64, scopeType string, scopeID *int64) (bool, error) {
	var (
		query string
		tag   pgconn.CommandTag
		err   error
	)
	if scopeID == nil {
		query = `INSERT INTO user_roles (id, user_id, role_id, scope_type, scope_id)
			VALUES ($1, $2, $3, $4, NULL)
			ON CONFLICT (user_id, role_id, scope_type)
			WHERE deleted_at IS NULL AND scope_id IS NULL DO NOTHING`
		tag, err = r.db(ctx).Exec(ctx, query, idgen.New(), userID, roleID, scopeType)
	} else {
		query = `INSERT INTO user_roles (id, user_id, role_id, scope_type, scope_id)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (user_id, role_id, scope_type, scope_id)
			WHERE deleted_at IS NULL DO NOTHING`
		tag, err = r.db(ctx).Exec(ctx, query, idgen.New(), userID, roleID, scopeType, scopeID)
	}
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func (r *PgRoleRepository) ReplaceRoleScopes(ctx context.Context, userID string, roleID int64, scopeType string, scopeIDs []int64) (bool, error) {
	uniqueScopeIDs := uniquePositiveIDs(scopeIDs)
	changed := false
	err := r.withTransaction(ctx, func(db transaction.Executor) error {
		tag, err := db.Exec(ctx, `UPDATE user_roles
			SET deleted_at = NOW()
			WHERE user_id = $1
			  AND role_id = $2
			  AND scope_type = $3
			  AND scope_id IS NOT NULL
			  AND deleted_at IS NULL
			  AND NOT (scope_id = ANY($4::bigint[]))`, userID, roleID, scopeType, uniqueScopeIDs)
		if err != nil {
			return err
		}
		changed = tag.RowsAffected() > 0
		for _, scopeID := range uniqueScopeIDs {
			insertTag, err := db.Exec(ctx, `INSERT INTO user_roles (id, user_id, role_id, scope_type, scope_id)
				VALUES ($1, $2, $3, $4, $5)
				ON CONFLICT (user_id, role_id, scope_type, scope_id)
				WHERE deleted_at IS NULL DO NOTHING`, idgen.New(), userID, roleID, scopeType, scopeID)
			if err != nil {
				return err
			}
			changed = changed || insertTag.RowsAffected() > 0
		}
		return nil
	})
	if err != nil {
		return false, err
	}
	return changed, nil
}

func (r *PgRoleRepository) RevokeRole(ctx context.Context, userID string, roleID int64) (bool, error) {
	query := `UPDATE user_roles
		SET deleted_at = NOW()
		WHERE user_id = $1
		  AND role_id = $2
		  AND deleted_at IS NULL`
	tag, err := r.db(ctx).Exec(ctx, query, userID, roleID)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func (r *PgRoleRepository) RevokeRoleUnlessLastGlobal(ctx context.Context, userID string, roleID int64) (bool, error) {
	revoked := false
	err := r.withTransaction(ctx, func(db transaction.Executor) error {
		// Serialize protected-role revocations per role. A row lock alone cannot
		// protect the aggregate when two administrators revoke each other.
		if _, err := db.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, roleID); err != nil {
			return err
		}
		var assigned bool
		if err := db.QueryRow(ctx, `SELECT EXISTS (
			SELECT 1 FROM user_roles
			WHERE user_id=$1 AND role_id=$2 AND scope_type='global' AND scope_id IS NULL AND deleted_at IS NULL
		)`, userID, roleID).Scan(&assigned); err != nil {
			return err
		}
		if !assigned {
			return nil
		}
		var targetActive bool
		if err := db.QueryRow(ctx, `SELECT EXISTS (
			SELECT 1 FROM identity_admin_accounts aa
			INNER JOIN users u ON u.id=aa.user_id AND u.deleted_at IS NULL AND u.status='active'
			INNER JOIN accounts a ON a.id=aa.credential_account_id AND a.user_id=aa.user_id AND a.deleted_at IS NULL
			WHERE aa.user_id=$1 AND aa.status='active'
		)`, userID).Scan(&targetActive); err != nil {
			return err
		}
		var count int
		if err := db.QueryRow(ctx, `SELECT count(*) FROM user_roles ur
			INNER JOIN identity_admin_accounts aa ON aa.user_id=ur.user_id AND aa.status='active'
			INNER JOIN users u ON u.id=aa.user_id AND u.deleted_at IS NULL AND u.status='active'
			INNER JOIN accounts a ON a.id=aa.credential_account_id AND a.user_id=aa.user_id AND a.deleted_at IS NULL
			WHERE ur.role_id=$1 AND ur.scope_type='global' AND ur.scope_id IS NULL AND ur.deleted_at IS NULL`, roleID).Scan(&count); err != nil {
			return err
		}
		if targetActive && count <= 1 {
			return ErrLastGlobalRoleAssignment
		}
		tag, err := db.Exec(ctx, `UPDATE user_roles SET deleted_at=NOW()
			WHERE user_id=$1 AND role_id=$2 AND scope_type='global' AND scope_id IS NULL AND deleted_at IS NULL`, userID, roleID)
		if err != nil {
			return err
		}
		revoked = tag.RowsAffected() > 0
		return nil
	})
	return revoked, err
}

func (r *PgRoleRepository) HasPermission(ctx context.Context, userID string, resource, action string) (bool, error) {
	query := `SELECT EXISTS (
		SELECT 1
		FROM role_permissions rp
		INNER JOIN permission_definitions pd ON pd.id = rp.permission_id AND pd.deprecated_at IS NULL
		INNER JOIN roles r ON r.id = rp.role_id AND r.deleted_at IS NULL
		WHERE pd.resource = $2
		  AND pd.action = $3
		  AND rp.deleted_at IS NULL
		  AND (
				(r.name = 'member' AND EXISTS (
					SELECT 1 FROM users u WHERE u.id = $1 AND u.deleted_at IS NULL
				))
				OR EXISTS (
					SELECT 1 FROM user_roles ur
					WHERE ur.user_id = $1
					  AND ur.role_id = r.id
					  AND ur.scope_type = 'global'
						  AND ur.scope_id IS NULL
						  AND ur.deleted_at IS NULL
						  AND (r.name <> 'admin' OR EXISTS (
							SELECT 1 FROM identity_admin_accounts aa
							WHERE aa.user_id=ur.user_id AND aa.status='active'
						  ))
					)
			  )
	)`
	var allowed bool
	err := r.db(ctx).QueryRow(ctx, query, userID, resource, action).Scan(&allowed)
	if err != nil {
		return false, err
	}
	return allowed, nil
}

func (r *PgRoleRepository) HasScopedPermission(ctx context.Context, userID string, resource, action, scopeType string, scopeID int64) (bool, error) {
	query := `SELECT EXISTS (
		SELECT 1
		FROM role_permissions rp
		INNER JOIN permission_definitions pd ON pd.id = rp.permission_id AND pd.deprecated_at IS NULL
		INNER JOIN roles r ON r.id = rp.role_id AND r.deleted_at IS NULL
		INNER JOIN user_roles ur ON ur.role_id = r.id AND ur.deleted_at IS NULL
		WHERE ur.user_id = $1
		  AND pd.resource = $2
		  AND pd.action = $3
		  AND rp.deleted_at IS NULL
			  AND (
					(ur.scope_type = 'global' AND ur.scope_id IS NULL)
					OR (ur.scope_type = $4 AND ur.scope_id = $5)
				  )
			  AND (r.name <> 'admin' OR EXISTS (
				SELECT 1 FROM identity_admin_accounts aa
				WHERE aa.user_id=ur.user_id AND aa.status='active'
			  ))
	)`
	var allowed bool
	if err := r.db(ctx).QueryRow(ctx, query, userID, resource, action, scopeType, scopeID).Scan(&allowed); err != nil {
		return false, err
	}
	return allowed, nil
}

func (r *PgRoleRepository) ListRoles(ctx context.Context) ([]*Role, error) {
	query := `SELECT id, name, description, is_system FROM roles WHERE deleted_at IS NULL ORDER BY id ASC`
	rows, err := r.db(ctx).Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var roles []*Role
	for rows.Next() {
		role := &Role{}
		if err := rows.Scan(&role.ID, &role.Name, &role.Description, &role.IsSystem); err != nil {
			return nil, err
		}
		roles = append(roles, role)
	}
	return roles, nil
}

func (r *PgRoleRepository) ListPermissionDefinitions(ctx context.Context) ([]PermissionDefinition, error) {
	rows, err := r.db(ctx).Query(ctx, `SELECT id, code, domain, resource, action, description, risk_level, allowed_scope_types, audit_level, deprecated_at, created_at, updated_at
		FROM permission_definitions ORDER BY domain, resource, action, code`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]PermissionDefinition, 0)
	for rows.Next() {
		item, scanErr := scanPermissionDefinition(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PgRoleRepository) ListRolePermissions(ctx context.Context, roleID int64) ([]RolePermission, error) {
	rows, err := r.db(ctx).Query(ctx, `SELECT rp.role_id, r.name, pd.id, pd.code, pd.domain, pd.resource, pd.action, pd.description, pd.risk_level, pd.allowed_scope_types, pd.audit_level, pd.deprecated_at, pd.created_at, pd.updated_at, rp.created_by, rp.created_at
		FROM role_permissions rp
		INNER JOIN roles r ON r.id = rp.role_id AND r.deleted_at IS NULL
		INNER JOIN permission_definitions pd ON pd.id = rp.permission_id
		WHERE rp.deleted_at IS NULL AND ($1 = 0 OR rp.role_id = $1)
		ORDER BY r.name, pd.code`, roleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]RolePermission, 0)
	for rows.Next() {
		var item RolePermission
		var scopes []byte
		if err := rows.Scan(&item.RoleID, &item.RoleName, &item.Permission.ID, &item.Permission.Code, &item.Permission.Domain, &item.Permission.Resource, &item.Permission.Action, &item.Permission.Description, &item.Permission.RiskLevel, &scopes, &item.Permission.AuditLevel, &item.Permission.DeprecatedAt, &item.Permission.CreatedAt, &item.Permission.UpdatedAt, &item.CreatedBy, &item.CreatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(scopes, &item.Permission.AllowedScopeTypes); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PgRoleRepository) ReplaceRolePermissions(ctx context.Context, roleID int64, codes []string, actorID string) error {
	if roleID <= 0 {
		return ErrRoleNotFound
	}
	return r.withTransaction(ctx, func(db transaction.Executor) error {
		if _, err := db.Exec(ctx, `UPDATE role_permissions SET deleted_at=NOW() WHERE role_id=$1 AND deleted_at IS NULL`, roleID); err != nil {
			return err
		}
		for _, code := range uniqueCodes(codes) {
			var permissionID int64
			if err := db.QueryRow(ctx, `SELECT id FROM permission_definitions WHERE code=$1 AND deprecated_at IS NULL`, code).Scan(&permissionID); err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return fmt.Errorf("permission definition %q: %w", code, ErrRoleNotFound)
				}
				return err
			}
			if _, err := db.Exec(ctx, `INSERT INTO role_permissions (id, role_id, permission_id, created_by, created_at, deleted_at)
				VALUES ($1,$2,$3,$4,NOW(),NULL)
				ON CONFLICT (role_id, permission_id) WHERE deleted_at IS NULL DO UPDATE SET created_by=EXCLUDED.created_by, created_at=EXCLUDED.created_at, deleted_at=NULL`, idgen.New(), roleID, permissionID, actorID); err != nil {
				return err
			}
		}
		return nil
	})
}

func (r *PgRoleRepository) CreateCustomRole(ctx context.Context, role Role) (*Role, error) {
	role.Name = strings.ToLower(strings.TrimSpace(role.Name))
	role.Description = strings.TrimSpace(role.Description)
	if role.Name == "" {
		return nil, ErrRoleNotFound
	}
	role.ID = idgen.New()
	role.IsSystem = false
	created := &Role{}
	err := r.db(ctx).QueryRow(ctx, `INSERT INTO roles (id, name, description, is_system, created_at, updated_at)
		VALUES ($1,$2,$3,FALSE,NOW(),NOW())
		RETURNING id,name,description,is_system`, role.ID, role.Name, role.Description).Scan(&created.ID, &created.Name, &created.Description, &created.IsSystem)
	if err != nil {
		return nil, err
	}
	return created, nil
}

func (r *PgRoleRepository) UpdateCustomRole(ctx context.Context, role Role) (*Role, error) {
	if role.ID <= 0 {
		return nil, ErrRoleNotFound
	}
	updated := &Role{}
	err := r.db(ctx).QueryRow(ctx, `UPDATE roles SET description=$2, updated_at=NOW()
		WHERE id=$1 AND is_system=FALSE AND deleted_at IS NULL
		RETURNING id,name,description,is_system`, role.ID, strings.TrimSpace(role.Description)).Scan(&updated.ID, &updated.Name, &updated.Description, &updated.IsSystem)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrRoleNotFound
	}
	if err != nil {
		return nil, err
	}
	return updated, nil
}

func (r *PgRoleRepository) HasPermissionCode(ctx context.Context, userID, code string) (bool, error) {
	var allowed bool
	err := r.db(ctx).QueryRow(ctx, `SELECT EXISTS (
		SELECT 1
		FROM role_permissions rp
		INNER JOIN permission_definitions pd ON pd.id=rp.permission_id AND pd.deprecated_at IS NULL
		INNER JOIN roles r ON r.id=rp.role_id AND r.deleted_at IS NULL
		WHERE rp.deleted_at IS NULL AND pd.code=$2 AND (
			(r.name='member' AND EXISTS (SELECT 1 FROM users u WHERE u.id=$1 AND u.deleted_at IS NULL))
				OR EXISTS (SELECT 1 FROM user_roles ur
					WHERE ur.user_id=$1 AND ur.role_id=r.id AND ur.scope_type='global' AND ur.scope_id IS NULL AND ur.deleted_at IS NULL
					  AND (r.name<>'admin' OR EXISTS (
						SELECT 1 FROM identity_admin_accounts aa WHERE aa.user_id=ur.user_id AND aa.status='active'
					  )))
		)
	)`, userID, code).Scan(&allowed)
	return allowed, err
}

func (r *PgRoleRepository) HasScopedPermissionCode(ctx context.Context, userID, code, scopeType string, scopeID int64) (bool, error) {
	var allowed bool
	err := r.db(ctx).QueryRow(ctx, `SELECT EXISTS (
		SELECT 1
		FROM role_permissions rp
		INNER JOIN permission_definitions pd ON pd.id=rp.permission_id AND pd.deprecated_at IS NULL
		INNER JOIN roles r ON r.id=rp.role_id AND r.deleted_at IS NULL
		INNER JOIN user_roles ur ON ur.role_id=r.id AND ur.deleted_at IS NULL
			WHERE rp.deleted_at IS NULL AND ur.user_id=$1 AND pd.code=$2 AND (
				(ur.scope_type='global' AND ur.scope_id IS NULL)
				OR (ur.scope_type=$3 AND ur.scope_id=$4)
			)
			AND (r.name<>'admin' OR EXISTS (
				SELECT 1 FROM identity_admin_accounts aa WHERE aa.user_id=ur.user_id AND aa.status='active'
			))
	)`, userID, code, scopeType, scopeID).Scan(&allowed)
	return allowed, err
}

// HasAnyScopedPermissionCode is used only by the HTTP route gate when the
// actual resource scope is not available until the application service loads
// it. The service must still call HasScopedPermissionCode with the stored
// category ID before it changes content.
func (r *PgRoleRepository) HasAnyScopedPermissionCode(ctx context.Context, userID, code, scopeType string) (bool, error) {
	var allowed bool
	err := r.db(ctx).QueryRow(ctx, `SELECT EXISTS (
		SELECT 1
		FROM role_permissions rp
		INNER JOIN permission_definitions pd ON pd.id=rp.permission_id AND pd.deprecated_at IS NULL
		INNER JOIN roles r ON r.id=rp.role_id AND r.deleted_at IS NULL
		INNER JOIN user_roles ur ON ur.role_id=r.id AND ur.deleted_at IS NULL
			WHERE rp.deleted_at IS NULL AND ur.user_id=$1 AND pd.code=$2 AND (
				(ur.scope_type='global' AND ur.scope_id IS NULL)
				OR (ur.scope_type=$3 AND ur.scope_id IS NOT NULL)
			)
			AND (r.name<>'admin' OR EXISTS (
				SELECT 1 FROM identity_admin_accounts aa WHERE aa.user_id=ur.user_id AND aa.status='active'
			))
	)`, userID, code, scopeType).Scan(&allowed)
	return allowed, err
}

func (r *PgRoleRepository) SyncRouteOperations(ctx context.Context, operations []RouteOperation) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	for _, operation := range operations {
		aliases, marshalErr := json.Marshal(uniqueStrings(operation.LegacyAliases))
		if marshalErr != nil {
			return marshalErr
		}
		var operationID int64
		err := tx.QueryRow(ctx, `INSERT INTO route_operations (id,operation_code,module_owner,method,path_template,audience,legacy_aliases,updated_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7::jsonb,NOW())
			ON CONFLICT (operation_code) DO UPDATE SET module_owner=EXCLUDED.module_owner,method=EXCLUDED.method,path_template=EXCLUDED.path_template,audience=EXCLUDED.audience,legacy_aliases=EXCLUDED.legacy_aliases,updated_at=NOW()
			RETURNING id`, idgen.New(), operation.OperationCode, operation.ModuleOwner, operation.Method, operation.PathTemplate, operation.Audience, string(aliases)).Scan(&operationID)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE route_permission_bindings SET deleted_at=NOW() WHERE route_operation_id=$1 AND deleted_at IS NULL`, operationID); err != nil {
			return err
		}
		if operation.PermissionCode == "" {
			continue
		}
		var permissionID int64
		if err := tx.QueryRow(ctx, `SELECT id FROM permission_definitions WHERE code=$1 AND deprecated_at IS NULL`, operation.PermissionCode).Scan(&permissionID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO route_permission_bindings (id,route_operation_id,permission_id,created_at,deleted_at)
			VALUES ($1,$2,$3,NOW(),NULL)
			ON CONFLICT (route_operation_id,permission_id) WHERE deleted_at IS NULL DO UPDATE SET deleted_at=NULL`, idgen.New(), operationID, permissionID); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (r *PgRoleRepository) ListRouteOperations(ctx context.Context) ([]RouteOperation, error) {
	rows, err := r.db(ctx).Query(ctx, `SELECT ro.id,ro.operation_code,ro.module_owner,ro.method,ro.path_template,ro.audience,COALESCE(pd.code,''),ro.legacy_aliases,ro.updated_at
		FROM route_operations ro
		LEFT JOIN route_permission_bindings rpb ON rpb.route_operation_id=ro.id AND rpb.deleted_at IS NULL
		LEFT JOIN permission_definitions pd ON pd.id=rpb.permission_id
		ORDER BY ro.operation_code`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]RouteOperation, 0)
	for rows.Next() {
		var item RouteOperation
		var aliases []byte
		if err := rows.Scan(&item.ID, &item.OperationCode, &item.ModuleOwner, &item.Method, &item.PathTemplate, &item.Audience, &item.PermissionCode, &aliases, &item.UpdatedAt); err != nil {
			return nil, err
		}
		if len(aliases) > 0 && string(aliases) != "null" {
			if err := json.Unmarshal(aliases, &item.LegacyAliases); err != nil {
				return nil, err
			}
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PgRoleRepository) RecordAuthorizationAudit(ctx context.Context, audit AuthorizationAudit) error {
	if audit.CreatedAt.IsZero() {
		audit.CreatedAt = time.Now().UTC()
	}
	_, err := r.db(ctx).Exec(ctx, `INSERT INTO authorization_audits (id,request_id,actor_id,permission_code,operation_code,scope_type,scope_id,resource_type,resource_id,outcome,reason,ip_address,created_at)
		VALUES ($1,$2,NULLIF($3,'')::bigint,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`, idgen.New(), audit.RequestID, audit.ActorID, audit.PermissionCode, audit.OperationCode, audit.ScopeType, audit.ScopeID, audit.ResourceType, audit.ResourceID, audit.Outcome, audit.Reason, audit.IPAddress, audit.CreatedAt)
	return err
}

func (r *PgRoleRepository) ListAuthorizationAudits(ctx context.Context, limit int) ([]AuthorizationAudit, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := r.db(ctx).Query(ctx, `SELECT id,request_id,COALESCE(actor_id::text,''),permission_code,operation_code,scope_type,scope_id,resource_type,resource_id,outcome,reason,ip_address,created_at
		FROM authorization_audits ORDER BY created_at DESC,id DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]AuthorizationAudit, 0)
	for rows.Next() {
		item := AuthorizationAudit{}
		if err := rows.Scan(&item.ID, &item.RequestID, &item.ActorID, &item.PermissionCode, &item.OperationCode, &item.ScopeType, &item.ScopeID, &item.ResourceType, &item.ResourceID, &item.Outcome, &item.Reason, &item.IPAddress, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PgRoleRepository) CountGlobalRoleAssignments(ctx context.Context, roleID int64) (int, error) {
	var count int
	err := r.db(ctx).QueryRow(ctx, `SELECT count(*) FROM user_roles WHERE role_id=$1 AND scope_type='global' AND scope_id IS NULL AND deleted_at IS NULL`, roleID).Scan(&count)
	return count, err
}

func scanPermissionDefinition(row interface{ Scan(...interface{}) error }) (PermissionDefinition, error) {
	item := PermissionDefinition{}
	var scopes []byte
	if err := row.Scan(&item.ID, &item.Code, &item.Domain, &item.Resource, &item.Action, &item.Description, &item.RiskLevel, &scopes, &item.AuditLevel, &item.DeprecatedAt, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return PermissionDefinition{}, err
	}
	if err := json.Unmarshal(scopes, &item.AllowedScopeTypes); err != nil {
		return PermissionDefinition{}, err
	}
	return item, nil
}

// MemoryRoleRepository 内存角色仓储
type MemoryRoleRepository struct {
	mu                  sync.RWMutex
	roles               map[string]*Role
	userRoles           map[string][]*UserRoleAssignment
	definitions         map[string]*PermissionDefinition
	rolePermissions     map[int64]map[string]bool
	routeOperations     map[string]RouteOperation
	authorizationAudits []AuthorizationAudit
}

type memoryRoleSnapshot struct {
	Roles               map[string]*Role                 `json:"roles"`
	UserRoles           map[string][]*UserRoleAssignment `json:"user_roles"`
	Definitions         map[string]*PermissionDefinition `json:"definitions"`
	RolePermissions     map[int64]map[string]bool        `json:"role_permissions"`
	RouteOperations     map[string]RouteOperation        `json:"route_operations"`
	AuthorizationAudits []AuthorizationAudit             `json:"authorization_audits"`
}

func NewMemoryRoleRepository() *MemoryRoleRepository {
	repo := &MemoryRoleRepository{
		roles:           make(map[string]*Role),
		userRoles:       make(map[string][]*UserRoleAssignment),
		definitions:     make(map[string]*PermissionDefinition),
		rolePermissions: make(map[int64]map[string]bool),
		routeOperations: make(map[string]RouteOperation),
	}
	// 预设内置角色
	repo.roles["admin"] = &Role{ID: 1, Name: "admin", Description: "系统管理员", IsSystem: true}
	repo.roles["moderator"] = &Role{ID: 2, Name: "moderator", Description: "版主", IsSystem: true}
	repo.roles["member"] = &Role{ID: 3, Name: "member", Description: "普通会员", IsSystem: true}
	repo.roles["guest"] = &Role{ID: 4, Name: "guest", Description: "未登录用户", IsSystem: true}
	repo.seedAuthorizationCatalog()
	return repo
}

func (r *MemoryRoleRepository) Snapshot() any {
	r.mu.RLock()
	defer r.mu.RUnlock()
	payload, err := json.Marshal(memoryRoleSnapshot{
		Roles:               r.roles,
		UserRoles:           r.userRoles,
		Definitions:         r.definitions,
		RolePermissions:     r.rolePermissions,
		RouteOperations:     r.routeOperations,
		AuthorizationAudits: r.authorizationAudits,
	})
	if err != nil {
		return []byte(nil)
	}
	return append([]byte(nil), payload...)
}

func (r *MemoryRoleRepository) Restore(value any) {
	payload, ok := value.([]byte)
	if !ok || len(payload) == 0 {
		return
	}
	state := memoryRoleSnapshot{}
	if err := json.Unmarshal(payload, &state); err != nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.roles = state.Roles
	r.userRoles = state.UserRoles
	r.definitions = state.Definitions
	r.rolePermissions = state.RolePermissions
	r.routeOperations = state.RouteOperations
	r.authorizationAudits = state.AuthorizationAudits
}

func (r *MemoryRoleRepository) GetRoleByName(_ context.Context, name string) (*Role, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	role, ok := r.roles[name]
	if !ok {
		return nil, ErrRoleNotFound
	}
	return role, nil
}

func (r *MemoryRoleRepository) GetRoleByID(_ context.Context, id int64) (*Role, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, role := range r.roles {
		if role.ID == id {
			return role, nil
		}
	}
	return nil, ErrRoleNotFound
}

func (r *MemoryRoleRepository) GetUserRoles(_ context.Context, userID string) ([]*Role, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	roles := make([]*Role, 0, len(r.userRoles[userID])+1)
	roles = append(roles, r.roles["member"])
	seen := map[int64]bool{r.roles["member"].ID: true}
	for _, assignment := range r.userRoles[userID] {
		role := r.roleByID(assignment.RoleID)
		if role != nil && role.Name != "member" && !seen[role.ID] {
			roles = append(roles, role)
			seen[role.ID] = true
		}
	}
	sort.Slice(roles, func(i, j int) bool { return roles[i].ID < roles[j].ID })
	return roles, nil
}

func (r *MemoryRoleRepository) ListRoleAssignments(_ context.Context, userID string, roleID int64) ([]*UserRoleAssignment, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	assignments := make([]*UserRoleAssignment, 0)
	for assignmentUserID, userAssignments := range r.userRoles {
		if userID != "" && userID != assignmentUserID {
			continue
		}
		for _, assignment := range userAssignments {
			if roleID != 0 && roleID != assignment.RoleID {
				continue
			}
			copy := *assignment
			assignments = append(assignments, &copy)
		}
	}
	sort.Slice(assignments, func(i, j int) bool {
		if assignments[i].UserID == assignments[j].UserID {
			return scopeIDValue(assignments[i].ScopeID) < scopeIDValue(assignments[j].ScopeID)
		}
		return assignments[i].UserID < assignments[j].UserID
	})
	return assignments, nil
}

func (r *MemoryRoleRepository) AssignRole(_ context.Context, userID string, roleID int64, scopeType string, scopeID *int64) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	role := r.roleByID(roleID)
	if role == nil {
		return false, ErrRoleNotFound
	}
	for _, assigned := range r.userRoles[userID] {
		if assigned.RoleID == roleID && assigned.ScopeType == scopeType && equalScopeID(assigned.ScopeID, scopeID) {
			return false, nil
		}
	}
	assignment := &UserRoleAssignment{
		ID:        idgen.New(),
		UserID:    userID,
		RoleID:    roleID,
		RoleName:  role.Name,
		ScopeType: scopeType,
		ScopeID:   copyScopeID(scopeID),
	}
	r.userRoles[userID] = append(r.userRoles[userID], assignment)
	return true, nil
}

func (r *MemoryRoleRepository) ReplaceRoleScopes(_ context.Context, userID string, roleID int64, scopeType string, scopeIDs []int64) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	role := r.roleByID(roleID)
	if role == nil {
		return false, ErrRoleNotFound
	}
	wanted := make(map[int64]bool)
	for _, scopeID := range uniquePositiveIDs(scopeIDs) {
		wanted[scopeID] = true
	}
	changed := false
	kept := make([]*UserRoleAssignment, 0, len(r.userRoles[userID])+len(wanted))
	existing := make(map[int64]bool)
	for _, assignment := range r.userRoles[userID] {
		if assignment.RoleID == roleID && assignment.ScopeType == scopeType && assignment.ScopeID != nil {
			if wanted[*assignment.ScopeID] {
				kept = append(kept, assignment)
				existing[*assignment.ScopeID] = true
			} else {
				changed = true
			}
			continue
		}
		kept = append(kept, assignment)
	}
	for scopeID := range wanted {
		if existing[scopeID] {
			continue
		}
		scopeIDCopy := scopeID
		kept = append(kept, &UserRoleAssignment{
			ID: idgen.New(), UserID: userID, RoleID: roleID, RoleName: role.Name,
			ScopeType: scopeType, ScopeID: &scopeIDCopy,
		})
		changed = true
	}
	r.userRoles[userID] = kept
	return changed, nil
}

func (r *MemoryRoleRepository) RevokeRole(_ context.Context, userID string, roleID int64) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.revokeRoleLocked(userID, roleID), nil
}

func (r *MemoryRoleRepository) RevokeRoleUnlessLastGlobal(_ context.Context, userID string, roleID int64) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	assigned := false
	count := 0
	for _, assignments := range r.userRoles {
		for _, assignment := range assignments {
			if assignment.RoleID != roleID || assignment.ScopeType != "global" || assignment.ScopeID != nil {
				continue
			}
			count++
			if assignment.UserID == userID {
				assigned = true
			}
		}
	}
	if !assigned {
		return false, nil
	}
	if count <= 1 {
		return false, ErrLastGlobalRoleAssignment
	}
	return r.revokeRoleLocked(userID, roleID), nil
}

func (r *MemoryRoleRepository) revokeRoleLocked(userID string, roleID int64) bool {
	assignments := r.userRoles[userID]
	kept := assignments[:0]
	revoked := false
	for _, assignment := range assignments {
		if assignment.RoleID == roleID {
			revoked = true
			continue
		}
		kept = append(kept, assignment)
	}
	r.userRoles[userID] = kept
	return revoked
}

// 简化的权限表（内存模式）
var memoryPermissions = map[string]map[string]bool{
	"admin": {
		"user:read": true, "user:write": true, "user:delete": true, "user:suspend": true,
		"thread:read": true, "thread:write": true, "thread:delete": true, "thread:pin": true, "thread:lock": true,
		"post:read": true, "post:write": true, "post:delete": true,
		"category:read": true, "category:write": true, "category:delete": true,
		"role:manage": true, "role:read": true, "role:assign": true, "role:revoke": true,
		"feature:read": true, "feature:configure": true, "feature:lifecycle": true,
	},
	"moderator": {
		"user:read":   true,
		"thread:read": true, "thread:pin": true, "thread:lock": true,
		"post:read": true, "post:delete": true,
	},
	"member": {
		"thread:read": true, "thread:write": true,
		"post:read": true, "post:write": true,
	},
	"guest": {
		"thread:read": true, "post:read": true, "category:read": true,
	},
}

func (r *MemoryRoleRepository) HasPermission(_ context.Context, userID string, resource, action string) (bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	permissionKey := resource + ":" + action
	if memoryPermissions["member"][permissionKey] {
		return true, nil
	}
	for _, assignment := range r.userRoles[userID] {
		if assignment.ScopeType != "global" || assignment.ScopeID != nil {
			continue
		}
		role := r.roleByID(assignment.RoleID)
		if role == nil {
			continue
		}
		perms := memoryPermissions[role.Name]
		if perms[permissionKey] {
			return true, nil
		}
	}
	return false, nil
}

func (r *MemoryRoleRepository) HasScopedPermission(_ context.Context, userID string, resource, action, scopeType string, scopeID int64) (bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	permissionKey := resource + ":" + action
	for _, assignment := range r.userRoles[userID] {
		global := assignment.ScopeType == "global" && assignment.ScopeID == nil
		matchingScope := assignment.ScopeType == scopeType && assignment.ScopeID != nil && *assignment.ScopeID == scopeID
		if !global && !matchingScope {
			continue
		}
		role := r.roleByID(assignment.RoleID)
		if role != nil && memoryPermissions[role.Name][permissionKey] {
			return true, nil
		}
	}
	return false, nil
}

func (r *MemoryRoleRepository) ListRoles(_ context.Context) ([]*Role, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var roles []*Role
	for _, role := range r.roles {
		roles = append(roles, role)
	}
	sort.Slice(roles, func(i, j int) bool { return roles[i].ID < roles[j].ID })
	return roles, nil
}

func (r *MemoryRoleRepository) seedAuthorizationCatalog() {
	now := time.Now().UTC()
	for roleName, permissions := range memoryPermissions {
		role := r.roles[roleName]
		if role == nil {
			continue
		}
		if r.rolePermissions[role.ID] == nil {
			r.rolePermissions[role.ID] = make(map[string]bool)
		}
		for legacy := range permissions {
			parts := strings.SplitN(legacy, ":", 2)
			if len(parts) != 2 {
				continue
			}
			code := permissioncode.FromLegacy(parts[0], parts[1])
			r.ensureMemoryDefinition(code, parts[0], parts[1], now)
			r.rolePermissions[role.ID][code] = true
		}
	}
	for _, definition := range []struct {
		code     string
		resource string
		action   string
		roles    []string
	}{
		{"community.thread.take_down", "thread", "delete", []string{"admin", "moderator"}},
		{"community.thread.review", "thread", "write", []string{"admin"}},
		{"community.thread.direct_restore", "thread", "delete", []string{"admin"}},
		{"community.thread.restore", "thread", "delete", []string{"admin"}},
		{"community.thread.purge", "thread", "delete", []string{"admin"}},
		{"community.thread.trash", "thread", "delete", []string{"admin"}},
		{"identity.role.create", "role", "manage", []string{"admin"}},
		{"identity.role.update_permissions", "role", "manage", []string{"admin"}},
		{"identity.role.read_audit", "role", "read", []string{"admin"}},
		{"identity.account.recovery.override", "user", "suspend", []string{"admin"}},
		{"identity.session.read", "user", "read", []string{"admin"}},
		{"identity.session.revoke", "user", "suspend", []string{"admin"}},
		{"identity.admin_account.read", "user", "read", []string{"admin"}},
		{"identity.admin_account.suspend", "user", "suspend", []string{"admin"}},
		{"identity.admin_account.restore", "user", "suspend", []string{"admin"}},
		{"identity.admin_account.read_audit", "user", "read", []string{"admin"}},
		{"identity.mfa_policy.read", "role", "read", []string{"admin"}},
		{"identity.mfa_policy.update", "role", "manage", []string{"admin"}},
		{"identity.challenge_policy.read", "metrics", "read", []string{"admin"}},
		{"identity.challenge_policy.update", "role", "manage", []string{"admin"}},
		{"platform.email_delivery.read", "metrics", "read", []string{"admin"}},
		{"platform.reliability.read", "metrics", "read", []string{"admin"}},
		{"platform.reliability.replay", "plugin", "configure", []string{"admin"}},
		{"platform.retention.preview", "metrics", "read", []string{"admin"}},
	} {
		r.ensureMemoryDefinition(definition.code, definition.resource, definition.action, now)
		for _, roleName := range definition.roles {
			if role := r.roles[roleName]; role != nil {
				if r.rolePermissions[role.ID] == nil {
					r.rolePermissions[role.ID] = make(map[string]bool)
				}
				r.rolePermissions[role.ID][definition.code] = true
			}
		}
	}
}

func (r *MemoryRoleRepository) ensureMemoryDefinition(code, resource, action string, now time.Time) {
	if _, exists := r.definitions[code]; exists {
		return
	}
	parts := strings.Split(code, ".")
	domain := "platform"
	if len(parts) > 0 {
		domain = parts[0]
	}
	r.definitions[code] = &PermissionDefinition{
		ID:                idgen.New(),
		Code:              code,
		Domain:            domain,
		Resource:          resource,
		Action:            action,
		Description:       code,
		RiskLevel:         riskForPermission(action),
		AllowedScopeTypes: []string{"global", "category"},
		AuditLevel:        auditForPermission(action),
		CreatedAt:         now,
		UpdatedAt:         now,
	}
}

func (r *MemoryRoleRepository) ListPermissionDefinitions(_ context.Context) ([]PermissionDefinition, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	items := make([]PermissionDefinition, 0, len(r.definitions))
	for _, definition := range r.definitions {
		copy := *definition
		copy.AllowedScopeTypes = append([]string(nil), definition.AllowedScopeTypes...)
		items = append(items, copy)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Code < items[j].Code })
	return items, nil
}

func (r *MemoryRoleRepository) ListRolePermissions(_ context.Context, roleID int64) ([]RolePermission, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	items := make([]RolePermission, 0)
	for _, role := range r.roles {
		if roleID != 0 && role.ID != roleID {
			continue
		}
		for code := range r.rolePermissions[role.ID] {
			definition := r.definitions[code]
			if definition == nil {
				continue
			}
			copy := *definition
			copy.AllowedScopeTypes = append([]string(nil), definition.AllowedScopeTypes...)
			items = append(items, RolePermission{RoleID: role.ID, RoleName: role.Name, Permission: copy, CreatedAt: copy.CreatedAt})
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].RoleName == items[j].RoleName {
			return items[i].Permission.Code < items[j].Permission.Code
		}
		return items[i].RoleName < items[j].RoleName
	})
	return items, nil
}

func (r *MemoryRoleRepository) ReplaceRolePermissions(_ context.Context, roleID int64, codes []string, _ string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.roleByID(roleID) == nil {
		return ErrRoleNotFound
	}
	updated := make(map[string]bool)
	for _, code := range uniqueCodes(codes) {
		if _, exists := r.definitions[code]; !exists {
			return fmt.Errorf("permission definition %q: %w", code, ErrRoleNotFound)
		}
		updated[code] = true
	}
	r.rolePermissions[roleID] = updated
	return nil
}

func (r *MemoryRoleRepository) CreateCustomRole(_ context.Context, role Role) (*Role, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	role.Name = strings.ToLower(strings.TrimSpace(role.Name))
	if role.Name == "" {
		return nil, ErrRoleNotFound
	}
	if _, exists := r.roles[role.Name]; exists {
		return nil, errors.New("role name already exists")
	}
	role.ID = idgen.New()
	role.Description = strings.TrimSpace(role.Description)
	role.IsSystem = false
	copy := role
	r.roles[role.Name] = &copy
	r.rolePermissions[role.ID] = make(map[string]bool)
	return &copy, nil
}

func (r *MemoryRoleRepository) UpdateCustomRole(_ context.Context, role Role) (*Role, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	current := r.roleByID(role.ID)
	if current == nil || current.IsSystem {
		return nil, ErrRoleNotFound
	}
	current.Description = strings.TrimSpace(role.Description)
	copy := *current
	return &copy, nil
}

func (r *MemoryRoleRepository) HasPermissionCode(_ context.Context, userID, code string) (bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.roleHasCodeLocked(r.roles["member"], code) {
		return true, nil
	}
	for _, assignment := range r.userRoles[userID] {
		if assignment.ScopeType != "global" || assignment.ScopeID != nil {
			continue
		}
		if r.roleHasCodeLocked(r.roleByID(assignment.RoleID), code) {
			return true, nil
		}
	}
	resource, action, ok := permissioncode.LegacyForCode(code)
	return ok && r.hasLegacyPermissionLocked(userID, resource, action, "", 0, false), nil
}

func (r *MemoryRoleRepository) HasScopedPermissionCode(_ context.Context, userID, code, scopeType string, scopeID int64) (bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, assignment := range r.userRoles[userID] {
		global := assignment.ScopeType == "global" && assignment.ScopeID == nil
		matchingScope := assignment.ScopeType == scopeType && assignment.ScopeID != nil && *assignment.ScopeID == scopeID
		if (global || matchingScope) && r.roleHasCodeLocked(r.roleByID(assignment.RoleID), code) {
			return true, nil
		}
	}
	resource, action, ok := permissioncode.LegacyForCode(code)
	return ok && r.hasLegacyPermissionLocked(userID, resource, action, scopeType, scopeID, true), nil
}

func (r *MemoryRoleRepository) HasAnyScopedPermissionCode(_ context.Context, userID, code, scopeType string) (bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, assignment := range r.userRoles[userID] {
		global := assignment.ScopeType == "global" && assignment.ScopeID == nil
		matchingScope := assignment.ScopeType == scopeType && assignment.ScopeID != nil
		if (global || matchingScope) && r.roleHasCodeLocked(r.roleByID(assignment.RoleID), code) {
			return true, nil
		}
	}
	resource, action, ok := permissioncode.LegacyForCode(code)
	if !ok {
		return false, nil
	}
	for _, assignment := range r.userRoles[userID] {
		if assignment.ScopeType != scopeType || assignment.ScopeID == nil {
			continue
		}
		if r.hasLegacyPermissionLocked(userID, resource, action, scopeType, *assignment.ScopeID, true) {
			return true, nil
		}
	}
	return false, nil
}

func (r *MemoryRoleRepository) SyncRouteOperations(_ context.Context, operations []RouteOperation) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, operation := range operations {
		if operation.OperationCode == "" {
			return errors.New("route operation code is required")
		}
		operation.ID = idgen.New()
		operation.LegacyAliases = uniqueStrings(operation.LegacyAliases)
		operation.UpdatedAt = time.Now().UTC()
		r.routeOperations[operation.OperationCode] = operation
	}
	return nil
}

func (r *MemoryRoleRepository) ListRouteOperations(_ context.Context) ([]RouteOperation, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	items := make([]RouteOperation, 0, len(r.routeOperations))
	for _, operation := range r.routeOperations {
		operation.LegacyAliases = append([]string(nil), operation.LegacyAliases...)
		items = append(items, operation)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].OperationCode < items[j].OperationCode })
	return items, nil
}

func (r *MemoryRoleRepository) RecordAuthorizationAudit(_ context.Context, audit AuthorizationAudit) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if audit.ID == 0 {
		audit.ID = idgen.New()
	}
	if audit.CreatedAt.IsZero() {
		audit.CreatedAt = time.Now().UTC()
	}
	r.authorizationAudits = append(r.authorizationAudits, audit)
	return nil
}

func (r *MemoryRoleRepository) ListAuthorizationAudits(_ context.Context, limit int) ([]AuthorizationAudit, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	items := append([]AuthorizationAudit(nil), r.authorizationAudits...)
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.After(items[j].CreatedAt) })
	if len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func (r *MemoryRoleRepository) CountGlobalRoleAssignments(_ context.Context, roleID int64) (int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	count := 0
	for _, assignments := range r.userRoles {
		for _, assignment := range assignments {
			if assignment.RoleID == roleID && assignment.ScopeType == "global" && assignment.ScopeID == nil {
				count++
			}
		}
	}
	return count, nil
}

func (r *MemoryRoleRepository) roleHasCodeLocked(role *Role, code string) bool {
	return role != nil && r.rolePermissions[role.ID][code]
}

func (r *MemoryRoleRepository) hasLegacyPermissionLocked(userID, resource, action, scopeType string, scopeID int64, scoped bool) bool {
	permissionKey := resource + ":" + action
	if !scoped && memoryPermissions["member"][permissionKey] {
		return true
	}
	for _, assignment := range r.userRoles[userID] {
		global := assignment.ScopeType == "global" && assignment.ScopeID == nil
		matchingScope := scoped && assignment.ScopeType == scopeType && assignment.ScopeID != nil && *assignment.ScopeID == scopeID
		if (!scoped && !global) || (scoped && !global && !matchingScope) {
			continue
		}
		role := r.roleByID(assignment.RoleID)
		if role != nil && memoryPermissions[role.Name][permissionKey] {
			return true
		}
	}
	return false
}

func riskForPermission(action string) string {
	switch action {
	case "delete", "purge", "take_down", "restore", "assign", "suspend", "execute", "install", "uninstall":
		return "high"
	case "write", "configure", "lifecycle", "lock", "pin":
		return "medium"
	default:
		return "low"
	}
}

func auditForPermission(action string) string {
	if riskForPermission(action) == "high" {
		return "required"
	}
	return "standard"
}

func (r *MemoryRoleRepository) roleByID(roleID int64) *Role {
	for _, role := range r.roles {
		if role.ID == roleID {
			return role
		}
	}
	return nil
}

func uniquePositiveIDs(ids []int64) []int64 {
	seen := make(map[int64]bool, len(ids))
	result := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id <= 0 || seen[id] {
			continue
		}
		seen[id] = true
		result = append(result, id)
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}

func equalScopeID(left, right *int64) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func copyScopeID(value *int64) *int64 {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func scopeIDValue(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

func uniqueCodes(values []string) []string {
	seen := make(map[string]bool, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if !permissioncode.IsCode(value) || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]bool, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}
