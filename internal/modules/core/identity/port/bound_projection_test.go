package port

import (
	"context"
	"errors"
	"testing"
)

func TestBoundProjectionFreshProofAndWholeSnapshot(t *testing.T) {
	for _, mode := range []string{"stable", "revoked", "credential", "actor", "snapshot", "empty"} {
		t.Run(mode, func(t *testing.T) {
			loads, proofs, closes := 0, 0, 0
			resolve := func(context.Context) (PrincipalContext, error) {
				proofs++
				p := PrincipalContext{Contract: PrincipalContract, Actor: PrincipalRef{Kind: "user", ID: "owner"}, Audience: "user", CredentialID: "session", AuthenticationStrength: "password"}
				if proofs > 1 {
					switch mode {
					case "revoked":
						return PrincipalContext{}, ErrUserSessionPrincipalUnavailable
					case "credential":
						p.CredentialID = "changed"
					case "actor":
						p.Actor.ID = "other"
					}
				}
				return p, nil
			}
			result, err := ReadBoundProjection(context.Background(), resolve, false,
				func(ctx context.Context, p PrincipalContext) ([]string, error) {
					loads++
					if SessionPrincipalResolverFromContext(ctx) == nil || p.Actor.ID != "owner" {
						t.Fatal("missing trusted context")
					}
					if mode == "empty" {
						return []string{}, nil
					}
					if mode == "snapshot" && loads == 2 {
						return []string{"different"}, nil
					}
					return []string{"member"}, nil
				},
				func(_ context.Context, _ PrincipalContext, _ []string) (string, error) { return "private-body", nil },
				func(value string) {
					if value != "private-body" {
						t.Fatal("lost owned result during error return")
					}
					closes++
				})
			allowed := mode == "stable" || mode == "empty"
			if (err == nil) != allowed || (!allowed && result != "") {
				t.Fatalf("result=%q err=%v", result, err)
			}
			if allowed && (proofs < 3 || loads != 2 || closes != 0) {
				t.Fatal("missing final proof")
			}
			if !allowed && closes != 1 {
				t.Fatalf("unclosed result: %d", closes)
			}
		})
	}
}

func TestBoundProjectionExplicitPublicAndFailClosed(t *testing.T) {
	load := func(_ context.Context, p PrincipalContext) (string, error) {
		if p.Actor.Kind != "anonymous" || p.Validate() != nil {
			t.Fatal("invalid public principal")
		}
		return "public-snapshot", nil
	}
	read := func(_ context.Context, _ PrincipalContext, s string) (string, error) { return s, nil }
	if _, err := ReadBoundProjection(context.Background(), nil, false, load, read, nil); !errors.Is(err, ErrUserSessionPrincipalUnavailable) {
		t.Fatal(err)
	}
	if result, err := ReadBoundProjection(context.Background(), nil, true, load, read, nil); err != nil || result != "public-snapshot" {
		t.Fatalf("%q %v", result, err)
	}
	if _, err := ReadBoundProjection[string, string](context.Background(), nil, true, nil, read, nil); !errors.Is(err, ErrPolicyDecisionMismatch) {
		t.Fatal(err)
	}
	closed := 0
	brokenRead := func(context.Context, PrincipalContext, string) (string, error) {
		return "partial-reader", ErrPolicyDecisionMismatch
	}
	if _, err := ReadBoundProjection(context.Background(), nil, true, load, brokenRead, func(string) { closed++ }); err == nil || closed != 1 {
		t.Fatal("read error did not close partial result")
	}
}
