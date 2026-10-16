package port

const PersonalSpaceAdminReadPolicyVersion = "personal-space-admin-read-1"

type PersonalSpaceAdminReadRequest struct {
	RequestID, PolicyVersion, Operation, OwnerID, UserStatus string
	Principal                                                PrincipalContext
	ExecutionQuery                                           AdminExecutionQuery
	ExecutionAllowed                                         bool
	ExecutionExpiresAtMS, EvaluatedAtMS                      int64
}
type PersonalSpaceAdminReadDecision struct {
	Request        PersonalSpaceAdminReadRequest
	Effect, Reason string
}

func (r PersonalSpaceAdminReadRequest) Validate() error {
	if !validOpaqueID(r.RequestID) || r.PolicyVersion != PersonalSpaceAdminReadPolicyVersion || r.Principal.Validate() != nil || r.ExecutionQuery.Validate() != nil || r.ExecutionQuery.PermissionCode != "personal_space.space.manage" || r.ExecutionExpiresAtMS < 1 || r.EvaluatedAtMS < 1 {
		return ErrResourcePolicyInvalid
	}
	switch r.Operation {
	case "storage_status":
		if !validOpaqueID(r.OwnerID) || (r.UserStatus != "active" && r.UserStatus != "suspended" && r.UserStatus != "deactivated") || r.ExecutionQuery.Scope != (PolicyResourceRef{Kind: "personal_space.storage_status", ID: r.OwnerID}) {
			return ErrResourcePolicyInvalid
		}
	case "space_summary":
		if r.OwnerID != "" || r.UserStatus != "" || r.ExecutionQuery.Scope != (PolicyResourceRef{Kind: "personal_space.space_summary", ID: "summary"}) {
			return ErrResourcePolicyInvalid
		}
	default:
		return ErrResourcePolicyUnknownAction
	}
	return nil
}
