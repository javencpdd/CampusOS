package port

import "reflect"

const PostReadContract = "campusos.post-read/v1"
const PostReadPolicyVersion = "community.post.read/v1"

type PostReadRequest struct {
	Contract, PolicyVersion, RequestID, Operation string
	Principal                                     PrincipalContext
	Parent                                        ThreadPublicReadFacts
	Post                                          *PostOwnerWriteFacts
}
type PostReadDecision struct {
	Request        PostReadRequest
	Effect, Reason string
}

func (r PostReadRequest) Validate() error {
	if r.Contract != PostReadContract || r.PolicyVersion != PostReadPolicyVersion || !validOpaqueID(r.RequestID) || r.Principal.Validate() != nil || r.Parent.Validate() != nil {
		return ErrResourcePolicyInvalid
	}
	switch r.Operation {
	case "list":
		if r.Post != nil {
			return ErrResourcePolicyInvalid
		}
	case "detail":
		if r.Post == nil || !validOpaqueID(r.Post.ID) || !validOpaqueID(r.Post.OwnerID) || r.Post.ThreadID != r.Parent.Resource.ID || r.Post.Version < 1 {
			return ErrResourcePolicyInvalid
		}
	default:
		return ErrResourcePolicyUnknownAction
	}
	return nil
}
func CheckPostReadDecision(request PostReadRequest, decision PostReadDecision, current PostReadRequest) error {
	if request.Validate() != nil || !reflect.DeepEqual(decision.Request, request) || (decision.Effect != "allow" && decision.Effect != "deny") {
		return ErrPolicyDecisionMismatch
	}
	if !reflect.DeepEqual(request, current) {
		return ErrPolicyFactsChanged
	}
	if decision.Effect != "allow" || decision.Reason != "policy.post_read" {
		return ErrPolicyResourceNotPublic
	}
	return nil
}
