package policy

import (
	"fmt"
	"github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"testing"
)

func TestAssetOwnerLifecyclePurePolicy(t *testing.T) {
	for _, kind := range []string{"user", "admin"} {
		for _, owner := range []string{"1", "2"} {
			for _, assetKind := range []string{"article_attachment", "richtext_image"} {
				for _, status := range []string{"active", "trashed", "quarantined", "purging", "deleted"} {
					for _, refs := range []int{0, 1} {
						for _, operation := range []string{"trash", "restore"} {
							p := port.PrincipalContext{Contract: port.PrincipalContract, Actor: port.PrincipalRef{Kind: kind, ID: "1"}, Audience: kind, CredentialID: "current-session", AuthenticationStrength: "password"}
							if kind == "admin" {
								p.AuthenticationStrength = "mfa"
							}
							r := port.RichTextAssetOwnerRequest{Contract: port.RichTextAssetOwnerContract, RequestID: "request", PolicyVersion: port.RichTextAssetOwnerPolicyVersion, Principal: p, Operation: operation, Facts: port.RichTextAssetOwnerFacts{ID: "asset", OwnerID: owner, Kind: assetKind, Status: status, FactsVersion: "7", ReferenceCount: refs}}
							d, err := (RichTextAssetOwnerPolicy{}).DecideRichTextAssetOwner(r)
							want := kind == "user" && owner == "1" && (operation == "trash" && status == "active" && refs == 0 || operation == "restore" && status == "trashed")
							if err != nil || (d.Effect == "allow") != want {
								t.Fatal(fmt.Sprint(kind, owner, assetKind, status, refs, operation), d, err)
							}
							if want {
								if err := CheckRichTextAssetOwnerDecision(r, d, r); err != nil {
									t.Fatal(err)
								}
								current := r
								current.Facts.FactsVersion = "8"
								if err := CheckRichTextAssetOwnerDecision(r, d, current); err == nil {
									t.Fatal("changed nonce reused decision")
								}
								current = r
								current.Principal.CredentialID = "new-session"
								if err := CheckRichTextAssetOwnerDecision(r, d, current); err == nil {
									t.Fatal("changed credential reused decision")
								}
							}
						}
					}
				}
			}
		}
	}
}
