package port

import "reflect"

const ThreadAdminPurgeContract = "campusos.thread-admin-purge/v1"
const ThreadAdminPurgePolicyVersion = "community.thread.admin_purge/v1"

type ThreadAdminPurgeRequest struct {
	Contract, PolicyVersion, RequestID, Operation string
	Principal                                     PrincipalContext
	Thread                                        ThreadPublicReadFacts
	ExecutionQuery                                AdminExecutionQuery
	ExecutionAllowed                              bool
	PhysicalDeleted                               bool
	ExecutionExpiresAtMS, EvaluatedAtMS           int64
}
type ThreadAdminPurgeDecision struct {
	Request        ThreadAdminPurgeRequest
	Effect, Reason string
}

func (r ThreadAdminPurgeRequest) Validate() error {
	if r.Contract != ThreadAdminPurgeContract || r.PolicyVersion != ThreadAdminPurgePolicyVersion || !validOpaqueID(r.RequestID) || r.Principal.Validate() != nil || r.Thread.Validate() != nil || r.ExecutionQuery.Validate() != nil || r.ExecutionQuery.PermissionCode != "community.thread.purge" || r.ExecutionQuery.Scope != r.Thread.Board || r.ExecutionExpiresAtMS < 1 || r.EvaluatedAtMS < 1 {
		return ErrResourcePolicyInvalid
	}
	if r.Operation != "purge" {
		return ErrResourcePolicyUnknownAction
	}
	return nil
}
func CheckThreadAdminPurgeDecision(request ThreadAdminPurgeRequest, decision ThreadAdminPurgeDecision, current ThreadAdminPurgeRequest) error {
	if request.Validate() != nil || !reflect.DeepEqual(request, decision.Request) || (decision.Effect != "allow" && decision.Effect != "deny") {
		return ErrPolicyDecisionMismatch
	}
	if !reflect.DeepEqual(request, current) {
		return ErrPolicyFactsChanged
	}
	if decision.Effect != "allow" || decision.Reason != "policy.thread_admin_purge" {
		return ErrPolicyResourceNotPublic
	}
	return nil
}
