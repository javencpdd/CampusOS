package policy

import "github.com/campusos/CampusOS/internal/modules/core/identity/port"

// Only availability/storage aggregates are projected. This decision supplies
// no private title, body, filename, or file-byte permission.
type PersonalSpaceAdminReadPolicy struct{}

func (PersonalSpaceAdminReadPolicy) DecidePersonalSpaceAdminRead(r port.PersonalSpaceAdminReadRequest) (port.PersonalSpaceAdminReadDecision, error) {
	if err := r.Validate(); err != nil {
		return port.PersonalSpaceAdminReadDecision{}, err
	}
	d := port.PersonalSpaceAdminReadDecision{Request: r, Effect: "deny", Reason: "policy.space_admin_metadata_denied"}
	if r.Principal.Actor.Kind == "admin" && r.Principal.Audience == "admin" && r.ExecutionAllowed && r.EvaluatedAtMS < r.ExecutionExpiresAtMS {
		d.Effect, d.Reason = "allow", "policy.space_admin_metadata"
	}
	return d, nil
}
