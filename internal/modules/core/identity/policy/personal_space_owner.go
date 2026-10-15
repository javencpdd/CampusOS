package policy

import (
	"github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"reflect"
)

type PersonalSpaceOwnerPolicy struct{}

func (PersonalSpaceOwnerPolicy) DecidePersonalSpaceOwner(r port.PersonalSpaceOwnerRequest) (port.PersonalSpaceOwnerDecision, error) {
	if err := r.Validate(); err != nil {
		return port.PersonalSpaceOwnerDecision{}, err
	}
	d := port.PersonalSpaceOwnerDecision{RequestID: r.RequestID, PolicyVersion: r.PolicyVersion, Effect: "deny", Reason: "policy.scope_denied", Obligations: []string{}}
	if r.Principal.Actor.Kind != "user" || r.Principal.Audience != "user" {
		d.Reason = "policy.principal_wrong_domain"
		return d, nil
	}
	readOnly := r.Operation == "read_profile" || r.Operation == "style_catalog"
	if r.Principal.Actor.ID == r.OwnerID && (readOnly || !r.Disabled) {
		d.Effect, d.Reason = "allow", "ALLOW"
		d.Obligations = []string{"recheck_facts"}
		if !readOnly {
			d.Obligations = append(d.Obligations, "required_audit")
		}
	}
	return d, nil
}
func CheckPersonalSpaceOwnerDecision(initial port.PersonalSpaceOwnerRequest, d port.PersonalSpaceOwnerDecision, current port.PersonalSpaceOwnerRequest) error {
	expected, err := (PersonalSpaceOwnerPolicy{}).DecidePersonalSpaceOwner(initial)
	if err != nil || expected.Effect != "allow" || !reflect.DeepEqual(expected, d) || !reflect.DeepEqual(initial, current) {
		return port.ErrPolicyFactsChanged
	}
	return nil
}
