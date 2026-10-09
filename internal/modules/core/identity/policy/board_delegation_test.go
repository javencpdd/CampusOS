package policy

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"

	"github.com/campusos/CampusOS/internal/modules/core/identity/port"
)

func delegationPrincipal(kind, id, strength string) port.PrincipalContext {
	p := port.PrincipalContext{Contract: port.PrincipalContract, Actor: port.PrincipalRef{Kind: kind, ID: id},
		Audience: kind, AuthenticationStrength: strength, CredentialID: "credential-" + id}
	switch kind {
	case "anonymous":
		p.Audience, p.CredentialID = "public", ""
	case "plugin_instance":
		p.Audience = "host_api"
	case "integration":
		p.Audience = "integration_api"
	}
	return p
}

func delegationFixture() port.BoardDelegationRequest {
	return port.BoardDelegationRequest{
		Contract: port.BoardDelegationContract, RequestID: "request-1", FactsVersion: "facts-1", EvaluatedAt: 1000,
		Principal: delegationPrincipal("admin", "admin-1", "mfa"),
		Management: port.BoardDelegationManagement{Actor: port.PrincipalRef{Kind: "admin", ID: "admin-1"},
			Action: "identity.role.assign", Status: "active", NotBefore: 900, ExpiresAt: 2000},
		Recipient: port.BoardDelegationRecipient{Subject: port.PrincipalRef{Kind: "user", ID: "user-1"}, Status: "active"},
		Boards: []port.BoardDelegationBoard{
			{Kind: "community.board", ID: "board-A", Status: "active"},
			{Kind: "community.board", ID: "board-B", Status: "active"},
		},
		Bounds: []port.BoardDelegationBound{{
			Action: "community.thread.take_down", BoardID: "board-A", NotBefore: 900, ExpiresAt: 2000,
			RequiredStrength: "mfa", ID: "bound-1", Actor: port.PrincipalRef{Kind: "admin", ID: "admin-1"},
			Status: "active", Delegable: true,
		}},
		Candidate: port.BoardDelegationCandidate{
			Recipient: port.PrincipalRef{Kind: "user", ID: "user-1"},
			Grants: []port.BoardGrantAtom{{Action: "community.thread.take_down", BoardID: "board-A",
				NotBefore: 1100, ExpiresAt: 1800, RequiredStrength: "mfa"}},
		},
	}
}

func cloneDelegationRequest(r port.BoardDelegationRequest) port.BoardDelegationRequest {
	r.Boards = append([]port.BoardDelegationBoard{}, r.Boards...)
	r.Bounds = append([]port.BoardDelegationBound{}, r.Bounds...)
	r.Candidate.Grants = append([]port.BoardGrantAtom{}, r.Candidate.Grants...)
	if r.Principal.EffectiveSubject != nil {
		v := *r.Principal.EffectiveSubject
		r.Principal.EffectiveSubject = &v
	}
	return r
}

func TestBoardDelegationAllowAndWitnessOrder(t *testing.T) {
	r := delegationFixture()
	before, _ := json.Marshal(r)
	d, err := (BoardDelegationPolicy{}).DecideBoardDelegation(r)
	if err != nil || d.Validate() != nil || d.Effect != "allow" || d.Reason != "delegation.allowed" {
		t.Fatalf("allow: %+v %v", d, err)
	}
	if !reflect.DeepEqual(d.Witnesses, []port.BoardDelegationWitness{{CandidateIndex: 0, BoundID: "bound-1"}}) ||
		!reflect.DeepEqual(d.Obligations, []string{"recheck_authority_in_transaction", "required_audit"}) ||
		d.Contract != port.BoardDelegationContract || d.RequestID != r.RequestID || d.FactsVersion != r.FactsVersion {
		t.Fatalf("decision binding: %+v", d)
	}
	after, _ := json.Marshal(r)
	if !bytes.Equal(before, after) {
		t.Fatal("input mutated")
	}
	// Multiple covering bounds resolve by ASCII bound ID order, never by input order.
	r = delegationFixture()
	covering := r.Bounds[0]
	covering.ID = "zz-later"
	r.Bounds = append(r.Bounds, covering)
	covering.ID = "aa-earlier"
	r.Bounds = append([]port.BoardDelegationBound{covering}, r.Bounds...)
	d, err = (BoardDelegationPolicy{}).DecideBoardDelegation(r)
	if err != nil || d.Effect != "allow" || d.Witnesses[0].BoundID != "aa-earlier" {
		t.Fatalf("tie-break: %+v %v", d, err)
	}
	// A password ceiling may be tightened to MFA, never the reverse.
	r = delegationFixture()
	r.Bounds[0].RequiredStrength = "password"
	d, err = (BoardDelegationPolicy{}).DecideBoardDelegation(r)
	if err != nil || d.Effect != "allow" {
		t.Fatalf("password bound covering mfa candidate: %+v %v", d, err)
	}
	r.Candidate.Grants[0].RequiredStrength = "password"
	d, err = (BoardDelegationPolicy{}).DecideBoardDelegation(r)
	if err != nil || d.Effect != "allow" {
		t.Fatalf("password bound covering password candidate: %+v %v", d, err)
	}
}

func TestBoardDelegationDenySequence(t *testing.T) {
	type mutation func(*port.BoardDelegationRequest)
	cases := []struct {
		name, want string
		edit       mutation
	}{
		{"user-actor", "delegation.actor_denied", func(r *port.BoardDelegationRequest) { r.Principal = delegationPrincipal("user", "admin-1", "mfa") }},
		{"integration-actor", "delegation.actor_denied", func(r *port.BoardDelegationRequest) {
			r.Principal = delegationPrincipal("integration", "admin-1", "credential")
		}},
		{"plugin-actor", "delegation.actor_denied", func(r *port.BoardDelegationRequest) {
			r.Principal = delegationPrincipal("plugin_instance", "p1", "workload")
			r.Principal.EffectiveSubject = &port.PrincipalRef{Kind: "user", ID: "admin-1"}
			r.Principal.DelegationID = "d1"
		}},
		{"worker-actor", "delegation.actor_denied", func(r *port.BoardDelegationRequest) { r.Principal = delegationPrincipal("worker", "w1", "workload") }},
		{"anonymous-actor", "delegation.actor_denied", func(r *port.BoardDelegationRequest) { r.Principal = delegationPrincipal("anonymous", "", "none") }},
		{"mfa-required", "delegation.mfa_required", func(r *port.BoardDelegationRequest) { r.Principal.AuthenticationStrength = "password" }},
		{"actor-before-mfa", "delegation.actor_denied", func(r *port.BoardDelegationRequest) { r.Principal = delegationPrincipal("user", "user-1", "password") }},
		{"management-other-admin", "delegation.management_denied", func(r *port.BoardDelegationRequest) { r.Management.Actor.ID = "admin-2" }},
		{"management-suspended", "delegation.management_denied", func(r *port.BoardDelegationRequest) { r.Management.Status = "suspended" }},
		{"management-revoked", "delegation.management_denied", func(r *port.BoardDelegationRequest) { r.Management.Status = "revoked" }},
		{"management-expired", "delegation.management_denied", func(r *port.BoardDelegationRequest) { r.Management.ExpiresAt = 1000 }},
		{"management-not-yet", "delegation.management_denied", func(r *port.BoardDelegationRequest) { r.Management.NotBefore = 1001 }},
		{"recipient-mismatch", "delegation.recipient_denied", func(r *port.BoardDelegationRequest) { r.Candidate.Recipient.ID = "user-2" }},
		{"recipient-suspended", "delegation.recipient_denied", func(r *port.BoardDelegationRequest) { r.Recipient.Status = "suspended" }},
		{"recipient-deleted", "delegation.recipient_denied", func(r *port.BoardDelegationRequest) { r.Recipient.Status = "deleted" }},
		{"board-unknown", "delegation.board_denied", func(r *port.BoardDelegationRequest) { r.Candidate.Grants[0].BoardID = "board-C" }},
		{"board-archived", "delegation.board_denied", func(r *port.BoardDelegationRequest) {
			r.Candidate.Grants[0].BoardID = "board-B"
			r.Boards[1].Status = "archived"
		}},
		{"bound-revoked", "delegation.outside_bounds", func(r *port.BoardDelegationRequest) { r.Bounds[0].Status = "revoked" }},
		{"bound-suspended", "delegation.outside_bounds", func(r *port.BoardDelegationRequest) { r.Bounds[0].Status = "suspended" }},
		{"bound-not-delegable", "delegation.outside_bounds", func(r *port.BoardDelegationRequest) { r.Bounds[0].Delegable = false }},
		{"bound-other-admin", "delegation.outside_bounds", func(r *port.BoardDelegationRequest) { r.Bounds[0].Actor.ID = "admin-2" }},
		{"bound-not-yet", "delegation.outside_bounds", func(r *port.BoardDelegationRequest) { r.Bounds[0].NotBefore = 1001 }},
		{"bound-expired", "delegation.outside_bounds", func(r *port.BoardDelegationRequest) { r.Bounds[0].ExpiresAt = 1000 }},
		{"bound-window-short", "delegation.outside_bounds", func(r *port.BoardDelegationRequest) { r.Bounds[0].ExpiresAt = 1700 }},
		{"bound-window-late", "delegation.outside_bounds", func(r *port.BoardDelegationRequest) {
			r.Bounds[0].NotBefore = 901
			r.Candidate.Grants[0].NotBefore = 900
		}},
		{"candidate-starts-past", "delegation.outside_bounds", func(r *port.BoardDelegationRequest) { r.Candidate.Grants[0].NotBefore = 999 }},
		{"candidate-strength-downgrade", "delegation.outside_bounds", func(r *port.BoardDelegationRequest) { r.Candidate.Grants[0].RequiredStrength = "password" }},
		{"bound-other-action", "delegation.outside_bounds", func(r *port.BoardDelegationRequest) { r.Bounds[0].Action = "community.post.delete" }},
		{"bound-other-board", "delegation.outside_bounds", func(r *port.BoardDelegationRequest) { r.Bounds[0].BoardID = "board-B" }},
		{"no-bounds", "delegation.outside_bounds", func(r *port.BoardDelegationRequest) { r.Bounds = []port.BoardDelegationBound{} }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := delegationFixture()
			c.edit(&r)
			before, _ := json.Marshal(r)
			d, err := (BoardDelegationPolicy{}).DecideBoardDelegation(r)
			if err != nil || d.Validate() != nil || d.Effect != "deny" || d.Reason != c.want {
				t.Fatalf("want %s got %+v error %v", c.want, d, err)
			}
			if len(d.Witnesses) != 0 || len(d.Obligations) != 0 {
				t.Fatal("deny leaked witnesses or obligations")
			}
			after, _ := json.Marshal(r)
			if !bytes.Equal(before, after) {
				t.Fatal("input mutated")
			}
		})
	}
}

func TestBoardDelegationBatchIsAtomic(t *testing.T) {
	r := delegationFixture()
	r.Candidate.Grants = append(r.Candidate.Grants, port.BoardGrantAtom{
		Action: "community.post.delete", BoardID: "board-B", NotBefore: 1100, ExpiresAt: 1800, RequiredStrength: "mfa"})
	d, err := (BoardDelegationPolicy{}).DecideBoardDelegation(r)
	if err != nil || d.Effect != "deny" || d.Reason != "delegation.outside_bounds" || len(d.Witnesses) != 0 {
		t.Fatalf("partial batch allowed: %+v %v", d, err)
	}
	// Covering the second atom with an exact bound allows the whole batch in order.
	r.Bounds = append(r.Bounds, port.BoardDelegationBound{
		Action: "community.post.delete", BoardID: "board-B", NotBefore: 900, ExpiresAt: 2000,
		RequiredStrength: "password", ID: "bound-2", Actor: port.PrincipalRef{Kind: "admin", ID: "admin-1"},
		Status: "active", Delegable: true})
	d, err = (BoardDelegationPolicy{}).DecideBoardDelegation(r)
	if err != nil || d.Effect != "allow" ||
		!reflect.DeepEqual(d.Witnesses, []port.BoardDelegationWitness{{CandidateIndex: 0, BoundID: "bound-1"}, {CandidateIndex: 1, BoundID: "bound-2"}}) {
		t.Fatalf("batch witnesses: %+v %v", d, err)
	}
}

func TestBoardDelegationMalformedTypedInput(t *testing.T) {
	for _, change := range []func(*port.BoardDelegationRequest){
		func(r *port.BoardDelegationRequest) { r.Contract = "" },
		func(r *port.BoardDelegationRequest) { r.Principal.Actor.Kind = "root" },
		func(r *port.BoardDelegationRequest) { r.EvaluatedAt = -1 },
		func(r *port.BoardDelegationRequest) { r.Management.NotBefore, r.Management.ExpiresAt = 2000, 900 },
		func(r *port.BoardDelegationRequest) { r.Bounds[0].NotBefore, r.Bounds[0].ExpiresAt = 2000, 900 },
		func(r *port.BoardDelegationRequest) {
			r.Candidate.Grants[0].NotBefore, r.Candidate.Grants[0].ExpiresAt = 1800, 1100
		},
		func(r *port.BoardDelegationRequest) {
			r.Boards = append(r.Boards, port.BoardDelegationBoard{Kind: "community.board", ID: "board-A", Status: "archived"})
		},
		func(r *port.BoardDelegationRequest) { r.Bounds = append(r.Bounds, r.Bounds[0]) },
		func(r *port.BoardDelegationRequest) {
			r.Candidate.Grants = append(r.Candidate.Grants, r.Candidate.Grants[0])
		},
		func(r *port.BoardDelegationRequest) { r.Bounds = nil },
		func(r *port.BoardDelegationRequest) { r.Boards = nil },
		func(r *port.BoardDelegationRequest) { r.Recipient.Status = "archived" },
	} {
		r := delegationFixture()
		change(&r)
		d, err := (BoardDelegationPolicy{}).DecideBoardDelegation(r)
		if !errors.Is(err, port.ErrDelegationInputInvalid) || d.Effect == "allow" {
			t.Fatalf("invalid request authorized: %+v %v", d, err)
		}
	}
	r := delegationFixture()
	r.Contract = "campusos.board-delegation/v2"
	if _, err := (BoardDelegationPolicy{}).DecideBoardDelegation(r); !errors.Is(err, port.ErrDelegationContractUnsupported) {
		t.Fatalf("unknown contract: %v", err)
	}
}

func TestBoardDelegationDecisionBindingAndCurrentFacts(t *testing.T) {
	type change func(*port.BoardDelegationRequest, *port.BoardDelegationDecision, *port.BoardDelegationRequest)
	cases := []struct {
		name  string
		edit  change
		allow bool
	}{
		{"unchanged", func(_ *port.BoardDelegationRequest, _ *port.BoardDelegationDecision, _ *port.BoardDelegationRequest) {
		}, true},
		{"time-forward", func(_ *port.BoardDelegationRequest, _ *port.BoardDelegationDecision, c *port.BoardDelegationRequest) {
			c.EvaluatedAt++
		}, true},
		{"clock-backward", func(_ *port.BoardDelegationRequest, _ *port.BoardDelegationDecision, c *port.BoardDelegationRequest) {
			c.EvaluatedAt--
		}, false},
		{"clock-expired", func(_ *port.BoardDelegationRequest, _ *port.BoardDelegationDecision, c *port.BoardDelegationRequest) {
			c.EvaluatedAt = 2000
		}, false},
		{"facts-version", func(_ *port.BoardDelegationRequest, _ *port.BoardDelegationDecision, c *port.BoardDelegationRequest) {
			c.FactsVersion = "facts-2"
		}, false},
		{"request-id", func(_ *port.BoardDelegationRequest, _ *port.BoardDelegationDecision, c *port.BoardDelegationRequest) {
			c.RequestID = "request-2"
		}, false},
		{"bound-revoked", func(_ *port.BoardDelegationRequest, _ *port.BoardDelegationDecision, c *port.BoardDelegationRequest) {
			c.Bounds[0].Status = "revoked"
		}, false},
		{"management-suspended", func(_ *port.BoardDelegationRequest, _ *port.BoardDelegationDecision, c *port.BoardDelegationRequest) {
			c.Management.Status = "suspended"
		}, false},
		{"recipient-suspended", func(_ *port.BoardDelegationRequest, _ *port.BoardDelegationDecision, c *port.BoardDelegationRequest) {
			c.Recipient.Status = "suspended"
		}, false},
		{"board-archived", func(_ *port.BoardDelegationRequest, _ *port.BoardDelegationDecision, c *port.BoardDelegationRequest) {
			c.Boards[0].Status = "archived"
		}, false},
		{"lost-mfa", func(_ *port.BoardDelegationRequest, _ *port.BoardDelegationDecision, c *port.BoardDelegationRequest) {
			c.Principal.AuthenticationStrength = "password"
		}, false},
		{"credential-changed", func(_ *port.BoardDelegationRequest, _ *port.BoardDelegationDecision, c *port.BoardDelegationRequest) {
			c.Principal.CredentialID = "credential-2"
		}, false},
		{"candidate-changed", func(_ *port.BoardDelegationRequest, _ *port.BoardDelegationDecision, c *port.BoardDelegationRequest) {
			c.Candidate.Recipient.ID = "user-2"
		}, false},
		{"forged-allow-on-deny", func(i *port.BoardDelegationRequest, _ *port.BoardDelegationDecision, c *port.BoardDelegationRequest) {
			i.Bounds[0].Status = "revoked"
			c.Bounds[0].Status = "revoked"
		}, false},
		{"wrong-decision-request", func(_ *port.BoardDelegationRequest, d *port.BoardDelegationDecision, _ *port.BoardDelegationRequest) {
			d.RequestID = "request-2"
		}, false},
		{"missing-audit", func(_ *port.BoardDelegationRequest, d *port.BoardDelegationDecision, _ *port.BoardDelegationRequest) {
			d.Obligations = d.Obligations[:1]
		}, false},
		{"wrong-witness", func(_ *port.BoardDelegationRequest, d *port.BoardDelegationDecision, _ *port.BoardDelegationRequest) {
			d.Witnesses[0].BoundID = "invented"
		}, false},
		{"missing-witness", func(_ *port.BoardDelegationRequest, d *port.BoardDelegationDecision, _ *port.BoardDelegationRequest) {
			d.Witnesses = []port.BoardDelegationWitness{}
		}, false},
		{"duplicate-witness", func(_ *port.BoardDelegationRequest, d *port.BoardDelegationDecision, _ *port.BoardDelegationRequest) {
			d.Witnesses = append(d.Witnesses, d.Witnesses[0])
		}, false},
		{"witness-reordered-extra-bound-still-allow", func(_ *port.BoardDelegationRequest, _ *port.BoardDelegationDecision, c *port.BoardDelegationRequest) {
			extra := c.Bounds[0]
			extra.ID = "zz-unused"
			extra.NotBefore = 950
			c.Bounds = append(c.Bounds, extra)
		}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			i := delegationFixture()
			d, err := (BoardDelegationPolicy{}).DecideBoardDelegation(i)
			if err != nil {
				t.Fatal(err)
			}
			current := cloneDelegationRequest(i)
			c.edit(&i, &d, &current)
			err = CheckBoardDelegationDecision(i, d, current)
			if c.allow && err != nil || !c.allow && err == nil {
				t.Fatalf("allow=%v error=%v", c.allow, err)
			}
			if !c.allow {
				mismatch := errors.Is(err, port.ErrDelegationDecisionMismatch)
				changed := errors.Is(err, port.ErrDelegationFactsChanged)
				var deny *port.DelegationDenyError
				if !mismatch && !changed && !errors.As(err, &deny) {
					t.Fatalf("unexpected error kind: %v", err)
				}
			}
		})
	}
}

func TestBoardDelegationDenyDecisionConsumption(t *testing.T) {
	r := delegationFixture()
	r.Bounds[0].Status = "revoked"
	d, err := (BoardDelegationPolicy{}).DecideBoardDelegation(r)
	if err != nil || d.Effect != "deny" {
		t.Fatal(err)
	}
	err = CheckBoardDelegationDecision(r, d, cloneDelegationRequest(r))
	var deny *port.DelegationDenyError
	if !errors.As(err, &deny) || deny.Reason != "delegation.outside_bounds" {
		t.Fatalf("deny reason lost: %v", err)
	}
	// A tampered deny decision is a mismatch, not a reclassified deny.
	d.Reason = "delegation.actor_denied"
	if err = CheckBoardDelegationDecision(r, d, cloneDelegationRequest(r)); !errors.Is(err, port.ErrDelegationDecisionMismatch) {
		t.Fatalf("tampered deny: %v", err)
	}
}

func TestBoardDelegationParallelWithoutCache(t *testing.T) {
	var evaluator port.BoardDelegationPolicy = BoardDelegationPolicy{}
	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r := delegationFixture()
			r.RequestID = fmt.Sprintf("request-%d", i)
			first, err := evaluator.DecideBoardDelegation(r)
			if err != nil {
				t.Error(err)
				return
			}
			first.Witnesses[0].BoundID = "mutated"
			second, err := evaluator.DecideBoardDelegation(r)
			if err != nil || second.Validate() != nil || second.Witnesses[0].BoundID != "bound-1" ||
				CheckBoardDelegationDecision(r, second, cloneDelegationRequest(r)) != nil {
				t.Errorf("shared decision state: %v", err)
			}
			r.Bounds[0].Status = "revoked"
			denied, err := evaluator.DecideBoardDelegation(r)
			if err != nil || denied.Effect != "deny" {
				t.Errorf("cached bound authorized: %+v %v", denied, err)
			}
		}(i)
	}
	wg.Wait()
}
