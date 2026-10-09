package policy

import (
	"bytes"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/campusos/CampusOS/internal/modules/core/identity/port"
)

func scopeDelegationFixture() port.ScopeDelegationInput {
	return port.ScopeDelegationInput{
		Contract:   port.ScopeDelegationContract,
		Actor:      port.ScopeDelegationActor{Kind: "admin", ID: "admin-1", AuthenticationStrength: "mfa"},
		Management: port.ScopeDelegationManagement{Action: "plugin.grant.manage", Active: true, ExpiresAtMS: 5000},
		Bound: port.ScopeDelegationBound{
			Action:      "knowledge.source.read",
			Scope:       port.ScopeDelegationScope{Kind: "public_collection", IDs: []string{"public-a", "public-b"}},
			NotBeforeMS: 1000, ExpiresAtMS: 4000, RequiredStrength: "password", Status: "active", Delegable: true,
		},
		Candidate: port.ScopeDelegationCandidate{
			Action:      "knowledge.source.read",
			Scope:       port.ScopeDelegationScope{Kind: "public_collection", IDs: []string{"public-a"}},
			NotBeforeMS: 1500, ExpiresAtMS: 3000, RequiredStrength: "mfa",
			Recipient: port.ScopeDelegationRecipient{Kind: "plugin", ID: "plugin-1"},
		},
		NowMS: 1500, FactsVersion: "rev-1",
	}
}

func cloneScopeDelegationInput(r port.ScopeDelegationInput) port.ScopeDelegationInput {
	r.Bound.Scope.IDs = append([]string{}, r.Bound.Scope.IDs...)
	r.Candidate.Scope.IDs = append([]string{}, r.Candidate.Scope.IDs...)
	return r
}

func scopeDelegationOutcome(err error) string {
	if err == nil {
		return "accepted"
	}
	var deny *port.ScopeDelegationDenyError
	if errors.As(err, &deny) {
		return deny.Reason
	}
	return err.Error()
}

func TestScopeDelegationAcceptedScopes(t *testing.T) {
	r := scopeDelegationFixture()
	before, _ := json.Marshal(r)
	if err := (ScopeDelegationPolicy{}).CheckScopeDelegation(r); err != nil {
		t.Fatal(err)
	}
	if after, _ := json.Marshal(r); !bytes.Equal(before, after) {
		t.Fatal("input mutated")
	}
	// System config keys and an exact HTTPS endpoint follow the same judgement.
	r = scopeDelegationFixture()
	r.Bound.Action = "plugin.config.system.read"
	r.Bound.Scope = port.ScopeDelegationScope{Kind: "system_config", IDs: []string{"display.locale", "display.theme"}}
	r.Candidate.Action = "plugin.config.system.read"
	r.Candidate.Scope = port.ScopeDelegationScope{Kind: "system_config", IDs: []string{"display.locale"}}
	if err := (ScopeDelegationPolicy{}).CheckScopeDelegation(r); err != nil {
		t.Fatal(err)
	}
	r = scopeDelegationFixture()
	r.Management.Action = "integration.grant.manage"
	r.Bound.Action = "integration.webhook.invoke"
	r.Bound.Scope = port.ScopeDelegationScope{Kind: "endpoint", IDs: []string{"https://hooks.example.test", "https://ops.example.test:8443"}}
	r.Candidate.Action = "integration.webhook.invoke"
	r.Candidate.Scope = port.ScopeDelegationScope{Kind: "endpoint", IDs: []string{"https://ops.example.test:8443"}}
	r.Candidate.Recipient.Kind = "integration"
	if err := (ScopeDelegationPolicy{}).CheckScopeDelegation(r); err != nil {
		t.Fatal(err)
	}
}

func TestScopeDelegationDenySequence(t *testing.T) {
	cases := []struct {
		name, want string
		edit       func(*port.ScopeDelegationInput)
	}{
		{"user-actor", "scope.actor_denied", func(r *port.ScopeDelegationInput) { r.Actor.Kind = "user" }},
		{"admin-no-mfa", "scope.actor_denied", func(r *port.ScopeDelegationInput) { r.Actor.AuthenticationStrength = "password" }},
		{"management-wrong-action", "scope.management_denied", func(r *port.ScopeDelegationInput) { r.Management.Action = "identity.role.assign" }},
		{"management-inactive", "scope.management_denied", func(r *port.ScopeDelegationInput) { r.Management.Active = false }},
		{"management-expired", "scope.management_denied", func(r *port.ScopeDelegationInput) { r.Management.ExpiresAtMS = 1500 }},
		{"management-matches-integration-recipient", "scope.management_denied", func(r *port.ScopeDelegationInput) {
			r.Candidate.Recipient.Kind = "integration"
		}},
		{"candidate-private-document", "scope.non_delegable", func(r *port.ScopeDelegationInput) {
			r.Candidate.Action = "personal.document.read"
			r.Candidate.Scope = port.ScopeDelegationScope{Kind: "system_config", IDs: []string{"doc-1"}}
		}},
		{"bound-private-document", "scope.non_delegable", func(r *port.ScopeDelegationInput) {
			r.Bound.Action = "personal.document.read"
			r.Bound.Scope = port.ScopeDelegationScope{Kind: "system_config", IDs: []string{"doc-1"}}
		}},
		{"user-recipient", "scope.recipient_mismatch", func(r *port.ScopeDelegationInput) {
			r.Management.Action = "identity.role.assign"
			r.Candidate.Recipient.Kind = "user"
		}},
		{"bound-suspended", "scope.bound_inactive", func(r *port.ScopeDelegationInput) { r.Bound.Status = "suspended" }},
		{"bound-revoked", "scope.bound_inactive", func(r *port.ScopeDelegationInput) { r.Bound.Status = "revoked" }},
		{"bound-not-delegable", "scope.bound_inactive", func(r *port.ScopeDelegationInput) { r.Bound.Delegable = false }},
		{"bound-not-yet", "scope.bound_inactive", func(r *port.ScopeDelegationInput) { r.Bound.NotBeforeMS = 1501 }},
		{"bound-expired", "scope.bound_inactive", func(r *port.ScopeDelegationInput) { r.Bound.ExpiresAtMS = 1500 }},
		{"wrong-action", "scope.outside_bounds", func(r *port.ScopeDelegationInput) { r.Candidate.Action = "plugin.config.system.read" }},
		{"wrong-scope-kind", "scope.outside_bounds", func(r *port.ScopeDelegationInput) {
			r.Candidate.Scope = port.ScopeDelegationScope{Kind: "system_config", IDs: []string{"public-a"}}
		}},
		{"outside-collection", "scope.outside_bounds", func(r *port.ScopeDelegationInput) { r.Candidate.Scope.IDs = []string{"private-c"} }},
		{"widened-collection", "scope.outside_bounds", func(r *port.ScopeDelegationInput) { r.Candidate.Scope.IDs = append(r.Candidate.Scope.IDs, "private-c") }},
		{"candidate-before-now", "scope.outside_bounds", func(r *port.ScopeDelegationInput) { r.Candidate.NotBeforeMS = 1499 }},
		{"candidate-before-bound", "scope.outside_bounds", func(r *port.ScopeDelegationInput) {
			r.NowMS = 1000
			r.Candidate.NotBeforeMS = 999
		}},
		{"candidate-after-bound", "scope.outside_bounds", func(r *port.ScopeDelegationInput) { r.Candidate.ExpiresAtMS = 4001 }},
		{"candidate-empty-window", "scope.outside_bounds", func(r *port.ScopeDelegationInput) { r.Candidate.ExpiresAtMS = r.Candidate.NotBeforeMS }},
		{"mfa-downgrade", "scope.outside_bounds", func(r *port.ScopeDelegationInput) {
			r.Bound.RequiredStrength = "mfa"
			r.Candidate.RequiredStrength = "password"
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := scopeDelegationFixture()
			c.edit(&r)
			before, _ := json.Marshal(r)
			err := (ScopeDelegationPolicy{}).CheckScopeDelegation(r)
			if got := scopeDelegationOutcome(err); got != c.want {
				t.Fatalf("want %s got %s (%v)", c.want, got, err)
			}
			if after, _ := json.Marshal(r); !bytes.Equal(before, after) {
				t.Fatal("input mutated")
			}
		})
	}
	// Judgement order: actor failures precede management, which precedes
	// non-delegable, recipient, bound and bounds coverage.
	r := scopeDelegationFixture()
	r.Actor.Kind = "user"
	r.Management.Action = "identity.role.assign"
	r.Candidate.Recipient.Kind = "user"
	if got := scopeDelegationOutcome((ScopeDelegationPolicy{}).CheckScopeDelegation(r)); got != "scope.actor_denied" {
		t.Fatalf("order: %s", got)
	}
	r = scopeDelegationFixture()
	r.Management.Action = "identity.role.assign"
	r.Candidate.Recipient.Kind = "user"
	if got := scopeDelegationOutcome((ScopeDelegationPolicy{}).CheckScopeDelegation(r)); got != "scope.recipient_mismatch" {
		t.Fatalf("order: %s", got)
	}
}

func TestScopeDelegationEndpointShapes(t *testing.T) {
	for _, c := range []struct {
		id    string
		valid bool
	}{
		{"https://hooks.example.test", true},
		{"https://hooks.example.test:8443", true},
		{"https://sub.ops-example.test:1", true},
		{"http://hooks.example.test", false},
		{"https://*.example.test", false},
		{"https://localhost", false},
		{"https://localhost.evil.test", false},
		{"https://127.0.0.1", false},
		{"https://127.0.0.1:8443", false},
		{"https://hooks.example.test/path", false},
		{"https://hooks.example.test:0", false},
		{"https://Hooks.example.test", false},
	} {
		t.Run(c.id, func(t *testing.T) {
			r := scopeDelegationFixture()
			r.Management.Action = "integration.grant.manage"
			r.Bound.Action = "integration.webhook.invoke"
			r.Bound.Scope = port.ScopeDelegationScope{Kind: "endpoint", IDs: []string{c.id}}
			r.Candidate.Action = "integration.webhook.invoke"
			r.Candidate.Scope = port.ScopeDelegationScope{Kind: "endpoint", IDs: []string{c.id}}
			r.Candidate.Recipient.Kind = "integration"
			err := (ScopeDelegationPolicy{}).CheckScopeDelegation(r)
			if (err == nil) != c.valid {
				t.Fatalf("endpoint %s: %v", c.id, err)
			}
			if !c.valid && scopeDelegationOutcome(err) != "scope.outside_bounds" {
				t.Fatalf("endpoint %s: %v", c.id, err)
			}
		})
	}
}

func TestScopeDelegationMalformedTypedInput(t *testing.T) {
	for _, change := range []func(*port.ScopeDelegationInput){
		func(r *port.ScopeDelegationInput) { r.Contract = "campusos.scope-delegation/v2" },
		func(r *port.ScopeDelegationInput) { r.Contract = "" },
		func(r *port.ScopeDelegationInput) { r.Actor.Kind = "root" },
		func(r *port.ScopeDelegationInput) { r.NowMS = -1 },
		func(r *port.ScopeDelegationInput) { r.Management.ExpiresAtMS = 0 },
		func(r *port.ScopeDelegationInput) { r.Bound.Scope.IDs = nil },
		func(r *port.ScopeDelegationInput) { r.Bound.Scope.IDs = append(r.Bound.Scope.IDs, "public-a") },
		func(r *port.ScopeDelegationInput) { r.FactsVersion = "" },
	} {
		r := scopeDelegationFixture()
		change(&r)
		err := (ScopeDelegationPolicy{}).CheckScopeDelegation(r)
		if !errors.Is(err, port.ErrScopeInputInvalid) {
			t.Fatalf("invalid input accepted: %v", err)
		}
	}
}

func TestScopeDelegationParallelWithoutCache(t *testing.T) {
	var evaluator port.ScopeDelegationPolicy = ScopeDelegationPolicy{}
	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r := scopeDelegationFixture()
			if err := evaluator.CheckScopeDelegation(r); err != nil {
				t.Error(err)
			}
			r.Bound.Status = "revoked"
			if err := evaluator.CheckScopeDelegation(r); scopeDelegationOutcome(err) != "scope.bound_inactive" {
				t.Errorf("cached bound accepted: %v", err)
			}
		}(i)
	}
	wg.Wait()
}
