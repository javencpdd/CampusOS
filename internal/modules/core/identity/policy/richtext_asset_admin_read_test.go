package policy

import (
	"github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"testing"
)

func TestAssetAdminReadPurePolicies(t *testing.T) {
	count := 0
	for _, kind := range []string{"user", "admin"} {
		for _, strength := range []string{"password", "mfa"} {
			for _, allowed := range []bool{false, true} {
				for _, expired := range []bool{false, true} {
					p := port.PrincipalContext{Contract: port.PrincipalContract, Actor: port.PrincipalRef{Kind: kind, ID: "1001"}, Audience: kind, AuthenticationStrength: strength, CredentialID: "current"}
					expiry := int64(2000)
					if expired {
						expiry = 1000
					}
					s := port.RichTextAssetSummaryRequest{Contract: port.RichTextAssetSummaryContract, PolicyVersion: port.RichTextAssetSummaryPolicyVersion, RequestID: "summary.actual", Principal: p, ExecutionQuery: port.AdminExecutionQuery{PermissionCode: "personal_space.asset.read_audit", Scope: port.PolicyResourceRef{Kind: "richtext.asset_governance", ID: "summary"}}, ExecutionAllowed: allowed, EvaluatedAtMS: 1000, ExecutionExpiresAtMS: expiry}
					d, err := (RichTextAssetSummaryPolicy{}).DecideRichTextAssetSummary(s)
					if err != nil || (d.Effect == "allow") != (kind == "admin" && allowed && !expired) {
						t.Fatal(s, d, err)
					}
					wrong := s
					wrong.ExecutionQuery.Scope.ID = "other"
					if wrong.Validate() == nil {
						t.Fatal("aggregate scope replay")
					}
					for _, status := range []string{"active", "trashed", "quarantined", "purging", "deleted"} {
						for _, refs := range []int{0, 2} {
							for _, assetKind := range []string{"article_attachment", "richtext_image"} {
								r := port.RichTextAssetPurgeRequest{Contract: port.RichTextAssetPurgeContract, PolicyVersion: port.RichTextAssetPurgePolicyVersion, RequestID: "purge.actual", Principal: p, Facts: port.RichTextAssetOwnerFacts{ID: "actual.asset", OwnerID: "other.owner", Kind: assetKind, Status: status, FactsVersion: "2", ReferenceCount: refs}, ExecutionQuery: port.AdminExecutionQuery{PermissionCode: "personal_space.asset.manage", Scope: port.PolicyResourceRef{Kind: "richtext.user_asset", ID: "actual.asset"}}, ExecutionAllowed: allowed, EvaluatedAtMS: 1000, ExecutionExpiresAtMS: expiry}
								d, err := (RichTextAssetPurgePolicy{}).DecideRichTextAssetPurge(r)
								want := kind == "admin" && strength == "mfa" && allowed && !expired && refs == 0 && (status == "trashed" || status == "quarantined" || status == "purging")
								if err != nil || (d.Effect == "allow") != want {
									t.Fatal(r, d, err)
								}
								if want {
									if err := CheckRichTextAssetPurgeDecision(r, d, r); err != nil {
										t.Fatal(err)
									}
									changed := r
									changed.Facts.FactsVersion = "3"
									if CheckRichTextAssetPurgeDecision(r, d, changed) == nil {
										t.Fatal("stale purge")
									}
								}
								count++
							}
						}
					}
				}
			}
		}
	}
	if count != 320 {
		t.Fatal(count)
	}
}
