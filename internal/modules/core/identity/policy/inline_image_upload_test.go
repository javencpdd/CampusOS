package policy

import (
	"github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"testing"
)

func TestInlineImageUploadFixedOwnerDomainPolicy(t *testing.T) {
	for _, namespace := range []string{"content", "richtext"} {
		for _, kind := range []string{"user", "admin", "integration", "plugin_instance", "anonymous"} {
			for _, owner := range []string{"1001", "1002"} {
				p := port.PrincipalContext{Contract: port.PrincipalContract, Actor: port.PrincipalRef{Kind: kind, ID: "1001"}, Audience: kind, AuthenticationStrength: "password", CredentialID: "current-session"}
				switch kind {
				case "integration":
					p.Audience, p.AuthenticationStrength = "integration_api", "credential"
				case "plugin_instance":
					p.Audience, p.AuthenticationStrength = "host_api", "workload"
				case "anonymous":
					p.Actor.ID, p.Audience, p.AuthenticationStrength, p.CredentialID = "", "public", "none", ""
				}
				r := port.InlineImageUploadRequest{RequestID: "request", PolicyVersion: port.InlineImageUploadPolicyVersion, Namespace: namespace, OwnerID: owner, Principal: p}
				d, err := (InlineImageUploadPolicy{}).DecideInlineImageUpload(r)
				if err != nil || (d.Effect == "allow") != (kind == "user" && owner == "1001") {
					t.Fatal(r, d, err)
				}
				if d.Effect == "allow" {
					if err := CheckInlineImageUploadDecision(r, d, r); err != nil {
						t.Fatal(err)
					}
					changed := r
					changed.Principal.CredentialID = "new-session"
					if CheckInlineImageUploadDecision(r, d, changed) == nil {
						t.Fatal("credential change reused allow")
					}
					d.Obligations = nil
					if CheckInlineImageUploadDecision(r, d, r) == nil {
						t.Fatal("forged decision accepted")
					}
				}
			}
		}
	}
}
