package policy

import (
	"github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"testing"
)

func TestAssetAdminLifecyclePurePolicy(t *testing.T) {
	count := 0
	for _, op := range []string{"quarantine", "restore"} {
		for _, kind := range []string{"user", "admin"} {
			for _, strength := range []string{"password", "mfa"} {
				for _, allowed := range []bool{false, true} {
					for _, expired := range []bool{false, true} {
						for _, status := range []string{"active", "trashed", "quarantined", "purging", "deleted"} {
							for _, assetKind := range []string{"article_attachment", "richtext_image"} {
								for _, refs := range []int{0, 2} {
									r := port.RichTextAssetAdminRequest{Contract: port.RichTextAssetAdminContract, PolicyVersion: port.RichTextAssetAdminPolicyVersion, RequestID: "test.asset.admin", Operation: op,
										Principal:      port.PrincipalContext{Contract: port.PrincipalContract, Actor: port.PrincipalRef{Kind: kind, ID: "1001"}, Audience: kind, AuthenticationStrength: strength, CredentialID: "actual-session"},
										Facts:          port.RichTextAssetOwnerFacts{ID: "asset.actual", OwnerID: "other.owner", Kind: assetKind, Status: status, FactsVersion: "3", ReferenceCount: refs},
										ExecutionQuery: port.AdminExecutionQuery{PermissionCode: "personal_space.asset.manage", Scope: port.PolicyResourceRef{Kind: "richtext.user_asset", ID: "asset.actual"}}, ExecutionAllowed: allowed, EvaluatedAtMS: 1000, ExecutionExpiresAtMS: 2000}
									if expired {
										r.ExecutionExpiresAtMS = 1000
									}
									d, err := (RichTextAssetAdminPolicy{}).DecideRichTextAssetAdmin(r)
									want := kind == "admin" && strength == "mfa" && allowed && !expired && (op == "quarantine" && (status == "active" || status == "trashed") || op == "restore" && status == "quarantined")
									if err != nil || (d.Effect == "allow") != want {
										t.Fatal(r, d, err)
									}
									if want {
										if err := CheckRichTextAssetAdminDecision(r, d, r); err != nil {
											t.Fatal(err)
										}
										changed := r
										changed.Facts.FactsVersion = "4"
										if CheckRichTextAssetAdminDecision(r, d, changed) == nil {
											t.Fatal("nonce replay")
										}
										changed = r
										changed.Principal.CredentialID = "another"
										if CheckRichTextAssetAdminDecision(r, d, changed) == nil {
											t.Fatal("credential replay")
										}
										changed = r
										changed.ExecutionQuery.Scope.Kind = "community.board"
										if changed.Validate() == nil {
											t.Fatal("foreign scope")
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
	}
	if count != 640 {
		t.Fatal(count)
	}
}
