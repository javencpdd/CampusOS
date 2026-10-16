package port

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/campusos/CampusOS/pkg/auth"
)

type adminAuthorityReaderFunc func(context.Context, string, string) (AdminSessionAuthority, error)

func (f adminAuthorityReaderFunc) CurrentAdminSession(ctx context.Context, subject, id string) (AdminSessionAuthority, error) {
	return f(ctx, subject, id)
}

func validAdminAuthority() AdminSessionAuthority {
	now := time.Now().UTC()
	return AdminSessionAuthority{AccountID: "admin.subject", SessionID: "admin.session", AuthenticationStrength: "mfa", AccountVersion: 1, SessionVersion: 2, ExpiresAt: now.Add(time.Hour), MFAAuthenticatedAt: &now}
}
func validAdminClaims() *auth.JWTClaims {
	return &auth.JWTClaims{UserID: "admin.subject", SessionID: "admin.session", AuthVersion: 1, TokenType: auth.AccessTokenType}
}

func TestAdminSessionPrincipalBinding(t *testing.T) {
	for _, mode := range []string{"current", "password", "missing", "typed_nil", "subject", "credential", "account_version", "session_version", "expires_shape", "strength", "mfa_missing", "invalid_claims", "protocol", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			a := validAdminAuthority()
			claims := validAdminClaims()
			ctx := context.Background()
			switch mode {
			case "password":
				a.AuthenticationStrength = "password"
			case "subject":
				a.AccountID = "admin.other"
			case "credential":
				a.SessionID = "admin.other-session"
			case "account_version":
				a.AccountVersion = 0
			case "session_version":
				a.SessionVersion = 0
			case "expires_shape":
				a.ExpiresAt = time.Time{}
			case "strength":
				a.AuthenticationStrength = "workload"
			case "mfa_missing":
				a.MFAAuthenticatedAt = nil
			case "invalid_claims":
				claims.TokenType = "refresh"
			case "protocol":
				claims.AuthVersion = 2
			case "cancelled":
				c, cancel := context.WithCancel(ctx)
				cancel()
				ctx = c
			}
			var reader AdminSessionAuthorityReader = adminAuthorityReaderFunc(func(context.Context, string, string) (AdminSessionAuthority, error) { return a, nil })
			if mode == "missing" {
				reader = nil
			}
			if mode == "typed_nil" {
				var f adminAuthorityReaderFunc
				reader = f
			}
			p, err := ResolveAdminSessionPrincipal(ctx, reader, claims)
			allowed := mode == "current" || mode == "password"
			if (err == nil) != allowed {
				t.Fatal(p, err)
			}
			if allowed {
				if p.Actor.Kind != "admin" || p.Actor.ID != claims.UserID || p.Audience != "admin" || p.AuthenticationStrength != a.AuthenticationStrength || p.CredentialID != claims.SessionID {
					t.Fatal(p)
				}
			} else if !errors.Is(err, ErrAdminSessionPrincipalUnavailable) || p.Actor.ID != "" {
				t.Fatal(p, err)
			}
		})
	}
}
func TestAdminSessionPrincipalCurrentProofDrift(t *testing.T) {
	for _, field := range []string{"account", "session", "account_version", "session_version", "strength", "expiry", "mfa_pointer", "revoked"} {
		t.Run(field, func(t *testing.T) {
			a := validAdminAuthority()
			calls := 0
			reader := adminAuthorityReaderFunc(func(context.Context, string, string) (AdminSessionAuthority, error) {
				calls++
				if calls == 2 {
					switch field {
					case "account":
						a.AccountID = "admin.other"
					case "session":
						a.SessionID = "admin.other-session"
					case "account_version":
						a.AccountVersion++
					case "session_version":
						a.SessionVersion++
					case "strength":
						a.AuthenticationStrength = "password"
					case "expiry":
						a.ExpiresAt = a.ExpiresAt.Add(-time.Second)
					case "mfa_pointer":
						*a.MFAAuthenticatedAt = a.MFAAuthenticatedAt.Add(time.Second)
					case "revoked":
						return AdminSessionAuthority{}, errors.New("revoked")
					}
				}
				return a, nil
			})
			p, err := ResolveAdminSessionPrincipal(context.Background(), reader, validAdminClaims())
			if !errors.Is(err, ErrAdminSessionPrincipalUnavailable) || p.Actor.ID != "" {
				t.Fatal("drift accepted", p, err)
			}
		})
	}
}
