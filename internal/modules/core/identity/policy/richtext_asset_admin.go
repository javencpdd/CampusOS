package policy

import (
	"github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"reflect"
)

// Independent Admin metadata governance grants no private file read.
type RichTextAssetAdminPolicy struct{}

func (RichTextAssetAdminPolicy) DecideRichTextAssetAdmin(r port.RichTextAssetAdminRequest) (port.RichTextAssetAdminDecision, error) {
	if err := r.Validate(); err != nil {
		return port.RichTextAssetAdminDecision{}, err
	}
	d := port.RichTextAssetAdminDecision{Request: r, Effect: "deny", Reason: "policy.asset_admin_lifecycle_denied"}
	state := r.Operation == "quarantine" && (r.Facts.Status == "active" || r.Facts.Status == "trashed") || r.Operation == "restore" && r.Facts.Status == "quarantined"
	if r.Principal.Actor.Kind == "admin" && r.Principal.Audience == "admin" && r.Principal.AuthenticationStrength == "mfa" && r.ExecutionAllowed && r.EvaluatedAtMS < r.ExecutionExpiresAtMS && state {
		d.Effect, d.Reason = "allow", "policy.asset_admin_lifecycle"
	}
	return d, nil
}
func CheckRichTextAssetAdminDecision(initial port.RichTextAssetAdminRequest, d port.RichTextAssetAdminDecision, current port.RichTextAssetAdminRequest) error {
	expected, err := (RichTextAssetAdminPolicy{}).DecideRichTextAssetAdmin(initial)
	if err != nil || expected.Effect != "allow" || !reflect.DeepEqual(expected, d) || !reflect.DeepEqual(initial, current) {
		return port.ErrPolicyFactsChanged
	}
	return nil
}
