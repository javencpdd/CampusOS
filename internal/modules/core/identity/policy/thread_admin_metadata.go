package policy

import "github.com/campusos/CampusOS/internal/modules/core/identity/port"

type ThreadAdminMetadataPolicy struct{}

func (ThreadAdminMetadataPolicy) DecideThreadAdminMetadata(r port.ThreadAdminMetadataRequest) (port.ThreadAdminMetadataDecision, error) {
	if err := r.Validate(); err != nil {
		return port.ThreadAdminMetadataDecision{}, err
	}
	d := port.ThreadAdminMetadataDecision{Request: r, Effect: "deny", Reason: "policy.thread_admin_metadata_denied"}
	state := r.Operation == "history" || r.Operation == "list" && r.Thread.DeletionStatus == "active" || r.Operation == "trash" && r.Thread.DeletionStatus == "trashed"
	if r.Principal.Actor.Kind == "admin" && r.Principal.Audience == "admin" && r.ExecutionAllowed && r.EvaluatedAtMS < r.ExecutionExpiresAtMS && state {
		d.Effect = "allow"
		d.Reason = "policy.thread_admin_metadata"
	}
	return d, nil
}
