package policy

import "github.com/campusos/CampusOS/internal/modules/core/identity/port"

type ThreadAdminFlagPolicy struct{}

func (ThreadAdminFlagPolicy) DecideThreadAdminFlag(r port.ThreadAdminFlagRequest) (port.ThreadAdminFlagDecision, error) {
	if err := r.Validate(); err != nil {
		return port.ThreadAdminFlagDecision{}, err
	}
	d := port.ThreadAdminFlagDecision{Request: r, Effect: "deny", Reason: "policy.thread_admin_flag_denied"}
	if r.Principal.Actor.Kind == "admin" && r.Principal.AuthenticationStrength == "mfa" && r.ExecutionAllowed && r.EvaluatedAtMS < r.ExecutionExpiresAtMS && r.Thread.DeletionStatus == "active" {
		d.Effect, d.Reason = "allow", "policy.thread_admin_flag"
	}
	return d, nil
}
