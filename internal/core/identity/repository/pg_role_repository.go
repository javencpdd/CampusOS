package repository

import (
	"context"
	"errors"
	"sort"
	"sync"

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

func (r *PgRoleRepository) GetRoleByName(ctx context.Context, name string) (*Role, error) {
	query := `SELECT id, name, description, is_system FROM roles WHERE name = $1 AND deleted_at IS NULL`
	role := &Role{}
	err := r.pool.QueryRow(ctx, query, name).Scan(&role.ID, &role.Name, &role.Description, &role.IsSystem)
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
	err := r.pool.QueryRow(ctx, query, id).Scan(&role.ID, &role.Name, &role.Description, &role.IsSystem)
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
				)
			  )
		ORDER BY r.id ASC`
	rows, err := r.pool.Query(ctx, query, userID)
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
	rows, err := r.pool.Query(ctx, query, userID, roleID)
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
		tag, err = r.pool.Exec(ctx, query, idgen.New(), userID, roleID, scopeType)
	} else {
		query = `INSERT INTO user_roles (id, user_id, role_id, scope_type, scope_id)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (user_id, role_id, scope_type, scope_id)
			WHERE deleted_at IS NULL DO NOTHING`
		tag, err = r.pool.Exec(ctx, query, idgen.New(), userID, roleID, scopeType, scopeID)
	}
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func (r *PgRoleRepository) ReplaceRoleScopes(ctx context.Context, userID string, roleID int64, scopeType string, scopeIDs []int64) (bool, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)

	uniqueScopeIDs := uniquePositiveIDs(scopeIDs)
	tag, err := tx.Exec(ctx, `UPDATE user_roles
		SET deleted_at = NOW()
		WHERE user_id = $1
		  AND role_id = $2
		  AND scope_type = $3
		  AND scope_id IS NOT NULL
		  AND deleted_at IS NULL
		  AND NOT (scope_id = ANY($4::bigint[]))`, userID, roleID, scopeType, uniqueScopeIDs)
	if err != nil {
		return false, err
	}
	changed := tag.RowsAffected() > 0
	for _, scopeID := range uniqueScopeIDs {
		insertTag, err := tx.Exec(ctx, `INSERT INTO user_roles (id, user_id, role_id, scope_type, scope_id)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (user_id, role_id, scope_type, scope_id)
			WHERE deleted_at IS NULL DO NOTHING`, idgen.New(), userID, roleID, scopeType, scopeID)
		if err != nil {
			return false, err
		}
		changed = changed || insertTag.RowsAffected() > 0
	}
	if err := tx.Commit(ctx); err != nil {
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
	tag, err := r.pool.Exec(ctx, query, userID, roleID)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func (r *PgRoleRepository) HasPermission(ctx context.Context, userID string, resource, action string) (bool, error) {
	query := `SELECT EXISTS (
		SELECT 1
		FROM permissions p
		INNER JOIN roles r ON r.id = p.role_id AND r.deleted_at IS NULL
		WHERE p.resource = $2
		  AND p.action = $3
		  AND p.deleted_at IS NULL
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
				)
			  )
	)`
	var allowed bool
	err := r.pool.QueryRow(ctx, query, userID, resource, action).Scan(&allowed)
	if err != nil {
		return false, err
	}
	return allowed, nil
}

func (r *PgRoleRepository) HasScopedPermission(ctx context.Context, userID string, resource, action, scopeType string, scopeID int64) (bool, error) {
	query := `SELECT EXISTS (
		SELECT 1
		FROM permissions p
		INNER JOIN roles r ON r.id = p.role_id AND r.deleted_at IS NULL
		INNER JOIN user_roles ur ON ur.role_id = r.id AND ur.deleted_at IS NULL
		WHERE ur.user_id = $1
		  AND p.resource = $2
		  AND p.action = $3
		  AND p.deleted_at IS NULL
		  AND (
				(ur.scope_type = 'global' AND ur.scope_id IS NULL)
				OR (ur.scope_type = $4 AND ur.scope_id = $5)
			  )
	)`
	var allowed bool
	if err := r.pool.QueryRow(ctx, query, userID, resource, action, scopeType, scopeID).Scan(&allowed); err != nil {
		return false, err
	}
	return allowed, nil
}

func (r *PgRoleRepository) ListRoles(ctx context.Context) ([]*Role, error) {
	query := `SELECT id, name, description, is_system FROM roles WHERE deleted_at IS NULL ORDER BY id ASC`
	rows, err := r.pool.Query(ctx, query)
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

// MemoryRoleRepository 内存角色仓储
type MemoryRoleRepository struct {
	mu        sync.RWMutex
	roles     map[string]*Role
	userRoles map[string][]*UserRoleAssignment
}

func NewMemoryRoleRepository() *MemoryRoleRepository {
	repo := &MemoryRoleRepository{
		roles:     make(map[string]*Role),
		userRoles: make(map[string][]*UserRoleAssignment),
	}
	// 预设内置角色
	repo.roles["admin"] = &Role{ID: 1, Name: "admin", Description: "系统管理员", IsSystem: true}
	repo.roles["moderator"] = &Role{ID: 2, Name: "moderator", Description: "版主", IsSystem: true}
	repo.roles["member"] = &Role{ID: 3, Name: "member", Description: "普通会员", IsSystem: true}
	repo.roles["guest"] = &Role{ID: 4, Name: "guest", Description: "未登录用户", IsSystem: true}
	return repo
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
	return revoked, nil
}

// 简化的权限表（内存模式）
var memoryPermissions = map[string]map[string]bool{
	"admin": {
		"user:read": true, "user:write": true, "user:delete": true, "user:suspend": true,
		"thread:read": true, "thread:write": true, "thread:delete": true, "thread:pin": true, "thread:lock": true,
		"post:read": true, "post:write": true, "post:delete": true,
		"category:read": true, "category:write": true, "category:delete": true,
		"role:manage": true, "role:read": true, "role:assign": true, "role:revoke": true,
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
