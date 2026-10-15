package policy

import (
	"github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"testing"
)

func TestSpaceOwnerPolicyActualDomainStateAndBoundDecision(t *testing.T) {
	for _, op := range []string{"profile", "read_profile", "avatar_select", "avatar_upload", "avatar_retention", "style_apply", "style_default", "style_rollback", "style_html", "style_zip", "style_source", "style_catalog"} {
		for _, kind := range []string{"user", "admin"} {
			for _, owner := range []string{"1001", "1002"} {
				for _, exists := range []bool{false, true} {
					for _, disabled := range []bool{false, true} {
						r := port.PersonalSpaceOwnerRequest{RequestID: "request", PolicyVersion: port.PersonalSpaceOwnerPolicyVersion, Operation: op, OwnerID: owner, Principal: port.PrincipalContext{Contract: port.PrincipalContract, Actor: port.PrincipalRef{Kind: kind, ID: "1001"}, Audience: kind, AuthenticationStrength: "password", CredentialID: "current-session"}, Exists: exists, Disabled: disabled}
						if exists {
							r.FactsVersion = 17
						}
						d, err := (PersonalSpaceOwnerPolicy{}).DecidePersonalSpaceOwner(r)
						if err != nil || (d.Effect == "allow") != (kind == "user" && owner == "1001" && (op == "read_profile" || op == "style_catalog" || !disabled)) {
							t.Fatal(r, d, err)
						}
						if d.Effect == "allow" {
							if err := CheckPersonalSpaceOwnerDecision(r, d, r); err != nil {
								t.Fatal(err)
							}
							changed := r
							changed.Principal.CredentialID = "new-session"
							if CheckPersonalSpaceOwnerDecision(r, d, changed) == nil {
								t.Fatal("credential change accepted")
							}
							d.Obligations = nil
							if CheckPersonalSpaceOwnerDecision(r, d, r) == nil {
								t.Fatal("forged decision accepted")
							}
						}
					}
				}
			}
		}
	}
}
