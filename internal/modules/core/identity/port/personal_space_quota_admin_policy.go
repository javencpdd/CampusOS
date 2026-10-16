package port

const PersonalSpaceQuotaAdminPolicyVersion = "personal-space-quota-admin-1"

type PersonalSpaceQuotaAdminRequest struct {
	RequestID, PolicyVersion, OwnerID, UserStatus string
	Principal                                     PrincipalContext
	Exists                                        bool
	FactsVersion, RequestedQuotaBytes             int64
	ExecutionQuery                                AdminExecutionQuery
	ExecutionAllowed                              bool
	ExecutionExpiresAtMS, EvaluatedAtMS           int64
}
type PersonalSpaceQuotaAdminDecision struct {
	Request        PersonalSpaceQuotaAdminRequest
	Effect, Reason string
}

func (r PersonalSpaceQuotaAdminRequest) Validate() error {
	if !validOpaqueID(r.RequestID) || r.PolicyVersion != PersonalSpaceQuotaAdminPolicyVersion || !validOpaqueID(r.OwnerID) || r.Principal.Validate() != nil || r.ExecutionQuery.Validate() != nil || r.ExecutionQuery.PermissionCode != "personal_space.space.manage" || r.ExecutionQuery.Scope != (PolicyResourceRef{Kind: "personal_space.storage_quota", ID: r.OwnerID}) || r.ExecutionExpiresAtMS < 1 || r.EvaluatedAtMS < 1 || r.RequestedQuotaBytes < 1048576 || r.RequestedQuotaBytes > 107374182400 || r.Exists && r.FactsVersion < 1 || !r.Exists && r.FactsVersion != 0 {
		return ErrResourcePolicyInvalid
	}
	if r.UserStatus != "active" && r.UserStatus != "suspended" && r.UserStatus != "deactivated" {
		return ErrResourcePolicyInvalid
	}
	return nil
}
