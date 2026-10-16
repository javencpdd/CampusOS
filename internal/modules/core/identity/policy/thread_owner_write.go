package policy

import (
	"reflect"

	"github.com/campusos/CampusOS/internal/modules/core/identity/port"
)

type ThreadOwnerWritePolicy struct{}

func (ThreadOwnerWritePolicy) DecideThreadOwnerWrite(r port.ThreadOwnerWriteRequest) (port.ThreadOwnerWriteDecision, error) {
	if err := r.Validate(); err != nil {
		return port.ThreadOwnerWriteDecision{}, err
	}
	d := port.ThreadOwnerWriteDecision{RequestID: r.RequestID, Effect: "deny", Reason: "policy.scope_denied", FactsVersion: r.Facts.FactsVersion, PolicyVersion: r.PolicyVersion, Obligations: []string{}}
	if r.Principal.Actor.Kind != "user" || r.Principal.Audience != "user" {
		d.Reason = "policy.principal_wrong_domain"
		return d, nil
	}
	if r.Principal.Actor.ID != r.Facts.Owner.ID {
		return d, nil
	}
	allow := false
	switch r.Operation {
	case "trash":
		allow = r.Facts.DeletionStatus == "active"
	case "restore":
		allow = r.Facts.DeletionStatus == "trashed"
	case "submit_review":
		allow = r.Facts.DeletionStatus == "active" && (r.Facts.ModerationStatus == "taken_down" || r.Facts.ModerationStatus == "rejected")
	}
	if allow {
		d.Effect, d.Reason = "allow", "ALLOW"
		d.Obligations = []string{"recheck_facts", "required_audit"}
	}
	return d, nil
}
func CheckThreadOwnerWriteDecision(initial port.ThreadOwnerWriteRequest, d port.ThreadOwnerWriteDecision, current port.ThreadOwnerWriteRequest) error {
	expected, err := (ThreadOwnerWritePolicy{}).DecideThreadOwnerWrite(initial)
	if err != nil || expected.Effect != "allow" || !reflect.DeepEqual(expected, d) || !reflect.DeepEqual(initial, current) {
		return port.ErrPolicyFactsChanged
	}
	return nil
}
