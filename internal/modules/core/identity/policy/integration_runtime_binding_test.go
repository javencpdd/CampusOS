package policy

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/campusos/CampusOS/internal/modules/core/identity/port"
)

func TestIntegrationRuntimeVersionAndFinalBinding(t *testing.T) {
	r := integrationPolicyFixture()
	r.Target.Action = "community.thread.read"
	r.Target.Resource.Kind = "community.thread"
	r.Facts.Endpoint.Action = r.Target.Action
	r.Facts.Endpoint.ResourceKind = r.Target.Resource.Kind
	r.Facts.Rules[0].Action = r.Target.Action
	r.Facts.Rules[0].ResourceKind = r.Target.Resource.Kind
	v1, err := (IntegrationAccessPolicy{}).DecideIntegrationAccess(r)
	if err != nil || v1.Reason != "integration.scope_denied" {
		t.Fatal("v1 silently widened", err)
	}
	r.Contract = port.IntegrationRuntimeContract
	p := IntegrationRuntimePolicy{}
	d, err := p.DecideIntegrationAccess(r)
	if err != nil || d.Effect != "allow" {
		t.Fatal(err, d)
	}
	if err := p.CheckIntegrationAccessDecision(r, d, r); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"v1_replay", "future_expiry", "facts_aba", "rule_aba", "endpoint_off", "forged", "clock_rollback"} {
		t.Run(mode, func(t *testing.T) {
			current := r.Clone()
			dec := d
			switch mode {
			case "v1_replay":
				current.Contract = port.IntegrationAccessContract
			case "future_expiry":
				current.NowMS = r.Facts.ExpiresAtMS
			case "facts_aba":
				current.Facts.FactsVersion = "p2.c3"
			case "rule_aba":
				current.Facts.Rules[0].ID = "new_rule"
			case "endpoint_off":
				current.Facts.Endpoint.Enabled = false
			case "forged":
				dec.RuleID = "forged"
			case "clock_rollback":
				current.NowMS--
			}
			if err := p.CheckIntegrationAccessDecision(r, dec, current); err == nil {
				t.Fatal("stale/forged accepted")
			}
		})
	}
	raw, _ := json.Marshal(r)
	if _, err := port.ParseIntegrationAccessInputJSON(raw); err == nil {
		t.Fatal("v2 accepted as v1 codec")
	}
	if _, err := port.ParseIntegrationRuntimeInputJSON(raw); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{strings.Replace(string(raw), `"request_id":`, `"request_id":"duplicate","request_id":`, 1), strings.Replace(string(raw), `"facts_version":"facts.1"`, `"facts_version":null`, 1), strings.Replace(string(raw), `"request.1"`, `"\ud800"`, 1), string(raw) + "{}"} {
		if _, err := port.ParseIntegrationRuntimeInputJSON([]byte(bad)); !errors.Is(err, port.ErrIntegrationInputInvalid) {
			t.Fatal("ambiguous v2 JSON accepted", err)
		}
	}
}
