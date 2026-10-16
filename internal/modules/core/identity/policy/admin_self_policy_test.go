package policy

import (
	"github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"testing"
)

func TestAdminSPASelfReadNamedDomainBindingAndForgedAllowClose(t *testing.T) {
	for _, kind := range []string{"admin", "user"} {
		for _, status := range []string{"active", "suspended", "revoked"} {
			for _, strength := range []string{"password", "mfa"} {
				for _, expired := range []bool{false, true} {
					for _, same := range []bool{false, true} {
						p := port.PrincipalContext{Contract: port.PrincipalContract, Actor: port.PrincipalRef{Kind: kind, ID: "adm.opaque"}, Audience: kind, AuthenticationStrength: strength, CredentialID: "adms.opaque"}
						r := port.AdminSelfReadRequest{RequestID: "self-read", PolicyVersion: port.AdminSelfReadPolicyVersion, Principal: p, AccountID: "adm.opaque", Status: status, SessionID: "adms.opaque", AccountVersion: 1, SessionVersion: 1, ExpiresAtMS: 2000, EvaluatedAtMS: 1000}
						if expired {
							r.EvaluatedAtMS = 2000
						}
						if !same {
							r.SessionID = "adms.other"
						}
						d, err := (AdminSelfReadPolicy{}).DecideAdminSelfRead(r)
						allow := kind == "admin" && status == "active" && !expired && same
						if err != nil || (d.Effect == "allow") != allow {
							t.Fatal(kind, status, strength, expired, same, d, err)
						}
						if (port.CheckAdminSelfReadDecision(r, d, r) == nil) != allow {
							t.Fatal("decision binding")
						}
						d.Effect = "allow"
						if !allow && port.CheckAdminSelfReadDecision(r, d, r) == nil {
							t.Fatal("forged allow accepted")
						}
						if allow {
							current := r
							current.AccountVersion++
							if port.CheckAdminSelfReadDecision(r, d, current) == nil {
								t.Fatal("stale account version accepted")
							}
						}
					}
				}
			}
		}
	}
}
