package policy

import "github.com/campusos/CampusOS/internal/modules/core/identity/port"

type ThreadAdminReviewPolicy struct{}

func (ThreadAdminReviewPolicy) DecideThreadAdminReview(r port.ThreadAdminReviewRequest) (port.ThreadAdminReviewDecision, error) {
	if err := r.Validate(); err != nil {
		return port.ThreadAdminReviewDecision{}, err
	}
	d := port.ThreadAdminReviewDecision{Request: r, Effect: "deny", Reason: "policy.thread_admin_review_denied"}
	if r.Principal.SatisfiesAdminMFARequirement() && r.ExecutionAllowed && r.EvaluatedAtMS < r.ExecutionExpiresAtMS && r.Thread.DeletionStatus == "active" && r.Thread.ModerationStatus == "pending" {
		d.Effect, d.Reason = "allow", "policy.thread_admin_review"
	}
	return d, nil
}
