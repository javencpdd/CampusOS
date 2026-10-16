package policy

import "github.com/campusos/CampusOS/internal/modules/core/identity/port"

type AdminSelfReadPolicy struct{}

func (AdminSelfReadPolicy) DecideAdminSelfRead(r port.AdminSelfReadRequest) (port.AdminSelfReadDecision, error) {
	if err := r.Validate(); err != nil {
		return port.AdminSelfReadDecision{}, err
	}
	d := port.AdminSelfReadDecision{Request: r, Effect: "deny"}
	if r.Principal.Actor.Kind == "admin" && r.Principal.Audience == "admin" && r.Principal.Actor.ID == r.AccountID && r.Principal.CredentialID == r.SessionID && r.Status == "active" && r.EvaluatedAtMS < r.ExpiresAtMS {
		d.Effect = "allow"
	}
	return d, nil
}
