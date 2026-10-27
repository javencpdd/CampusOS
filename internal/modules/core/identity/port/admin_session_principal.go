package port

import (
	"context"
	"errors"
	"reflect"
	"time"

	"github.com/campusos/CampusOS/pkg/auth"
)

var ErrAdminSessionPrincipalUnavailable = errors.New("admin session principal unavailable")

// AdminSessionAuthority contains only verified authority facts, never refresh
// material, password hashes or MFA secrets. The Identity adapter reads current
// active account/session records and derives current strength from its clock.
// It is a host-side Port, not a wire authentication DTO.
type AdminSessionAuthority struct {
	AccountID              string
	SessionID              string
	AuthenticationStrength string
	AccountVersion         int64
	SessionVersion         int64
	ExpiresAt              time.Time
	MFAAuthenticatedAt     *time.Time
	AdminMFAOptional       bool `json:"-"`
}

type AdminSessionAuthorityReader interface {
	CurrentAdminSession(context.Context, string, string) (AdminSessionAuthority, error)
}

// ResolveAdminSessionPrincipal accepts only claims from the admin-domain JWT
// ingress and binds its signed subject/session tuple to two current reads.
// AuthVersion is the admin JWT protocol version, not the mutable session CAS
// version: a successful MFA upgrade must not invalidate its access JWT.
func ResolveAdminSessionPrincipal(ctx context.Context, reader AdminSessionAuthorityReader, claims *auth.JWTClaims) (PrincipalContext, error) {
	fail := func(err error) (PrincipalContext, error) {
		return PrincipalContext{}, errors.Join(ErrAdminSessionPrincipalUnavailable, err)
	}
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	if nilUserSessionAdapter(reader) || claims == nil || !validOpaqueID(claims.UserID) || !validOpaqueID(claims.SessionID) || claims.TokenType != auth.AccessTokenType || claims.AuthVersion != 1 {
		return fail(nil)
	}
	bound := *claims
	first, err := reader.CurrentAdminSession(ctx, bound.UserID, bound.SessionID)
	if err != nil {
		return fail(err)
	}
	first = detachedAdminAuthority(first)
	if first.AccountID != bound.UserID || first.SessionID != bound.SessionID || first.AccountVersion < 1 || first.SessionVersion < 1 || first.ExpiresAt.IsZero() || (first.AuthenticationStrength == "mfa" && first.MFAAuthenticatedAt == nil) {
		return fail(nil)
	}
	p := PrincipalContext{Contract: PrincipalContract, Actor: PrincipalRef{Kind: "admin", ID: bound.UserID}, Audience: "admin", AuthenticationStrength: first.AuthenticationStrength, CredentialID: bound.SessionID, AdminMFAOptional: first.AdminMFAOptional}
	if err := p.Validate(); err != nil {
		return fail(err)
	}
	second, err := reader.CurrentAdminSession(ctx, bound.UserID, bound.SessionID)
	if err != nil {
		return fail(err)
	}
	if !reflect.DeepEqual(first, detachedAdminAuthority(second)) {
		return fail(nil)
	}
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	return p, nil
}

func detachedAdminAuthority(a AdminSessionAuthority) AdminSessionAuthority {
	a.ExpiresAt = a.ExpiresAt.UTC()
	if a.MFAAuthenticatedAt != nil {
		at := a.MFAAuthenticatedAt.UTC()
		a.MFAAuthenticatedAt = &at
	}
	return a
}
