package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
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
	GetUserRoles(ctx context.Context, userID string) ([]*Role, error)
	AssignRole(ctx context.Context, userID string, roleID int64, scopeType string, scopeID *int64) error
	RevokeRole(ctx context.Context, userID string, roleID int64) error
	HasPermission(ctx context.Context, userID string, resource, action string) (bool, error)
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

func (r *PgRoleRepository) GetUserRoles(ctx context.Context, userID string) ([]*Role, error) {
	query := `SELECT r.id, r.name, r.description, r.is_system
		FROM roles r
		INNER JOIN user_roles ur ON r.id = ur.role_id AND ur.deleted_at IS NULL
		WHERE ur.user_id = $1 AND r.deleted_at IS NULL`
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

func (r *PgRoleRepository) AssignRole(ctx context.Context, userID string, roleID int64, scopeType string, scopeID *int64) error {
	query := `INSERT INTO user_roles (id, user_id, role_id, scope_type, scope_id) VALUES (nextval('user_roles_id_seq'), $1, $2, $3, $4)
		ON CONFLICT (user_id, role_id, scope_type, scope_id) WHERE deleted_at IS NULL DO NOTHING`
	_, err := r.pool.Exec(ctx, query, userID, roleID, scopeType, scopeID)
	return err
}

func (r *PgRoleRepository) RevokeRole(ctx context.Context, userID string, roleID int64) error {
	query := `UPDATE user_roles SET deleted_at = NOW() WHERE user_id = $1 AND role_id = $2 AND deleted_at IS NULL`
	_, err := r.pool.Exec(ctx, query, userID, roleID)
	return err
}

func (r *PgRoleRepository) HasPermission(ctx context.Context, userID string, resource, action string) (bool, error) {
	query := `SELECT COUNT(*) FROM permissions p
		INNER JOIN user_roles ur ON p.role_id = ur.role_id AND ur.deleted_at IS NULL
		INNER JOIN roles r ON r.id = ur.role_id AND r.deleted_at IS NULL
		WHERE ur.user_id = $1 AND p.resource = $2 AND p.action = $3 AND p.deleted_at IS NULL`
	var count int64
	err := r.pool.QueryRow(ctx, query, userID, resource, action).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
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
	roles     map[string]*Role
	userRoles map[string][]*Role // userID -> roles
}

func NewMemoryRoleRepository() *MemoryRoleRepository {
	repo := &MemoryRoleRepository{
		roles:     make(map[string]*Role),
		userRoles: make(map[string][]*Role),
	}
	// 预设内置角色
	repo.roles["admin"] = &Role{ID: 1, Name: "admin", Description: "系统管理员", IsSystem: true}
	repo.roles["moderator"] = &Role{ID: 2, Name: "moderator", Description: "版主", IsSystem: true}
	repo.roles["member"] = &Role{ID: 3, Name: "member", Description: "普通会员", IsSystem: true}
	repo.roles["guest"] = &Role{ID: 4, Name: "guest", Description: "未登录用户", IsSystem: true}
	return repo
}

func (r *MemoryRoleRepository) GetRoleByName(_ context.Context, name string) (*Role, error) {
	role, ok := r.roles[name]
	if !ok {
		return nil, ErrRoleNotFound
	}
	return role, nil
}

func (r *MemoryRoleRepository) GetUserRoles(_ context.Context, userID string) ([]*Role, error) {
	roles, ok := r.userRoles[userID]
	if !ok {
		// 默认返回 member 角色
		return []*Role{r.roles["member"]}, nil
	}
	return roles, nil
}

func (r *MemoryRoleRepository) AssignRole(_ context.Context, userID string, roleID int64, _ string, _ *int64) error {
	var role *Role
	for _, ro := range r.roles {
		if ro.ID == roleID {
			role = ro
			break
		}
	}
	if role == nil {
		return ErrRoleNotFound
	}
	r.userRoles[userID] = append(r.userRoles[userID], role)
	return nil
}

func (r *MemoryRoleRepository) RevokeRole(_ context.Context, userID string, roleID int64) error {
	roles := r.userRoles[userID]
	for i, role := range roles {
		if role.ID == roleID {
			r.userRoles[userID] = append(roles[:i], roles[i+1:]...)
			return nil
		}
	}
	return nil
}

// 简化的权限表（内存模式）
var memoryPermissions = map[string]map[string]bool{
	"admin": {
		"user:read": true, "user:write": true, "user:delete": true, "user:suspend": true,
		"thread:read": true, "thread:write": true, "thread:delete": true, "thread:pin": true,
		"post:read": true, "post:write": true, "post:delete": true,
		"category:read": true, "category:write": true, "category:delete": true,
		"role:manage": true,
	},
	"moderator": {
		"user:read": true, "user:suspend": true,
		"thread:read": true, "thread:write": true, "thread:delete": true, "thread:pin": true,
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
	roles, _ := r.userRoles[userID]
	if len(roles) == 0 {
		// 默认 member
		perms := memoryPermissions["member"]
		return perms[resource+":"+action], nil
	}
	for _, role := range roles {
		perms := memoryPermissions[role.Name]
		if perms[resource+":"+action] {
			return true, nil
		}
	}
	return false, nil
}

func (r *MemoryRoleRepository) ListRoles(_ context.Context) ([]*Role, error) {
	var roles []*Role
	for _, role := range r.roles {
		roles = append(roles, role)
	}
	return roles, nil
}
