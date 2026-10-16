package policy

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sync"
	"testing"

	"github.com/campusos/CampusOS/internal/modules/core/identity/port"
)

func integrationPolicyFixture() port.IntegrationAccessInput {
	return port.IntegrationAccessInput{
		Contract: port.IntegrationAccessContract, RequestID: "request.1", NowMS: 1000,
		Principal: port.PrincipalContext{Contract: port.PrincipalContract, Actor: port.PrincipalRef{Kind: "integration", ID: "same-42"}, Audience: "integration_api", AuthenticationStrength: "credential", CredentialID: "credential.1"},
		Target:    port.IntegrationAccessTarget{Channel: "integration_http", Endpoint: "public.sources", Action: "knowledge.source.read", Resource: port.PolicyResourceRef{Kind: "public_collection", ID: "collection.1"}},
		Facts: port.IntegrationAccessFacts{
			IntegrationID: "same-42", CredentialID: "credential.1", PrincipalStatus: "active", CredentialStatus: "active", CredentialAudience: "integration_api", NotBeforeMS: 100, ExpiresAtMS: 2000, FactsVersion: "facts.1",
			Endpoint: port.IntegrationAccessEndpoint{Channel: "integration_http", ID: "public.sources", Action: "knowledge.source.read", ResourceKind: "public_collection", Enabled: true, IntegrationAllowed: true},
			Rules:    []port.IntegrationAccessRule{{ID: "rule.1", Channel: "integration_http", Endpoint: "public.sources", Action: "knowledge.source.read", ResourceKind: "public_collection", ResourceIDs: []string{"collection.1"}, Status: "active", NotBeforeMS: 100, ExpiresAtMS: 1800}},
		},
	}
}

func TestIntegrationAccessExactRulesNoCrossRuleSplicing(t *testing.T) {
	p := IntegrationAccessPolicy{}
	r := integrationPolicyFixture()
	r.Facts.Rules = append(r.Facts.Rules, r.Facts.Rules[0])
	r.Facts.Rules[0].ID, r.Facts.Rules[1].ID = "rule.z", "rule.a"
	d, err := p.DecideIntegrationAccess(r)
	if err != nil || d.Effect != "allow" || d.RuleID != "rule.a" {
		t.Fatal("deterministic complete witness", d, err)
	}
	r.Facts.Rules[0].Endpoint = "other.endpoint"
	r.Facts.Rules[1].ResourceIDs = []string{"other.collection"}
	d, err = p.DecideIntegrationAccess(r)
	if err != nil || d.Effect != "deny" || d.Reason != "integration.whitelist_denied" || d.RuleID != "" {
		t.Fatal("cross-rule splice accepted", d, err)
	}
	if err := p.CheckIntegrationAccessDecision(r, d, r); err == nil || err.Error() != "integration.whitelist_denied" {
		t.Fatal("denial reused as permission", err)
	}
	for _, channel := range []string{"integration_http", "integration_mcp"} {
		r = integrationPolicyFixture()
		r.Target.Channel, r.Facts.Endpoint.Channel, r.Facts.Rules[0].Channel = channel, channel, channel
		d, err = p.DecideIntegrationAccess(r)
		if err != nil || d.Effect != "allow" {
			t.Fatal("explicit machine channel", channel, err)
		}
	}
}

func TestIntegrationAccessDomainsLifecycleEndpointAndScope(t *testing.T) {
	mutations := []struct {
		name, reason string
		mutate       func(*port.IntegrationAccessInput)
	}{
		{"same_id_user", "actor_denied", func(r *port.IntegrationAccessInput) {
			r.Principal.Actor.Kind = "user"
			r.Principal.Audience = "user"
			r.Principal.AuthenticationStrength = "mfa"
		}},
		{"same_id_admin", "actor_denied", func(r *port.IntegrationAccessInput) {
			r.Principal.Actor.Kind = "admin"
			r.Principal.Audience = "admin"
			r.Principal.AuthenticationStrength = "mfa"
		}},
		{"plugin", "actor_denied", func(r *port.IntegrationAccessInput) {
			r.Principal.Actor.Kind = "plugin_instance"
			r.Principal.Audience = "host_api"
			r.Principal.AuthenticationStrength = "workload"
		}},
		{"worker", "actor_denied", func(r *port.IntegrationAccessInput) {
			r.Principal.Actor.Kind = "worker"
			r.Principal.Audience = "worker"
			r.Principal.AuthenticationStrength = "workload"
		}},
		{"anonymous", "actor_denied", func(r *port.IntegrationAccessInput) {
			r.Principal = port.PrincipalContext{Contract: port.PrincipalContract, Actor: port.PrincipalRef{Kind: "anonymous"}, Audience: "public", AuthenticationStrength: "none"}
		}},
		{"different_actor", "actor_denied", func(r *port.IntegrationAccessInput) { r.Facts.IntegrationID = "other" }},
		{"different_credential", "actor_denied", func(r *port.IntegrationAccessInput) { r.Facts.CredentialID = "other" }},
		{"suspended", "principal_inactive", func(r *port.IntegrationAccessInput) { r.Facts.PrincipalStatus = "suspended" }},
		{"revoked_principal", "principal_inactive", func(r *port.IntegrationAccessInput) { r.Facts.PrincipalStatus = "revoked" }},
		{"revoked_credential", "credential_denied", func(r *port.IntegrationAccessInput) { r.Facts.CredentialStatus = "revoked" }},
		{"credential_purpose", "credential_denied", func(r *port.IntegrationAccessInput) { r.Facts.CredentialAudience = "admin" }},
		{"not_yet_valid", "credential_denied", func(r *port.IntegrationAccessInput) { r.NowMS = 99 }},
		{"exact_expiry", "credential_denied", func(r *port.IntegrationAccessInput) { r.NowMS = 2000 }},
		{"admin_channel", "channel_denied", func(r *port.IntegrationAccessInput) { r.Target.Channel = "admin_http" }},
		{"host_channel", "channel_denied", func(r *port.IntegrationAccessInput) { r.Target.Channel = "host_api" }},
		{"private_resource", "scope_denied", func(r *port.IntegrationAccessInput) { r.Target.Resource.Kind = "personal.document" }},
		{"admin_action", "scope_denied", func(r *port.IntegrationAccessInput) { r.Target.Action = "identity.admin.read" }},
		{"endpoint_disabled", "endpoint_denied", func(r *port.IntegrationAccessInput) { r.Facts.Endpoint.Enabled = false }},
		{"endpoint_not_published", "endpoint_denied", func(r *port.IntegrationAccessInput) { r.Facts.Endpoint.IntegrationAllowed = false }},
		{"endpoint_other_channel", "endpoint_denied", func(r *port.IntegrationAccessInput) { r.Facts.Endpoint.Channel = "integration_mcp" }},
		{"endpoint_other_id", "endpoint_denied", func(r *port.IntegrationAccessInput) { r.Facts.Endpoint.ID = "other" }},
		{"endpoint_other_action", "endpoint_denied", func(r *port.IntegrationAccessInput) { r.Facts.Endpoint.Action = "other" }},
		{"endpoint_other_resource", "endpoint_denied", func(r *port.IntegrationAccessInput) { r.Facts.Endpoint.ResourceKind = "private_collection" }},
		{"empty_whitelist", "whitelist_denied", func(r *port.IntegrationAccessInput) { r.Facts.Rules = []port.IntegrationAccessRule{} }},
		{"other_scope", "whitelist_denied", func(r *port.IntegrationAccessInput) { r.Target.Resource.ID = "other" }},
	}
	for _, tc := range mutations {
		t.Run(tc.name, func(t *testing.T) {
			r := integrationPolicyFixture()
			tc.mutate(&r)
			d, err := (IntegrationAccessPolicy{}).DecideIntegrationAccess(r)
			if err != nil || d.Effect != "deny" || d.Reason != "integration."+tc.reason || d.RuleID != "" {
				t.Fatal("expected closed domain/endpoint judgement", d, err)
			}
		})
	}
}

func TestIntegrationAccessCurrentFactsClockAndForgedDecision(t *testing.T) {
	p, r := IntegrationAccessPolicy{}, integrationPolicyFixture()
	d, err := p.DecideIntegrationAccess(r)
	if err != nil {
		t.Fatal(err)
	}
	mutations := map[string]func(*port.IntegrationAccessInput){
		"request":    func(r *port.IntegrationAccessInput) { r.RequestID = "request.2" },
		"principal":  func(r *port.IntegrationAccessInput) { r.Principal.CredentialID = "credential.2" },
		"version":    func(r *port.IntegrationAccessInput) { r.Facts.FactsVersion = "facts.2" },
		"status":     func(r *port.IntegrationAccessInput) { r.Facts.PrincipalStatus = "suspended" },
		"credential": func(r *port.IntegrationAccessInput) { r.Facts.CredentialStatus = "revoked" },
		"endpoint":   func(r *port.IntegrationAccessInput) { r.Facts.Endpoint.IntegrationAllowed = false },
		"scope": func(r *port.IntegrationAccessInput) {
			r.Facts.Rules[0].ResourceIDs = append(r.Facts.Rules[0].ResourceIDs, "unrelated")
		},
		"earlier_clock": func(r *port.IntegrationAccessInput) { r.NowMS-- },
		"missing_facts": func(r *port.IntegrationAccessInput) { r.Facts.FactsVersion = "" },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			current := r.Clone()
			mutate(&current)
			if err := p.CheckIntegrationAccessDecision(r, d, current); !errors.Is(err, port.ErrIntegrationFactsChanged) {
				t.Fatal("changed snapshot accepted", err)
			}
		})
	}
	current := r.Clone()
	current.NowMS++
	if err := p.CheckIntegrationAccessDecision(r, d, current); err != nil {
		t.Fatal("forward clock before expiry", err)
	}
	current.NowMS = r.Facts.ExpiresAtMS
	if err := p.CheckIntegrationAccessDecision(r, d, current); err == nil || err.Error() != "integration.credential_denied" {
		t.Fatal("expired result accepted", err)
	}
	for _, field := range []string{"request", "effect", "reason", "rule"} {
		forged := d
		forged.Request = d.Request.Clone()
		switch field {
		case "request":
			forged.Request.Target.Resource.ID = "other"
		case "effect":
			forged.Effect = "deny"
		case "reason":
			forged.Reason = "integration.other"
		case "rule":
			forged.RuleID = "invented"
		}
		if err := p.CheckIntegrationAccessDecision(r, forged, r); !errors.Is(err, port.ErrIntegrationDecisionMismatch) {
			t.Fatal("forged decision accepted", field, err)
		}
	}
	denied := r.Clone()
	denied.Facts.CredentialStatus = "revoked"
	forged := d
	forged.Request = denied.Clone()
	if err := p.CheckIntegrationAccessDecision(denied, forged, denied); !errors.Is(err, port.ErrIntegrationDecisionMismatch) {
		t.Fatal("forged allow over revoked credential", err)
	}
}

func TestIntegrationAccessParallelAndDetachedDecisions(t *testing.T) {
	p, r := IntegrationAccessPolicy{}, integrationPolicyFixture()
	d, _ := p.DecideIntegrationAccess(r)
	r.Facts.Rules[0].ResourceIDs[0] = "changed"
	if d.Request.Facts.Rules[0].ResourceIDs[0] != "collection.1" {
		t.Fatal("decision shares caller storage")
	}
	r = integrationPolicyFixture()
	before := r.Clone()
	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d, err := p.DecideIntegrationAccess(r)
			if err != nil || p.CheckIntegrationAccessDecision(r, d, r) != nil {
				t.Error("parallel deterministic decision failed", err)
			}
			d.Request.Facts.Rules[0].ResourceIDs[0] = "private-mutation"
		}()
	}
	wg.Wait()
	if !reflect.DeepEqual(r, before) {
		t.Fatal("policy mutated shared input")
	}
}

func TestIntegrationAccessRuleWindowAndResourceKind(t *testing.T) {
	p, base := IntegrationAccessPolicy{}, integrationPolicyFixture()
	for _, name := range []string{"revoked", "suspended", "future", "expired", "wrong_kind"} {
		r := base.Clone()
		switch name {
		case "revoked", "suspended":
			r.Facts.Rules[0].Status = name
		case "future":
			r.Facts.Rules[0].NotBeforeMS = r.NowMS + 1
		case "expired":
			r.Facts.Rules[0].ExpiresAtMS = r.NowMS
		case "wrong_kind":
			r.Facts.Rules[0].ResourceKind = "private_collection"
		}
		d, err := p.DecideIntegrationAccess(r)
		if err != nil || d.Reason != "integration.whitelist_denied" {
			t.Fatal("rule scope/window bypass", name, d, err)
		}
	}
	d, err := p.DecideIntegrationAccess(base)
	if err != nil {
		t.Fatal(err)
	}
	current := base.Clone()
	current.NowMS = base.Facts.Rules[0].ExpiresAtMS
	if err := p.CheckIntegrationAccessDecision(base, d, current); err == nil || err.Error() != "integration.whitelist_denied" {
		t.Fatal("rule expiry borrowed longer credential lifetime", err)
	}
	base.Facts.Rules[0].ID, base.Facts.Rules[0].ExpiresAtMS = "rule.a", 1001
	other := base.Facts.Rules[0]
	other.ID, other.ExpiresAtMS = "rule.z", 1800
	base.Facts.Rules = append(base.Facts.Rules, other)
	d, err = p.DecideIntegrationAccess(base)
	if err != nil {
		t.Fatal(err)
	}
	current = base.Clone()
	current.NowMS = 1001
	if err := p.CheckIntegrationAccessDecision(base, d, current); !errors.Is(err, port.ErrIntegrationFactsChanged) {
		t.Fatal("old decision borrowed another rule after its witness expired", err)
	}
}

// The same corpus is executed in ordinary package tests and the env-i Linux
// binary. The drill asks this binary to retain its actual wire outcomes.
func TestIntegrationAccessIndependentWireCorpus(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("source path unavailable")
	}
	path := filepath.Join(filepath.Dir(file), "../../../../../sdk/typescript/tests/integration-access-v1.fixtures.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name     string          `json:"name"`
		Input    json.RawMessage `json:"input"`
		Expected string          `json:"expected"`
	}
	if err := json.Unmarshal(raw, &cases); err != nil || len(cases) != 192 {
		t.Fatal("complete independent corpus required", err)
	}
	results := make([]map[string]any, 0, len(cases))
	for _, tc := range cases {
		r, err := port.ParseIntegrationAccessInputJSON(tc.Input)
		result := ""
		var decision *port.IntegrationAccessDecision
		if err == nil {
			d, e := (IntegrationAccessPolicy{}).DecideIntegrationAccess(r)
			err = e
			if e == nil {
				result = d.Reason
				decision = &d
			}
		}
		if err != nil {
			result = err.Error()
		}
		if result != tc.Expected {
			t.Fatalf("%s: got %s want %s", tc.Name, result, tc.Expected)
		}
		results = append(results, map[string]any{"name": tc.Name, "input": tc.Input, "result": result, "decision": decision})
	}
	if output := os.Getenv("CAMPUSOS_INTEGRATION_ACCESS_OUTPUT"); output != "" {
		encoded, err := json.Marshal(results)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(output, encoded, 0600); err != nil {
			t.Fatal(err)
		}
	}
}
