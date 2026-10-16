package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/campusos/CampusOS/pkg/auth"
)

func TestAdminCurrentPrincipalStrengthAndLifecycle(t *testing.T) {
	for _, mode := range []string{"password", "fresh_mfa", "expired_mfa", "boundary_mfa", "nil_mfa", "future_mfa", "subject", "session_revoked", "account_suspended", "account_revoked", "session_expired", "transaction", "missing_locks"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			env := newAdminSessionEnv()
			now := time.Now().UTC().Truncate(time.Microsecond)
			env.service.SetClock(func() time.Time { return now })
			const accountID = "admin.current"
			const sessionID = "admin.current-session"
			if err := env.admins.CreateAccount(ctx, repository.AdminDomainAccount{ID: accountID, Username: "current-admin", Email: "current-admin@example.test", DisplayName: "Current Admin"}); err != nil {
				t.Fatal(err)
			}
			strength := "password"
			var mfaAt *time.Time
			if strings.Contains(mode, "mfa") {
				strength = "mfa"
				at := now.Add(-time.Minute)
				mfaAt = &at
			}
			switch mode {
			case "expired_mfa":
				at := now.Add(-AdminGovernanceMFAWindow - time.Second)
				mfaAt = &at
			case "boundary_mfa":
				at := now.Add(-AdminGovernanceMFAWindow)
				mfaAt = &at
			case "nil_mfa":
				mfaAt = nil
			case "future_mfa":
				at := now.Add(time.Second)
				mfaAt = &at
			}
			if err := env.sessions.CreateSession(ctx, repository.AdminSession{ID: sessionID, AccountID: accountID, RefreshTokenDigest: strings.Repeat("a", 64), TokenFamilyID: sessionID, AuthenticationStrength: strength, MFAAuthenticatedAt: mfaAt, CreatedAt: now, ExpiresAt: now.Add(time.Hour)}); err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "session_revoked":
				if _, _, err := env.sessions.RevokeSessionCAS(ctx, sessionID, 1, "test", now); err != nil {
					t.Fatal(err)
				}
			case "account_suspended", "account_revoked":
				state := "suspended"
				if mode == "account_revoked" {
					state = "revoked"
				}
				if _, _, err := env.admins.SetAccountStatusCAS(ctx, accountID, 1, state, "test", "admin.test", now); err != nil {
					t.Fatal(err)
				}
			case "session_expired":
				env.service.SetClock(func() time.Time { return now.Add(time.Hour) })
			}
			claims := &auth.JWTClaims{UserID: accountID, SessionID: sessionID, TokenType: auth.AccessTokenType, AuthVersion: 1}
			if mode == "subject" {
				claims.UserID = "admin.other"
			}
			var p identityport.PrincipalContext
			var err error
			if mode == "transaction" || mode == "missing_locks" {
				if mode == "missing_locks" {
					env.service.admins = &adminAuthorityWithoutLock{AdminDomainRepository: env.admins}
				}
				err = transaction.NewMemory().Within(ctx, func(txCtx context.Context) error {
					p, err = identityport.ResolveAdminSessionPrincipal(txCtx, env.service, claims)
					return err
				})
			} else {
				p, err = identityport.ResolveAdminSessionPrincipal(ctx, env.service, claims)
			}
			allowed := mode == "password" || mode == "fresh_mfa" || mode == "expired_mfa" || mode == "boundary_mfa" || mode == "transaction"
			if (err == nil) != allowed {
				t.Fatal(p, err)
			}
			if allowed {
				want := "password"
				if mode == "fresh_mfa" {
					want = "mfa"
				}
				if p.Actor.Kind != "admin" || p.AuthenticationStrength != want {
					t.Fatal(p)
				}
			} else if !errors.Is(err, identityport.ErrAdminSessionPrincipalUnavailable) {
				t.Fatal(err)
			}
		})
	}
}

type adminAuthorityWithoutLock struct {
	repository.AdminDomainRepository
}
