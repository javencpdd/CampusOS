package policy

import (
	"errors"
	"testing"

	"github.com/campusos/CampusOS/internal/modules/core/identity/port"
)

func TestThreadAdminFlagPolicyDomainStateAuthorityAndBinding(t *testing.T) {
	for _, op := range []string{"pin", "unpin", "lock", "unlock"} {
		for _, actor := range []string{"admin", "user", "anonymous"} {
			for _, strength := range []string{"password", "mfa"} {
				if actor == "anonymous" && strength == "mfa" {
					continue
				}
				for _, pub := range []string{"published", "private", "draft"} {
					for _, mod := range []string{"clear", "pending", "rejected", "taken_down"} {
						for _, deletion := range []string{"active", "trashed", "purged"} {
							for _, allowed := range []bool{false, true} {
								p := port.PrincipalContext{Contract: port.PrincipalContract, Actor: port.PrincipalRef{Kind: actor, ID: "owner"}, Audience: actor, AuthenticationStrength: strength, CredentialID: "credential"}
								if actor == "anonymous" {
									p.Actor.ID, p.Audience, p.AuthenticationStrength, p.CredentialID = "", "public", "none", ""
								}
								code := "community.thread.pin"
								if op == "lock" || op == "unlock" {
									code = "community.thread.lock"
								}
								r := port.ThreadAdminFlagRequest{Contract: port.ThreadAdminFlagContract, PolicyVersion: port.ThreadAdminFlagPolicyVersion, RequestID: "request", Operation: op, Principal: p,
									Thread:         port.ThreadPublicReadFacts{Resource: port.PolicyResourceRef{Kind: "community.thread", ID: "thread"}, Owner: port.PrincipalRef{Kind: "user", ID: "owner"}, Board: port.PolicyResourceRef{Kind: "community.board", ID: "board"}, FactsVersion: "1", PublicationStatus: pub, ModerationStatus: mod, DeletionStatus: deletion},
									ExecutionQuery: port.AdminExecutionQuery{PermissionCode: code, Scope: port.PolicyResourceRef{Kind: "community.board", ID: "board"}}, ExecutionAllowed: allowed, ExecutionExpiresAtMS: 2000, EvaluatedAtMS: 1000}
								d, err := (ThreadAdminFlagPolicy{}).DecideThreadAdminFlag(r)
								if err != nil {
									t.Fatal(r, err)
								}
								want := actor == "admin" && strength == "mfa" && allowed && deletion == "active"
								if (d.Effect == "allow") != want {
									t.Fatal(r, d, want)
								}
								if want {
									for _, change := range []func(*port.ThreadAdminFlagRequest){
										func(v *port.ThreadAdminFlagRequest) { v.Thread.FactsVersion = "2" },
										func(v *port.ThreadAdminFlagRequest) { v.Principal.CredentialID = "replacement" },
										func(v *port.ThreadAdminFlagRequest) { v.Pinned = true },
										func(v *port.ThreadAdminFlagRequest) { v.Locked = true },
										func(v *port.ThreadAdminFlagRequest) { v.ExecutionAllowed = false },
										func(v *port.ThreadAdminFlagRequest) { v.ExecutionExpiresAtMS = 1999 },
									} {
										current := r
										change(&current)
										if !errors.Is(port.CheckThreadAdminFlagDecision(r, d, current), port.ErrPolicyFactsChanged) {
											t.Fatal("changed input replayed")
										}
									}
									r.EvaluatedAtMS = r.ExecutionExpiresAtMS
									if expired, err := (ThreadAdminFlagPolicy{}).DecideThreadAdminFlag(r); err != nil || expired.Effect != "deny" {
										t.Fatal(expired, err)
									}
								}
							}
						}
					}
				}
			}
		}
	}
}
