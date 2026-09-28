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

func resourcePrincipal(kind, id, strength string) port.PrincipalContext {
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

func resourcePolicyFixture(action string) port.ResourcePolicyRequest {
	r := port.ResourcePolicyRequest{
		Contract: port.ResourcePolicyContract, RequestID: "resource-request", Action: action,
		PolicyVersion: "policy-1", Principal: resourcePrincipal("user", "u1", "mfa"),
		EvaluatedAtMS: 1000, Grants: []port.ResourcePolicyGrant{},
		Facts: port.ResourcePolicyFacts{Kind: "thread", ID: "t1", OwnerID: "u1", BoardID: "b1",
			Status: "published", PublicationStatus: "published", Version: "r1"},
	}
	switch action {
	case "community.thread.take_down", "community.post.delete":
		r.Grants = []port.ResourcePolicyGrant{{SubjectKind: "user", SubjectID: "u1", Action: action,
			BoardID: "b1", ExpiresAtMS: 2000, Status: "active", RequiredStrength: "mfa"}}
		if action == "community.post.delete" {
			r.Facts.Kind, r.Facts.ID, r.Facts.OwnerID = "post", "p1", "u2"
		}
	case "personal.document.read":
		r.Facts = port.ResourcePolicyFacts{Kind: "document", ID: "d1", OwnerID: "u1", Status: "active", Version: "r1"}
	case "knowledge.source.read":
		visible := true
		r.Facts = port.ResourcePolicyFacts{Kind: "knowledge_source", ID: "s1", CollectionID: "public-a",
			Status: "published", PublicationStatus: "published", OriginVisible: &visible, Version: "r1"}
	}
	return r
}

func cloneResourceRequest(r port.ResourcePolicyRequest) port.ResourcePolicyRequest {
	r.Grants = append([]port.ResourcePolicyGrant{}, r.Grants...)
	if r.Facts.OriginVisible != nil {
		v := *r.Facts.OriginVisible
		r.Facts.OriginVisible = &v
	}
	if r.Principal.EffectiveSubject != nil {
		v := *r.Principal.EffectiveSubject
		r.Principal.EffectiveSubject = &v
	}
	return r
}

func TestResourcePolicyNamedActions(t *testing.T) {
	type mutation func(*port.ResourcePolicyRequest)
	cases := []struct {
		name, action, want string
		edit               mutation
	}{
		{"author", "community.thread.edit", "ALLOW", nil},
		{"other-author", "community.thread.edit", "policy.scope_denied", func(r *port.ResourcePolicyRequest) { r.Principal.Actor.ID = "u2" }},
		{"admin-same-id", "community.thread.edit", "policy.principal_wrong_domain", func(r *port.ResourcePolicyRequest) { r.Principal = resourcePrincipal("admin", "u1", "mfa") }},
		{"edit-draft", "community.thread.edit", "ALLOW", func(r *port.ResourcePolicyRequest) { r.Facts.Status, r.Facts.PublicationStatus = "draft", "draft" }},
		{"edit-private", "community.thread.edit", "ALLOW", func(r *port.ResourcePolicyRequest) { r.Facts.Status, r.Facts.PublicationStatus = "private", "private" }},
		{"edit-no-owner", "community.thread.edit", "policy.resource_unavailable", func(r *port.ResourcePolicyRequest) { r.Facts.OwnerID = "" }},
		{"edit-no-board", "community.thread.edit", "policy.resource_unavailable", func(r *port.ResourcePolicyRequest) { r.Facts.BoardID = "" }},
		{"edit-no-publication", "community.thread.edit", "policy.resource_unavailable", func(r *port.ResourcePolicyRequest) { r.Facts.PublicationStatus = "" }},
		{"take-down-granted", "community.thread.take_down", "ALLOW", nil},
		{"take-down-owner-no-grant", "community.thread.take_down", "policy.scope_denied", func(r *port.ResourcePolicyRequest) { r.Grants = []port.ResourcePolicyGrant{} }},
		{"take-down-wrong-board", "community.thread.take_down", "policy.scope_denied", func(r *port.ResourcePolicyRequest) { r.Facts.BoardID = "b2" }},
		{"take-down-wrong-action", "community.thread.take_down", "policy.scope_denied", func(r *port.ResourcePolicyRequest) { r.Grants[0].Action = "community.post.delete" }},
		{"take-down-other-subject", "community.thread.take_down", "policy.scope_denied", func(r *port.ResourcePolicyRequest) { r.Grants[0].SubjectID = "u2" }},
		{"take-down-other-domain-same-id", "community.thread.take_down", "policy.scope_denied", func(r *port.ResourcePolicyRequest) { r.Grants[0].SubjectKind = "admin" }},
		{"take-down-expired", "community.thread.take_down", "policy.scope_denied", func(r *port.ResourcePolicyRequest) { r.EvaluatedAtMS = 2000 }},
		{"take-down-before-expiry", "community.thread.take_down", "ALLOW", func(r *port.ResourcePolicyRequest) { r.EvaluatedAtMS = 1999 }},
		{"take-down-revoked", "community.thread.take_down", "policy.scope_denied", func(r *port.ResourcePolicyRequest) { r.Grants[0].Status = "revoked" }},
		{"take-down-needs-mfa", "community.thread.take_down", "policy.scope_denied", func(r *port.ResourcePolicyRequest) { r.Principal.AuthenticationStrength = "password" }},
		{"take-down-password-grant", "community.thread.take_down", "ALLOW", func(r *port.ResourcePolicyRequest) {
			r.Principal.AuthenticationStrength = "password"
			r.Grants[0].RequiredStrength = "password"
		}},
		{"take-down-admin-granted", "community.thread.take_down", "ALLOW", func(r *port.ResourcePolicyRequest) {
			r.Principal = resourcePrincipal("admin", "a1", "mfa")
			r.Grants[0].SubjectKind, r.Grants[0].SubjectID = "admin", "a1"
		}},
		{"take-down-admin-always-mfa", "community.thread.take_down", "policy.scope_denied", func(r *port.ResourcePolicyRequest) {
			r.Principal = resourcePrincipal("admin", "a1", "password")
			r.Grants[0].SubjectKind, r.Grants[0].SubjectID, r.Grants[0].RequiredStrength = "admin", "a1", "password"
		}},
		{"delete-granted", "community.post.delete", "ALLOW", nil},
		{"delete-owner-no-grant", "community.post.delete", "policy.scope_denied", func(r *port.ResourcePolicyRequest) { r.Facts.OwnerID = "u1"; r.Grants = []port.ResourcePolicyGrant{} }},
		{"delete-no-owner", "community.post.delete", "policy.resource_unavailable", func(r *port.ResourcePolicyRequest) { r.Facts.OwnerID = "" }},
		{"delete-no-board", "community.post.delete", "policy.resource_unavailable", func(r *port.ResourcePolicyRequest) { r.Facts.BoardID = "" }},
		{"delete-no-publication", "community.post.delete", "policy.resource_unavailable", func(r *port.ResourcePolicyRequest) { r.Facts.PublicationStatus = "" }},
		{"document-owner", "personal.document.read", "ALLOW", nil},
		{"document-other", "personal.document.read", "policy.scope_denied", func(r *port.ResourcePolicyRequest) { r.Principal.Actor.ID = "u2" }},
		{"document-private-status", "personal.document.read", "policy.scope_denied", func(r *port.ResourcePolicyRequest) { r.Facts.Status = "private" }},
		{"document-missing-owner", "personal.document.read", "policy.resource_unavailable", func(r *port.ResourcePolicyRequest) { r.Facts.OwnerID = "" }},
		{"document-admin-same-id", "personal.document.read", "policy.principal_wrong_domain", func(r *port.ResourcePolicyRequest) { r.Principal = resourcePrincipal("admin", "u1", "mfa") }},
		{"source-public", "knowledge.source.read", "ALLOW", func(r *port.ResourcePolicyRequest) { r.Principal = resourcePrincipal("anonymous", "", "none") }},
		{"source-private", "knowledge.source.read", "policy.scope_denied", func(r *port.ResourcePolicyRequest) { r.Facts.PublicationStatus = "private" }},
		{"source-draft", "knowledge.source.read", "policy.scope_denied", func(r *port.ResourcePolicyRequest) { r.Facts.Status = "draft" }},
		{"source-origin-hidden", "knowledge.source.read", "policy.scope_denied", func(r *port.ResourcePolicyRequest) { *r.Facts.OriginVisible = false }},
		{"source-no-origin", "knowledge.source.read", "policy.resource_unavailable", func(r *port.ResourcePolicyRequest) { r.Facts.OriginVisible = nil }},
		{"source-no-collection", "knowledge.source.read", "policy.resource_unavailable", func(r *port.ResourcePolicyRequest) { r.Facts.CollectionID = "" }},
		{"source-no-publication", "knowledge.source.read", "policy.resource_unavailable", func(r *port.ResourcePolicyRequest) { r.Facts.PublicationStatus = "" }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := resourcePolicyFixture(c.action)
			if c.edit != nil {
				c.edit(&r)
			}
			before, _ := json.Marshal(r)
			d, err := (ResourcePolicy{}).DecideResource(r)
			if err != nil || d.Validate() != nil || d.Reason != c.want {
				t.Fatalf("want %s got %+v error %v", c.want, d, err)
			}
			if (d.Effect == "allow") != (c.want == "ALLOW") {
				t.Fatal("effect disagrees with reason")
			}
			wantObligations := []string{}
			if d.Effect == "allow" {
				wantObligations = []string{"recheck_facts"}
				if c.action != "personal.document.read" && c.action != "knowledge.source.read" {
					wantObligations = append(wantObligations, "required_audit")
				}
			}
			if !reflect.DeepEqual(d.Obligations, wantObligations) || d.RequestID != r.RequestID || d.FactsVersion != r.Facts.Version || d.PolicyVersion != r.PolicyVersion {
				t.Fatal("wrong decision binding or obligations")
			}
			after, _ := json.Marshal(r)
			if !bytes.Equal(before, after) {
				t.Fatal("input mutated")
			}
		})
	}
}

func TestResourcePolicyDomainsStatesAndNoDelegationPrivilege(t *testing.T) {
	principals := principalPolicyFixtures(t)
	for _, action := range []string{"community.thread.edit", "community.thread.take_down", "community.post.delete", "personal.document.read", "knowledge.source.read"} {
		for _, p := range principals {
			for _, status := range []string{"archived", "deleted"} {
				r := resourcePolicyFixture(action)
				r.Principal, r.Facts.Status = p, status
				d, err := (ResourcePolicy{}).DecideResource(r)
				if err != nil || d.Reason != "policy.resource_unavailable" {
					t.Fatalf("%s %s %s: %+v %v", action, p.Actor.Kind, status, d, err)
				}
			}
			r := resourcePolicyFixture(action)
			r.Principal = p
			if p.Actor.ID != "" {
				r.Facts.OwnerID = p.Actor.ID
			}
			if p.EffectiveSubject != nil {
				r.Facts.OwnerID = p.EffectiveSubject.ID
			}
			d, err := (ResourcePolicy{}).DecideResource(r)
			if err != nil {
				t.Fatal(err)
			}
			if action == "knowledge.source.read" {
				if d.Effect != "allow" {
					t.Fatalf("public source denied for %s", p.Actor.Kind)
				}
			} else if p.Actor.Kind != "user" && (p.Actor.Kind != "admin" || action == "community.thread.edit" || action == "personal.document.read") {
				if d.Reason != "policy.principal_wrong_domain" {
					t.Fatalf("domain %s acquired %s: %+v", p.Actor.Kind, action, d)
				}
			}
		}
		r := resourcePolicyFixture(action)
		if r.Facts.Kind == "document" {
			r.Facts.Kind = "thread"
		} else {
			r.Facts.Kind = "document"
		}
		d, err := (ResourcePolicy{}).DecideResource(r)
		if err != nil || d.Reason != "policy.resource_unavailable" {
			t.Fatalf("wrong kind: %+v %v", d, err)
		}
	}
}

func TestResourcePolicyGrantMustBeSingleWitness(t *testing.T) {
	for _, action := range []string{"community.thread.take_down", "community.post.delete"} {
		r := resourcePolicyFixture(action)
		valid := r.Grants[0]
		r.Grants = nil
		for _, change := range []func(*port.ResourcePolicyGrant){
			func(g *port.ResourcePolicyGrant) { g.SubjectKind = "admin" },
			func(g *port.ResourcePolicyGrant) { g.SubjectID = "other" },
			func(g *port.ResourcePolicyGrant) {
				if g.Action == "community.thread.take_down" {
					g.Action = "community.post.delete"
				} else {
					g.Action = "community.thread.take_down"
				}
			},
			func(g *port.ResourcePolicyGrant) { g.BoardID = "other" },
			func(g *port.ResourcePolicyGrant) { g.ExpiresAtMS = r.EvaluatedAtMS },
			func(g *port.ResourcePolicyGrant) { g.Status = "revoked" },
		} {
			g := valid
			change(&g)
			r.Grants = append(r.Grants, g)
		}
		d, err := (ResourcePolicy{}).DecideResource(r)
		if err != nil || d.Reason != "policy.scope_denied" {
			t.Fatalf("partial grants combined: %+v %v", d, err)
		}
		r.Grants = append(r.Grants, valid)
		d, err = (ResourcePolicy{}).DecideResource(r)
		if err != nil || d.Effect != "allow" {
			t.Fatalf("valid later grant ignored: %+v %v", d, err)
		}
		// A password grant on another board cannot lend its authentication
		// requirement to a matching grant that requires MFA.
		r.Principal.AuthenticationStrength = "password"
		r.Grants = []port.ResourcePolicyGrant{valid, valid}
		r.Grants[1].BoardID, r.Grants[1].RequiredStrength = "other", "password"
		d, err = (ResourcePolicy{}).DecideResource(r)
		if err != nil || d.Reason != "policy.scope_denied" {
			t.Fatalf("strength joined across grants: %+v %v", d, err)
		}
	}
}

func TestResourcePolicyMalformedTypedInput(t *testing.T) {
	for _, change := range []func(*port.ResourcePolicyRequest){
		func(r *port.ResourcePolicyRequest) { r.Contract = "unsupported" },
		func(r *port.ResourcePolicyRequest) { r.Principal.Actor.Kind = "root" },
		func(r *port.ResourcePolicyRequest) { r.Facts.Status = "unknown" },
		func(r *port.ResourcePolicyRequest) { r.PolicyVersion = "" },
		func(r *port.ResourcePolicyRequest) { r.Grants[0].Status = "unknown" },
		func(r *port.ResourcePolicyRequest) { r.EvaluatedAtMS = -1 },
	} {
		r := resourcePolicyFixture("community.thread.take_down")
		change(&r)
		d, err := (ResourcePolicy{}).DecideResource(r)
		if !errors.Is(err, port.ErrResourcePolicyInvalid) || d.Effect == "allow" {
			t.Fatalf("invalid request authorized: %+v %v", d, err)
		}
	}
	r := resourcePolicyFixture("community.thread.edit")
	r.Action = "community.thread.view_all"
	if _, err := (ResourcePolicy{}).DecideResource(r); !errors.Is(err, port.ErrResourcePolicyUnknownAction) {
		t.Fatalf("unknown action: %v", err)
	}
}

func TestResourcePolicyDecisionBindingAndCurrentFacts(t *testing.T) {
	type change func(*port.ResourcePolicyRequest, *port.ResourcePolicyDecision, *port.ResourcePolicyRequest)
	cases := []struct {
		name  string
		edit  change
		allow bool
	}{
		{"unchanged", func(_ *port.ResourcePolicyRequest, _ *port.ResourcePolicyDecision, _ *port.ResourcePolicyRequest) {}, true},
		{"time-forward", func(_ *port.ResourcePolicyRequest, _ *port.ResourcePolicyDecision, c *port.ResourcePolicyRequest) {
			c.EvaluatedAtMS++
		}, true},
		{"clock-backward", func(_ *port.ResourcePolicyRequest, _ *port.ResourcePolicyDecision, c *port.ResourcePolicyRequest) {
			c.EvaluatedAtMS--
		}, false},
		{"expires-at-consumption", func(_ *port.ResourcePolicyRequest, _ *port.ResourcePolicyDecision, c *port.ResourcePolicyRequest) {
			c.EvaluatedAtMS = 2000
		}, false},
		{"initial-deny-forged-allow", func(i *port.ResourcePolicyRequest, _ *port.ResourcePolicyDecision, c *port.ResourcePolicyRequest) {
			i.Grants = []port.ResourcePolicyGrant{}
			c.Grants = []port.ResourcePolicyGrant{}
		}, false},
		{"invalid-initial", func(i *port.ResourcePolicyRequest, _ *port.ResourcePolicyDecision, _ *port.ResourcePolicyRequest) {
			i.Principal.Actor.Kind = "root"
		}, false},
		{"invalid-current", func(_ *port.ResourcePolicyRequest, _ *port.ResourcePolicyDecision, c *port.ResourcePolicyRequest) {
			c.Facts.Status = "unknown"
		}, false},
		{"decision-request", func(_ *port.ResourcePolicyRequest, d *port.ResourcePolicyDecision, _ *port.ResourcePolicyRequest) {
			d.RequestID = "other"
		}, false},
		{"decision-reason", func(_ *port.ResourcePolicyRequest, d *port.ResourcePolicyDecision, _ *port.ResourcePolicyRequest) {
			d.Reason = "forged"
		}, false},
		{"decision-deny", func(_ *port.ResourcePolicyRequest, d *port.ResourcePolicyDecision, _ *port.ResourcePolicyRequest) {
			d.Effect = "deny"
			d.Reason = "policy.scope_denied"
			d.Obligations = []string{}
		}, false},
		{"decision-facts-version", func(_ *port.ResourcePolicyRequest, d *port.ResourcePolicyDecision, _ *port.ResourcePolicyRequest) {
			d.FactsVersion = "other"
		}, false},
		{"decision-policy-version", func(_ *port.ResourcePolicyRequest, d *port.ResourcePolicyDecision, _ *port.ResourcePolicyRequest) {
			d.PolicyVersion = "other"
		}, false},
		{"missing-recheck", func(_ *port.ResourcePolicyRequest, d *port.ResourcePolicyDecision, _ *port.ResourcePolicyRequest) {
			d.Obligations = []string{"required_audit"}
		}, false},
		{"missing-audit", func(_ *port.ResourcePolicyRequest, d *port.ResourcePolicyDecision, _ *port.ResourcePolicyRequest) {
			d.Obligations = []string{"recheck_facts"}
		}, false},
		{"reordered-obligations", func(_ *port.ResourcePolicyRequest, d *port.ResourcePolicyDecision, _ *port.ResourcePolicyRequest) {
			d.Obligations = []string{"required_audit", "recheck_facts"}
		}, false},
		{"request-id", func(_ *port.ResourcePolicyRequest, _ *port.ResourcePolicyDecision, c *port.ResourcePolicyRequest) {
			c.RequestID = "other"
		}, false},
		{"action", func(_ *port.ResourcePolicyRequest, _ *port.ResourcePolicyDecision, c *port.ResourcePolicyRequest) {
			c.Action = "community.thread.edit"
		}, false},
		{"principal-domain", func(_ *port.ResourcePolicyRequest, _ *port.ResourcePolicyDecision, c *port.ResourcePolicyRequest) {
			c.Principal = resourcePrincipal("admin", "u1", "mfa")
		}, false},
		{"principal-id", func(_ *port.ResourcePolicyRequest, _ *port.ResourcePolicyDecision, c *port.ResourcePolicyRequest) {
			c.Principal.Actor.ID = "other"
		}, false},
		{"principal-credential", func(_ *port.ResourcePolicyRequest, _ *port.ResourcePolicyDecision, c *port.ResourcePolicyRequest) {
			c.Principal.CredentialID = "another-current-credential"
		}, false},
		{"principal-strength", func(_ *port.ResourcePolicyRequest, _ *port.ResourcePolicyDecision, c *port.ResourcePolicyRequest) {
			c.Principal.AuthenticationStrength = "password"
		}, false},
		{"same-version-other-resource", func(_ *port.ResourcePolicyRequest, _ *port.ResourcePolicyDecision, c *port.ResourcePolicyRequest) {
			c.Facts.ID = "other"
		}, false},
		{"facts-version", func(_ *port.ResourcePolicyRequest, _ *port.ResourcePolicyDecision, c *port.ResourcePolicyRequest) {
			c.Facts.Version = "r2"
		}, false},
		{"owner-without-version", func(_ *port.ResourcePolicyRequest, _ *port.ResourcePolicyDecision, c *port.ResourcePolicyRequest) {
			c.Facts.OwnerID = "other"
		}, false},
		{"board-without-version", func(_ *port.ResourcePolicyRequest, _ *port.ResourcePolicyDecision, c *port.ResourcePolicyRequest) {
			c.Facts.BoardID = "other"
		}, false},
		{"status-without-version-still-allow", func(_ *port.ResourcePolicyRequest, _ *port.ResourcePolicyDecision, c *port.ResourcePolicyRequest) {
			c.Facts.Status = "private"
		}, false},
		{"publication-without-version-still-allow", func(_ *port.ResourcePolicyRequest, _ *port.ResourcePolicyDecision, c *port.ResourcePolicyRequest) {
			c.Facts.PublicationStatus = "private"
		}, false},
		{"policy-version", func(_ *port.ResourcePolicyRequest, _ *port.ResourcePolicyDecision, c *port.ResourcePolicyRequest) {
			c.PolicyVersion = "policy-2"
		}, false},
		{"grant-revoked", func(_ *port.ResourcePolicyRequest, _ *port.ResourcePolicyDecision, c *port.ResourcePolicyRequest) {
			c.Grants[0].Status = "revoked"
		}, false},
		{"grant-removed", func(_ *port.ResourcePolicyRequest, _ *port.ResourcePolicyDecision, c *port.ResourcePolicyRequest) {
			c.Grants = []port.ResourcePolicyGrant{}
		}, false},
		{"grant-extended-still-allow", func(_ *port.ResourcePolicyRequest, _ *port.ResourcePolicyDecision, c *port.ResourcePolicyRequest) {
			c.Grants[0].ExpiresAtMS++
		}, false},
		{"grant-strength-changed-still-allow", func(_ *port.ResourcePolicyRequest, _ *port.ResourcePolicyDecision, c *port.ResourcePolicyRequest) {
			c.Grants[0].RequiredStrength = "password"
		}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			i := resourcePolicyFixture("community.thread.take_down")
			d, err := (ResourcePolicy{}).DecideResource(i)
			if err != nil {
				t.Fatal(err)
			}
			current := cloneResourceRequest(i)
			c.edit(&i, &d, &current)
			err = CheckResourceDecision(i, d, current)
			if c.allow && err != nil || !c.allow && !errors.Is(err, port.ErrResourcePolicyFactsChanged) {
				t.Fatalf("allow=%v error=%v", c.allow, err)
			}
		})
	}
}

func TestResourcePolicyReadDecisionBindings(t *testing.T) {
	r := resourcePolicyFixture("knowledge.source.read")
	r.Principal = resourcePrincipal("plugin_instance", "plugin1", "workload")
	r.Principal.EffectiveSubject = &port.PrincipalRef{Kind: "user", ID: "u1"}
	r.Principal.DelegationID = "delegation1"
	d, err := (ResourcePolicy{}).DecideResource(r)
	if err != nil {
		t.Fatal(err)
	}
	if err = CheckResourceDecision(r, d, cloneResourceRequest(r)); err != nil {
		t.Fatal(err)
	}
	for _, edit := range []func(*port.ResourcePolicyRequest){
		func(c *port.ResourcePolicyRequest) { c.Principal.EffectiveSubject.ID = "u2" },
		func(c *port.ResourcePolicyRequest) { c.Principal.DelegationID = "delegation2" },
		func(c *port.ResourcePolicyRequest) { c.Principal.EffectiveSubject = nil; c.Principal.DelegationID = "" },
		func(c *port.ResourcePolicyRequest) { c.Facts.CollectionID = "another-collection" },
		func(c *port.ResourcePolicyRequest) { *c.Facts.OriginVisible = false },
	} {
		current := cloneResourceRequest(r)
		edit(&current)
		if err = CheckResourceDecision(r, d, current); !errors.Is(err, port.ErrResourcePolicyFactsChanged) {
			t.Fatalf("read binding lost: %v", err)
		}
	}
	// Grant ordering is part of the conservative snapshot binding, including
	// irrelevant grants. Re-evaluation after any change yields a new decision.
	r = resourcePolicyFixture("community.thread.take_down")
	extra := r.Grants[0]
	extra.BoardID = "other"
	r.Grants = append(r.Grants, extra)
	d, _ = (ResourcePolicy{}).DecideResource(r)
	current := cloneResourceRequest(r)
	current.Grants[0], current.Grants[1] = current.Grants[1], current.Grants[0]
	if err = CheckResourceDecision(r, d, current); !errors.Is(err, port.ErrResourcePolicyFactsChanged) {
		t.Fatalf("reordered grant snapshot accepted: %v", err)
	}
}

func TestResourcePolicyParallelWithoutCache(t *testing.T) {
	var evaluator port.ResourcePolicy = ResourcePolicy{}
	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r := resourcePolicyFixture("community.thread.take_down")
			r.RequestID = fmt.Sprintf("request-%d", i)
			first, err := evaluator.DecideResource(r)
			if err != nil {
				t.Error(err)
				return
			}
			first.Obligations[0] = "mutated"
			second, err := evaluator.DecideResource(r)
			if err != nil || second.Validate() != nil || second.RequestID != r.RequestID || CheckResourceDecision(r, second, cloneResourceRequest(r)) != nil {
				t.Errorf("shared decision state: %v", err)
			}
			r.Grants[0].Status = "revoked"
			denied, err := evaluator.DecideResource(r)
			if err != nil || denied.Effect != "deny" {
				t.Errorf("cached grant authorized: %+v %v", denied, err)
			}
		}(i)
	}
	wg.Wait()
}
