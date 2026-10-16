package policy

import (
	"github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"reflect"
)

// Space administration governs availability metadata, never private content.
type PersonalSpaceAdminPolicy struct{}

func (PersonalSpaceAdminPolicy) DecidePersonalSpaceAdmin(r port.PersonalSpaceAdminRequest) (port.PersonalSpaceAdminDecision, error) {
	if err := r.Validate(); err != nil {
		return port.PersonalSpaceAdminDecision{}, err
	}
	d := port.PersonalSpaceAdminDecision{Request: r, Effect: "deny", Reason: "policy.space_admin_denied"}
	if r.Principal.Actor.Kind == "admin" && r.Principal.Audience == "admin" && r.Principal.AuthenticationStrength == "mfa" && r.ExecutionAllowed && r.EvaluatedAtMS < r.ExecutionExpiresAtMS {
		d.Effect, d.Reason = "allow", "policy.space_admin"
	}
	return d, nil
}
func CheckPersonalSpaceAdminDecision(initial port.PersonalSpaceAdminRequest, d port.PersonalSpaceAdminDecision, current port.PersonalSpaceAdminRequest) error {
	expected, err := (PersonalSpaceAdminPolicy{}).DecidePersonalSpaceAdmin(initial)
	if err != nil || expected.Effect != "allow" || !reflect.DeepEqual(expected, d) || !reflect.DeepEqual(initial, current) {
		return port.ErrPolicyFactsChanged
	}
	return nil
}
