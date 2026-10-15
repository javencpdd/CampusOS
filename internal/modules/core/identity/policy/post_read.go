package policy

import "github.com/campusos/CampusOS/internal/modules/core/identity/port"

type PostReadPolicy struct{}

func (PostReadPolicy) DecidePostRead(r port.PostReadRequest) (port.PostReadDecision, error) {
	if err := r.Validate(); err != nil {
		return port.PostReadDecision{}, err
	}
	d := port.PostReadDecision{Request: r, Effect: "deny", Reason: "policy.post_read_denied"}
	if r.Post != nil {
		copied := *r.Post
		d.Request.Post = &copied
	}
	p, f := r.Principal, r.Parent
	public := f.PublicationStatus == "published" && f.ModerationStatus == "clear"
	owner := p.Actor.Kind == "user" && p.Actor.ID == f.Owner.ID
	if (p.Actor.Kind == "anonymous" || p.Actor.Kind == "user") && f.DeletionStatus == "active" && (public || owner) && (r.Post == nil || (r.Post.Status == "published" && !r.Post.Deleted)) {
		d.Effect, d.Reason = "allow", "policy.post_read"
	}
	return d, nil
}
