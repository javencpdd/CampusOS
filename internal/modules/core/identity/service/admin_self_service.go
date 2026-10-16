package service

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/identity/policy"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/modules/core/identity/repository"
)

var ErrAdminSelfUnavailable = errors.New("current admin self facts unavailable")

type AdminSelfAccount struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Status      string `json:"status"`
}
type AdminSelfRole struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
type AdminSelfProfile struct {
	Domain                 string           `json:"domain"`
	Account                AdminSelfAccount `json:"account"`
	Roles                  []AdminSelfRole  `json:"roles"`
	PermissionCodes        []string         `json:"permission_codes"`
	AuthenticationStrength string           `json:"authentication_strength"`
	MFAEnrolled            bool             `json:"mfa_enrolled"`
	SessionExpiresAt       time.Time        `json:"session_expires_at"`
	MFAExpiresAt           *time.Time       `json:"mfa_expires_at,omitempty"`
}
type AdminSelfService struct {
	sessions *AdminSessionService
	admins   repository.AdminDomainRepository
	security repository.AdminSecurityRepository
}

func NewAdminSelfService(s *AdminSessionService, a repository.AdminDomainRepository, m repository.AdminSecurityRepository) *AdminSelfService {
	return &AdminSelfService{s, a, m}
}

type adminSelfRoleWitness struct {
	Binding repository.AdminDomainRoleBinding
	Role    repository.AdminDomainRole
}
type adminSelfSnapshot struct {
	Account     repository.AdminDomainAccount
	Session     repository.AdminSession
	Authority   identityport.AdminSessionAuthority
	Roles       []adminSelfRoleWitness
	Permissions []repository.AdminRoleExecutionFact
	Method      *repository.AdminTOTPMethod
}

func adminSelfTime(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	value := t.UTC()
	return &value
}
func (s *AdminSelfService) load(ctx context.Context, p identityport.PrincipalContext) (adminSelfSnapshot, error) {
	if s == nil || s.sessions == nil || nilGovernanceAdapter(s.sessions.sessions) || nilGovernanceAdapter(s.sessions.admins) || nilGovernanceAdapter(s.admins) || nilGovernanceAdapter(s.security) {
		return adminSelfSnapshot{}, ErrAdminSelfUnavailable
	}
	if p.Validate() != nil || p.Actor.Kind != "admin" || p.Audience != "admin" {
		return adminSelfSnapshot{}, identityport.ErrAdminSessionPrincipalUnavailable
	}
	session, account, err := s.sessions.verifiedSession(ctx, p.Actor.ID, p.CredentialID)
	if err != nil {
		return adminSelfSnapshot{}, errors.Join(identityport.ErrAdminSessionPrincipalUnavailable, err)
	}
	authority, err := s.sessions.CurrentAdminSession(ctx, p.Actor.ID, p.CredentialID)
	if err != nil || authority.AuthenticationStrength != p.AuthenticationStrength {
		return adminSelfSnapshot{}, errors.Join(identityport.ErrAdminSessionPrincipalUnavailable, err)
	}
	r := identityport.AdminSelfReadRequest{RequestID: "admin-current-self-read", PolicyVersion: identityport.AdminSelfReadPolicyVersion, Principal: p, AccountID: account.ID, Status: account.Status, SessionID: session.ID, AccountVersion: account.Version, SessionVersion: session.Version, ExpiresAtMS: authority.ExpiresAt.UnixMilli(), EvaluatedAtMS: s.sessions.now().UnixMilli()}
	d, err := (policy.AdminSelfReadPolicy{}).DecideAdminSelfRead(r)
	if err != nil || identityport.CheckAdminSelfReadDecision(r, d, r) != nil {
		return adminSelfSnapshot{}, identityport.ErrAdminSessionPrincipalUnavailable
	}
	reader, ok := s.admins.(repository.AdminRoleExecutionFactsReader)
	if !ok || nilGovernanceAdapter(reader) {
		return adminSelfSnapshot{}, ErrAdminSelfUnavailable
	}
	facts, err := reader.LoadAdminRoleExecutionFacts(ctx, p.Actor.ID, false)
	if err != nil {
		return adminSelfSnapshot{}, errors.Join(ErrAdminSelfUnavailable, err)
	}
	facts = canonicalGovernanceRoles(facts)
	bindings, err := s.admins.ListActiveBindings(ctx, p.Actor.ID)
	if err != nil {
		return adminSelfSnapshot{}, errors.Join(ErrAdminSelfUnavailable, err)
	}
	roles := make([]adminSelfRoleWitness, 0, len(bindings))
	seen := map[string]bool{}
	for _, b := range bindings {
		if b.ID == "" || b.AccountID != p.Actor.ID || b.Status != "active" || b.RevokedAt != nil || b.Version < 1 || seen[b.ID] {
			return adminSelfSnapshot{}, ErrAdminSelfUnavailable
		}
		seen[b.ID] = true
		role, err := s.admins.GetRole(ctx, b.RoleID)
		if err != nil || role.ID != b.RoleID || role.Name == "" {
			return adminSelfSnapshot{}, errors.Join(ErrAdminSelfUnavailable, err)
		}
		b.CreatedAt = b.CreatedAt.UTC()
		roles = append(roles, adminSelfRoleWitness{b, role})
	}
	sort.Slice(roles, func(i, j int) bool { return roles[i].Binding.ID < roles[j].Binding.ID })
	for _, f := range facts {
		if f.PermissionCode == "" || strings.TrimSpace(f.PermissionCode) != f.PermissionCode || len(f.PermissionCode) > 160 || f.PermissionFactsVersion < 1 {
			return adminSelfSnapshot{}, ErrAdminSelfUnavailable
		}
		found := false
		for _, role := range roles {
			if reflect.DeepEqual(role.Binding, f.Binding) && reflect.DeepEqual(role.Role, f.Role) {
				found = true
				break
			}
		}
		if !found {
			return adminSelfSnapshot{}, identityport.ErrPolicyFactsChanged
		}
	}
	method, err := s.security.GetTOTP(ctx, p.Actor.ID)
	var m *repository.AdminTOTPMethod
	if !errors.Is(err, repository.ErrAdminDomainTOTPNotFound) {
		if err != nil {
			return adminSelfSnapshot{}, errors.Join(ErrAdminSelfUnavailable, err)
		}
		if method.AccountID != p.Actor.ID || method.Version < 1 || method.KeyID == "" || len(method.Nonce) < 8 || len(method.Ciphertext) < 16 || method.LastAcceptedStep < 0 || method.Status == "active" && method.ConfirmedAt == nil || (method.Status != "active" && method.Status != "pending" && method.Status != "disabled") {
			return adminSelfSnapshot{}, ErrAdminSelfUnavailable
		}
		method.ConfirmedAt = adminSelfTime(method.ConfirmedAt)
		method.DisabledAt = adminSelfTime(method.DisabledAt)
		method.EnrollmentExpiresAt = method.EnrollmentExpiresAt.UTC()
		m = &method
	}
	account.CreatedAt = account.CreatedAt.UTC()
	account.UpdatedAt = account.UpdatedAt.UTC()
	account.RevokedAt = adminSelfTime(account.RevokedAt)
	session.CreatedAt = session.CreatedAt.UTC()
	session.UpdatedAt = session.UpdatedAt.UTC()
	session.LastActiveAt = session.LastActiveAt.UTC()
	session.ExpiresAt = session.ExpiresAt.UTC()
	session.MFAAuthenticatedAt = adminSelfTime(session.MFAAuthenticatedAt)
	session.RevokedAt = adminSelfTime(session.RevokedAt)
	return adminSelfSnapshot{account, session, authority, roles, canonicalGovernanceRoles(facts), m}, nil
}
func (s *AdminSelfService) CurrentProfile(ctx context.Context, resolve identityport.SessionPrincipalResolver) (*AdminSelfProfile, error) {
	if resolve == nil {
		return nil, identityport.ErrAdminSessionPrincipalUnavailable
	}
	p, err := resolve(ctx)
	if err != nil {
		return nil, err
	}
	initial, err := s.load(ctx, p)
	if err != nil {
		return nil, err
	}
	result := &AdminSelfProfile{Domain: "admin", Account: AdminSelfAccount{initial.Account.ID, initial.Account.Username, initial.Account.DisplayName, initial.Account.Status}, Roles: []AdminSelfRole{}, PermissionCodes: []string{}, AuthenticationStrength: initial.Authority.AuthenticationStrength, MFAEnrolled: initial.Method != nil && initial.Method.Status == "active", SessionExpiresAt: initial.Authority.ExpiresAt}
	if initial.Authority.AuthenticationStrength == "mfa" && initial.Authority.MFAAuthenticatedAt != nil {
		expires := initial.Authority.MFAAuthenticatedAt.Add(AdminGovernanceMFAWindow)
		result.MFAExpiresAt = &expires
	}
	seenRoles, seenCodes := map[string]bool{}, map[string]bool{}
	for _, f := range initial.Roles {
		if !seenRoles[f.Role.ID] {
			result.Roles = append(result.Roles, AdminSelfRole{f.Role.ID, f.Role.Name})
			seenRoles[f.Role.ID] = true
		}
	}
	sort.Slice(result.Roles, func(i, j int) bool { return result.Roles[i].ID < result.Roles[j].ID })
	for _, f := range initial.Permissions {
		if !seenCodes[f.PermissionCode] {
			result.PermissionCodes = append(result.PermissionCodes, f.PermissionCode)
			seenCodes[f.PermissionCode] = true
		}
	}
	sort.Strings(result.PermissionCodes)
	check := func() error {
		current, err := resolve(ctx)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(p, current) {
			return identityport.ErrPolicyFactsChanged
		}
		return ctx.Err()
	}
	if err := check(); err != nil {
		return nil, err
	}
	current, err := s.load(ctx, p)
	if err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(initial, current) {
		return nil, identityport.ErrPolicyFactsChanged
	}
	if err := check(); err != nil {
		return nil, err
	}
	return result, nil
}
