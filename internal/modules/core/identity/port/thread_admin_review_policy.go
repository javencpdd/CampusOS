package port

import "reflect"

const ThreadAdminReviewContract = "campusos.thread-admin-review/v1"
const ThreadAdminReviewPolicyVersion = "community.thread.admin_review/v1"

type ThreadAdminReviewRequest struct {
	Contract, PolicyVersion, RequestID, Operation string
	Principal                                     PrincipalContext
	Thread                                        ThreadPublicReadFacts
	ExecutionQuery                                AdminExecutionQuery
	ExecutionAllowed                              bool
	ExecutionExpiresAtMS, EvaluatedAtMS           int64
}
type ThreadAdminReviewDecision struct {
	Request        ThreadAdminReviewRequest
	Effect, Reason string
}

func (r ThreadAdminReviewRequest) Validate() error {
	if r.Contract != ThreadAdminReviewContract || r.PolicyVersion != ThreadAdminReviewPolicyVersion || !validOpaqueID(r.RequestID) || r.Principal.Validate() != nil || r.Thread.Validate() != nil || r.ExecutionQuery.Validate() != nil || r.ExecutionQuery.PermissionCode != "community.thread.review" || r.ExecutionQuery.Scope != r.Thread.Board || r.ExecutionExpiresAtMS < 1 || r.EvaluatedAtMS < 1 {
		return ErrResourcePolicyInvalid
	}
	if r.Operation != "approve" && r.Operation != "reject" {
		return ErrResourcePolicyUnknownAction
	}
	return nil
}
func CheckThreadAdminReviewDecision(request ThreadAdminReviewRequest, decision ThreadAdminReviewDecision, current ThreadAdminReviewRequest) error {
	if request.Validate() != nil || !reflect.DeepEqual(request, decision.Request) || (decision.Effect != "allow" && decision.Effect != "deny") {
		return ErrPolicyDecisionMismatch
	}
	if !reflect.DeepEqual(request, current) {
		return ErrPolicyFactsChanged
	}
	if decision.Effect != "allow" || decision.Reason != "policy.thread_admin_review" {
		return ErrPolicyResourceNotPublic
	}
	return nil
}
