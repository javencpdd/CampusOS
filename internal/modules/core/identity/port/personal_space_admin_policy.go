package port

const PersonalSpaceAdminPolicyVersion = "personal-space-admin-1"

type PersonalSpaceAdminRequest struct {
	RequestID, PolicyVersion, Operation, OwnerID, SpaceID, UserStatus string
	Principal                                                         PrincipalContext
	Exists, Disabled                                                  bool
	FactsVersion                                                      int64
	ExecutionQuery                                                    AdminExecutionQuery
	ExecutionAllowed                                                  bool
	ExecutionExpiresAtMS, EvaluatedAtMS                               int64
}
type PersonalSpaceAdminDecision struct {
	Request        PersonalSpaceAdminRequest
	Effect, Reason string
}

func (r PersonalSpaceAdminRequest) Validate() error {
	if r.Operation != "disable" && r.Operation != "enable" {
		return ErrResourcePolicyUnknownAction
	}
	if r.RequestID == "" || r.PolicyVersion != PersonalSpaceAdminPolicyVersion || !validOpaqueID(r.OwnerID) || r.Principal.Validate() != nil || r.ExecutionQuery.Validate() != nil || r.ExecutionQuery.PermissionCode != "personal_space.space.manage" || r.ExecutionQuery.Scope != (PolicyResourceRef{Kind: "personal_space.space", ID: r.OwnerID}) || r.ExecutionExpiresAtMS < 1 || r.EvaluatedAtMS < 1 {
		return ErrResourcePolicyInvalid
	}
	if r.UserStatus != "active" && r.UserStatus != "suspended" && r.UserStatus != "deactivated" {
		return ErrResourcePolicyInvalid
	}
	if r.Exists && (!validOpaqueID(r.SpaceID) || r.FactsVersion < 1) || !r.Exists && (r.SpaceID != "" || r.FactsVersion != 0 || r.Disabled) {
		return ErrResourcePolicyInvalid
	}
	return nil
}
