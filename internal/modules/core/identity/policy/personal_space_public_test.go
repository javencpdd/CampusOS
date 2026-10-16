package policy

import (
	"github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"testing"
)

func TestSpaceCurrentReadPublicPolicyAndOwnerReadOnlyMatrix(t *testing.T) {
	for _, kind := range []string{"anonymous", "user", "admin"} {
		for _, visibility := range []string{"public", "unlisted", "private"} {
			for _, disabled := range []bool{false, true} {
				for _, status := range []string{"active", "suspended", "deactivated"} {
					for _, exists := range []bool{false, true} {
						for _, op := range []string{"profile", "contents"} {
							principal := port.PrincipalContext{Contract: port.PrincipalContract, Actor: port.PrincipalRef{Kind: kind, ID: "1001"}, Audience: kind, AuthenticationStrength: "password", CredentialID: "current"}
							if kind == "anonymous" {
								principal.Actor.ID = ""
								principal.Audience = "public"
								principal.AuthenticationStrength = "none"
								principal.CredentialID = ""
							}
							r := port.PersonalSpacePublicRequest{RequestID: "public-read", PolicyVersion: port.PersonalSpacePublicPolicyVersion, Operation: op, OwnerID: "1001", Principal: principal, Visibility: visibility, UserStatus: status, Exists: exists, Disabled: disabled}
							if exists {
								r.FactsVersion = 1
							}
							d, err := (PersonalSpacePublicPolicy{}).DecidePersonalSpacePublic(r)
							want := kind == "anonymous" && visibility == "public" && !disabled && status == "active"
							if err != nil || (d.Effect == "allow") != want {
								t.Fatal(r, d, err)
							}
							if (port.CheckPersonalSpacePublicDecision(r, d, r) == nil) != want {
								t.Fatal("checked decision", r, d)
							}
							fake := d
							fake.Effect = "allow"
							fake.Reason = "policy.public_space_read"
							if !want && port.CheckPersonalSpacePublicDecision(r, fake, r) == nil {
								t.Fatal("forged allow accepted")
							}
							changed := r
							changed.Disabled = !r.Disabled
							if port.CheckPersonalSpacePublicDecision(r, d, changed) == nil {
								t.Fatal("stale facts accepted")
							}
						}
					}
				}
			}
		}
	}
	for _, op := range []string{"read_contents", "read_sync_status"} {
		for _, disabled := range []bool{false, true} {
			r := port.PersonalSpaceOwnerRequest{RequestID: "owner-read", PolicyVersion: port.PersonalSpaceOwnerPolicyVersion, Operation: op, OwnerID: "1001", Principal: port.PrincipalContext{Contract: port.PrincipalContract, Actor: port.PrincipalRef{Kind: "user", ID: "1001"}, Audience: "user", AuthenticationStrength: "password", CredentialID: "current"}, Exists: true, FactsVersion: 1, Disabled: disabled}
			d, err := (PersonalSpaceOwnerPolicy{}).DecidePersonalSpaceOwner(r)
			if err != nil || d.Effect != "allow" || len(d.Obligations) != 1 || d.Obligations[0] != "recheck_facts" {
				t.Fatal(r, d, err)
			}
		}
	}
}
