package service

import (
	"context"
	"reflect"
	"sort"
	"time"

	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	"github.com/campusos/CampusOS/internal/platform/transaction"
)

type BoardGovernanceAuthorityService struct {
	delegations   repository.DelegationRepository
	admins        repository.AdminDomainRepository
	adminSessions identityport.AdminSessionAuthorityReader
	now           func() time.Time
}

func NewBoardGovernanceAuthorityService(delegations repository.DelegationRepository, admins repository.AdminDomainRepository, sessions identityport.AdminSessionAuthorityReader) *BoardGovernanceAuthorityService {
	return &BoardGovernanceAuthorityService{delegations: delegations, admins: admins, adminSessions: sessions, now: time.Now}
}

type boardGovernanceBinding struct {
	Query        identityport.BoardGovernanceQuery
	Delegations  []repository.Delegation
	AdminSession *identityport.AdminSessionAuthority
	AdminRoles   []repository.AdminRoleExecutionFact
}

func governancePrincipal(ctx context.Context, resolve identityport.SessionPrincipalResolver) (identityport.PrincipalContext, error) {
	if err := ctx.Err(); err != nil {
		return identityport.PrincipalContext{}, err
	}
	if resolve == nil {
		return identityport.PrincipalContext{}, identityport.ErrGovernanceAuthorityUnavailable
	}
	p, err := resolve(ctx)
	if err != nil {
		return identityport.PrincipalContext{}, err
	}
	if p.Validate() != nil || (p.Actor.Kind != "user" && p.Actor.Kind != "admin") {
		return identityport.PrincipalContext{}, identityport.ErrGovernanceAuthorityUnavailable
	}
	return p, nil
}

func nilGovernanceAdapter(v any) bool {
	if v == nil {
		return true
	}
	r := reflect.ValueOf(v)
	switch r.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return r.IsNil()
	}
	return false
}

func (s *BoardGovernanceAuthorityService) LoadBoardGovernanceAuthority(ctx context.Context, q identityport.BoardGovernanceQuery, resolve identityport.SessionPrincipalResolver, lock bool) (identityport.BoardGovernanceAuthority, error) {
	if err := q.Validate(); err != nil {
		return identityport.BoardGovernanceAuthority{}, err
	}
	if s == nil || s.now == nil || lock && !transaction.Active(ctx) {
		return identityport.BoardGovernanceAuthority{}, identityport.ErrGovernanceAuthorityUnavailable
	}
	p, err := governancePrincipal(ctx, resolve)
	if err != nil {
		return identityport.BoardGovernanceAuthority{}, err
	}
	binding := boardGovernanceBinding{Query: q}
	authority := identityport.BoardGovernanceAuthority{Principal: p, Grants: []identityport.ResourcePolicyGrant{}}
	now := s.now()
	if p.Actor.Kind == "user" {
		if nilGovernanceAdapter(s.delegations) {
			return identityport.BoardGovernanceAuthority{}, identityport.ErrGovernanceAuthorityUnavailable
		}
		list := s.delegations.ListByKindSubject
		if lock {
			locker, ok := s.delegations.(repository.DelegationAuthorityLocker)
			if !ok || nilGovernanceAdapter(locker) {
				return identityport.BoardGovernanceAuthority{}, identityport.ErrGovernanceAuthorityUnavailable
			}
			list = locker.ListByKindSubjectForUpdate
		}
		rows, err := list(ctx, repository.DelegationKindGrant, p.Actor.ID)
		if err != nil {
			return identityport.BoardGovernanceAuthority{}, err
		}
		binding.Delegations = canonicalGovernanceDelegations(rows)
		for _, d := range binding.Delegations {
			if d.Validate() != nil {
				return identityport.BoardGovernanceAuthority{}, identityport.ErrGovernanceAuthorityUnavailable
			}
			if d.Kind != repository.DelegationKindGrant || d.SubjectKind != "user" || d.SubjectID != p.Actor.ID || d.Action != q.Action || d.BoardID != q.BoardID || d.Status != repository.DelegationStatusActive || d.Delegable || now.Before(d.NotBefore) || !now.Before(d.ExpiresAt) || d.RequiredStrength == "mfa" && p.AuthenticationStrength != "mfa" {
				continue
			}
			authority.Grants = append(authority.Grants, identityport.ResourcePolicyGrant{SubjectKind: "user", SubjectID: p.Actor.ID, Action: q.Action, BoardID: q.BoardID, ExpiresAtMS: d.ExpiresAt.UnixMilli(), Status: "active", RequiredStrength: d.RequiredStrength})
			break // A single complete execution witness; never combine rows.
		}
	} else {
		if nilGovernanceAdapter(s.admins) || nilGovernanceAdapter(s.adminSessions) {
			return identityport.BoardGovernanceAuthority{}, identityport.ErrGovernanceAuthorityUnavailable
		}
		current, err := s.adminSessions.CurrentAdminSession(ctx, p.Actor.ID, p.CredentialID)
		if err != nil {
			return identityport.BoardGovernanceAuthority{}, err
		}
		if current.AccountID != p.Actor.ID || current.SessionID != p.CredentialID || current.AuthenticationStrength != p.AuthenticationStrength || current.AccountVersion < 1 || current.SessionVersion < 1 || !now.Before(current.ExpiresAt) {
			return identityport.BoardGovernanceAuthority{}, identityport.ErrAdminSessionPrincipalUnavailable
		}
		if current.MFAAuthenticatedAt != nil {
			at := current.MFAAuthenticatedAt.UTC()
			current.MFAAuthenticatedAt = &at
		}
		current.ExpiresAt = current.ExpiresAt.UTC()
		binding.AdminSession = &current
		reader, ok := s.admins.(repository.AdminRoleExecutionFactsReader)
		if !ok || nilGovernanceAdapter(reader) {
			return identityport.BoardGovernanceAuthority{}, identityport.ErrGovernanceAuthorityUnavailable
		}
		rows, err := reader.LoadAdminRoleExecutionFacts(ctx, p.Actor.ID, lock)
		if err != nil {
			return identityport.BoardGovernanceAuthority{}, err
		}
		binding.AdminRoles = canonicalGovernanceRoles(rows)
		for _, f := range binding.AdminRoles {
			if f.Binding.AccountID != p.Actor.ID || f.Binding.ID == "" || f.Binding.RoleID != f.Role.ID || f.Binding.Status != "active" || f.Binding.RevokedAt != nil || f.Binding.Version < 1 || f.PermissionFactsVersion < 1 {
				return identityport.BoardGovernanceAuthority{}, identityport.ErrGovernanceAuthorityUnavailable
			}
			if f.PermissionCode != q.Action || p.AuthenticationStrength != "mfa" || current.MFAAuthenticatedAt == nil {
				continue
			}
			expires := current.ExpiresAt
			if mfaExpiry := current.MFAAuthenticatedAt.Add(AdminGovernanceMFAWindow); mfaExpiry.Before(expires) {
				expires = mfaExpiry
			}
			if !now.Before(expires) {
				continue
			}
			authority.Grants = append(authority.Grants, identityport.ResourcePolicyGrant{SubjectKind: "admin", SubjectID: p.Actor.ID, Action: q.Action, BoardID: q.BoardID, ExpiresAtMS: expires.UnixMilli(), Status: "active", RequiredStrength: "mfa"})
			break
		}
	}
	authority.Binding = binding
	current, err := governancePrincipal(ctx, resolve)
	if err != nil {
		return identityport.BoardGovernanceAuthority{}, err
	}
	if !reflect.DeepEqual(p, current) {
		return identityport.BoardGovernanceAuthority{}, identityport.ErrPolicyFactsChanged
	}
	if err := s.recheckBoardGovernanceBinding(ctx, binding, p, lock); err != nil {
		return identityport.BoardGovernanceAuthority{}, err
	}
	for _, grant := range authority.Grants {
		if s.now().UnixMilli() >= grant.ExpiresAtMS {
			return identityport.BoardGovernanceAuthority{}, identityport.ErrPolicyFactsChanged
		}
	}
	if err := ctx.Err(); err != nil {
		return identityport.BoardGovernanceAuthority{}, err
	}
	return authority, nil
}

func canonicalGovernanceDelegations(rows []repository.Delegation) []repository.Delegation {
	out := append([]repository.Delegation{}, rows...)
	for i := range out {
		out[i].NotBefore = out[i].NotBefore.UTC()
		out[i].ExpiresAt = out[i].ExpiresAt.UTC()
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
func canonicalGovernanceRoles(rows []repository.AdminRoleExecutionFact) []repository.AdminRoleExecutionFact {
	out := append([]repository.AdminRoleExecutionFact{}, rows...)
	for i := range out {
		out[i].Binding.CreatedAt = out[i].Binding.CreatedAt.UTC()
		out[i].PermissionCreatedAt = out[i].PermissionCreatedAt.UTC()
		if out[i].Binding.RevokedAt != nil {
			at := out[i].Binding.RevokedAt.UTC()
			out[i].Binding.RevokedAt = &at
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Binding.ID != out[j].Binding.ID {
			return out[i].Binding.ID < out[j].Binding.ID
		}
		return out[i].PermissionCode < out[j].PermissionCode
	})
	return out
}
func (s *BoardGovernanceAuthorityService) recheckBoardGovernanceBinding(ctx context.Context, b boardGovernanceBinding, p identityport.PrincipalContext, lock bool) error {
	if p.Actor.Kind == "user" {
		get := s.delegations.ListByKindSubject
		if lock {
			get = s.delegations.(repository.DelegationAuthorityLocker).ListByKindSubjectForUpdate
		}
		rows, err := get(ctx, repository.DelegationKindGrant, p.Actor.ID)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(b.Delegations, canonicalGovernanceDelegations(rows)) {
			return identityport.ErrPolicyFactsChanged
		}
	} else {
		current, err := s.adminSessions.CurrentAdminSession(ctx, p.Actor.ID, p.CredentialID)
		if err != nil {
			return err
		}
		current.ExpiresAt = current.ExpiresAt.UTC()
		if current.MFAAuthenticatedAt != nil {
			at := current.MFAAuthenticatedAt.UTC()
			current.MFAAuthenticatedAt = &at
		}
		if !reflect.DeepEqual(b.AdminSession, &current) {
			return identityport.ErrPolicyFactsChanged
		}
		rows, err := s.admins.(repository.AdminRoleExecutionFactsReader).LoadAdminRoleExecutionFacts(ctx, p.Actor.ID, lock)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(b.AdminRoles, canonicalGovernanceRoles(rows)) {
			return identityport.ErrPolicyFactsChanged
		}
	}
	return nil
}
