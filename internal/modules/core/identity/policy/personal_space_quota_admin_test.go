package policy

import (
	"github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"testing"
)

func TestSpaceQuotaAdminNamedWriteAndMetadataReadPolicyMatrix(t *testing.T) {
	for _, operation := range []string{"quota", "storage_status", "space_summary"} {
		for bits := 0; bits < 32; bits++ {
			p := port.PrincipalContext{Contract: port.PrincipalContract, Actor: port.PrincipalRef{Kind: "admin", ID: "1001"}, Audience: "admin", AuthenticationStrength: "mfa", CredentialID: "session"}
			if bits&1 != 0 {
				p.Actor.Kind, p.Audience = "user", "user"
			}
			if bits&2 != 0 {
				p.AuthenticationStrength = "password"
			}
			allowed := bits&4 == 0
			at := int64(1000)
			if bits&8 != 0 {
				at = 2000
			}
			kind := "personal_space." + operation
			owner, status := "1001", "suspended"
			id := owner
			if operation == "space_summary" {
				owner, status, id = "", "", "summary"
			}
			if operation == "quota" {
				kind = "personal_space.storage_quota"
			}
			q := port.AdminExecutionQuery{PermissionCode: "personal_space.space.manage", Scope: port.PolicyResourceRef{Kind: kind, ID: id}}
			if err := q.Validate(); err != nil || q.RequiresCurrentMFA() != (operation == "quota") {
				t.Fatal(q, err)
			}
			if operation == "quota" {
				r := port.PersonalSpaceQuotaAdminRequest{RequestID: "request", PolicyVersion: port.PersonalSpaceQuotaAdminPolicyVersion, OwnerID: owner, UserStatus: status, Principal: p, Exists: true, FactsVersion: 7, RequestedQuotaBytes: 2097152, ExecutionQuery: q, ExecutionAllowed: allowed, ExecutionExpiresAtMS: 2000, EvaluatedAtMS: at}
				if bits&16 != 0 {
					r.Exists = false
					r.FactsVersion = 0
				}
				d, err := (PersonalSpaceQuotaAdminPolicy{}).DecidePersonalSpaceQuotaAdmin(r)
				if err != nil || (d.Effect == "allow") != (bits&15 == 0) {
					t.Fatal(bits, d, err)
				}
				if d.Effect == "allow" {
					if err := CheckPersonalSpaceQuotaAdminDecision(r, d, r); err != nil {
						t.Fatal(err)
					}
					current := r
					current.RequestedQuotaBytes++
					if CheckPersonalSpaceQuotaAdminDecision(r, d, current) == nil {
						t.Fatal("changed quota request accepted")
					}
					current = r
					current.FactsVersion++
					if CheckPersonalSpaceQuotaAdminDecision(r, d, current) == nil {
						t.Fatal("changed quota version accepted")
					}
				}
				r.RequestedQuotaBytes = 107374182401
				if _, err := (PersonalSpaceQuotaAdminPolicy{}).DecidePersonalSpaceQuotaAdmin(r); err == nil {
					t.Fatal("unbounded quota accepted")
				}
			} else {
				r := port.PersonalSpaceAdminReadRequest{RequestID: "request", PolicyVersion: port.PersonalSpaceAdminReadPolicyVersion, Operation: operation, OwnerID: owner, UserStatus: status, Principal: p, ExecutionQuery: q, ExecutionAllowed: allowed, ExecutionExpiresAtMS: 2000, EvaluatedAtMS: at}
				d, err := (PersonalSpaceAdminReadPolicy{}).DecidePersonalSpaceAdminRead(r)
				if err != nil || (d.Effect == "allow") != (bits&13 == 0) {
					t.Fatal(bits, d, err)
				}
				r.ExecutionQuery.Scope.Kind = "personal_space.space"
				if _, err := (PersonalSpaceAdminReadPolicy{}).DecidePersonalSpaceAdminRead(r); err == nil {
					t.Fatal("wrong read scope accepted")
				}
			}
		}
	}
}
