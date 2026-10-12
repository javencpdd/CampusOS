package port

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/campusos/CampusOS/pkg/auth"
)

type userSessionProofTest struct {
	calls          []string
	strength       string
	verifyAt       int
	verifyError    error
	strengthErr    error
	afterRead      func()
	verifyCount    int
	mutateClaims   bool
	currentVersion int64
	revoked        bool
}

func (p *userSessionProofTest) VerifyAccess(_ context.Context, claims *auth.JWTClaims) error {
	p.calls = append(p.calls, "verify")
	p.verifyCount++
	if claims.UserID != "user-1" || claims.SessionID != "session-1" || claims.AuthVersion != 2 || claims.TokenType != auth.AccessTokenType {
		return errors.New("invalid session owner/version")
	}
	currentVersion := p.currentVersion
	if currentVersion == 0 {
		currentVersion = 2
	}
	if p.revoked || claims.AuthVersion != currentVersion {
		return errors.New("current session revoked or account version changed")
	}
	if p.verifyCount == p.verifyAt {
		return p.verifyError
	}
	if p.mutateClaims {
		claims.UserID = "other-user"
		claims.SessionID = "other-session"
	}
	return nil
}

func (p *userSessionProofTest) AuthenticationStrengthForSession(_ context.Context, id string) (string, error) {
	p.calls = append(p.calls, "strength")
	if id != "session-1" {
		return "", errors.New("wrong credential")
	}
	if p.afterRead != nil {
		p.afterRead()
	}
	return p.strength, p.strengthErr
}

func userSessionClaimsTest() *auth.JWTClaims {
	return &auth.JWTClaims{UserID: "user-1", SessionID: "session-1", AuthVersion: 2, TokenType: auth.AccessTokenType}
}

func TestResolveUserSessionPrincipalUsesCurrentProof(t *testing.T) {
	for _, strength := range []string{"password", "mfa"} {
		t.Run(strength, func(t *testing.T) {
			proof := &userSessionProofTest{strength: strength, mutateClaims: true}
			claims := userSessionClaimsTest()
			principal, err := ResolveUserSessionPrincipal(context.Background(), proof, proof, claims)
			want := PrincipalContext{Contract: PrincipalContract, Actor: PrincipalRef{Kind: "user", ID: "user-1"},
				Audience: "user", CredentialID: "session-1", AuthenticationStrength: strength}
			if err != nil || !reflect.DeepEqual(principal, want) || !reflect.DeepEqual(proof.calls, []string{"verify", "strength", "verify"}) {
				t.Fatalf("proof got=%+v calls=%v err=%v", principal, proof.calls, err)
			}
			if !reflect.DeepEqual(claims, userSessionClaimsTest()) {
				t.Fatal("principal resolution changed the input authority tuple")
			}
		})
	}
}

func TestResolveUserSessionPrincipalRejectsMissingAndInvalidProof(t *testing.T) {
	for _, name := range []string{"nil-claims", "nil-verifier", "nil-reader", "typed-nil-verifier", "typed-nil-reader", "no-user", "bad-user", "no-session", "bad-session", "zero-version", "negative-version", "refresh", "no-type"} {
		t.Run(name, func(t *testing.T) {
			claims := userSessionClaimsTest()
			proof := &userSessionProofTest{strength: "password"}
			var verifier SessionVerifier = proof
			var reader SessionStrengthReader = proof
			switch name {
			case "nil-claims":
				claims = nil
			case "nil-verifier":
				verifier = nil
			case "nil-reader":
				reader = nil
			case "typed-nil-verifier":
				verifier = (*userSessionProofTest)(nil)
			case "typed-nil-reader":
				reader = (*userSessionProofTest)(nil)
			case "no-user":
				claims.UserID = ""
			case "bad-user":
				claims.UserID = " user"
			case "no-session":
				claims.SessionID = ""
			case "bad-session":
				claims.SessionID = "session/1"
			case "zero-version":
				claims.AuthVersion = 0
			case "negative-version":
				claims.AuthVersion = -1
			case "refresh":
				claims.TokenType = "refresh"
			case "no-type":
				claims.TokenType = ""
			}
			principal, err := ResolveUserSessionPrincipal(context.Background(), verifier, reader, claims)
			if !errors.Is(err, ErrUserSessionPrincipalUnavailable) || principal != (PrincipalContext{}) || len(proof.calls) != 0 {
				t.Fatalf("invalid proof got=%+v calls=%v err=%v", principal, proof.calls, err)
			}
		})
	}
}

func TestResolveUserSessionPrincipalFailsClosedOnChangingAuthority(t *testing.T) {
	fault := errors.New("current authority is unavailable")
	for _, name := range []string{"initial-verifier", "strength-error", "second-verifier", "revoked-during-strength", "version-changed-during-strength", "missing-strength", "workload-strength", "canceled-during-strength"} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			proof := &userSessionProofTest{strength: "password"}
			switch name {
			case "initial-verifier":
				proof.verifyAt, proof.verifyError = 1, fault
			case "strength-error":
				proof.strengthErr = fault
			case "second-verifier":
				proof.afterRead = func() { proof.verifyAt, proof.verifyError = 2, fault }
			case "revoked-during-strength":
				proof.afterRead = func() { proof.revoked = true }
			case "version-changed-during-strength":
				proof.afterRead = func() { proof.currentVersion = 3 }
			case "missing-strength":
				proof.strength = ""
			case "workload-strength":
				proof.strength = "workload"
			case "canceled-during-strength":
				proof.afterRead = cancel
			}
			principal, err := ResolveUserSessionPrincipal(ctx, proof, proof, userSessionClaimsTest())
			if !errors.Is(err, ErrUserSessionPrincipalUnavailable) || principal != (PrincipalContext{}) {
				t.Fatalf("authority failure returned a principal: %+v err=%v", principal, err)
			}
			if name == "initial-verifier" || name == "strength-error" || name == "second-verifier" {
				if !errors.Is(err, fault) {
					t.Fatal("authority cause was lost")
				}
			}
		})
	}
}
