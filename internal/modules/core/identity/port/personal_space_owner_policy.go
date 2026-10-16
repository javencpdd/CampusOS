package port

const PersonalSpaceOwnerPolicyVersion = "personal-space-owner-1"

type PersonalSpaceOwnerRequest struct {
	RequestID, PolicyVersion, Operation, OwnerID string
	Principal                                    PrincipalContext
	Exists, Disabled                             bool
	FactsVersion                                 int64
}
type PersonalSpaceOwnerDecision struct {
	RequestID, PolicyVersion, Effect, Reason string
	Obligations                              []string
}

func (r PersonalSpaceOwnerRequest) Validate() error {
	if r.PolicyVersion != PersonalSpaceOwnerPolicyVersion || !validOpaqueID(r.RequestID) || !validOpaqueID(r.OwnerID) || r.Principal.Validate() != nil || r.Exists && r.FactsVersion < 1 || !r.Exists && r.FactsVersion != 0 {
		return ErrPolicyDecisionMismatch
	}
	if r.Operation != "profile" && r.Operation != "read_profile" && r.Operation != "avatar_select" && r.Operation != "avatar_upload" && r.Operation != "avatar_retention" && r.Operation != "style_apply" && r.Operation != "style_default" && r.Operation != "style_rollback" && r.Operation != "style_html" && r.Operation != "style_zip" && r.Operation != "style_source" && r.Operation != "style_catalog" && r.Operation != "style_read" && r.Operation != "read_contents" && r.Operation != "read_sync_status" {
		return ErrResourcePolicyUnknownAction
	}
	return nil
}
