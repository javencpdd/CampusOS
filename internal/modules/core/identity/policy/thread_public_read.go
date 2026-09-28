package policy

import "github.com/campusos/CampusOS/internal/modules/core/identity/port"

// ThreadPublicReadPolicy evaluates only the v1 public visibility predicate.
// Its zero value is usable, with no cache, repository, registry or fact loader.
type ThreadPublicReadPolicy struct{}

var _ port.ThreadPublicReadPolicy = ThreadPublicReadPolicy{}

func (ThreadPublicReadPolicy) DecideThreadPublicRead(r port.ThreadPublicReadRequest) (port.ThreadPublicReadDecision, error) {
	if err := r.Validate(); err != nil {
		return port.ThreadPublicReadDecision{}, err
	}
	d := port.ThreadPublicReadDecision{
		Contract: port.ThreadPublicReadContract, RequestID: r.RequestID, Policy: port.ThreadPublicReadPolicyVersion,
		Resource: r.Facts.Resource, FactsVersion: r.Facts.FactsVersion,
		Effect: "deny", Reason: "policy.resource_not_public", Obligations: []string{},
	}
	if r.Facts.PublicationStatus == "published" && r.Facts.ModerationStatus == "clear" && r.Facts.DeletionStatus == "active" {
		d.Effect, d.Reason = "allow", "policy.public_read"
		d.Obligations = []string{"recheck_current_facts"}
	}
	return d, nil
}
