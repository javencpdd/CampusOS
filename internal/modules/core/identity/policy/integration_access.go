package policy

import (
	"reflect"

	"github.com/campusos/CampusOS/internal/modules/core/identity/port"
)

// IntegrationAccessPolicy is a pure machine-entry whitelist gate. It does not
// authenticate a token or grant access to the original resource. This first
// contract admits only public-collection source reads on explicit Integration
// HTTP/MCP endpoints; resource ACLs remain with the owning module.
type IntegrationAccessPolicy struct{}

var _ port.IntegrationAccessPolicy = IntegrationAccessPolicy{}

func (IntegrationAccessPolicy) DecideIntegrationAccess(r port.IntegrationAccessInput) (port.IntegrationAccessDecision, error) {
	return decideIntegrationAccess(r, false)
}

func decideIntegrationAccess(r port.IntegrationAccessInput, runtime bool) (port.IntegrationAccessDecision, error) {
	validate := r.Validate
	if runtime {
		validate = func() error { return runtimeShape(r) }
	}
	if err := validate(); err != nil {
		return port.IntegrationAccessDecision{}, err
	}
	d := port.IntegrationAccessDecision{Request: r.Clone(), Effect: "deny"}
	f, target, actor := r.Facts, r.Target, r.Principal
	switch {
	case actor.Actor.Kind != "integration" || actor.Actor.ID != f.IntegrationID || actor.CredentialID != f.CredentialID:
		d.Reason = "integration.actor_denied"
	case f.PrincipalStatus != "active":
		d.Reason = "integration.principal_inactive"
	case f.CredentialStatus != "active" || f.CredentialAudience != "integration_api" || r.NowMS < f.NotBeforeMS || r.NowMS >= f.ExpiresAtMS:
		d.Reason = "integration.credential_denied"
	case target.Channel != "integration_http" && target.Channel != "integration_mcp":
		d.Reason = "integration.channel_denied"
	case !(target.Action == "knowledge.source.read" && target.Resource.Kind == "public_collection" || runtime && target.Action == "community.thread.read" && target.Resource.Kind == "community.thread"):
		d.Reason = "integration.scope_denied"
	case !f.Endpoint.Enabled || !f.Endpoint.IntegrationAllowed || f.Endpoint.Channel != target.Channel ||
		f.Endpoint.ID != target.Endpoint || f.Endpoint.Action != target.Action || f.Endpoint.ResourceKind != target.Resource.Kind:
		d.Reason = "integration.endpoint_denied"
	default:
		d.Reason = "integration.whitelist_denied"
		for _, rule := range f.Rules {
			if rule.Channel != target.Channel || rule.Endpoint != target.Endpoint || rule.Action != target.Action ||
				rule.ResourceKind != target.Resource.Kind || rule.Status != "active" || r.NowMS < rule.NotBeforeMS || r.NowMS >= rule.ExpiresAtMS {
				continue
			}
			for _, id := range rule.ResourceIDs {
				if id == target.Resource.ID && (d.RuleID == "" || rule.ID < d.RuleID) {
					d.RuleID = rule.ID
				}
			}
		}
		if d.RuleID != "" {
			d.Effect, d.Reason = "allow", "integration.allowed"
		}
	}
	return d, nil
}

// CheckIntegrationAccessDecision re-evaluates the original judgement rather
// than accepting a forged allow. The caller reloads the complete current
// principal, credential, endpoint and rule inventory. Only forward clock
// movement is permitted; it rechecks expiry with that current explicit clock.
func (p IntegrationAccessPolicy) CheckIntegrationAccessDecision(r port.IntegrationAccessInput, d port.IntegrationAccessDecision, current port.IntegrationAccessInput) error {
	expected, err := p.DecideIntegrationAccess(r)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(expected, d) {
		return port.ErrIntegrationDecisionMismatch
	}
	if d.Effect != "allow" {
		return &port.IntegrationAccessDenied{Reason: d.Reason}
	}
	if current.Validate() != nil || current.NowMS < r.NowMS {
		return port.ErrIntegrationFactsChanged
	}
	comparison := current.Clone()
	comparison.NowMS = r.NowMS
	if !reflect.DeepEqual(r, comparison) {
		return port.ErrIntegrationFactsChanged
	}
	fresh, err := p.DecideIntegrationAccess(current)
	if err != nil {
		return port.ErrIntegrationFactsChanged
	}
	if fresh.Effect != "allow" {
		return &port.IntegrationAccessDenied{Reason: fresh.Reason}
	}
	if fresh.RuleID != d.RuleID {
		return port.ErrIntegrationFactsChanged
	}
	return nil
}
