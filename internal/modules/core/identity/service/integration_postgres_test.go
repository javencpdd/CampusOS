package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func integrationPG(t *testing.T) (*IntegrationService, port.SessionPrincipalResolver, *pgxpool.Pool) {
	t.Helper()
	url := os.Getenv("CAMPUSOS_INTEGRATION_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("owned Integration PostgreSQL target not configured")
	}
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ConnConfig.Database != "campusos_v12_02c" {
		t.Fatal("requires owned campusos_v12_02c")
	}
	ctx := context.Background()
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	id, err := integrationID("adm.integration.", 8)
	if err != nil {
		t.Fatal(err)
	}
	admins := repository.NewPgAdminDomainRepository(pool)
	sessions := repository.NewPgAdminSessionRepository(pool)
	if err := admins.CreateAccount(ctx, repository.AdminDomainAccount{ID: id, Username: id, Email: id + "@example.test", DisplayName: "Integration lifecycle target"}); err != nil {
		t.Fatal(err)
	}
	if err := admins.BindRole(ctx, repository.AdminDomainRoleBinding{ID: id + ".role", AccountID: id, RoleID: "security_admin", GrantedBy: id}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	mfa := now.Add(-time.Minute)
	sid := id + ".session"
	if err := sessions.CreateSession(ctx, repository.AdminSession{ID: sid, AccountID: id, RefreshTokenDigest: fmt.Sprintf("%x", sha256.Sum256([]byte(sid))), TokenFamilyID: id, AuthenticationStrength: "mfa", MFAAuthenticatedAt: &mfa, CreatedAt: now, UpdatedAt: now, LastActiveAt: now, ExpiresAt: now.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	jwt := auth.NewJWTManager(auth.JWTConfig{Secret: "owned-integration-pg-session-signature", Issuer: "integration-pg", Audience: "admin", AccessTTL: time.Hour})
	signed, err := jwt.GenerateAccessToken(id, id, auth.AccessTokenContext{SessionID: sid, AuthVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	claims, err := jwt.VerifyAccessToken(signed)
	if err != nil {
		t.Fatal(err)
	}
	adminSessions := NewAdminSessionService(admins, sessions, jwt)
	reliable := reliability.NewService(transaction.NewPostgreSQL(pool), reliability.NewPostgreSQLStore(pool))
	adminSessions.SetReliability(reliable)
	resolve := func(ctx context.Context) (port.PrincipalContext, error) {
		return port.ResolveAdminSessionPrincipal(ctx, adminSessions, claims)
	}
	return NewIntegrationService(repository.NewPgIntegrationRepository(pool), NewBoardGovernanceAuthorityService(nil, admins, adminSessions), reliable), resolve, pool
}
func TestIntegrationPostgreSQLLifecycleAndAtomicRotation(t *testing.T) {
	s, proof, pool := integrationPG(t)
	ctx := context.Background()
	p, err := s.CreatePrincipal(ctx, "PG rotation", proof)
	if err != nil {
		t.Fatal(err)
	}
	issued, err := s.IssueCredential(ctx, p.ID, integrationRequest(p), proof)
	if err != nil {
		t.Fatal(err)
	}
	var winners atomic.Int32
	var wg sync.WaitGroup
	req := integrationRequest(p)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := s.RotateCredential(ctx, p.ID, issued.Credential.ID, issued.Credential.Version, req, proof)
			if err == nil {
				if result.Token == "" {
					t.Error("empty key")
				}
				winners.Add(1)
			}
		}()
	}
	wg.Wait()
	if winners.Load() != 1 {
		t.Fatal("rotation winners", winners.Load())
	}
	if _, err := s.CurrentIntegration(ctx, issued.Token); !errors.Is(err, ErrIntegrationCredentialInvalid) {
		t.Fatal("old raw key valid after rotation", err)
	}
	keys, err := s.repo.ListIntegrationCredentials(ctx, p.ID, "", 100)
	if err != nil || len(keys) != 2 {
		t.Fatal("partial rotation", err, len(keys))
	}
	var audits, events int
	if err := pool.QueryRow(ctx, `SELECT count(*),count(event_id) FROM platform_command_audits WHERE resource_id=$1 AND command_code='identity.integration.rotate' AND actor_type='admin'`, p.ID).Scan(&audits, &events); err != nil || audits != 1 || events != 1 {
		t.Fatal("atomic rotation receipt", audits, events, err)
	}
}
func TestIntegrationPostgreSQLAuditOutboxRollback(t *testing.T) {
	for _, failure := range []string{"audit", "outbox"} {
		t.Run(failure, func(t *testing.T) {
			s, proof, pool := integrationPG(t)
			ctx := context.Background()
			p, err := s.CreatePrincipal(ctx, "PG rollback", proof)
			if err != nil {
				t.Fatal(err)
			}
			issued, err := s.IssueCredential(ctx, p.ID, integrationRequest(p), proof)
			if err != nil {
				t.Fatal(err)
			}
			s.reliable = reliability.NewService(transaction.NewPostgreSQL(pool), integrationFailStore{Store: reliability.NewPostgreSQLStore(pool), audit: failure == "audit", event: failure == "outbox"})
			out, err := s.RotateCredential(ctx, p.ID, issued.Credential.ID, issued.Credential.Version, integrationRequest(p), proof)
			if err == nil || out.Token != "" {
				t.Fatal("failed rotation returned secret")
			}
			old, _ := s.repo.GetIntegrationCredential(ctx, issued.Credential.ID, false)
			keys, _ := s.repo.ListIntegrationCredentials(ctx, p.ID, "", 100)
			if old.Status != "active" || old.Version != issued.Credential.Version || len(keys) != 1 {
				t.Fatal("rollback did not preserve old")
			}
			var rows int
			if err := pool.QueryRow(ctx, `SELECT count(*) FROM platform_command_audits WHERE resource_id=$1 AND command_code='identity.integration.rotate'`, p.ID).Scan(&rows); err != nil || rows != 0 {
				t.Fatal("failed receipt persisted", err)
			}
		})
	}
}
func TestIntegrationPostgreSQLReadLocksOrderRevocation(t *testing.T) {
	s, proof, pool := integrationPG(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	p, err := s.CreatePrincipal(ctx, "PG ordered revoke", proof)
	if err != nil {
		t.Fatal(err)
	}
	issued, err := s.IssueCredential(ctx, p.ID, integrationRequest(p), proof)
	if err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	release := make(chan struct{})
	readDone := make(chan error, 1)
	go func() {
		_, err := s.ReadForIntegration(ctx, issued.Token, integrationTarget(), func(tx context.Context) (any, error) {
			for _, q := range []struct{ sql, id string }{{`SELECT id FROM integration_principals WHERE id=$1 FOR UPDATE NOWAIT`, p.ID}, {`SELECT id FROM integration_credentials WHERE id=$1 FOR UPDATE NOWAIT`, issued.Credential.ID}} {
				_, err := pool.Exec(ctx, q.sql, q.id)
				var pe *pgconn.PgError
				if !errors.As(err, &pe) || pe.Code != "55P03" {
					return nil, errors.New("actual machine fact row not locked")
				}
			}
			close(entered)
			select {
			case <-release:
				return "checked body", nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		})
		readDone <- err
	}()
	select {
	case <-entered:
	case err := <-readDone:
		t.Fatal(err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	revokeDone := make(chan error, 1)
	go func() {
		_, err := s.UpdateCredential(ctx, p.ID, issued.Credential.ID, issued.Credential.Version, nil, true, proof)
		revokeDone <- err
	}()
	blocked := false
	for i := 0; i < 100; i++ {
		var count int
		err := pool.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '%integration_principals%'`).Scan(&count)
		if err != nil {
			t.Fatal(err)
		}
		if count > 0 {
			blocked = true
			break
		}
		select {
		case err := <-revokeDone:
			t.Fatalf("revoke failed to wait: %v", err)
		case <-time.After(10 * time.Millisecond):
		}
	}
	close(release)
	if err := <-readDone; err != nil {
		t.Fatal(err)
	}
	if err := <-revokeDone; err != nil {
		t.Fatal(err)
	}
	if !blocked {
		t.Fatal("actual pg lock contention not observed")
	}
	called := false
	data, err := s.ReadForIntegration(ctx, issued.Token, integrationTarget(), func(context.Context) (any, error) { called = true; return "bad", nil })
	if err == nil || data != nil || called {
		t.Fatal("read after revoke commit")
	}
}

type integrationPrelockHook struct {
	repository.IntegrationRepository
	calls int
	hook  func()
}

func (r *integrationPrelockHook) GetIntegrationCredential(ctx context.Context, id string, lock bool) (repository.IntegrationCredentialRecord, error) {
	r.calls++
	if r.calls == 3 {
		r.hook()
	}
	return r.IntegrationRepository.GetIntegrationCredential(ctx, id, lock)
}
func TestIntegrationPostgreSQLCommittedRevokeAndABABeforeRead(t *testing.T) {
	for _, mode := range []string{"revoke", "principal_aba", "scope_aba", "admin_revoke"} {
		t.Run(mode, func(t *testing.T) {
			s, proof, pool := integrationPG(t)
			ctx := context.Background()
			p, err := s.CreatePrincipal(ctx, "PG late facts", proof)
			if err != nil {
				t.Fatal(err)
			}
			issued, err := s.IssueCredential(ctx, p.ID, integrationRequest(p), proof)
			if err != nil {
				t.Fatal(err)
			}
			original := s.repo
			if mode == "admin_revoke" {
				a, _ := proof(ctx)
				if _, err := pool.Exec(ctx, `UPDATE admin_sessions SET revoked_at=NOW(),revoke_reason='test',version=version+1 WHERE id=$1`, a.CredentialID); err != nil {
					t.Fatal(err)
				}
				if _, err := s.IssueCredential(ctx, p.ID, integrationRequest(p), proof); err == nil {
					t.Fatal("revoked Admin can issue")
				}
				return
			}
			s.repo = &integrationPrelockHook{IntegrationRepository: original, hook: func() {
				switch mode {
				case "revoke":
					clone := *s
					clone.repo = original
					if _, err := clone.UpdateCredential(ctx, p.ID, issued.Credential.ID, issued.Credential.Version, nil, true, proof); err != nil {
						t.Fatal(err)
					}
				case "principal_aba":
					if _, err := pool.Exec(ctx, `UPDATE integration_principals SET status='suspended' WHERE id=$1;`, p.ID); err != nil {
						t.Fatal(err)
					}
					if _, err := pool.Exec(ctx, `UPDATE integration_principals SET status='active' WHERE id=$1`, p.ID); err != nil {
						t.Fatal(err)
					}
				case "scope_aba":
					if _, err := pool.Exec(ctx, `UPDATE integration_credentials SET rules='[]'::jsonb WHERE id=$1`, issued.Credential.ID); err != nil {
						t.Fatal(err)
					}
					raw, _ := json.Marshal(issued.Credential.Rules)
					if _, err := pool.Exec(ctx, `UPDATE integration_credentials SET rules=$2 WHERE id=$1`, issued.Credential.ID, raw); err != nil {
						t.Fatal(err)
					}
				}
			}}
			called := false
			data, err := s.ReadForIntegration(ctx, issued.Token, integrationTarget(), func(context.Context) (any, error) { called = true; return "never", nil })
			if err == nil || data != nil || called {
				t.Fatal("committed change escaped final gate", data, err)
			}
		})
	}
}
