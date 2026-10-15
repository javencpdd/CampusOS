package policy

import (
	"errors"
	"testing"

	"github.com/campusos/CampusOS/internal/modules/core/identity/port"
)

func TestPostReadPolicyMatrixAndBoundDecision(t *testing.T) {
	for _, kind := range []string{"anonymous", "user", "other", "admin"} {
		for _, pub := range []string{"draft", "published", "private"} {
			for _, mod := range []string{"clear", "pending", "rejected", "taken_down"} {
				for _, deletion := range []string{"active", "trashed", "purged"} {
					for _, op := range []string{"list", "detail"} {
						p := port.PrincipalContext{Contract: port.PrincipalContract, Actor: port.PrincipalRef{Kind: "user", ID: "u1"}, Audience: "user", AuthenticationStrength: "password", CredentialID: "s1"}
						switch kind {
						case "anonymous":
							p = port.PrincipalContext{Contract: port.PrincipalContract, Actor: port.PrincipalRef{Kind: "anonymous"}, Audience: "public", AuthenticationStrength: "none"}
						case "other":
							p.Actor.ID = "u2"
						case "admin":
							p.Actor.Kind = "admin"
							p.Audience = "admin"
							p.AuthenticationStrength = "mfa"
						}
						r := port.PostReadRequest{Contract: port.PostReadContract, PolicyVersion: port.PostReadPolicyVersion, RequestID: "request-1", Operation: op, Principal: p, Parent: port.ThreadPublicReadFacts{Resource: port.PolicyResourceRef{Kind: "community.thread", ID: "t1"}, Owner: port.PrincipalRef{Kind: "user", ID: "u1"}, Board: port.PolicyResourceRef{Kind: "community.board", ID: "b1"}, FactsVersion: "1", PublicationStatus: pub, ModerationStatus: mod, DeletionStatus: deletion}}
						if op == "detail" {
							r.Post = &port.PostOwnerWriteFacts{ID: "p1", ThreadID: "t1", OwnerID: "u2", Status: "published", Version: 2}
						}
						d, err := (PostReadPolicy{}).DecidePostRead(r)
						if err != nil {
							t.Fatal(err)
						}
						want := kind != "admin" && deletion == "active" && (kind == "user" || (pub == "published" && mod == "clear"))
						if (d.Effect == "allow") != want {
							t.Fatal(kind, pub, mod, deletion, op, d)
						}
						if !want {
							continue
						}
						if err := port.CheckPostReadDecision(r, d, r); err != nil {
							t.Fatal(err)
						}
						changed := r
						changed.Principal.CredentialID = "s2"
						if !errors.Is(port.CheckPostReadDecision(r, d, changed), port.ErrPolicyFactsChanged) {
							t.Fatal("credential replay accepted")
						}
						changed = r
						changed.Parent.FactsVersion = "3"
						if !errors.Is(port.CheckPostReadDecision(r, d, changed), port.ErrPolicyFactsChanged) {
							t.Fatal("parent replay accepted")
						}
						if r.Post != nil {
							x := *r.Post
							x.Version++
							changed = r
							changed.Post = &x
							if !errors.Is(port.CheckPostReadDecision(r, d, changed), port.ErrPolicyFactsChanged) {
								t.Fatal("post replay accepted")
							}
							x = *r.Post
							x.Deleted = true
							r.Post = &x
							denied, err := (PostReadPolicy{}).DecidePostRead(r)
							if err != nil || denied.Effect != "deny" {
								t.Fatal(denied, err)
							}
						}
					}
				}
			}
		}
	}
}
