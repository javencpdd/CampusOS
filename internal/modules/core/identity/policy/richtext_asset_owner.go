package policy

import (
	"github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"reflect"
)

// Separate fixed owner lifecycle rules; the frozen five ResourcePolicy
// actions and independent Admin governance remain unchanged.
type RichTextAssetOwnerPolicy struct{}

func (RichTextAssetOwnerPolicy) DecideRichTextAssetOwner(r port.RichTextAssetOwnerRequest) (port.RichTextAssetOwnerDecision, error) {
	if err := r.Validate(); err != nil {
		return port.RichTextAssetOwnerDecision{}, err
	}
	d := port.RichTextAssetOwnerDecision{RequestID: r.RequestID, Effect: "deny", Reason: "policy.scope_denied", FactsVersion: r.Facts.FactsVersion, PolicyVersion: r.PolicyVersion, Obligations: []string{}}
	if r.Principal.Actor.Kind != "user" || r.Principal.Audience != "user" {
		d.Reason = "policy.principal_wrong_domain"
		return d, nil
	}
	if r.Principal.Actor.ID != r.Facts.OwnerID {
		return d, nil
	}
	allow := r.Operation == "trash" && r.Facts.Status == "active" && r.Facts.ReferenceCount == 0 || r.Operation == "restore" && r.Facts.Status == "trashed"
	if allow {
		d.Effect, d.Reason = "allow", "ALLOW"
		d.Obligations = []string{"recheck_facts", "required_audit"}
	}
	return d, nil
}
func CheckRichTextAssetOwnerDecision(initial port.RichTextAssetOwnerRequest, d port.RichTextAssetOwnerDecision, current port.RichTextAssetOwnerRequest) error {
	expected, err := (RichTextAssetOwnerPolicy{}).DecideRichTextAssetOwner(initial)
	if err != nil || expected.Effect != "allow" || !reflect.DeepEqual(expected, d) || !reflect.DeepEqual(initial, current) {
		return port.ErrPolicyFactsChanged
	}
	return nil
}
