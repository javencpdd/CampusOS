package policy

import (
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"reflect"
)

type PostOwnerWritePolicy struct{}

func (PostOwnerWritePolicy) DecidePostOwnerWrite(r identityport.PostOwnerWriteRequest) (identityport.PostOwnerWriteDecision, error) {
	if err := r.Validate(); err != nil {
		return identityport.PostOwnerWriteDecision{}, err
	}
	d := identityport.PostOwnerWriteDecision{RequestID: r.RequestID, Effect: "deny", Reason: "policy.post_owner_write_unavailable", PolicyVersion: r.PolicyVersion}
	p := r.Principal
	if p.Actor.Kind != "user" || p.Audience != "user" {
		d.Reason = "policy.principal_wrong_domain"
		return d, nil
	}
	if r.Parent.DeletionStatus != "active" {
		return d, nil
	}
	allowed := false
	if r.Operation == "create" {
		allowed = !r.ParentLocked && (r.Parent.Owner.ID == p.Actor.ID || r.Parent.PublicationStatus == "published" && r.Parent.ModerationStatus == "clear")
	} else {
		allowed = r.Post.OwnerID == p.Actor.ID && !r.Post.Deleted && r.Post.Status == "published"
	}
	if allowed {
		d.Effect = "allow"
		d.Reason = "policy.post_owner_write_allowed"
	}
	return d, nil
}
func CheckPostOwnerWriteDecision(initial identityport.PostOwnerWriteRequest, d identityport.PostOwnerWriteDecision, current identityport.PostOwnerWriteRequest) error {
	expected, err := (PostOwnerWritePolicy{}).DecidePostOwnerWrite(initial)
	if err != nil || expected.Effect != "allow" || !reflect.DeepEqual(expected, d) || !reflect.DeepEqual(initial, current) {
		return identityport.ErrPolicyFactsChanged
	}
	latest, err := (PostOwnerWritePolicy{}).DecidePostOwnerWrite(current)
	if err != nil || latest.Effect != "allow" {
		return identityport.ErrPolicyFactsChanged
	}
	return nil
}
