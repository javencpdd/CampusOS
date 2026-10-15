package port

import "reflect"

const ThreadAdminFlagContract = "campusos.thread-admin-flag/v1"
const ThreadAdminFlagPolicyVersion = "community.thread.admin_flag/v1"

type ThreadAdminFlagRequest struct {
	Contract, PolicyVersion, RequestID, Operation string
	Principal                                     PrincipalContext
	Thread                                        ThreadPublicReadFacts
	Pinned, Locked                                bool
	ExecutionQuery                                AdminExecutionQuery
	ExecutionAllowed                              bool
	ExecutionExpiresAtMS, EvaluatedAtMS           int64
}
type ThreadAdminFlagDecision struct {
	Request        ThreadAdminFlagRequest
	Effect, Reason string
}

func (r ThreadAdminFlagRequest) Validate() error {
	if r.Contract != ThreadAdminFlagContract || r.PolicyVersion != ThreadAdminFlagPolicyVersion || !validOpaqueID(r.RequestID) || r.Principal.Validate() != nil || r.Thread.Validate() != nil || r.ExecutionQuery.Validate() != nil || r.ExecutionQuery.Scope != r.Thread.Board || r.ExecutionExpiresAtMS < 1 || r.EvaluatedAtMS < 1 {
		return ErrResourcePolicyInvalid
	}
	code := ""
	switch r.Operation {
	case "pin", "unpin":
		code = "community.thread.pin"
	case "lock", "unlock":
		code = "community.thread.lock"
	default:
		return ErrResourcePolicyUnknownAction
	}
	if r.ExecutionQuery.PermissionCode != code {
		return ErrResourcePolicyInvalid
	}
	return nil
}
func CheckThreadAdminFlagDecision(request ThreadAdminFlagRequest, decision ThreadAdminFlagDecision, current ThreadAdminFlagRequest) error {
	if request.Validate() != nil || !reflect.DeepEqual(request, decision.Request) || (decision.Effect != "allow" && decision.Effect != "deny") {
		return ErrPolicyDecisionMismatch
	}
	if !reflect.DeepEqual(request, current) {
		return ErrPolicyFactsChanged
	}
	if decision.Effect != "allow" || decision.Reason != "policy.thread_admin_flag" {
		return ErrPolicyResourceNotPublic
	}
	return nil
}
