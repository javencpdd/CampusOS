package policy

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/campusos/CampusOS/internal/modules/core/identity/port"
)

func publicPolicyFixture(t *testing.T) port.ThreadPublicReadRequest {
	t.Helper()
	return port.ThreadPublicReadRequest{
		Contract: port.ThreadPublicReadContract, Policy: port.ThreadPublicReadPolicyVersion, RequestID: "request-1",
		Principal: port.PrincipalContext{Contract: port.PrincipalContract, Actor: port.PrincipalRef{Kind: "anonymous"}, Audience: "public", AuthenticationStrength: "none"},
		Facts: port.ThreadPublicReadFacts{Resource: port.PolicyResourceRef{Kind: "community.thread", ID: "thread-1"},
			Owner: port.PrincipalRef{Kind: "user", ID: "user-1"}, Board: port.PolicyResourceRef{Kind: "community.board", ID: "board-1"},
			FactsVersion: "facts-1", PublicationStatus: "published", ModerationStatus: "clear", DeletionStatus: "active"},
	}
}

func principalPolicyFixtures(t *testing.T) []port.PrincipalContext {
	t.Helper()
	raw, err := os.ReadFile("../../../../../sdk/typescript/tests/principal-context-v1.fixtures.json")
	if err != nil {
		t.Fatal(err)
	}
	var corpus struct {
		Cases []struct {
			Valid   bool
			Context json.RawMessage
		}
	}
	if err = json.Unmarshal(raw, &corpus); err != nil {
		t.Fatal(err)
	}
	var principals []port.PrincipalContext
	for _, c := range corpus.Cases {
		if !c.Valid {
			continue
		}
		p, err := port.ParsePrincipalJSON(c.Context)
		if err != nil {
			t.Fatal(err)
		}
		principals = append(principals, p)
	}
	if len(principals) != 10 {
		t.Fatalf("expected all 10 G1 principals, got %d", len(principals))
	}
	return principals
}

func TestThreadPublicReadG1Fixtures(t *testing.T) {
	raw, err := os.ReadFile("../../../../../sdk/typescript/tests/policy-thread-public-read-v1.fixtures.json")
	if err != nil {
		t.Fatal(err)
	}
	var corpus struct {
		Cases []struct {
			Name, Target string
			Valid        bool
			Value        json.RawMessage
		}
	}
	if err = json.Unmarshal(raw, &corpus); err != nil {
		t.Fatal(err)
	}
	if len(corpus.Cases) != 60 {
		t.Fatalf("expected 60 G1 cases, got %d", len(corpus.Cases))
	}
	for _, c := range corpus.Cases {
		t.Run(c.Name, func(t *testing.T) {
			var err error
			if c.Target == "request" {
				var r port.ThreadPublicReadRequest
				r, err = port.ParseThreadPublicReadRequestJSON(c.Value)
				if err == nil {
					d, evalErr := (ThreadPublicReadPolicy{}).DecideThreadPublicRead(r)
					if evalErr != nil || d.Validate() != nil {
						t.Fatalf("invalid evaluation: %v", evalErr)
					}
					encoded, marshalErr := json.Marshal(d)
					if marshalErr != nil {
						t.Fatal(marshalErr)
					}
					if _, decodeErr := port.ParseThreadPublicReadDecisionJSON(encoded); decodeErr != nil {
						t.Fatal(decodeErr)
					}
				}
			} else if c.Target == "decision" {
				_, err = port.ParseThreadPublicReadDecisionJSON(c.Value)
			} else {
				t.Fatal("unknown fixture target")
			}
			if (err == nil) != c.Valid {
				t.Fatalf("valid=%v error=%v", c.Valid, err)
			}
			if c.Name == "unsupported-contract" && !errors.Is(err, port.ErrPolicyContractUnsupported) {
				t.Fatalf("wrong version error: %v", err)
			}
		})
	}
}

func TestThreadPublicReadStatePrincipalMatrix(t *testing.T) {
	principals := principalPolicyFixtures(t)
	count, allows, owners, sameIDAdmins, delegatedOwners := 0, 0, 0, 0, 0
	var wireOutputs []map[string]any
	for _, publication := range []string{"draft", "published", "private"} {
		for _, moderation := range []string{"clear", "pending", "rejected", "taken_down"} {
			for _, deletion := range []string{"active", "trashed", "purged"} {
				for _, p := range principals {
					r := publicPolicyFixture(t)
					r.Principal = p
					switch {
					case p.Actor.Kind == "user":
						r.Facts.Owner.ID = p.Actor.ID
						owners++
					case p.Actor.Kind == "admin":
						r.Facts.Owner.ID = p.Actor.ID
						sameIDAdmins++
					case p.EffectiveSubject != nil:
						r.Facts.Owner.ID = p.EffectiveSubject.ID
						delegatedOwners++
					}
					r.Facts.PublicationStatus, r.Facts.ModerationStatus, r.Facts.DeletionStatus = publication, moderation, deletion
					before, _ := json.Marshal(r)
					d, err := (ThreadPublicReadPolicy{}).DecideThreadPublicRead(r)
					if err != nil || d.Validate() != nil {
						t.Fatalf("decision invalid: %v", err)
					}
					expected := strings.Join([]string{publication, moderation, deletion}, "/") == "published/clear/active"
					if (d.Effect == "allow") != expected {
						t.Fatalf("tuple %s/%s/%s actor %s", publication, moderation, deletion, p.Actor.Kind)
					}
					if got := port.CheckThreadPublicReadDecision(r, d, r.Facts); expected && got != nil || !expected && !errors.Is(got, port.ErrPolicyResourceNotPublic) {
						t.Fatalf("consume: %v", got)
					}
					after, _ := json.Marshal(r)
					if !bytes.Equal(before, after) {
						t.Fatal("input mutated")
					}
					encoded, _ := json.Marshal(d)
					for _, sensitive := range []string{"owner", "principal", "credential", "board", "publication_status"} {
						if bytes.Contains(encoded, []byte(sensitive)) {
							t.Fatalf("decision leaks %s", sensitive)
						}
					}
					wireOutputs = append(wireOutputs, map[string]any{"request": r, "decision": d})
					count++
					if expected {
						allows++
					}
				}
			}
		}
	}
	if count != 360 || allows != 10 || owners != 72 || sameIDAdmins != 72 || delegatedOwners != 72 {
		t.Fatalf("matrix counts: %d %d %d %d %d", count, allows, owners, sameIDAdmins, delegatedOwners)
	}
	if path := os.Getenv("CAMPUSOS_POLICY_CORPUS_OUT"); path != "" {
		raw, err := json.Marshal(wireOutputs)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestThreadPublicReadDecisionBindingAndCurrentFacts(t *testing.T) {
	type mutate func(*port.ThreadPublicReadRequest, *port.ThreadPublicReadDecision, *port.ThreadPublicReadFacts)
	cases := []struct {
		name string
		edit mutate
		want error
	}{
		{"unchanged", func(_ *port.ThreadPublicReadRequest, _ *port.ThreadPublicReadDecision, _ *port.ThreadPublicReadFacts) {
		}, nil},
		{"request_id", func(_ *port.ThreadPublicReadRequest, d *port.ThreadPublicReadDecision, _ *port.ThreadPublicReadFacts) {
			d.RequestID = "other"
		}, port.ErrPolicyDecisionMismatch},
		{"policy", func(_ *port.ThreadPublicReadRequest, d *port.ThreadPublicReadDecision, _ *port.ThreadPublicReadFacts) {
			d.Policy = "community.thread.public_read/v2"
		}, port.ErrPolicyDecisionMismatch},
		{"contract", func(_ *port.ThreadPublicReadRequest, d *port.ThreadPublicReadDecision, _ *port.ThreadPublicReadFacts) {
			d.Contract += "2"
		}, port.ErrPolicyDecisionMismatch},
		{"resource", func(_ *port.ThreadPublicReadRequest, d *port.ThreadPublicReadDecision, _ *port.ThreadPublicReadFacts) {
			d.Resource.ID = "other"
		}, port.ErrPolicyDecisionMismatch},
		{"typed_resource", func(_ *port.ThreadPublicReadRequest, d *port.ThreadPublicReadDecision, _ *port.ThreadPublicReadFacts) {
			d.Resource.Kind = "community.board"
		}, port.ErrPolicyDecisionMismatch},
		{"version", func(_ *port.ThreadPublicReadRequest, d *port.ThreadPublicReadDecision, _ *port.ThreadPublicReadFacts) {
			d.FactsVersion = "other"
		}, port.ErrPolicyDecisionMismatch},
		{"missing_obligation", func(_ *port.ThreadPublicReadRequest, d *port.ThreadPublicReadDecision, _ *port.ThreadPublicReadFacts) {
			d.Obligations = nil
		}, port.ErrPolicyDecisionMismatch},
		{"forged_allow_private", func(r *port.ThreadPublicReadRequest, _ *port.ThreadPublicReadDecision, c *port.ThreadPublicReadFacts) {
			r.Facts.PublicationStatus = "private"
			c.PublicationStatus = "private"
		}, port.ErrPolicyDecisionMismatch},
		{"publication_without_version", func(_ *port.ThreadPublicReadRequest, _ *port.ThreadPublicReadDecision, c *port.ThreadPublicReadFacts) {
			c.PublicationStatus = "private"
		}, port.ErrPolicyFactsChanged},
		{"moderation_without_version", func(_ *port.ThreadPublicReadRequest, _ *port.ThreadPublicReadDecision, c *port.ThreadPublicReadFacts) {
			c.ModerationStatus = "taken_down"
		}, port.ErrPolicyFactsChanged},
		{"deletion_without_version", func(_ *port.ThreadPublicReadRequest, _ *port.ThreadPublicReadDecision, c *port.ThreadPublicReadFacts) {
			c.DeletionStatus = "trashed"
		}, port.ErrPolicyFactsChanged},
		{"content_revision_or_ABA", func(_ *port.ThreadPublicReadRequest, _ *port.ThreadPublicReadDecision, c *port.ThreadPublicReadFacts) {
			c.FactsVersion = "facts-3"
		}, port.ErrPolicyFactsChanged},
		{"current_resource", func(_ *port.ThreadPublicReadRequest, _ *port.ThreadPublicReadDecision, c *port.ThreadPublicReadFacts) {
			c.Resource.ID = "other"
		}, port.ErrPolicyFactsChanged},
		{"current_owner", func(_ *port.ThreadPublicReadRequest, _ *port.ThreadPublicReadDecision, c *port.ThreadPublicReadFacts) {
			c.Owner.ID = "other"
		}, port.ErrPolicyFactsChanged},
		{"current_board", func(_ *port.ThreadPublicReadRequest, _ *port.ThreadPublicReadDecision, c *port.ThreadPublicReadFacts) {
			c.Board.ID = "other"
		}, port.ErrPolicyFactsChanged},
		{"missing_current_fact", func(_ *port.ThreadPublicReadRequest, _ *port.ThreadPublicReadDecision, c *port.ThreadPublicReadFacts) {
			c.ModerationStatus = ""
		}, port.ErrPolicyFactsChanged},
		{"invalid_current", func(_ *port.ThreadPublicReadRequest, _ *port.ThreadPublicReadDecision, c *port.ThreadPublicReadFacts) {
			*c = port.ThreadPublicReadFacts{}
		}, port.ErrPolicyFactsChanged},
		{"unsupported_original_contract", func(r *port.ThreadPublicReadRequest, _ *port.ThreadPublicReadDecision, _ *port.ThreadPublicReadFacts) {
			r.Contract = "future"
		}, port.ErrPolicyInputInvalid},
		{"invalid_original", func(r *port.ThreadPublicReadRequest, _ *port.ThreadPublicReadDecision, _ *port.ThreadPublicReadFacts) {
			r.Principal.Actor.Kind = "superadmin"
		}, port.ErrPolicyInputInvalid},
		{"deny_before_recheck", func(_ *port.ThreadPublicReadRequest, d *port.ThreadPublicReadDecision, c *port.ThreadPublicReadFacts) {
			d.Effect = "deny"
			d.Reason = "policy.resource_not_public"
			d.Obligations = []string{}
			*c = port.ThreadPublicReadFacts{}
		}, port.ErrPolicyResourceNotPublic},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := publicPolicyFixture(t)
			d, err := (ThreadPublicReadPolicy{}).DecideThreadPublicRead(r)
			if err != nil {
				t.Fatal(err)
			}
			current := r.Facts
			c.edit(&r, &d, &current)
			if got := port.CheckThreadPublicReadDecision(r, d, current); !errors.Is(got, c.want) {
				t.Fatalf("want %v got %v", c.want, got)
			}
		})
	}
}

func TestThreadPublicReadStrictJSON(t *testing.T) {
	raw, _ := json.Marshal(publicPolicyFixture(t))
	base := string(raw)
	cases := []struct {
		name, raw string
		want      error
	}{
		{"duplicate_top", strings.Replace(base, `"request_id":"request-1"`, `"request_id":"x","request_id":"request-1"`, 1), port.ErrPolicyInputInvalid},
		{"duplicate_nested", strings.Replace(base, `"owner":{"kind":"user"`, `"owner":{"kind":"admin","kind":"user"`, 1), port.ErrPolicyInputInvalid},
		{"duplicate_principal", strings.Replace(base, `"audience":"public"`, `"audience":"admin","audience":"public"`, 1), port.ErrPolicyInputInvalid},
		{"case_alias", strings.Replace(base, `"publication_status"`, `"Publication_Status"`, 1), port.ErrPolicyInputInvalid},
		{"null_field", strings.Replace(base, `"deletion_status":"active"`, `"deletion_status":null`, 1), port.ErrPolicyInputInvalid},
		{"anonymous_null_id", strings.Replace(base, `"kind":"anonymous"`, `"kind":"anonymous","id":null`, 1), port.ErrPolicyInputInvalid},
		{"trailing_object", base + " {}", port.ErrPolicyInputInvalid},
		{"oversized", strings.Repeat(" ", 32768) + base, port.ErrPolicyInputInvalid},
		{"unknown_contract_first", `{"contract":"unsupported","surprise":null}`, port.ErrPolicyContractUnsupported},
		{"null_contract", `{"contract":null}`, port.ErrPolicyInputInvalid},
		{"empty_contract_string", `{"contract":""}`, port.ErrPolicyContractUnsupported},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := port.ParseThreadPublicReadRequestJSON([]byte(c.raw)); !errors.Is(err, c.want) {
				t.Fatalf("want %v got %v", c.want, err)
			}
		})
	}
	// Malformed typed callers cannot bypass the byte codec's semantic checks.
	for _, mutation := range []func(*port.ThreadPublicReadRequest){
		func(r *port.ThreadPublicReadRequest) { r.Facts.PublicationStatus = "" },
		func(r *port.ThreadPublicReadRequest) { r.Facts.ModerationStatus = "unknown" },
		func(r *port.ThreadPublicReadRequest) { r.Principal.Audience = "admin" },
		func(r *port.ThreadPublicReadRequest) { r.Policy = "owner" },
	} {
		r := publicPolicyFixture(t)
		mutation(&r)
		d, err := (ThreadPublicReadPolicy{}).DecideThreadPublicRead(r)
		if !errors.Is(err, port.ErrPolicyInputInvalid) || d.Effect == "allow" {
			t.Fatalf("invalid input authorized: %v", err)
		}
	}
}

func TestThreadPublicReadParallelWithoutCache(t *testing.T) {
	var policy port.ThreadPublicReadPolicy = ThreadPublicReadPolicy{}
	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r := publicPolicyFixture(t)
			r.RequestID = fmt.Sprintf("request-%d", i)
			first, err := policy.DecideThreadPublicRead(r)
			if err != nil {
				t.Error(err)
				return
			}
			first.Obligations[0] = "mutated"
			second, err := policy.DecideThreadPublicRead(r)
			if err != nil || second.Validate() != nil || second.RequestID != r.RequestID {
				t.Errorf("shared output state: %v", err)
			}
			r.Facts.ModerationStatus = "taken_down"
			denied, err := policy.DecideThreadPublicRead(r)
			if err != nil || denied.Effect != "deny" {
				t.Errorf("stale authorization: %v", err)
			}
		}(i)
	}
	wg.Wait()
}
