package ai

import (
	"context"
	"time"

	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/platform/transaction"
)

func (s *ManagedService) admin(ctx context.Context, code string, resolve identityport.SessionPrincipalResolver, lock bool) (identityport.AdminExecutionAuthority, error) {
	var zero identityport.AdminExecutionAuthority
	if s == nil || s.pool == nil || s.admins == nil {
		return zero, ErrManagedUnavailable
	}
	q := identityport.AdminExecutionQuery{PermissionCode: code, Scope: identityport.PolicyResourceRef{Kind: "ai.profile_inventory", ID: "all"}}
	a, e := s.admins.LoadAdminExecutionAuthority(ctx, q, resolve, lock)
	if e != nil {
		return zero, e
	}
	if code == "ai.ai.manage" && (!a.Allowed || a.ExpiresAtMS <= managedNow()) && (!a.Principal.SatisfiesAdminMFARequirement() || a.ExpiresAtMS <= managedNow()) {
		return zero, ErrManagedMFA
	}
	if identityport.CheckAdminExecutionAuthority(a, a) != nil || a.Query != q || !a.Allowed || a.ExpiresAtMS <= managedNow() {
		return zero, ErrManagedDenied
	}
	return a, nil
}
func (s *ManagedService) Inventory(ctx context.Context, resolve identityport.SessionPrincipalResolver) (ManagedInventory, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var out ManagedInventory
	a, e := s.admin(ctx, "ai.ai.read", resolve, false)
	if e != nil {
		return out, e
	}
	rows, e := s.pool.Query(ctx, `SELECT id FROM ai_model_profiles ORDER BY id LIMIT 3`)
	if e != nil {
		return out, ErrManagedUnavailable
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return out, ErrManagedUnavailable
		}
		ids = append(ids, id)
	}
	e = rows.Err()
	rows.Close()
	if e != nil || len(ids) > 2 {
		return out, ErrManagedUnavailable
	}
	out.Profiles = []ManagedProfile{}
	out.Budgets = []ManagedBudget{}
	out.Bindings = []ManagedBinding{}
	for _, id := range ids {
		p, e := s.profile(ctx, id, false)
		if e != nil {
			return ManagedInventory{}, ErrManagedUnavailable
		}
		out.Profiles = append(out.Profiles, p)
		b, e := s.budget(ctx, id, false)
		if e != nil {
			return ManagedInventory{}, ErrManagedUnavailable
		}
		out.Budgets = append(out.Budgets, b)
	}
	if len(ids) > 0 {
		b, e := s.binding(ctx, false)
		if e != nil {
			return ManagedInventory{}, ErrManagedUnavailable
		}
		out.Bindings = append(out.Bindings, b)
	}
	current, e := s.admin(ctx, "ai.ai.read", resolve, false)
	if e != nil || identityport.CheckAdminExecutionAuthority(a, current) != nil {
		return ManagedInventory{}, ErrManagedDenied
	}
	return out, nil
}
func (s *ManagedService) adminChange(ctx context.Context, resolve identityport.SessionPrincipalResolver, operation, id string, fn func(context.Context) error) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	a, e := s.admin(ctx, "ai.ai.manage", resolve, false)
	if e != nil {
		return e
	}
	e = s.command(ctx, operation, "admin", a.Principal.Actor.ID, id, func(tx context.Context) error {
		current, e := s.admin(tx, "ai.ai.manage", resolve, true)
		if e != nil || identityport.CheckAdminExecutionAuthority(a, current) != nil {
			return ErrManagedDenied
		}
		if e = fn(tx); e != nil {
			return e
		}
		current, e = s.admin(tx, "ai.ai.manage", resolve, true)
		if e != nil || identityport.CheckAdminExecutionAuthority(a, current) != nil {
			return ErrManagedDenied
		}
		return nil
	})
	if e == nil {
		s.invalidate()
	}
	return managedPublicError(e)
}
func (s *ManagedService) Bind(ctx context.Context, resolve identityport.SessionPrincipalResolver, profileID string, bindingVersion, profileVersion int64) error {
	if (profileID != "remote-chat" && profileID != "local-chat") || bindingVersion < 1 || profileVersion < 1 {
		return ErrManagedDenied
	}
	return s.adminChange(ctx, resolve, "binding.change", "chat", func(tx context.Context) error {
		if e := s.check(tx); e != nil {
			return e
		}
		b, e := s.binding(tx, true)
		if e != nil {
			return e
		}
		if b.Revision != bindingVersion {
			return ErrManagedConflict
		}
		p, e := s.profile(tx, profileID, true)
		if e != nil {
			return e
		}
		if p.Revision != profileVersion || !p.Enabled {
			return ErrManagedConflict
		}
		secret, e := s.secret(tx, p)
		clear(secret)
		if e != nil {
			return e
		}
		_, e = transaction.ExecutorFor(tx, s.pool).Exec(tx, `UPDATE ai_model_bindings SET profile_id=$1,revision=nextval('ai_gateway_epoch_seq') WHERE name='chat'`, profileID)
		return e
	})
}
func (s *ManagedService) SetProfileEnabled(ctx context.Context, resolve identityport.SessionPrincipalResolver, id string, version int64, enabled bool) error {
	if (id != "remote-chat" && id != "local-chat") || version < 1 {
		return ErrManagedDenied
	}
	return s.adminChange(ctx, resolve, "profile.state", id, func(tx context.Context) error {
		if _, e := s.binding(tx, true); e != nil {
			return e
		}
		p, e := s.profile(tx, id, true)
		if e != nil {
			return e
		}
		if p.Revision != version {
			return ErrManagedConflict
		}
		if enabled {
			if e = s.check(tx); e != nil {
				return e
			}
			secret, e := s.secret(tx, p)
			clear(secret)
			if e != nil {
				return e
			}
		}
		_, e = transaction.ExecutorFor(tx, s.pool).Exec(tx, `UPDATE ai_model_profiles SET enabled=$2,revision=nextval('ai_gateway_epoch_seq') WHERE id=$1`, id, enabled)
		return e
	})
}
