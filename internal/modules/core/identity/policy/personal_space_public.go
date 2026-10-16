package policy

import "github.com/campusos/CampusOS/internal/modules/core/identity/port"

type PersonalSpacePublicPolicy struct{}

func (PersonalSpacePublicPolicy) DecidePersonalSpacePublic(r port.PersonalSpacePublicRequest) (port.PersonalSpacePublicDecision, error) {
	if err := r.Validate(); err != nil {
		return port.PersonalSpacePublicDecision{}, err
	}
	d := port.PersonalSpacePublicDecision{Request: r, Effect: "deny", Reason: "policy.space_not_public"}
	if r.Principal.Actor.Kind == "anonymous" && r.Principal.Audience == "public" && r.UserStatus == "active" && r.Visibility == "public" && !r.Disabled {
		d.Effect = "allow"
		d.Reason = "policy.public_space_read"
	}
	return d, nil
}
