package policy

import (
	"bytes"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/campusos/CampusOS/internal/modules/core/identity/port"
)

func identityDomainConfig(prefix string) port.IdentityDomainConfig {
	return port.IdentityDomainConfig{
		Issuer: "campusos-" + prefix, Audience: prefix + "-api", KeyID: prefix + "-key",
		CredentialStore: prefix + "-credentials", SessionStore: prefix + "-sessions",
		RecoveryStore: prefix + "-recovery", RoleStore: prefix + "-roles",
	}
}

func identityDomainFixture() port.IdentityDomainInput {
	admin := identityDomainConfig("admin")
	return port.IdentityDomainInput{
		Contract: port.IdentityDomainsContract,
		Domains:  port.IdentityDomainPair{User: identityDomainConfig("user"), Admin: admin},
		Request: port.IdentityDomainRequest{
			Kind: "admin", AccountID: "same-42", Issuer: admin.Issuer, Audience: admin.Audience,
			KeyID: admin.KeyID, CredentialStore: admin.CredentialStore, SessionStore: admin.SessionStore,
			ChallengeKind: "access", ChallengeDomain: "admin", TargetAccountID: "same-42",
			Status: "active", ActivationSource: "direct",
		},
	}
}

func identityDomainOperation(kind string) *port.IdentityDomainOperation {
	return &port.IdentityDomainOperation{
		Kind: kind, ActorKind: "admin", ActorMFA: true, TargetAdminID: "same-42",
		TargetIsSecurityAdmin: false, ActiveSecurityAdmins: 2, BootstrapEnabled: false, AuditReason: "required audit",
	}
}

func identityDomainOutcome(err error) string {
	if err == nil {
		return "accepted"
	}
	var deny *port.IdentityDomainDenyError
	if errors.As(err, &deny) {
		return deny.Reason
	}
	return err.Error()
}

func TestIdentityDomainAcceptedShapes(t *testing.T) {
	r := identityDomainFixture()
	before, _ := json.Marshal(r)
	if err := (IdentityDomainPolicy{}).EvaluateIdentityDomain(r); err != nil {
		t.Fatal(err)
	}
	if after, _ := json.Marshal(r); !bytes.Equal(before, after) {
		t.Fatal("input mutated")
	}
	// Same numeric ID in the other domain is a different principal.
	r = identityDomainFixture()
	user := r.Domains.User
	r.Request.Kind = "user"
	r.Request.Issuer, r.Request.Audience, r.Request.KeyID = user.Issuer, user.Audience, user.KeyID
	r.Request.CredentialStore, r.Request.SessionStore = user.CredentialStore, user.SessionStore
	r.Request.ChallengeDomain = "user"
	if err := (IdentityDomainPolicy{}).EvaluateIdentityDomain(r); err != nil {
		t.Fatal("same-id user principal denied:", err)
	}
	// Operations on ordinary admins by MFA admins are accepted.
	r = identityDomainFixture()
	r.Operation = identityDomainOperation("create")
	if err := (IdentityDomainPolicy{}).EvaluateIdentityDomain(r); err != nil {
		t.Fatal(err)
	}
	// First bootstrap by the controlled system entry.
	r = identityDomainFixture()
	r.Operation = identityDomainOperation("bootstrap")
	r.Operation.ActorKind = "system"
	r.Operation.ActiveSecurityAdmins = 0
	r.Operation.BootstrapEnabled = true
	if err := (IdentityDomainPolicy{}).EvaluateIdentityDomain(r); err != nil {
		t.Fatal(err)
	}
}

func TestIdentityDomainDenySequence(t *testing.T) {
	cases := []struct {
		name, want string
		edit       func(*port.IdentityDomainInput)
	}{
		{"shared-issuer", "identity.domain_not_isolated", func(r *port.IdentityDomainInput) { r.Domains.Admin.Issuer = r.Domains.User.Issuer }},
		{"shared-role-store", "identity.domain_not_isolated", func(r *port.IdentityDomainInput) { r.Domains.Admin.RoleStore = r.Domains.User.RoleStore }},
		{"role-auto-admin", "identity.admin_auto_activation_forbidden", func(r *port.IdentityDomainInput) { r.Request.ActivationSource = "user_role" }},
		{"suspended", "identity.account_inactive", func(r *port.IdentityDomainInput) { r.Request.Status = "suspended" }},
		{"revoked", "identity.account_inactive", func(r *port.IdentityDomainInput) { r.Request.Status = "revoked" }},
		{"user-issuer", "identity.cross_domain_credential", func(r *port.IdentityDomainInput) { r.Request.Issuer = r.Domains.User.Issuer }},
		{"user-audience", "identity.cross_domain_credential", func(r *port.IdentityDomainInput) { r.Request.Audience = r.Domains.User.Audience }},
		{"user-key", "identity.cross_domain_credential", func(r *port.IdentityDomainInput) { r.Request.KeyID = r.Domains.User.KeyID }},
		{"user-credential-store", "identity.cross_domain_credential", func(r *port.IdentityDomainInput) { r.Request.CredentialStore = r.Domains.User.CredentialStore }},
		{"user-session-store", "identity.cross_domain_credential", func(r *port.IdentityDomainInput) { r.Request.SessionStore = r.Domains.User.SessionStore }},
		{"challenge-user-domain", "identity.challenge_wrong_domain", func(r *port.IdentityDomainInput) { r.Request.ChallengeDomain = "user" }},
		{"challenge-other-target", "identity.challenge_wrong_domain", func(r *port.IdentityDomainInput) {
			r.Request.ChallengeKind = "recovery"
			r.Request.TargetAccountID = "other"
		}},
		{"create-by-user", "identity.admin_actor_required", func(r *port.IdentityDomainInput) {
			r.Operation = identityDomainOperation("create")
			r.Operation.ActorKind = "user"
		}},
		{"create-no-mfa", "identity.admin_mfa_required", func(r *port.IdentityDomainInput) {
			r.Operation = identityDomainOperation("create")
			r.Operation.ActorMFA = false
		}},
		{"suspend-last-admin", "identity.last_security_admin", func(r *port.IdentityDomainInput) {
			r.Operation = identityDomainOperation("suspend")
			r.Operation.TargetIsSecurityAdmin = true
			r.Operation.ActiveSecurityAdmins = 1
		}},
		{"revoke-last-admin", "identity.last_security_admin", func(r *port.IdentityDomainInput) {
			r.Operation = identityDomainOperation("revoke")
			r.Operation.TargetIsSecurityAdmin = true
			r.Operation.ActiveSecurityAdmins = 1
		}},
		{"bootstrap-with-admin", "identity.admin_bootstrap_forbidden", func(r *port.IdentityDomainInput) {
			r.Operation = identityDomainOperation("bootstrap")
			r.Operation.ActorKind = "system"
			r.Operation.BootstrapEnabled = true
			r.Operation.ActiveSecurityAdmins = 1
		}},
		{"bootstrap-by-user", "identity.admin_bootstrap_forbidden", func(r *port.IdentityDomainInput) {
			r.Operation = identityDomainOperation("bootstrap")
			r.Operation.ActorKind = "user"
			r.Operation.BootstrapEnabled = true
			r.Operation.ActiveSecurityAdmins = 0
		}},
		{"bootstrap-disabled", "identity.admin_bootstrap_forbidden", func(r *port.IdentityDomainInput) {
			r.Operation = identityDomainOperation("bootstrap")
			r.Operation.ActorKind = "system"
			r.Operation.BootstrapEnabled = false
			r.Operation.ActiveSecurityAdmins = 0
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := identityDomainFixture()
			c.edit(&r)
			before, _ := json.Marshal(r)
			err := (IdentityDomainPolicy{}).EvaluateIdentityDomain(r)
			if got := identityDomainOutcome(err); got != c.want {
				t.Fatalf("want %s got %s (%v)", c.want, got, err)
			}
			if after, _ := json.Marshal(r); !bytes.Equal(before, after) {
				t.Fatal("input mutated")
			}
		})
	}
	// Isolation failures precede every credential or lifecycle judgement.
	r := identityDomainFixture()
	r.Domains.Admin.Issuer = r.Domains.User.Issuer
	r.Request.Status = "revoked"
	if got := identityDomainOutcome((IdentityDomainPolicy{}).EvaluateIdentityDomain(r)); got != "identity.domain_not_isolated" {
		t.Fatalf("order: %s", got)
	}
}

func TestIdentityDomainMalformedTypedInput(t *testing.T) {
	for _, change := range []func(*port.IdentityDomainInput){
		func(r *port.IdentityDomainInput) { r.Contract = "" },
		func(r *port.IdentityDomainInput) { r.Contract = "campusos.identity-domains/v2" },
		func(r *port.IdentityDomainInput) { r.Request.Kind = "integration" },
		func(r *port.IdentityDomainInput) { r.Request.ChallengeKind = "password_reset_any" },
		func(r *port.IdentityDomainInput) { r.Request.Status = "legacy" },
		func(r *port.IdentityDomainInput) { r.Request.ActivationSource = "user_admin_role" },
		func(r *port.IdentityDomainInput) { r.Domains.Admin.Issuer = "" },
		func(r *port.IdentityDomainInput) {
			r.Operation = &port.IdentityDomainOperation{Kind: "assign_user_role"}
		},
	} {
		r := identityDomainFixture()
		change(&r)
		if err := (IdentityDomainPolicy{}).EvaluateIdentityDomain(r); !errors.Is(err, port.ErrIdentityDomainInvalid) {
			t.Fatalf("invalid input accepted: %v", err)
		}
	}
}

func TestIdentityDomainParallelWithoutCache(t *testing.T) {
	var evaluator port.IdentityDomainPolicy = IdentityDomainPolicy{}
	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r := identityDomainFixture()
			if err := evaluator.EvaluateIdentityDomain(r); err != nil {
				t.Error(err)
			}
			r.Request.ChallengeDomain = "user"
			if err := evaluator.EvaluateIdentityDomain(r); identityDomainOutcome(err) != "identity.challenge_wrong_domain" {
				t.Errorf("cached state leaked: %v", err)
			}
		}(i)
	}
	wg.Wait()
}
