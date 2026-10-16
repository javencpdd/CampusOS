package policy

import (
	"github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"testing"
)

func TestAdminThreadMetadataNamedPolicyReadScopeStateDomainAndBinding(t *testing.T) {
	for _, kind := range []string{"user", "admin"} {
		for _, allowed := range []bool{false, true} {
			for _, deletion := range []string{"active", "trashed", "purged"} {
				for _, operation := range []string{"list", "trash", "history"} {
					for _, expired := range []bool{false, true} {
						p := port.PrincipalContext{Contract: port.PrincipalContract, Actor: port.PrincipalRef{Kind: kind, ID: "same.id"}, Audience: kind, AuthenticationStrength: "password", CredentialID: "current.session"}
						r := port.ThreadAdminMetadataRequest{PolicyVersion: port.ThreadAdminMetadataPolicyVersion, RequestID: "request", Operation: operation, Principal: p, Thread: port.ThreadPublicReadFacts{Resource: port.PolicyResourceRef{Kind: "community.thread", ID: "thread"}, Owner: port.PrincipalRef{Kind: "user", ID: "same.id"}, Board: port.PolicyResourceRef{Kind: "community.board", ID: "board"}, FactsVersion: "1", PublicationStatus: "private", ModerationStatus: "pending", DeletionStatus: deletion}, ExecutionQuery: port.AdminExecutionQuery{PermissionCode: "community.thread.read", Scope: port.PolicyResourceRef{Kind: "community.board", ID: "board"}}, ExecutionAllowed: allowed, ExecutionExpiresAtMS: 1000, EvaluatedAtMS: 500}
						if expired {
							r.EvaluatedAtMS = 1000
						}
						d, err := (ThreadAdminMetadataPolicy{}).DecideThreadAdminMetadata(r)
						want := kind == "admin" && allowed && !expired && (operation == "history" || operation == "list" && deletion == "active" || operation == "trash" && deletion == "trashed")
						if err != nil || (d.Effect == "allow") != want {
							t.Fatal(r, d, err)
						}
						if (port.CheckThreadAdminMetadataDecision(r, d, r) == nil) != want {
							t.Fatal("bound decision", r, d)
						}
						changed := r
						changed.Thread.FactsVersion = "2"
						if port.CheckThreadAdminMetadataDecision(r, d, changed) == nil {
							t.Fatal("stale decision")
						}
					}
				}
			}
		}
	}
	r := port.ThreadAdminMetadataRequest{}
	if _, err := (ThreadAdminMetadataPolicy{}).DecideThreadAdminMetadata(r); err == nil {
		t.Fatal("missing facts allowed")
	}
	for _, q := range []port.AdminExecutionQuery{{PermissionCode: "community.thread.pin", Scope: port.PolicyResourceRef{Kind: "community.thread_inventory", ID: "all"}}, {PermissionCode: "community.thread.read", Scope: port.PolicyResourceRef{Kind: "community.thread_inventory", ID: "client"}}, {PermissionCode: "community.thread.read", Scope: port.PolicyResourceRef{Kind: "community.thread", ID: "123"}}} {
		if q.Validate() == nil {
			t.Fatal("wrong scope allowed", q)
		}
	}
}
