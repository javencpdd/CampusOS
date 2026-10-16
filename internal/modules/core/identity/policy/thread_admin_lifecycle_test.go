package policy

import (
	"errors"
	"testing"

	"github.com/campusos/CampusOS/internal/modules/core/identity/port"
)

func TestThreadAdminLifecyclePolicyDomainStateAuthorityAndBinding(t *testing.T) {
	for _, op := range []string{"trash", "restore", "direct_restore"} {
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
								code := "community.thread." + op
								r := port.ThreadAdminLifecycleRequest{Contract: port.ThreadAdminLifecycleContract, PolicyVersion: port.ThreadAdminLifecyclePolicyVersion, RequestID: "request", Operation: op, Principal: p,
									Thread:         port.ThreadPublicReadFacts{Resource: port.PolicyResourceRef{Kind: "community.thread", ID: "thread"}, Owner: port.PrincipalRef{Kind: "user", ID: "owner"}, Board: port.PolicyResourceRef{Kind: "community.board", ID: "board"}, FactsVersion: "1", PublicationStatus: pub, ModerationStatus: mod, DeletionStatus: deletion},
									ExecutionQuery: port.AdminExecutionQuery{PermissionCode: code, Scope: port.PolicyResourceRef{Kind: "community.board", ID: "board"}}, ExecutionAllowed: allowed, ExecutionExpiresAtMS: 2000, EvaluatedAtMS: 1000}
								d, err := (ThreadAdminLifecyclePolicy{}).DecideThreadAdminLifecycle(r)
								if err != nil {
									t.Fatal(r, err)
								}
								state := deletion == "active"
								if op == "restore" {
									state = deletion == "trashed"
								}
								if op == "direct_restore" {
									state = state && mod == "taken_down"
								}
								want := actor == "admin" && strength == "mfa" && allowed && state
								if (d.Effect == "allow") != want {
									t.Fatal(r, d, want)
								}
								if want {
									if err := port.CheckThreadAdminLifecycleDecision(r, d, r); err != nil {
										t.Fatal(err)
									}
									for _, change := range []func(*port.ThreadAdminLifecycleRequest){
										func(v *port.ThreadAdminLifecycleRequest) { v.Thread.FactsVersion = "2" },
										func(v *port.ThreadAdminLifecycleRequest) { v.Principal.CredentialID = "replacement" },
										func(v *port.ThreadAdminLifecycleRequest) { v.ExecutionAllowed = false },
										func(v *port.ThreadAdminLifecycleRequest) { v.ExecutionExpiresAtMS = 1999 },
									} {
										current := r
										change(&current)
										if !errors.Is(port.CheckThreadAdminLifecycleDecision(r, d, current), port.ErrPolicyFactsChanged) {
											t.Fatal("changed input replayed")
										}
									}
									r.EvaluatedAtMS = r.ExecutionExpiresAtMS
									if expired, err := (ThreadAdminLifecyclePolicy{}).DecideThreadAdminLifecycle(r); err != nil || expired.Effect != "deny" {
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
