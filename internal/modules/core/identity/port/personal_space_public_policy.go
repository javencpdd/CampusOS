package port

import "reflect"

const PersonalSpacePublicPolicyVersion = "personal-space-public-1"

type PersonalSpacePublicRequest struct {
	RequestID, PolicyVersion, Operation, OwnerID, Visibility, UserStatus string
	Principal                                                            PrincipalContext
	Exists, Disabled                                                     bool
	FactsVersion                                                         int64
}
type PersonalSpacePublicDecision struct {
	Request        PersonalSpacePublicRequest
	Effect, Reason string
}

func (r PersonalSpacePublicRequest) Validate() error {
	if r.PolicyVersion != PersonalSpacePublicPolicyVersion || !validOpaqueID(r.RequestID) || !validOpaqueID(r.OwnerID) || r.Principal.Validate() != nil || r.Exists && r.FactsVersion < 1 || !r.Exists && r.FactsVersion != 0 {
		return ErrPolicyDecisionMismatch
	}
	if r.Operation != "profile" && r.Operation != "contents" {
		return ErrResourcePolicyUnknownAction
	}
	if r.Visibility != "public" && r.Visibility != "unlisted" && r.Visibility != "private" {
		return ErrResourcePolicyInvalid
	}
	return nil
}
func CheckPersonalSpacePublicDecision(r PersonalSpacePublicRequest, d PersonalSpacePublicDecision, current PersonalSpacePublicRequest) error {
	if r.Validate() != nil || !reflect.DeepEqual(r, d.Request) {
		return ErrPolicyDecisionMismatch
	}
	if !reflect.DeepEqual(r, current) {
		return ErrPolicyFactsChanged
	}
	if d.Effect != "allow" || d.Reason != "policy.public_space_read" || r.UserStatus != "active" || r.Visibility != "public" || r.Disabled || r.Principal.Actor.Kind != "anonymous" || r.Principal.Audience != "public" {
		return ErrPolicyResourceNotPublic
	}
	return nil
}
