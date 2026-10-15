package policy

import (
	"github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"reflect"
)

type RichTextAssetPurgePolicy struct{}

func (RichTextAssetPurgePolicy) DecideRichTextAssetPurge(r port.RichTextAssetPurgeRequest) (port.RichTextAssetPurgeDecision, error) {
	if err := r.Validate(); err != nil {
		return port.RichTextAssetPurgeDecision{}, err
	}
	d := port.RichTextAssetPurgeDecision{Request: r, Effect: "deny", Reason: "policy.asset_purge_denied"}
	state := r.Facts.Status == "trashed" || r.Facts.Status == "quarantined" || r.Facts.Status == "purging"
	if r.Principal.Actor.Kind == "admin" && r.Principal.Audience == "admin" && r.Principal.AuthenticationStrength == "mfa" && r.ExecutionAllowed && r.EvaluatedAtMS < r.ExecutionExpiresAtMS && state && r.Facts.ReferenceCount == 0 {
		d.Effect, d.Reason = "allow", "policy.asset_purge"
	}
	return d, nil
}
func CheckRichTextAssetPurgeDecision(initial port.RichTextAssetPurgeRequest, d port.RichTextAssetPurgeDecision, current port.RichTextAssetPurgeRequest) error {
	expected, err := (RichTextAssetPurgePolicy{}).DecideRichTextAssetPurge(initial)
	if err != nil || expected.Effect != "allow" || !reflect.DeepEqual(expected, d) || !reflect.DeepEqual(initial, current) {
		return port.ErrPolicyFactsChanged
	}
	return nil
}

type RichTextAssetSummaryPolicy struct{}

func (RichTextAssetSummaryPolicy) DecideRichTextAssetSummary(r port.RichTextAssetSummaryRequest) (port.RichTextAssetSummaryDecision, error) {
	if err := r.Validate(); err != nil {
		return port.RichTextAssetSummaryDecision{}, err
	}
	d := port.RichTextAssetSummaryDecision{Request: r, Effect: "deny", Reason: "policy.asset_summary_denied"}
	if r.Principal.Actor.Kind == "admin" && r.Principal.Audience == "admin" && r.ExecutionAllowed && r.EvaluatedAtMS < r.ExecutionExpiresAtMS {
		d.Effect, d.Reason = "allow", "policy.asset_summary"
	}
	return d, nil
}
