package policy

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"

	"github.com/campusos/CampusOS/internal/modules/core/identity/port"
)

func ownerReadRequest() port.ThreadOwnerReadRequest {
	return port.ThreadOwnerReadRequest{
		Contract: port.ThreadOwnerReadContract, RequestID: "owner-request-1", Policy: port.ThreadOwnerReadPolicyVersion,
		Principal: port.PrincipalContext{Contract: port.PrincipalContract,
			Actor: port.PrincipalRef{Kind: "user", ID: "user-1"}, Audience: "user", AuthenticationStrength: "password", CredentialID: "session-1"},
		Facts: port.ThreadOwnerReadFacts{
			Resource: port.PolicyResourceRef{Kind: "community.thread", ID: "thread-1"},
			Owner:    port.PrincipalRef{Kind: "user", ID: "user-1"}, Board: port.PolicyResourceRef{Kind: "community.board", ID: "board-1"},
			FactsVersion: "facts-1", PublicationStatus: "private", ModerationStatus: "clear", DeletionStatus: "active",
		},
	}
}

func ownerReadPrincipal(kind string) port.PrincipalContext {
	p := ownerReadRequest().Principal
	p.Actor.Kind = kind
	switch kind {
	case "anonymous":
		p.Actor.ID, p.Audience, p.AuthenticationStrength, p.CredentialID = "", "public", "none", ""
	case "admin":
		p.Audience, p.AuthenticationStrength = "admin", "mfa"
	case "integration":
		p.Audience, p.AuthenticationStrength = "integration_api", "credential"
	case "plugin_instance", "worker":
		p.Audience, p.AuthenticationStrength = "host_api", "workload"
		if kind == "worker" {
			p.Audience = "worker"
		}
		p.EffectiveSubject, p.DelegationID = &port.PrincipalRef{Kind: "user", ID: "user-1"}, "delegation-1"
	}
	return p
}

func TestThreadOwnerReadPolicyRealmStateMatrix(t *testing.T) {
	combinations, allows := 0, 0
	for _, kind := range []string{"anonymous", "user", "admin", "integration", "plugin_instance", "worker"} {
		for _, publication := range []string{"draft", "published", "private"} {
			for _, moderation := range []string{"clear", "pending", "rejected", "taken_down"} {
				for _, deletion := range []string{"active", "trashed", "purged"} {
					t.Run(fmt.Sprintf("%s/%s/%s/%s", kind, publication, moderation, deletion), func(t *testing.T) {
						r := ownerReadRequest()
						r.Principal = ownerReadPrincipal(kind)
						r.Facts.PublicationStatus, r.Facts.ModerationStatus, r.Facts.DeletionStatus = publication, moderation, deletion
						d, err := (ThreadOwnerReadPolicy{}).DecideThreadOwnerRead(r)
						if err != nil || d.Validate() != nil || d.RequestID != r.RequestID || d.Contract != r.Contract ||
							d.Policy != r.Policy || d.Resource != r.Facts.Resource || d.FactsVersion != r.Facts.FactsVersion ||
							!reflect.DeepEqual(d.Principal, r.Principal) {
							t.Fatalf("unbound or invalid decision: %+v err=%v", d, err)
						}
						combinations++
						allowed := kind == "user" && deletion != "purged"
						consumeErr := port.CheckThreadOwnerReadDecision(r, d, r.Facts, r.Principal)
						if allowed {
							allows++
							if d.Effect != "allow" || d.Reason != "policy.owner_read" ||
								!reflect.DeepEqual(d.Obligations, []string{"recheck_current_facts"}) || consumeErr != nil {
								t.Fatalf("owner unavailable: %+v consume=%v", d, consumeErr)
							}
						} else if d.Effect != "deny" || d.Reason != "policy.owner_read_denied" || len(d.Obligations) != 0 ||
							!errors.Is(consumeErr, port.ErrPolicyOwnerReadDenied) {
							t.Fatalf("other domain or purged resource allowed: %+v consume=%v", d, consumeErr)
						}
					})
				}
			}
		}
	}
	if combinations != 216 || allows != 24 {
		t.Fatalf("matrix incomplete: combinations=%d allows=%d", combinations, allows)
	}
}

func TestThreadOwnerReadPolicyNonOwnerAndMFA(t *testing.T) {
	for _, strength := range []string{"password", "mfa"} {
		for _, owner := range []bool{false, true} {
			r := ownerReadRequest()
			r.Principal.AuthenticationStrength = strength
			if !owner {
				r.Principal.Actor.ID = "another-user"
			}
			d, err := (ThreadOwnerReadPolicy{}).DecideThreadOwnerRead(r)
			if err != nil || (d.Effect == "allow") != owner {
				t.Fatalf("owner=%t strength=%s: %+v err=%v", owner, strength, d, err)
			}
		}
	}
}

func TestThreadOwnerReadPolicyRejectsInvalidInputs(t *testing.T) {
	for name, mutate := range map[string]func(*port.ThreadOwnerReadRequest){
		"contract-missing":    func(r *port.ThreadOwnerReadRequest) { r.Contract = "" },
		"policy-missing":      func(r *port.ThreadOwnerReadRequest) { r.Policy = "" },
		"public-policy":       func(r *port.ThreadOwnerReadRequest) { r.Policy = port.ThreadPublicReadPolicyVersion },
		"request-missing":     func(r *port.ThreadOwnerReadRequest) { r.RequestID = "" },
		"principal-missing":   func(r *port.ThreadOwnerReadRequest) { r.Principal = port.PrincipalContext{} },
		"credential-missing":  func(r *port.ThreadOwnerReadRequest) { r.Principal.CredentialID = "" },
		"audience":            func(r *port.ThreadOwnerReadRequest) { r.Principal.Audience = "admin" },
		"strength":            func(r *port.ThreadOwnerReadRequest) { r.Principal.AuthenticationStrength = "workload" },
		"unknown-actor":       func(r *port.ThreadOwnerReadRequest) { r.Principal.Actor.Kind = "owner" },
		"resource-missing":    func(r *port.ThreadOwnerReadRequest) { r.Facts.Resource.ID = "" },
		"resource-kind":       func(r *port.ThreadOwnerReadRequest) { r.Facts.Resource.Kind = "thread" },
		"owner-missing":       func(r *port.ThreadOwnerReadRequest) { r.Facts.Owner.ID = "" },
		"owner-kind":          func(r *port.ThreadOwnerReadRequest) { r.Facts.Owner.Kind = "admin" },
		"board-missing":       func(r *port.ThreadOwnerReadRequest) { r.Facts.Board.ID = "" },
		"version-missing":     func(r *port.ThreadOwnerReadRequest) { r.Facts.FactsVersion = "" },
		"publication-missing": func(r *port.ThreadOwnerReadRequest) { r.Facts.PublicationStatus = "" },
		"publication-unknown": func(r *port.ThreadOwnerReadRequest) { r.Facts.PublicationStatus = "all" },
		"moderation-missing":  func(r *port.ThreadOwnerReadRequest) { r.Facts.ModerationStatus = "" },
		"moderation-unknown":  func(r *port.ThreadOwnerReadRequest) { r.Facts.ModerationStatus = "archived" },
		"deletion-missing":    func(r *port.ThreadOwnerReadRequest) { r.Facts.DeletionStatus = "" },
		"deletion-unknown":    func(r *port.ThreadOwnerReadRequest) { r.Facts.DeletionStatus = "deleted" },
	} {
		t.Run(name, func(t *testing.T) {
			r := ownerReadRequest()
			mutate(&r)
			d, err := (ThreadOwnerReadPolicy{}).DecideThreadOwnerRead(r)
			if !errors.Is(err, port.ErrPolicyInputInvalid) || !reflect.DeepEqual(d, port.ThreadOwnerReadDecision{}) {
				t.Fatalf("invalid input acquired a decision: %+v err=%v", d, err)
			}
		})
	}
	for _, contract := range []string{"campusos.policy.thread-owner-read/v2", port.ThreadPublicReadContract} {
		r := port.ThreadOwnerReadRequest{Contract: contract}
		if _, err := (ThreadOwnerReadPolicy{}).DecideThreadOwnerRead(r); !errors.Is(err, port.ErrPolicyContractUnsupported) {
			t.Fatalf("unknown contract %s: %v", contract, err)
		}
	}
}

func TestThreadOwnerReadPolicyDoesNotAliasOrCache(t *testing.T) {
	r := ownerReadRequest()
	r.Principal = ownerReadPrincipal("plugin_instance")
	d, err := (ThreadOwnerReadPolicy{}).DecideThreadOwnerRead(r)
	if err != nil {
		t.Fatal(err)
	}
	r.Principal.EffectiveSubject.ID = "changed-subject"
	if d.Principal.EffectiveSubject.ID != "user-1" {
		t.Fatal("decision principal aliases the input's mutable subject")
	}
	var workers sync.WaitGroup
	failures := make(chan error, 64)
	for i := 0; i < 64; i++ {
		workers.Add(1)
		go func(i int) {
			defer workers.Done()
			r := ownerReadRequest()
			r.RequestID, r.Facts.FactsVersion = fmt.Sprintf("call-%d", i), fmt.Sprintf("version-%d", i)
			if i%2 == 1 {
				r.Principal.Actor.ID = "other-user"
			}
			d, err := (ThreadOwnerReadPolicy{}).DecideThreadOwnerRead(r)
			if err != nil || d.RequestID != r.RequestID || d.FactsVersion != r.Facts.FactsVersion || (d.Effect == "allow") != (i%2 == 0) {
				failures <- fmt.Errorf("call %d reused an unrelated result: %+v err=%v", i, d, err)
			}
		}(i)
	}
	workers.Wait()
	close(failures)
	for err := range failures {
		t.Error(err)
	}
}
