package policy

import (
	"reflect"

	"github.com/campusos/CampusOS/internal/modules/core/identity/port"
)

// ResourcePolicy implements the five registered G1 predicates using only
// current facts supplied by the owning service. Its zero value requires no
// cache, repository, global loader, credential store or ambient clock.
// EffectiveSubject is not an ownership or governance grant for these actions.
type ResourcePolicy struct{}

var _ port.ResourcePolicy = ResourcePolicy{}

func (ResourcePolicy) DecideResource(r port.ResourcePolicyRequest) (port.ResourcePolicyDecision, error) {
	if err := r.Validate(); err != nil {
		return port.ResourcePolicyDecision{}, err
	}
	d := port.ResourcePolicyDecision{
		RequestID: r.RequestID, Effect: "deny", Reason: "policy.resource_unavailable",
		FactsVersion: r.Facts.Version, PolicyVersion: r.PolicyVersion, Obligations: []string{},
	}
	f, p := r.Facts, r.Principal
	kind := ""
	switch r.Action {
	case "community.thread.edit", "community.thread.take_down":
		kind = "thread"
	case "community.post.delete":
		kind = "post"
	case "personal.document.read":
		kind = "document"
	case "knowledge.source.read":
		kind = "knowledge_source"
	}
	if f.Kind != kind || f.Status == "archived" || f.Status == "deleted" {
		return d, nil
	}
	allow, write := false, false
	switch r.Action {
	case "community.thread.edit":
		if f.OwnerID == "" || f.BoardID == "" || f.PublicationStatus == "" {
			return d, nil
		}
		if p.Actor.Kind != "user" {
			d.Reason = "policy.principal_wrong_domain"
			return d, nil
		}
		allow, write = p.Actor.ID == f.OwnerID, true
	case "community.thread.take_down", "community.post.delete":
		if f.OwnerID == "" || f.BoardID == "" || f.PublicationStatus == "" {
			return d, nil
		}
		if p.Actor.Kind != "user" && p.Actor.Kind != "admin" {
			d.Reason = "policy.principal_wrong_domain"
			return d, nil
		}
		// One current grant must witness every condition. Combining the actor
		// of one grant with another grant's board or strength would widen scope.
		for _, g := range r.Grants {
			if g.SubjectKind == p.Actor.Kind && g.SubjectID == p.Actor.ID &&
				g.Action == r.Action && g.BoardID == f.BoardID && g.Status == "active" &&
				r.EvaluatedAtMS < g.ExpiresAtMS &&
				(g.RequiredStrength == "password" || p.AuthenticationStrength == "mfa") {
				allow = true
				break
			}
		}
		if p.Actor.Kind == "admin" && p.AuthenticationStrength != "mfa" {
			allow = false
		}
		write = true
	case "personal.document.read":
		if f.OwnerID == "" {
			return d, nil
		}
		if p.Actor.Kind != "user" {
			d.Reason = "policy.principal_wrong_domain"
			return d, nil
		}
		allow = p.Actor.ID == f.OwnerID && f.Status == "active"
	case "knowledge.source.read":
		if f.CollectionID == "" || f.OriginVisible == nil || f.PublicationStatus == "" {
			return d, nil
		}
		allow = f.Status == "published" && f.PublicationStatus == "published" && *f.OriginVisible
	default:
		// Validate rejects all unregistered actions. Keep a closed default in
		// case the DTO registry is ever extended without a matching predicate.
		return port.ResourcePolicyDecision{}, port.ErrResourcePolicyUnknownAction
	}
	if !allow {
		d.Reason = "policy.scope_denied"
		return d, nil
	}
	d.Effect, d.Reason = "allow", "ALLOW"
	d.Obligations = []string{"recheck_facts"}
	if write {
		d.Obligations = append(d.Obligations, "required_audit")
	}
	return d, nil
}

// CheckResourceDecision binds a decision to the original evaluation and the
// host's current snapshot. Time may move forward; every other request fact must
// remain identical. A current re-evaluation also catches grant expiration.
// Callers still own fresh loading, the atomic write/recheck and required audit;
// this pure function neither acquires a database lock nor performs an audit.
func CheckResourceDecision(initial port.ResourcePolicyRequest, decision port.ResourcePolicyDecision, current port.ResourcePolicyRequest) error {
	if initial.Validate() != nil || current.Validate() != nil || decision.Validate() != nil {
		return port.ErrResourcePolicyFactsChanged
	}
	expected, err := (ResourcePolicy{}).DecideResource(initial)
	if err != nil || expected.Effect != "allow" || !reflect.DeepEqual(expected, decision) || current.EvaluatedAtMS < initial.EvaluatedAtMS {
		return port.ErrResourcePolicyFactsChanged
	}
	bound := current
	bound.EvaluatedAtMS = initial.EvaluatedAtMS
	if !reflect.DeepEqual(initial, bound) {
		return port.ErrResourcePolicyFactsChanged
	}
	latest, err := (ResourcePolicy{}).DecideResource(current)
	if err != nil || latest.Effect != "allow" {
		return port.ErrResourcePolicyFactsChanged
	}
	return nil
}
