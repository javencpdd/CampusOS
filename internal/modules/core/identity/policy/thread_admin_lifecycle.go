package policy

import "github.com/campusos/CampusOS/internal/modules/core/identity/port"

type ThreadAdminLifecyclePolicy struct{}

func (ThreadAdminLifecyclePolicy) DecideThreadAdminLifecycle(r port.ThreadAdminLifecycleRequest) (port.ThreadAdminLifecycleDecision, error) {
	if err := r.Validate(); err != nil {
		return port.ThreadAdminLifecycleDecision{}, err
	}
	d := port.ThreadAdminLifecycleDecision{Request: r, Effect: "deny", Reason: "policy.thread_admin_lifecycle_denied"}
	state := false
	switch r.Operation {
	case "trash":
		state = r.Thread.DeletionStatus == "active"
	case "restore":
		state = r.Thread.DeletionStatus == "trashed"
	case "direct_restore":
		state = r.Thread.DeletionStatus == "active" && r.Thread.ModerationStatus == "taken_down"
	}
	if r.Principal.Actor.Kind == "admin" && r.Principal.AuthenticationStrength == "mfa" && r.ExecutionAllowed && r.EvaluatedAtMS < r.ExecutionExpiresAtMS && state {
		d.Effect, d.Reason = "allow", "policy.thread_admin_lifecycle"
	}
	return d, nil
}
