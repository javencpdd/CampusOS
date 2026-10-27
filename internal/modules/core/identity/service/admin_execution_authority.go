package service

import (
	"context"
	"reflect"

	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	"github.com/campusos/CampusOS/internal/platform/transaction"
)

type adminExecutionBinding struct {
	Query   identityport.AdminExecutionQuery
	Session identityport.AdminSessionAuthority
	Roles   []repository.AdminRoleExecutionFact
}

// This method does not expand the frozen BoardGovernanceQuery/ResourceGrant
// contract. User board delegations and management bounds never enter it.
func (s *BoardGovernanceAuthorityService) LoadAdminExecutionAuthority(ctx context.Context, q identityport.AdminExecutionQuery, resolve identityport.SessionPrincipalResolver, lock bool) (identityport.AdminExecutionAuthority, error) {
	if err := q.Validate(); err != nil {
		return identityport.AdminExecutionAuthority{}, err
	}
	if s == nil || s.now == nil || nilGovernanceAdapter(s.admins) || nilGovernanceAdapter(s.adminSessions) || lock && !transaction.Active(ctx) {
		return identityport.AdminExecutionAuthority{}, identityport.ErrGovernanceAuthorityUnavailable
	}
	p, err := governancePrincipal(ctx, resolve)
	if err != nil {
		return identityport.AdminExecutionAuthority{}, err
	}
	if p.Actor.Kind != "admin" {
		return identityport.AdminExecutionAuthority{}, identityport.ErrAdminSessionPrincipalUnavailable
	}
	reader, ok := s.admins.(repository.AdminRoleExecutionFactsReader)
	if !ok || nilGovernanceAdapter(reader) {
		return identityport.AdminExecutionAuthority{}, identityport.ErrGovernanceAuthorityUnavailable
	}
	load := func() (adminExecutionBinding, error) {
		current, err := s.adminSessions.CurrentAdminSession(ctx, p.Actor.ID, p.CredentialID)
		if err != nil {
			return adminExecutionBinding{}, err
		}
		if current.AccountID != p.Actor.ID || current.SessionID != p.CredentialID || current.AuthenticationStrength != p.AuthenticationStrength || current.AdminMFAOptional != p.AdminMFAOptional || current.AccountVersion < 1 || current.SessionVersion < 1 || !s.now().Before(current.ExpiresAt) {
			return adminExecutionBinding{}, identityport.ErrAdminSessionPrincipalUnavailable
		}
		current.ExpiresAt = current.ExpiresAt.UTC()
		if current.MFAAuthenticatedAt != nil {
			at := current.MFAAuthenticatedAt.UTC()
			current.MFAAuthenticatedAt = &at
		}
		rows, err := reader.LoadAdminRoleExecutionFacts(ctx, p.Actor.ID, lock)
		if err != nil {
			return adminExecutionBinding{}, err
		}
		return adminExecutionBinding{Query: q, Session: current, Roles: canonicalGovernanceRoles(rows)}, nil
	}
	binding, err := load()
	if err != nil {
		return identityport.AdminExecutionAuthority{}, err
	}
	a := identityport.AdminExecutionAuthority{Principal: p, Query: q, ExpiresAtMS: binding.Session.ExpiresAt.UnixMilli(), Binding: binding}
	strength := true
	if q.RequiresCurrentMFA() && !binding.Session.AdminMFAOptional {
		strength = p.AuthenticationStrength == "mfa" && binding.Session.MFAAuthenticatedAt != nil
		if strength {
			expiry := binding.Session.MFAAuthenticatedAt.Add(AdminGovernanceMFAWindow)
			if expiry.Before(binding.Session.ExpiresAt) {
				a.ExpiresAtMS = expiry.UnixMilli()
			}
			strength = s.now().UnixMilli() < a.ExpiresAtMS
		}
	}
	for _, f := range binding.Roles {
		if f.Binding.AccountID != p.Actor.ID || f.Binding.ID == "" || f.Binding.RoleID != f.Role.ID || f.Binding.Status != "active" || f.Binding.RevokedAt != nil || f.Binding.Version < 1 || f.PermissionFactsVersion < 1 {
			return identityport.AdminExecutionAuthority{}, identityport.ErrGovernanceAuthorityUnavailable
		}
		if f.PermissionCode == q.PermissionCode && strength {
			a.Allowed = true
		}
	}
	current, err := governancePrincipal(ctx, resolve)
	if err != nil {
		return identityport.AdminExecutionAuthority{}, err
	}
	if !reflect.DeepEqual(p, current) {
		return identityport.AdminExecutionAuthority{}, identityport.ErrPolicyFactsChanged
	}
	final, err := load()
	if err != nil {
		return identityport.AdminExecutionAuthority{}, err
	}
	if !reflect.DeepEqual(binding, final) || a.Allowed && s.now().UnixMilli() >= a.ExpiresAtMS {
		return identityport.AdminExecutionAuthority{}, identityport.ErrPolicyFactsChanged
	}
	if err := ctx.Err(); err != nil {
		return identityport.AdminExecutionAuthority{}, err
	}
	return a, nil
}
