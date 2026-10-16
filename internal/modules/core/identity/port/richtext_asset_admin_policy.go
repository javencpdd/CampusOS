package port

const RichTextAssetAdminContract = "campusos.richtext-asset-admin-lifecycle/v1"
const RichTextAssetAdminPolicyVersion = "richtext-asset-admin-lifecycle-1"

type RichTextAssetAdminRequest struct {
	Contract, RequestID, PolicyVersion, Operation string
	Principal                                     PrincipalContext
	Facts                                         RichTextAssetOwnerFacts
	ExecutionQuery                                AdminExecutionQuery
	ExecutionAllowed                              bool
	ExecutionExpiresAtMS, EvaluatedAtMS           int64
}
type RichTextAssetAdminDecision struct {
	Request        RichTextAssetAdminRequest
	Effect, Reason string
}

func (r RichTextAssetAdminRequest) Validate() error {
	// Reuse the existing fact shape, without applying the owner's policy.
	facts := RichTextAssetOwnerRequest{Contract: RichTextAssetOwnerContract, PolicyVersion: RichTextAssetOwnerPolicyVersion, RequestID: r.RequestID, Principal: r.Principal, Facts: r.Facts, Operation: "restore"}
	if r.Contract != RichTextAssetAdminContract || r.PolicyVersion != RichTextAssetAdminPolicyVersion || facts.Validate() != nil || r.ExecutionQuery.Validate() != nil || r.ExecutionQuery.PermissionCode != "personal_space.asset.manage" || r.ExecutionQuery.Scope != (PolicyResourceRef{Kind: "richtext.user_asset", ID: r.Facts.ID}) || r.ExecutionExpiresAtMS < 1 || r.EvaluatedAtMS < 1 {
		return ErrResourcePolicyInvalid
	}
	if r.Operation != "quarantine" && r.Operation != "restore" {
		return ErrResourcePolicyUnknownAction
	}
	return nil
}
