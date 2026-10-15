package policy

import "github.com/campusos/CampusOS/internal/modules/core/identity/port"

type ThreadUserFlagPolicy struct{}

func (ThreadUserFlagPolicy) DecideThreadUserFlag(r port.ThreadUserFlagRequest) (port.ThreadUserFlagDecision, error) {
	if err := r.Validate(); err != nil {
		return port.ThreadUserFlagDecision{}, err
	}
	d := port.ThreadUserFlagDecision{Request: r, Effect: "deny", Reason: "policy.thread_user_flag_denied"}
	if r.Principal.Actor.Kind == "user" && r.ExecutionAllowed && r.EvaluatedAtMS < r.ExecutionExpiresAtMS && r.Thread.DeletionStatus == "active" {
		d.Effect, d.Reason = "allow", "policy.thread_user_flag"
	}
	return d, nil
}
