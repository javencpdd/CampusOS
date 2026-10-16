package policy

import (
	"github.com/campusos/CampusOS/internal/modules/core/identity/port"
)

// IdentityDomainPolicy implements the campusos.identity-domains/v1 judgement
// from host-supplied facts only. Its zero value requires no cache, repository,
// credential verifier or ambient clock. Acceptance is not an authenticated
// session; it is only the domain-separation verdict for one consumption.
type IdentityDomainPolicy struct{}

var _ port.IdentityDomainPolicy = IdentityDomainPolicy{}

// EvaluateIdentityDomain applies the frozen order: structural validity, domain
// isolation, role-based auto-activation, account status, credential binding to
// the target domain, challenge domain/target binding, then the admin lifecycle
// operation rules. Unknown conditions deny closed.
func (IdentityDomainPolicy) EvaluateIdentityDomain(r port.IdentityDomainInput) error {
	if err := r.Validate(); err != nil {
		return err
	}
	deny := func(reason string) error { return &port.IdentityDomainDenyError{Reason: reason} }
	u, a := r.Domains.User, r.Domains.Admin
	if u.Issuer == a.Issuer || u.Audience == a.Audience || u.KeyID == a.KeyID ||
		u.CredentialStore == a.CredentialStore || u.SessionStore == a.SessionStore ||
		u.RecoveryStore == a.RecoveryStore || u.RoleStore == a.RoleStore {
		return deny("identity.domain_not_isolated")
	}
	req := r.Request
	target := r.Domains.User
	if req.Kind == "admin" {
		target = r.Domains.Admin
	}
	if req.Kind == "admin" && req.ActivationSource == "user_role" {
		return deny("identity.admin_auto_activation_forbidden")
	}
	if req.Status != "active" {
		return deny("identity.account_inactive")
	}
	if req.Issuer != target.Issuer || req.Audience != target.Audience || req.KeyID != target.KeyID ||
		req.CredentialStore != target.CredentialStore || req.SessionStore != target.SessionStore {
		return deny("identity.cross_domain_credential")
	}
	if req.ChallengeDomain != req.Kind || req.TargetAccountID != req.AccountID {
		return deny("identity.challenge_wrong_domain")
	}
	if r.Operation != nil {
		op := r.Operation
		if op.Kind == "bootstrap" {
			if op.ActorKind != "system" || !op.BootstrapEnabled || op.ActiveSecurityAdmins != 0 {
				return deny("identity.admin_bootstrap_forbidden")
			}
			return nil
		}
		if op.ActorKind != "admin" {
			return deny("identity.admin_actor_required")
		}
		if !op.ActorMFA {
			return deny("identity.admin_mfa_required")
		}
		if (op.Kind == "suspend" || op.Kind == "revoke") && op.TargetIsSecurityAdmin && op.ActiveSecurityAdmins <= 1 {
			return deny("identity.last_security_admin")
		}
	}
	return nil
}
