package repository

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/campusos/CampusOS/internal/platform/transaction"
)

var ErrAdminExecutionFactsUnavailable = errors.New("admin execution facts are unavailable")

// AdminRoleExecutionFact is one complete actual role execution witness. It
// does not grant delegation, identify a resource, or authenticate a caller.
type AdminRoleExecutionFact struct {
	Binding                AdminDomainRoleBinding
	Role                   AdminDomainRole
	PermissionCode         string
	PermissionCreatedBy    string
	PermissionCreatedAt    time.Time
	PermissionFactsVersion int64
}

type AdminRoleExecutionFactsReader interface {
	LoadAdminRoleExecutionFacts(context.Context, string, bool) ([]AdminRoleExecutionFact, error)
}

func (r *PgAdminDomainRepository) LoadAdminRoleExecutionFacts(ctx context.Context, accountID string, lock bool) ([]AdminRoleExecutionFact, error) {
	if lock {
		if _, ok := transaction.FromContext(ctx); !ok {
			return nil, ErrAdminAuthorityLockUnavailable
		}
	}
	var managed bool
	if err := r.db(ctx).QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_trigger WHERE tgrelid='public.admin_role_permissions'::regclass AND tgname='trg_admin_permission_facts_version' AND NOT tgisinternal AND tgenabled IN('O','A') AND tgfoid=to_regprocedure('public.advance_admin_permission_facts_version()')) AND EXISTS(SELECT 1 FROM pg_sequence WHERE seqrelid=to_regclass('public.admin_permission_facts_version_seq') AND NOT seqcycle)`).Scan(&managed); err != nil {
		return nil, err
	}
	if !managed {
		return nil, ErrAdminExecutionFactsUnavailable
	}
	query := `SELECT b.id,b.account_id,b.role_id,b.granted_by,b.status,b.version,b.created_at,b.revoked_at,r.id,r.name,r.description,r.is_system,p.permission_code,p.created_by,p.created_at,p.facts_version FROM admin_role_bindings b JOIN admin_accounts a ON a.id=b.account_id JOIN admin_roles r ON r.id=b.role_id JOIN admin_role_permissions p ON p.role_id=r.id WHERE b.account_id=$1 AND b.status='active' AND a.status='active' AND a.revoked_at IS NULL ORDER BY b.id,p.permission_code`
	if lock {
		query += ` FOR UPDATE OF b,r,p`
	}
	rows, err := r.db(ctx).Query(ctx, query, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AdminRoleExecutionFact{}
	for rows.Next() {
		var f AdminRoleExecutionFact
		if err := rows.Scan(&f.Binding.ID, &f.Binding.AccountID, &f.Binding.RoleID, &f.Binding.GrantedBy, &f.Binding.Status, &f.Binding.Version, &f.Binding.CreatedAt, &f.Binding.RevokedAt, &f.Role.ID, &f.Role.Name, &f.Role.Description, &f.Role.IsSystem, &f.PermissionCode, &f.PermissionCreatedBy, &f.PermissionCreatedAt, &f.PermissionFactsVersion); err != nil {
			return nil, err
		}
		if f.PermissionFactsVersion < 1 || f.Binding.Version < 1 {
			return nil, ErrAdminExecutionFactsUnavailable
		}
		f.Binding.CreatedAt = f.Binding.CreatedAt.UTC()
		f.PermissionCreatedAt = f.PermissionCreatedAt.UTC()
		out = append(out, f)
	}
	return out, rows.Err()
}

type adminPermissionFact struct {
	CreatedBy string
	CreatedAt time.Time
	Version   int64
}

// ReplaceAdminRolePermissionFacts is a trusted repository write primitive;
// its caller owns catalogue validation and authorization. No HTTP/Host entry
// is added here. Unchanged rows retain their version; remove/re-add is fresh.
func (r *MemoryAdminDomainRepository) ReplaceAdminRolePermissionFacts(ctx context.Context, roleID string, codes []string, createdBy string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.roles[roleID]; !ok {
		return ErrAdminDomainRoleNotFound
	}
	next := map[string]bool{}
	for _, code := range codes {
		if strings.TrimSpace(code) != code || code == "" || len(code) > 160 {
			return ErrAdminDomainConflict
		}
		next[code] = true
	}
	if r.permissionFacts == nil {
		r.permissionFacts = map[string]map[string]adminPermissionFact{}
	}
	prior := r.permissionFacts[roleID]
	facts := map[string]adminPermissionFact{}
	for code := range next {
		f, ok := prior[code]
		if !ok || !r.rolePermissions[roleID][code] || f.CreatedBy != createdBy {
			r.permissionHighWater++
			f = adminPermissionFact{CreatedBy: createdBy, CreatedAt: time.Now().UTC().Truncate(time.Microsecond), Version: r.permissionHighWater}
		}
		facts[code] = f
	}
	r.rolePermissions[roleID], r.permissionFacts[roleID] = next, facts
	return nil
}

func (r *MemoryAdminDomainRepository) LoadAdminRoleExecutionFacts(ctx context.Context, accountID string, lock bool) ([]AdminRoleExecutionFact, error) {
	if lock && !transaction.Active(ctx) {
		return nil, ErrAdminAuthorityLockUnavailable
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := []AdminRoleExecutionFact{}
	a, ok := r.accounts[accountID]
	if !ok || a.Status != "active" || a.RevokedAt != nil {
		return out, nil
	}
	for _, b := range r.bindings {
		if b.AccountID != accountID || b.Status != "active" {
			continue
		}
		role, ok := r.roles[b.RoleID]
		if !ok {
			return nil, ErrAdminExecutionFactsUnavailable
		}
		for code, enabled := range r.rolePermissions[b.RoleID] {
			if !enabled {
				continue
			}
			f, ok := r.permissionFacts[b.RoleID][code]
			if !ok || f.Version < 1 || b.Version < 1 {
				return nil, ErrAdminExecutionFactsUnavailable
			}
			if b.RevokedAt != nil {
				return nil, ErrAdminExecutionFactsUnavailable
			}
			out = append(out, AdminRoleExecutionFact{Binding: b, Role: role, PermissionCode: code, PermissionCreatedBy: f.CreatedBy, PermissionCreatedAt: f.CreatedAt, PermissionFactsVersion: f.Version})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Binding.ID != out[j].Binding.ID {
			return out[i].Binding.ID < out[j].Binding.ID
		}
		return out[i].PermissionCode < out[j].PermissionCode
	})
	return out, nil
}
