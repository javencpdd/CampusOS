package port

import "strconv"

const RichTextAssetOwnerContract = "campusos.richtext-asset-owner-lifecycle/v1"
const RichTextAssetOwnerPolicyVersion = "richtext-asset-owner-lifecycle-1"

type RichTextAssetOwnerFacts struct {
	ID, OwnerID, Kind, Status, FactsVersion string
	ReferenceCount                          int
}
type RichTextAssetOwnerRequest struct {
	Contract, RequestID, PolicyVersion, Operation string
	Principal                                     PrincipalContext
	Facts                                         RichTextAssetOwnerFacts
}
type RichTextAssetOwnerDecision struct {
	RequestID, Effect, Reason, FactsVersion, PolicyVersion string
	Obligations                                            []string
}

func (r RichTextAssetOwnerRequest) Validate() error {
	f := r.Facts
	nonce, err := strconv.ParseInt(f.FactsVersion, 10, 64)
	if r.Contract != RichTextAssetOwnerContract || r.PolicyVersion != RichTextAssetOwnerPolicyVersion || !validOpaqueID(r.RequestID) || r.Principal.Validate() != nil || !validOpaqueID(f.ID) || !validOpaqueID(f.OwnerID) || f.ReferenceCount < 0 || err != nil || nonce < 1 || strconv.FormatInt(nonce, 10) != f.FactsVersion {
		return ErrPolicyDecisionMismatch
	}
	if f.Kind != "article_attachment" && f.Kind != "richtext_image" {
		return ErrPolicyDecisionMismatch
	}
	switch f.Status {
	case "active", "trashed", "quarantined", "purging", "deleted":
	default:
		return ErrPolicyDecisionMismatch
	}
	if r.Operation != "trash" && r.Operation != "restore" {
		return ErrResourcePolicyUnknownAction
	}
	return nil
}
