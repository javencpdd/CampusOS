package policy

import (
	"github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"reflect"
)

type PersonalSpaceQuotaAdminPolicy struct{}

func (PersonalSpaceQuotaAdminPolicy) DecidePersonalSpaceQuotaAdmin(r port.PersonalSpaceQuotaAdminRequest) (port.PersonalSpaceQuotaAdminDecision, error) {
	if err := r.Validate(); err != nil {
		return port.PersonalSpaceQuotaAdminDecision{}, err
	}
	d := port.PersonalSpaceQuotaAdminDecision{Request: r, Effect: "deny", Reason: "policy.space_quota_admin_denied"}
	if r.Principal.Actor.Kind == "admin" && r.Principal.Audience == "admin" && r.Principal.AuthenticationStrength == "mfa" && r.ExecutionAllowed && r.EvaluatedAtMS < r.ExecutionExpiresAtMS {
		d.Effect, d.Reason = "allow", "policy.space_quota_admin"
	}
	return d, nil
}
func CheckPersonalSpaceQuotaAdminDecision(initial port.PersonalSpaceQuotaAdminRequest, d port.PersonalSpaceQuotaAdminDecision, current port.PersonalSpaceQuotaAdminRequest) error {
	expected, err := (PersonalSpaceQuotaAdminPolicy{}).DecidePersonalSpaceQuotaAdmin(initial)
	if err != nil || expected.Effect != "allow" || !reflect.DeepEqual(expected, d) || !reflect.DeepEqual(initial, current) {
		return port.ErrPolicyFactsChanged
	}
	return nil
}
