package policy

import (
	"github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"testing"
)

func TestSpaceAdminPolicyMatrixAndCompleteBinding(t *testing.T) {
	for _, op := range []string{"disable", "enable"} {
		for bits := 0; bits < 32; bits++ {
			r := port.PersonalSpaceAdminRequest{RequestID: "request", PolicyVersion: port.PersonalSpaceAdminPolicyVersion, Operation: op, OwnerID: "1001", SpaceID: "2001", UserStatus: "suspended", Exists: true, FactsVersion: 7, Principal: port.PrincipalContext{Contract: port.PrincipalContract, Actor: port.PrincipalRef{Kind: "admin", ID: "1001"}, Audience: "admin", CredentialID: "session", AuthenticationStrength: "mfa"}, ExecutionQuery: port.AdminExecutionQuery{PermissionCode: "personal_space.space.manage", Scope: port.PolicyResourceRef{Kind: "personal_space.space", ID: "1001"}}, ExecutionAllowed: true, ExecutionExpiresAtMS: 2000, EvaluatedAtMS: 1000}
			if bits&1 != 0 {
				r.Principal.Actor.Kind, r.Principal.Audience = "user", "user"
			}
			if bits&2 != 0 {
				r.Principal.AuthenticationStrength = "password"
			}
			if bits&4 != 0 {
				r.ExecutionAllowed = false
			}
			if bits&8 != 0 {
				r.EvaluatedAtMS = 2000
			}
			if bits&16 != 0 {
				r.Exists = false
				r.SpaceID = ""
				r.FactsVersion = 0
			}
			d, err := (PersonalSpaceAdminPolicy{}).DecidePersonalSpaceAdmin(r)
			if err != nil || (d.Effect == "allow") != (bits&15 == 0) {
				t.Fatal(bits, d, err)
			}
			if d.Effect == "allow" {
				if err := CheckPersonalSpaceAdminDecision(r, d, r); err != nil {
					t.Fatal(err)
				}
				current := r
				current.Disabled = true
				if CheckPersonalSpaceAdminDecision(r, d, current) == nil {
					t.Fatal("changed facts accepted")
				}
				forged := d
				forged.Reason = "forged"
				if CheckPersonalSpaceAdminDecision(r, forged, r) == nil {
					t.Fatal("forged decision accepted")
				}
			}
		}
	}
	q := port.AdminExecutionQuery{PermissionCode: "personal_space.space.manage", Scope: port.PolicyResourceRef{Kind: "personal_space.space", ID: "1001"}}
	if q.Validate() != nil || !q.RequiresCurrentMFA() {
		t.Fatal(q)
	}
	q.Scope.Kind = "richtext.user_asset"
	if q.Validate() == nil {
		t.Fatal("unbound scope accepted")
	}
}
