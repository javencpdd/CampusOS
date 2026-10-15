package port

import "strconv"

const PostOwnerWriteContract = "campusos.post-owner-write/v1"
const PostOwnerWritePolicyVersion = "community.post.owner_write/v1"

type PostOwnerWriteFacts struct {
	ID       string
	ThreadID string
	OwnerID  string
	Status   string
	Version  int64
	Deleted  bool
}
type PostOwnerWriteRequest struct {
	Contract      string
	PolicyVersion string
	RequestID     string
	Principal     PrincipalContext
	Operation     string
	Parent        ThreadPublicReadFacts
	ParentLocked  bool
	Post          *PostOwnerWriteFacts
}
type PostOwnerWriteDecision struct{ RequestID, Effect, Reason, PolicyVersion string }

func (r PostOwnerWriteRequest) Validate() error {
	if r.Contract != PostOwnerWriteContract || r.PolicyVersion != PostOwnerWritePolicyVersion || !validOpaqueID(r.RequestID) || r.Principal.Validate() != nil || r.Parent.Validate() != nil {
		return ErrResourcePolicyInvalid
	}
	switch r.Operation {
	case "create":
		if r.Post != nil {
			return ErrResourcePolicyInvalid
		}
	case "edit", "delete":
		if r.Post == nil || !validOpaqueID(r.Post.ID) || !validOpaqueID(r.Post.OwnerID) || r.Post.ThreadID != r.Parent.Resource.ID || r.Post.Version < 1 || (r.Post.Status != "published" && r.Post.Status != "deleted") {
			return ErrResourcePolicyInvalid
		}
	default:
		return ErrResourcePolicyUnknownAction
	}
	if n, err := strconv.ParseInt(r.Parent.FactsVersion, 10, 64); err != nil || n < 1 {
		return ErrResourcePolicyInvalid
	}
	return nil
}
