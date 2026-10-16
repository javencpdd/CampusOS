package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func adminCurrentPostgres(t *testing.T, seed int) (*AdminSessionService, *pgxpool.Pool, *auth.JWTClaims) {
	t.Helper()
	url := strings.TrimSpace(os.Getenv("CAMPUSOS_AUTHOR_WRITE_TEST_DATABASE_URL"))
	if url == "" {
		t.Skip("CAMPUSOS_AUTHOR_WRITE_TEST_DATABASE_URL is not configured")
	}
	config, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	if config.ConnConfig.Database != "campusos_v12_02b_author_write" {
		t.Fatal("admin proof tests require owned campusos_v12_02b_author_write database")
	}
	ctx := context.Background()
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	admins := repository.NewPgAdminDomainRepository(pool)
	sessions := repository.NewPgAdminSessionRepository(pool)
	id := fmt.Sprintf("adm.proof.%d", seed)
	sessionID := fmt.Sprintf("adms.proof.%d", seed)
	if err := admins.CreateAccount(ctx, repository.AdminDomainAccount{ID: id, Username: fmt.Sprintf("proof-admin-%d", seed), Email: fmt.Sprintf("proof-%d@example.test", seed), DisplayName: "Proof Admin"}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	mfaAt := now.Add(-time.Minute)
	if err := sessions.CreateSession(ctx, repository.AdminSession{ID: sessionID, AccountID: id, RefreshTokenDigest: fmt.Sprintf("%064x", seed), TokenFamilyID: sessionID, AuthenticationStrength: "mfa", MFAAuthenticatedAt: &mfaAt, CreatedAt: now, UpdatedAt: now, LastActiveAt: now, ExpiresAt: now.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	jwt := auth.NewJWTManager(auth.JWTConfig{Secret: "admin-current-pg-test-key", Issuer: "admin-current-pg", Audience: "admin", AccessTTL: time.Hour})
	token, err := jwt.GenerateAccessToken(id, "Proof", auth.AccessTokenContext{SessionID: sessionID, AuthVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	claims, err := jwt.VerifyAccessToken(token)
	if err != nil {
		t.Fatal(err)
	}
	s := NewAdminSessionService(admins, sessions, jwt)
	s.SetReliability(reliability.NewService(transaction.NewPostgreSQL(pool), reliability.NewPostgreSQLStore(pool)))
	return s, pool, claims
}

func TestAdminCurrentPrincipalPostgreSQLLocksAndUpgrade(t *testing.T) {
	s, pool, claims := adminCurrentPostgres(t, 117501)
	ctx := context.Background()
	if _, _, err := s.sessions.UpgradeSessionStrengthCAS(ctx, claims.SessionID, 1, "mfa", time.Now().Add(-time.Minute), time.Now()); err != nil {
		t.Fatal(err)
	}
	err := transaction.NewPostgreSQL(pool).Within(ctx, func(txCtx context.Context) error {
		p, err := identityport.ResolveAdminSessionPrincipal(txCtx, s, claims)
		if err != nil {
			return err
		}
		if p.Actor.ID != claims.UserID || p.AuthenticationStrength != "mfa" {
			t.Fatal(p)
		}
		for _, q := range []struct{ sql, id string }{{`SELECT id FROM admin_accounts WHERE id=$1 FOR UPDATE NOWAIT`, claims.UserID}, {`SELECT id FROM admin_sessions WHERE id=$1 FOR UPDATE NOWAIT`, claims.SessionID}} {
			_, err := pool.Exec(context.Background(), q.sql, q.id)
			var pe *pgconn.PgError
			if !errors.As(err, &pe) || pe.Code != "55P03" {
				t.Fatal("actual Admin authority lock missing", q.sql, err)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// AuthVersion remains the JWT protocol version1; actual session version2
	// after step-up cannot invalidate the still-current signed access token.
	if claims.AuthVersion != 1 {
		t.Fatal(claims.AuthVersion)
	}
	if _, err := identityport.ResolveAdminSessionPrincipal(ctx, s, claims); err != nil {
		t.Fatal(err)
	}
}

type adminCurrentRaceReader struct {
	identityport.AdminSessionAuthorityReader
	beforeSecond func()
	calls        int
}

func (r *adminCurrentRaceReader) CurrentAdminSession(ctx context.Context, subject, id string) (identityport.AdminSessionAuthority, error) {
	r.calls++
	if r.calls == 2 {
		r.beforeSecond()
	}
	return r.AdminSessionAuthorityReader.CurrentAdminSession(ctx, subject, id)
}

func TestAdminCurrentPrincipalPostgreSQLCommittedChanges(t *testing.T) {
	for i, mode := range []string{"account_suspend", "account_aba", "session_revoke", "session_version", "mfa_expire", "mfa_future", "session_owner", "session_expire"} {
		t.Run(mode, func(t *testing.T) {
			s, pool, claims := adminCurrentPostgres(t, 117601+i)
			ctx := context.Background()
			reader := &adminCurrentRaceReader{AdminSessionAuthorityReader: s, beforeSecond: func() {
				tx, err := pool.Begin(ctx)
				if err != nil {
					t.Fatal(err)
				}
				defer tx.Rollback(ctx)
				switch mode {
				case "account_suspend":
					_, err = tx.Exec(ctx, `UPDATE admin_accounts SET status='suspended',version=version+1 WHERE id=$1`, claims.UserID)
				case "account_aba":
					_, err = tx.Exec(ctx, `UPDATE admin_accounts SET status='suspended',version=version+1 WHERE id=$1`, claims.UserID)
					if err == nil {
						_, err = tx.Exec(ctx, `UPDATE admin_accounts SET status='active',version=version+1 WHERE id=$1`, claims.UserID)
					}
				case "session_revoke":
					_, err = tx.Exec(ctx, `UPDATE admin_sessions SET revoked_at=NOW(),revoke_reason='proof-test',version=version+1 WHERE id=$1`, claims.SessionID)
				case "session_version":
					_, err = tx.Exec(ctx, `UPDATE admin_sessions SET version=version+1 WHERE id=$1`, claims.SessionID)
				case "mfa_expire":
					_, err = tx.Exec(ctx, `UPDATE admin_sessions SET mfa_authenticated_at=NOW()-interval '16 minutes',version=version+1 WHERE id=$1`, claims.SessionID)
				case "mfa_future":
					_, err = tx.Exec(ctx, `UPDATE admin_sessions SET mfa_authenticated_at=NOW()+interval '1 minute',version=version+1 WHERE id=$1`, claims.SessionID)
				case "session_owner":
					other := claims.UserID + ".other"
					if err = s.admins.CreateAccount(ctx, repository.AdminDomainAccount{ID: other, Username: fmt.Sprintf("other-proof-%d", i), Email: fmt.Sprintf("other-proof-%d@example.test", i)}); err == nil {
						_, err = tx.Exec(ctx, `UPDATE admin_sessions SET account_id=$2,version=version+1 WHERE id=$1`, claims.SessionID, other)
					}
				case "session_expire":
					_, err = tx.Exec(ctx, `UPDATE admin_sessions SET expires_at=NOW(),version=version+1 WHERE id=$1`, claims.SessionID)
				}
				if err != nil {
					t.Fatal(err)
				}
				if err = tx.Commit(ctx); err != nil {
					t.Fatal(err)
				}
			}}
			p, err := identityport.ResolveAdminSessionPrincipal(ctx, reader, claims)
			if !errors.Is(err, identityport.ErrAdminSessionPrincipalUnavailable) || p.Actor.ID != "" {
				t.Fatal("committed Admin proof change accepted", p, err)
			}
		})
	}
}
