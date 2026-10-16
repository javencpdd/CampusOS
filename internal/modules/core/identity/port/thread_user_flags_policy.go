package port

import "reflect"

const ThreadUserFlagContract = "campusos.thread-user-flag/v1"
const ThreadUserFlagPolicyVersion = "community.thread.user_flag/v1"

type ThreadUserFlagRequest struct {
	Contract, PolicyVersion, RequestID, Operation string
	Principal                                     PrincipalContext
	Thread                                        ThreadPublicReadFacts
	Pinned, Locked                                bool
	ExecutionQuery                                UserScopedExecutionQuery
	ExecutionAllowed                              bool
	ExecutionExpiresAtMS, EvaluatedAtMS           int64
}
type ThreadUserFlagDecision struct {
	Request        ThreadUserFlagRequest
	Effect, Reason string
}

func (r ThreadUserFlagRequest) Validate() error {
	if r.Contract != ThreadUserFlagContract || r.PolicyVersion != ThreadUserFlagPolicyVersion || !validOpaqueID(r.RequestID) || r.Principal.Validate() != nil || r.Thread.Validate() != nil || r.ExecutionQuery.Validate() != nil || r.ExecutionQuery.Scope != r.Thread.Board || r.ExecutionExpiresAtMS < 1 || r.EvaluatedAtMS < 1 {
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
func CheckThreadUserFlagDecision(request ThreadUserFlagRequest, decision ThreadUserFlagDecision, current ThreadUserFlagRequest) error {
	if request.Validate() != nil || !reflect.DeepEqual(request, decision.Request) || (decision.Effect != "allow" && decision.Effect != "deny") {
		return ErrPolicyDecisionMismatch
	}
	if !reflect.DeepEqual(request, current) {
		return ErrPolicyFactsChanged
	}
	if decision.Effect != "allow" || decision.Reason != "policy.thread_user_flag" {
		return ErrPolicyResourceNotPublic
	}
	return nil
}
