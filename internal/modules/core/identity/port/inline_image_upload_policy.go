package port

const InlineImageUploadPolicyVersion = "inline-image-owner-upload-1"

type InlineImageUploadRequest struct {
	RequestID, PolicyVersion, Namespace, OwnerID string
	Principal                                    PrincipalContext
}
type InlineImageUploadDecision struct {
	RequestID, PolicyVersion, Effect, Reason string
	Obligations                              []string
}

func (r InlineImageUploadRequest) Validate() error {
	if r.PolicyVersion != InlineImageUploadPolicyVersion || !validOpaqueID(r.RequestID) || !validOpaqueID(r.OwnerID) || r.Principal.Validate() != nil || (r.Namespace != "content" && r.Namespace != "richtext") {
		return ErrPolicyDecisionMismatch
	}
	return nil
}
