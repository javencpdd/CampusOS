package port

import "reflect"

const ThreadAdminLifecycleContract = "campusos.thread-admin-lifecycle/v1"
const ThreadAdminLifecyclePolicyVersion = "community.thread.admin_lifecycle/v1"

type ThreadAdminLifecycleRequest struct {
	Contract, PolicyVersion, RequestID, Operation string
	Principal                                     PrincipalContext
	Thread                                        ThreadPublicReadFacts
	ExecutionQuery                                AdminExecutionQuery
	ExecutionAllowed                              bool
	ExecutionExpiresAtMS, EvaluatedAtMS           int64
}
type ThreadAdminLifecycleDecision struct {
	Request        ThreadAdminLifecycleRequest
	Effect, Reason string
}

func (r ThreadAdminLifecycleRequest) Validate() error {
	if r.Contract != ThreadAdminLifecycleContract || r.PolicyVersion != ThreadAdminLifecyclePolicyVersion || !validOpaqueID(r.RequestID) || r.Principal.Validate() != nil || r.Thread.Validate() != nil || r.ExecutionQuery.Validate() != nil || r.ExecutionQuery.Scope != r.Thread.Board || r.ExecutionExpiresAtMS < 1 || r.EvaluatedAtMS < 1 {
		return ErrResourcePolicyInvalid
	}
	code := ""
	switch r.Operation {
	case "trash":
		code = "community.thread.trash"
	case "restore":
		code = "community.thread.restore"
	case "direct_restore":
		code = "community.thread.direct_restore"
	default:
		return ErrResourcePolicyUnknownAction
	}
	if r.ExecutionQuery.PermissionCode != code {
		return ErrResourcePolicyInvalid
	}
	return nil
}
func CheckThreadAdminLifecycleDecision(request ThreadAdminLifecycleRequest, decision ThreadAdminLifecycleDecision, current ThreadAdminLifecycleRequest) error {
	if request.Validate() != nil || !reflect.DeepEqual(request, decision.Request) || (decision.Effect != "allow" && decision.Effect != "deny") {
		return ErrPolicyDecisionMismatch
	}
	if !reflect.DeepEqual(request, current) {
		return ErrPolicyFactsChanged
	}
	if decision.Effect != "allow" || decision.Reason != "policy.thread_admin_lifecycle" {
		return ErrPolicyResourceNotPublic
	}
	return nil
}
