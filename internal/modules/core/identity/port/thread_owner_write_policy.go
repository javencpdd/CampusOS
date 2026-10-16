package port

const ThreadOwnerWriteContract = "campusos.thread-owner-lifecycle/v1"
const ThreadOwnerWritePolicyVersion = "thread-owner-lifecycle-1"

// ThreadOwnerWriteRequest is a separate owner lifecycle contract. It does not
// extend the frozen five-action ResourcePolicy or turn an owner read into a write.
type ThreadOwnerWriteRequest struct {
	Contract      string
	RequestID     string
	PolicyVersion string
	Principal     PrincipalContext
	Facts         ThreadPublicReadFacts
	Operation     string
}
type ThreadOwnerWriteDecision struct {
	RequestID, Effect, Reason, FactsVersion, PolicyVersion string
	Obligations                                            []string
}

func (r ThreadOwnerWriteRequest) Validate() error {
	if r.Contract != ThreadOwnerWriteContract || r.PolicyVersion != ThreadOwnerWritePolicyVersion || !validOpaqueID(r.RequestID) || r.Principal.Validate() != nil || r.Facts.Validate() != nil {
		return ErrPolicyDecisionMismatch
	}
	switch r.Operation {
	case "trash", "restore", "submit_review":
		return nil
	default:
		return ErrResourcePolicyUnknownAction
	}
}
