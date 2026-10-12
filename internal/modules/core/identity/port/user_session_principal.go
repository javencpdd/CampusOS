package port

import (
	"context"
	"errors"
	"reflect"

	"github.com/campusos/CampusOS/pkg/auth"
)

var ErrUserSessionPrincipalUnavailable = errors.New("user session principal unavailable")

// ResolveUserSessionPrincipal accepts claims from a trusted authenticated
// ingress. Parsing a DTO or supplying a user ID is not proof: current session
// ownership, account version and strength are checked through Identity Ports.
func ResolveUserSessionPrincipal(ctx context.Context, verifier SessionVerifier, reader SessionStrengthReader, claims *auth.JWTClaims) (PrincipalContext, error) {
	fail := func(err error) (PrincipalContext, error) {
		return PrincipalContext{}, errors.Join(ErrUserSessionPrincipalUnavailable, err)
	}
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	if nilUserSessionAdapter(verifier) || nilUserSessionAdapter(reader) || claims == nil ||
		!validOpaqueID(claims.UserID) || !validOpaqueID(claims.SessionID) || claims.AuthVersion < 1 || claims.TokenType != auth.AccessTokenType {
		return fail(nil)
	}
	// Use private claim copies. Display-name refreshes performed by the verifier
	// cannot change the authority tuple evaluated on either side of strength.
	bound := *claims
	verified := bound
	if err := verifier.VerifyAccess(ctx, &verified); err != nil {
		return fail(err)
	}
	strength, err := reader.AuthenticationStrengthForSession(ctx, bound.SessionID)
	if err != nil {
		return fail(err)
	}
	verified = bound
	if err := verifier.VerifyAccess(ctx, &verified); err != nil {
		return fail(err)
	}
	principal := PrincipalContext{Contract: PrincipalContract,
		Actor: PrincipalRef{Kind: "user", ID: bound.UserID}, Audience: "user", AuthenticationStrength: strength, CredentialID: bound.SessionID}
	if err := principal.Validate(); err != nil {
		return fail(err)
	}
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	return principal, nil
}

func nilUserSessionAdapter(adapter any) bool {
	if adapter == nil {
		return true
	}
	value := reflect.ValueOf(adapter)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}
