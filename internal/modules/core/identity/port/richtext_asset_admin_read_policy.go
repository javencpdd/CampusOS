package port

const RichTextAssetPurgeContract = "campusos.richtext-asset-purge/v1"
const RichTextAssetPurgePolicyVersion = "richtext-asset-purge-1"

// The metadata eligibility rule is shared by preview and actual purge.
// Byte receipts and command atomicity remain execution obligations.
type RichTextAssetPurgeRequest struct {
	Contract, RequestID, PolicyVersion  string
	Principal                           PrincipalContext
	Facts                               RichTextAssetOwnerFacts
	ExecutionQuery                      AdminExecutionQuery
	ExecutionAllowed                    bool
	ExecutionExpiresAtMS, EvaluatedAtMS int64
}
type RichTextAssetPurgeDecision struct {
	Request        RichTextAssetPurgeRequest
	Effect, Reason string
}

func (r RichTextAssetPurgeRequest) Validate() error {
	shape := RichTextAssetAdminRequest{Contract: RichTextAssetAdminContract, RequestID: r.RequestID, PolicyVersion: RichTextAssetAdminPolicyVersion, Operation: "quarantine", Principal: r.Principal, Facts: r.Facts, ExecutionQuery: r.ExecutionQuery, ExecutionAllowed: r.ExecutionAllowed, ExecutionExpiresAtMS: r.ExecutionExpiresAtMS, EvaluatedAtMS: r.EvaluatedAtMS}
	if r.Contract != RichTextAssetPurgeContract || r.PolicyVersion != RichTextAssetPurgePolicyVersion || shape.Validate() != nil {
		return ErrResourcePolicyInvalid
	}
	return nil
}

const RichTextAssetSummaryContract = "campusos.richtext-asset-summary/v1"
const RichTextAssetSummaryPolicyVersion = "richtext-asset-summary-1"

type RichTextAssetSummaryRequest struct {
	Contract, RequestID, PolicyVersion  string
	Principal                           PrincipalContext
	ExecutionQuery                      AdminExecutionQuery
	ExecutionAllowed                    bool
	ExecutionExpiresAtMS, EvaluatedAtMS int64
}
type RichTextAssetSummaryDecision struct {
	Request        RichTextAssetSummaryRequest
	Effect, Reason string
}

func (r RichTextAssetSummaryRequest) Validate() error {
	if r.Contract != RichTextAssetSummaryContract || r.PolicyVersion != RichTextAssetSummaryPolicyVersion || !validOpaqueID(r.RequestID) || r.Principal.Validate() != nil || r.ExecutionQuery.Validate() != nil || r.ExecutionQuery.PermissionCode != "personal_space.asset.read_audit" || r.ExecutionQuery.Scope != (PolicyResourceRef{Kind: "richtext.asset_governance", ID: "summary"}) || r.ExecutionExpiresAtMS < 1 || r.EvaluatedAtMS < 1 {
		return ErrResourcePolicyInvalid
	}
	return nil
}
