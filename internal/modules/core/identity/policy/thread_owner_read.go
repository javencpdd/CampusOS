package policy

import "github.com/campusos/CampusOS/internal/modules/core/identity/port"

// ThreadOwnerReadPolicy preserves the owner's recovery/editing read: every
// legal publication and moderation state, with active or recoverable trashed
// deletion state. Its zero value needs no registry, cache, or fact loader.
type ThreadOwnerReadPolicy struct{}

var _ port.ThreadOwnerReadPolicy = ThreadOwnerReadPolicy{}

func (ThreadOwnerReadPolicy) DecideThreadOwnerRead(r port.ThreadOwnerReadRequest) (port.ThreadOwnerReadDecision, error) {
	if err := r.Validate(); err != nil {
		return port.ThreadOwnerReadDecision{}, err
	}
	principal := r.Principal
	if principal.EffectiveSubject != nil {
		subject := *principal.EffectiveSubject
		principal.EffectiveSubject = &subject
	}
	d := port.ThreadOwnerReadDecision{
		Contract: port.ThreadOwnerReadContract, RequestID: r.RequestID, Policy: port.ThreadOwnerReadPolicyVersion,
		Principal: principal, Resource: r.Facts.Resource, FactsVersion: r.Facts.FactsVersion,
		Effect: "deny", Reason: "policy.owner_read_denied", Obligations: []string{},
	}
	if r.Principal.Actor.Kind == "user" && r.Principal.Actor.ID == r.Facts.Owner.ID &&
		(r.Facts.DeletionStatus == "active" || r.Facts.DeletionStatus == "trashed") {
		d.Effect, d.Reason = "allow", "policy.owner_read"
		d.Obligations = []string{"recheck_current_facts"}
	}
	return d, nil
}
