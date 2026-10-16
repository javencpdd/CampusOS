package policy

import (
	"github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"reflect"
)

// A new unbound image belongs to the currently authenticated User inventory.
// This rule grants no article binding, private read or governance capability.
type InlineImageUploadPolicy struct{}

func (InlineImageUploadPolicy) DecideInlineImageUpload(r port.InlineImageUploadRequest) (port.InlineImageUploadDecision, error) {
	if err := r.Validate(); err != nil {
		return port.InlineImageUploadDecision{}, err
	}
	d := port.InlineImageUploadDecision{RequestID: r.RequestID, PolicyVersion: r.PolicyVersion, Effect: "deny", Reason: "policy.scope_denied", Obligations: []string{}}
	if r.Principal.Actor.Kind != "user" || r.Principal.Audience != "user" {
		d.Reason = "policy.principal_wrong_domain"
		return d, nil
	}
	if r.Principal.Actor.ID == r.OwnerID {
		d.Effect, d.Reason = "allow", "ALLOW"
		d.Obligations = []string{"recheck_facts", "required_audit"}
	}
	return d, nil
}
func CheckInlineImageUploadDecision(initial port.InlineImageUploadRequest, decision port.InlineImageUploadDecision, current port.InlineImageUploadRequest) error {
	expected, err := (InlineImageUploadPolicy{}).DecideInlineImageUpload(initial)
	if err != nil || expected.Effect != "allow" || !reflect.DeepEqual(expected, decision) || !reflect.DeepEqual(initial, current) {
		return port.ErrPolicyFactsChanged
	}
	return nil
}
