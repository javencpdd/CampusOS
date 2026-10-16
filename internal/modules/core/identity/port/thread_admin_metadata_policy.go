package port

import "reflect"

const ThreadAdminMetadataPolicyVersion = "community.thread.admin_metadata/v1"

type ThreadAdminMetadataRequest struct {
	PolicyVersion, RequestID, Operation string
	Principal                           PrincipalContext
	Thread                              ThreadPublicReadFacts
	ExecutionQuery                      AdminExecutionQuery
	ExecutionAllowed                    bool
	ExecutionExpiresAtMS, EvaluatedAtMS int64
}
type ThreadAdminMetadataDecision struct {
	Request        ThreadAdminMetadataRequest
	Effect, Reason string
}

func (r ThreadAdminMetadataRequest) Validate() error {
	if r.PolicyVersion != ThreadAdminMetadataPolicyVersion || !validOpaqueID(r.RequestID) || r.Principal.Validate() != nil || r.Thread.Validate() != nil || r.ExecutionQuery.Validate() != nil || r.ExecutionQuery.PermissionCode != "community.thread.read" || r.ExecutionQuery.Scope != r.Thread.Board || r.ExecutionExpiresAtMS < 1 || r.EvaluatedAtMS < 1 {
		return ErrResourcePolicyInvalid
	}
	if r.Operation != "list" && r.Operation != "trash" && r.Operation != "history" {
		return ErrResourcePolicyUnknownAction
	}
	return nil
}
func CheckThreadAdminMetadataDecision(r ThreadAdminMetadataRequest, d ThreadAdminMetadataDecision, current ThreadAdminMetadataRequest) error {
	if r.Validate() != nil || !reflect.DeepEqual(r, d.Request) || d.Effect != "allow" && d.Effect != "deny" {
		return ErrPolicyDecisionMismatch
	}
	if !reflect.DeepEqual(r, current) {
		return ErrPolicyFactsChanged
	}
	if d.Effect != "allow" || d.Reason != "policy.thread_admin_metadata" {
		return ErrPolicyResourceNotPublic
	}
	return nil
}
