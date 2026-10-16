package policy

import (
	"errors"
	"testing"

	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
)

func TestPostAuthorPolicyDomainStateAndBinding(t *testing.T) {
	p := identityport.PrincipalContext{Contract: identityport.PrincipalContract, Actor: identityport.PrincipalRef{Kind: "user", ID: "owner"}, Audience: "user", AuthenticationStrength: "password", CredentialID: "session"}
	for _, op := range []string{"create", "edit", "delete"} {
		for _, kind := range []string{"user", "other", "admin"} {
			for _, pub := range []string{"published", "private", "draft"} {
				for _, mod := range []string{"clear", "pending", "rejected", "taken_down"} {
					for _, deletion := range []string{"active", "trashed", "purged"} {
						current := p
						if kind == "other" {
							current.Actor.ID = "other"
						}
						if kind == "admin" {
							current.Actor.Kind, current.Audience, current.AuthenticationStrength = "admin", "admin", "mfa"
						}
						r := identityport.PostOwnerWriteRequest{Contract: identityport.PostOwnerWriteContract, PolicyVersion: identityport.PostOwnerWritePolicyVersion, RequestID: "request", Principal: current, Operation: op, Parent: identityport.ThreadPublicReadFacts{Resource: identityport.PolicyResourceRef{Kind: "community.thread", ID: "thread"}, Owner: identityport.PrincipalRef{Kind: "user", ID: "owner"}, Board: identityport.PolicyResourceRef{Kind: "community.board", ID: "board"}, PublicationStatus: pub, ModerationStatus: mod, DeletionStatus: deletion, FactsVersion: "1"}}
						if op != "create" {
							r.Post = &identityport.PostOwnerWriteFacts{ID: "post", ThreadID: "thread", OwnerID: "owner", Status: "published", Version: 1}
						}
						d, err := (PostOwnerWritePolicy{}).DecidePostOwnerWrite(r)
						if err != nil {
							t.Fatal(r, err)
						}
						want := deletion == "active" && kind != "admin"
						if op == "create" {
							want = want && (kind == "user" || pub == "published" && mod == "clear")
						} else {
							want = want && kind == "user"
						}
						if (d.Effect == "allow") != want {
							t.Fatal(r, d, want)
						}
						if want {
							changed := r
							changed.Parent.FactsVersion = "2"
							if err := CheckPostOwnerWriteDecision(r, d, changed); !errors.Is(err, identityport.ErrPolicyFactsChanged) {
								t.Fatal("parent ABA allow reused", err)
							}
							changed = r
							changed.Principal.CredentialID = "replacement"
							if err := CheckPostOwnerWriteDecision(r, d, changed); !errors.Is(err, identityport.ErrPolicyFactsChanged) {
								t.Fatal("credential change accepted", err)
							}
							if err := CheckPostOwnerWriteDecision(r, d, r); err != nil {
								t.Fatal(err)
							}
						}
					}
				}
			}
		}
	}
	locked := identityport.PostOwnerWriteRequest{Contract: identityport.PostOwnerWriteContract, PolicyVersion: identityport.PostOwnerWritePolicyVersion, RequestID: "request", Principal: p, Operation: "create", Parent: identityport.ThreadPublicReadFacts{Resource: identityport.PolicyResourceRef{Kind: "community.thread", ID: "thread"}, Owner: identityport.PrincipalRef{Kind: "user", ID: "owner"}, Board: identityport.PolicyResourceRef{Kind: "community.board", ID: "board"}, PublicationStatus: "published", ModerationStatus: "clear", DeletionStatus: "active", FactsVersion: "1"}, ParentLocked: true}
	d, err := (PostOwnerWritePolicy{}).DecidePostOwnerWrite(locked)
	if err != nil || d.Effect != "deny" {
		t.Fatal(d, err)
	}
}
