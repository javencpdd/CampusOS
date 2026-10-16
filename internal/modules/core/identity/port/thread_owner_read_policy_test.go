package port_test

import (
	"errors"
	"testing"

	"github.com/campusos/CampusOS/internal/modules/core/identity/policy"
	"github.com/campusos/CampusOS/internal/modules/core/identity/port"
)

func ownerDecisionRequest() port.ThreadOwnerReadRequest {
	return port.ThreadOwnerReadRequest{
		Contract: port.ThreadOwnerReadContract, RequestID: "request-1", Policy: port.ThreadOwnerReadPolicyVersion,
		Principal: port.PrincipalContext{Contract: port.PrincipalContract,
			Actor: port.PrincipalRef{Kind: "user", ID: "user-1"}, Audience: "user", AuthenticationStrength: "password", CredentialID: "session-1"},
		Facts: port.ThreadOwnerReadFacts{
			Resource: port.PolicyResourceRef{Kind: "community.thread", ID: "thread-1"},
			Owner:    port.PrincipalRef{Kind: "user", ID: "user-1"}, Board: port.PolicyResourceRef{Kind: "community.board", ID: "board-1"},
			FactsVersion: "version-1", PublicationStatus: "private", ModerationStatus: "pending", DeletionStatus: "active",
		},
	}
}

func TestCheckThreadOwnerReadDecisionBinding(t *testing.T) {
	for name, mutate := range map[string]func(*port.ThreadOwnerReadDecision){
		"contract":            func(d *port.ThreadOwnerReadDecision) { d.Contract = port.ThreadPublicReadContract },
		"request":             func(d *port.ThreadOwnerReadDecision) { d.RequestID = "other-request" },
		"policy":              func(d *port.ThreadOwnerReadDecision) { d.Policy = port.ThreadPublicReadPolicyVersion },
		"resource":            func(d *port.ThreadOwnerReadDecision) { d.Resource.ID = "other-thread" },
		"kind":                func(d *port.ThreadOwnerReadDecision) { d.Resource.Kind = "community.post" },
		"version":             func(d *port.ThreadOwnerReadDecision) { d.FactsVersion = "other-version" },
		"principal":           func(d *port.ThreadOwnerReadDecision) { d.Principal = port.PrincipalContext{} },
		"credential":          func(d *port.ThreadOwnerReadDecision) { d.Principal.CredentialID = "other-session" },
		"actor":               func(d *port.ThreadOwnerReadDecision) { d.Principal.Actor.ID = "other-user" },
		"strength":            func(d *port.ThreadOwnerReadDecision) { d.Principal.AuthenticationStrength = "mfa" },
		"effect":              func(d *port.ThreadOwnerReadDecision) { d.Effect = "unknown" },
		"reason":              func(d *port.ThreadOwnerReadDecision) { d.Reason = "policy.public_read" },
		"contradiction":       func(d *port.ThreadOwnerReadDecision) { d.Reason = "policy.owner_read_denied" },
		"nil-obligations":     func(d *port.ThreadOwnerReadDecision) { d.Obligations = nil },
		"missing-obligations": func(d *port.ThreadOwnerReadDecision) { d.Obligations = []string{} },
		"duplicate-obligations": func(d *port.ThreadOwnerReadDecision) {
			d.Obligations = []string{"recheck_current_facts", "recheck_current_facts"}
		},
		"unknown-obligations": func(d *port.ThreadOwnerReadDecision) { d.Obligations = []string{"required_audit"} },
	} {
		t.Run(name, func(t *testing.T) {
			r := ownerDecisionRequest()
			d, err := (policy.ThreadOwnerReadPolicy{}).DecideThreadOwnerRead(r)
			if err != nil {
				t.Fatal(err)
			}
			mutate(&d)
			if err := port.CheckThreadOwnerReadDecision(r, d, r.Facts, r.Principal); !errors.Is(err, port.ErrPolicyDecisionMismatch) {
				t.Fatalf("unbound decision accepted: %v", err)
			}
		})
	}
}

func TestCheckThreadOwnerReadDecisionCurrentFactsAndPrincipal(t *testing.T) {
	for name, mutate := range map[string]func(*port.ThreadOwnerReadFacts, *port.PrincipalContext){
		"resource":           func(f *port.ThreadOwnerReadFacts, _ *port.PrincipalContext) { f.Resource.ID = "other-thread" },
		"owner":              func(f *port.ThreadOwnerReadFacts, _ *port.PrincipalContext) { f.Owner.ID = "other-user" },
		"board":              func(f *port.ThreadOwnerReadFacts, _ *port.PrincipalContext) { f.Board.ID = "other-board" },
		"version":            func(f *port.ThreadOwnerReadFacts, _ *port.PrincipalContext) { f.FactsVersion = "version-2" },
		"publication":        func(f *port.ThreadOwnerReadFacts, _ *port.PrincipalContext) { f.PublicationStatus = "draft" },
		"moderation":         func(f *port.ThreadOwnerReadFacts, _ *port.PrincipalContext) { f.ModerationStatus = "clear" },
		"deletion":           func(f *port.ThreadOwnerReadFacts, _ *port.PrincipalContext) { f.DeletionStatus = "trashed" },
		"missing-state":      func(f *port.ThreadOwnerReadFacts, _ *port.PrincipalContext) { f.DeletionStatus = "" },
		"principal-contract": func(_ *port.ThreadOwnerReadFacts, p *port.PrincipalContext) { p.Contract = "campusos.principal/v2" },
		"principal-id":       func(_ *port.ThreadOwnerReadFacts, p *port.PrincipalContext) { p.Actor.ID = "other-user" },
		"principal-domain": func(_ *port.ThreadOwnerReadFacts, p *port.PrincipalContext) {
			p.Actor.Kind, p.Audience = "admin", "admin"
		},
		"principal-audience":   func(_ *port.ThreadOwnerReadFacts, p *port.PrincipalContext) { p.Audience = "public" },
		"principal-strength":   func(_ *port.ThreadOwnerReadFacts, p *port.PrincipalContext) { p.AuthenticationStrength = "mfa" },
		"principal-credential": func(_ *port.ThreadOwnerReadFacts, p *port.PrincipalContext) { p.CredentialID = "session-2" },
		"principal-missing":    func(_ *port.ThreadOwnerReadFacts, p *port.PrincipalContext) { *p = port.PrincipalContext{} },
		"principal-delegation": func(_ *port.ThreadOwnerReadFacts, p *port.PrincipalContext) { p.DelegationID = "delegation-1" },
		"principal-subject": func(_ *port.ThreadOwnerReadFacts, p *port.PrincipalContext) {
			p.EffectiveSubject = &port.PrincipalRef{Kind: "user", ID: "user-1"}
		},
	} {
		t.Run(name, func(t *testing.T) {
			r := ownerDecisionRequest()
			d, err := (policy.ThreadOwnerReadPolicy{}).DecideThreadOwnerRead(r)
			if err != nil {
				t.Fatal(err)
			}
			facts, principal := r.Facts, r.Principal
			mutate(&facts, &principal)
			if err := port.CheckThreadOwnerReadDecision(r, d, facts, principal); !errors.Is(err, port.ErrPolicyFactsChanged) {
				t.Fatalf("stale allow survived current change: %v", err)
			}
		})
	}
}

func TestCheckThreadOwnerReadDecisionCannotForgeOwnerAllow(t *testing.T) {
	for _, condition := range []string{"non-owner", "same-id-admin", "proxy-owner", "purged"} {
		t.Run(condition, func(t *testing.T) {
			r := ownerDecisionRequest()
			switch condition {
			case "non-owner":
				r.Principal.Actor.ID = "other-user"
			case "same-id-admin":
				r.Principal.Actor.Kind, r.Principal.Audience = "admin", "admin"
			case "proxy-owner":
				r.Principal.Actor.Kind, r.Principal.Actor.ID = "plugin_instance", "plugin-1"
				r.Principal.Audience, r.Principal.AuthenticationStrength = "host_api", "workload"
				r.Principal.EffectiveSubject, r.Principal.DelegationID = &port.PrincipalRef{Kind: "user", ID: "user-1"}, "delegation-1"
			case "purged":
				r.Facts.DeletionStatus = "purged"
			}
			d, err := (policy.ThreadOwnerReadPolicy{}).DecideThreadOwnerRead(r)
			if err != nil || d.Effect != "deny" {
				t.Fatalf("expected valid denial: %+v %v", d, err)
			}
			if err := port.CheckThreadOwnerReadDecision(r, d, r.Facts, r.Principal); !errors.Is(err, port.ErrPolicyOwnerReadDenied) {
				t.Fatalf("denial did not remain denial: %v", err)
			}
			d.Effect, d.Reason, d.Obligations = "allow", "policy.owner_read", []string{"recheck_current_facts"}
			if err := port.CheckThreadOwnerReadDecision(r, d, r.Facts, r.Principal); !errors.Is(err, port.ErrPolicyDecisionMismatch) {
				t.Fatalf("forged allow consumed: %v", err)
			}
		})
	}
}

func TestCheckThreadOwnerReadDecisionRejectsInvalidInitialRequest(t *testing.T) {
	r := ownerDecisionRequest()
	d, err := (policy.ThreadOwnerReadPolicy{}).DecideThreadOwnerRead(r)
	if err != nil {
		t.Fatal(err)
	}
	r.Principal.CredentialID = ""
	if err := port.CheckThreadOwnerReadDecision(r, d, r.Facts, r.Principal); !errors.Is(err, port.ErrPolicyInputInvalid) {
		t.Fatalf("invalid principal input was consumed: %v", err)
	}
}
