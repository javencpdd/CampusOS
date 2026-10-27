package policy

import "github.com/campusos/CampusOS/internal/modules/core/identity/port"

type ThreadAdminPurgePolicy struct{}

func (ThreadAdminPurgePolicy) DecideThreadAdminPurge(r port.ThreadAdminPurgeRequest) (port.ThreadAdminPurgeDecision, error) {
	if err := r.Validate(); err != nil {
		return port.ThreadAdminPurgeDecision{}, err
	}
	d := port.ThreadAdminPurgeDecision{Request: r, Effect: "deny", Reason: "policy.thread_admin_purge_denied"}
	if r.Principal.SatisfiesAdminMFARequirement() && r.ExecutionAllowed && r.EvaluatedAtMS < r.ExecutionExpiresAtMS && r.Thread.DeletionStatus == "trashed" && !r.PhysicalDeleted {
		d.Effect, d.Reason = "allow", "policy.thread_admin_purge"
	}
	return d, nil
}
