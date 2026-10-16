package service

import (
	"context"
	"encoding/json"
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

type adminSPAPG struct {
	pool                    *pgxpool.Pool
	sessions                *AdminSessionService
	self                    *AdminSelfService
	mfa                     *AdminMFAService
	admins                  *repository.PgAdminDomainRepository
	security                *repository.PgAdminSecurityRepository
	repo                    *repository.PgAdminSessionRepository
	account, role, username string
	now                     time.Time
}

func adminSPAPostgres(t *testing.T, seed int) *adminSPAPG {
	t.Helper()
	raw := os.Getenv("CAMPUSOS_ADMIN_SPA_TEST_DATABASE_URL")
	if raw == "" {
		t.Skip("owned Admin SPA database not configured")
	}
	config, err := pgxpool.ParseConfig(raw)
	if err != nil {
		t.Fatal(err)
	}
	if config.ConnConfig.Database != "campusos_v12_02b_admin_spa" {
		t.Fatal("requires disposable campusos_v12_02b_admin_spa")
	}
	ctx := context.Background()
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	a := repository.NewPgAdminDomainRepository(pool)
	r := repository.NewPgAdminSessionRepository(pool)
	security := repository.NewPgAdminSecurityRepository(pool)
	id := fmt.Sprintf("adm.spa.pg.%d", seed)
	role := fmt.Sprintf("spa_r_%d", seed)
	username := fmt.Sprintf("spa-pg-%d", seed)
	if err := a.CreateAccount(ctx, repository.AdminDomainAccount{ID: id, Username: username, Email: username + "@example.test", DisplayName: "只读管理员"}); err != nil {
		t.Fatal(err)
	}
	hash, err := auth.HashPassword("pg-spa-password")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := a.SetCredentialCAS(ctx, id, hash, 0, time.Now()); err != nil || !ok {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO admin_roles(id,name,description) VALUES($1,'自定义盘点','fixture')`, role); err != nil {
		t.Fatal(err)
	}
	if err := a.BindRole(ctx, repository.AdminDomainRoleBinding{ID: id + ".binding", AccountID: id, RoleID: role, GrantedBy: id}); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO admin_role_permissions(role_id,permission_code,created_by) VALUES($1,'community.thread.read','spa-test')`, role); err != nil {
		t.Fatal(err)
	}
	jwt := auth.NewJWTManager(auth.JWTConfig{Secret: "pg-spa-test-signing-material", Issuer: "campusos-admin", Audience: "admin-api", AccessTTL: time.Hour, RefreshTTL: 24 * time.Hour})
	sessions := NewAdminSessionService(a, r, jwt)
	now := time.Now().UTC().Truncate(time.Microsecond)
	sessions.SetClock(func() time.Time { return now })
	reliable := reliability.NewService(transaction.NewPostgreSQL(pool), reliability.NewPostgreSQLStore(pool))
	sessions.SetReliability(reliable)
	protector, err := NewMFASecretProtector("spa-test", map[string]string{"spa-test": "0123456789abcdef0123456789abcdef"})
	if err != nil {
		t.Fatal(err)
	}
	mfa := NewAdminMFAService(a, security, r, protector)
	mfa.SetClock(func() time.Time { return now })
	mfa.SetReliability(reliable)
	return &adminSPAPG{pool, sessions, NewAdminSelfService(sessions, a, security), mfa, a, security, r, id, role, username, now}
}
func (f *adminSPAPG) login(t *testing.T) (*AdminSessionTokens, identityport.SessionPrincipalResolver, AdminActor) {
	t.Helper()
	tokens, err := f.sessions.Login(context.Background(), f.username, "pg-spa-password", AdminSessionMetadata{})
	if err != nil {
		t.Fatal(err)
	}
	claims, err := f.sessions.jwt.VerifyAccessToken(tokens.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	proof := func(ctx context.Context) (identityport.PrincipalContext, error) {
		return identityport.ResolveAdminSessionPrincipal(ctx, f.sessions, claims)
	}
	return tokens, proof, AdminActor{ID: f.account, CredentialID: claims.SessionID}
}
func (f *adminSPAPG) snapshot(t *testing.T) string {
	t.Helper()
	var raw string
	if err := f.pool.QueryRow(context.Background(), `SELECT jsonb_build_object('accounts',(SELECT jsonb_agg(to_jsonb(a) ORDER BY id) FROM admin_accounts a),'sessions',(SELECT jsonb_agg(to_jsonb(s) ORDER BY id) FROM admin_sessions s),'mfa',(SELECT jsonb_agg(to_jsonb(m) ORDER BY account_id) FROM admin_mfa_totp m),'audits',(SELECT count(*) FROM platform_command_audits),'events',(SELECT count(*) FROM platform_outbox))::text`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	return raw
}
func TestAdminSPAPostgreSQLCurrentSelfWholeFactsReadonlyAndCommittedChanges(t *testing.T) {
	ctx := context.Background()
	for i, mode := range []string{"read", "zero_permissions", "permission_aba", "binding", "role_metadata", "account_aba", "session", "factor", "guard_missing"} {
		t.Run(mode, func(t *testing.T) {
			f := adminSPAPostgres(t, 610100+i)
			_, proof, _ := f.login(t)
			if mode == "zero_permissions" {
				if _, err := f.pool.Exec(ctx, `DELETE FROM admin_role_permissions WHERE role_id=$1`, f.role); err != nil {
					t.Fatal(err)
				}
			}
			before := f.snapshot(t)
			calls := 0
			mutated := false
			resolve := func(ctx context.Context) (identityport.PrincipalContext, error) {
				calls++
				if calls == 2 && mode != "read" && mode != "zero_permissions" {
					mutated = true
					tx, err := f.pool.Begin(ctx)
					if err != nil {
						t.Fatal(err)
					}
					defer tx.Rollback(ctx)
					exec := func(q string, args ...any) {
						if _, err := tx.Exec(ctx, q, args...); err != nil {
							t.Fatal(err)
						}
					}
					switch mode {
					case "permission_aba":
						exec(`DELETE FROM admin_role_permissions WHERE role_id=$1`, f.role)
						exec(`INSERT INTO admin_role_permissions(role_id,permission_code,created_by) VALUES($1,'community.thread.read','spa-test')`, f.role)
					case "binding":
						exec(`UPDATE admin_role_bindings SET status='revoked',revoked_at=NOW(),version=version+1 WHERE account_id=$1`, f.account)
					case "role_metadata":
						exec(`UPDATE admin_roles SET description='changed' WHERE id=$1`, f.role)
					case "account_aba":
						exec(`UPDATE admin_accounts SET display_name='temp',version=version+1 WHERE id=$1`, f.account)
						exec(`UPDATE admin_accounts SET display_name='只读管理员',version=version+1 WHERE id=$1`, f.account)
					case "session":
						exec(`UPDATE admin_sessions SET version=version+1 WHERE account_id=$1`, f.account)
					case "factor":
						exec(`INSERT INTO admin_mfa_totp(account_id,key_id,nonce,ciphertext,enrollment_expires_at) VALUES($1,'test','12345678','0123456789abcdef',NOW()+interval '1 hour')`, f.account)
					case "guard_missing":
						exec(`ALTER TABLE admin_role_permissions DISABLE TRIGGER trg_admin_permission_facts_version`)
					}
					if err := tx.Commit(ctx); err != nil {
						t.Fatal(err)
					}
				}
				return proof(ctx)
			}
			result, err := f.self.CurrentProfile(ctx, resolve)
			if mode == "guard_missing" {
				if _, e := f.pool.Exec(ctx, `ALTER TABLE admin_role_permissions ENABLE TRIGGER trg_admin_permission_facts_version`); e != nil {
					t.Fatal(e)
				}
			}
			if mode == "read" || mode == "zero_permissions" {
				if err != nil || result == nil || result.Domain != "admin" || result.Account.ID != f.account || len(result.Roles) != 1 {
					t.Fatal("current independent read failed", err)
				}
				want := 1
				if mode == "zero_permissions" {
					want = 0
				}
				if len(result.PermissionCodes) != want {
					t.Fatal("current permissions")
				}
				if f.snapshot(t) != before {
					t.Fatal("read wrote current state")
				}
				raw, _ := json.Marshal(result)
				for _, secret := range []string{"email", "refresh", "nonce", "ciphertext", "credential", "granted_by"} {
					if strings.Contains(string(raw), secret) {
						t.Fatal("private self projection")
					}
				}
			} else if !mutated || err == nil || result != nil {
				t.Fatal("committed source change accepted", mode, err)
			}
		})
	}
}

type adminSPAPGSessionLocks struct {
	*repository.PgAdminSessionRepository
	pool    *pgxpool.Pool
	account string
	checked bool
}

func (r *adminSPAPGSessionLocks) RevokeSessionCAS(ctx context.Context, id string, version int64, reason string, at time.Time) (repository.AdminSession, bool, error) {
	if !transaction.Active(ctx) {
		return repository.AdminSession{}, false, errors.New("missing command transaction")
	}
	for _, q := range []struct{ sql, id string }{{`SELECT id FROM admin_accounts WHERE id=$1 FOR UPDATE NOWAIT`, r.account}, {`SELECT id FROM admin_sessions WHERE id=$1 FOR UPDATE NOWAIT`, id}} {
		_, err := r.pool.Exec(context.Background(), q.sql, q.id)
		var pe *pgconn.PgError
		if !errors.As(err, &pe) || pe.Code != "55P03" {
			return repository.AdminSession{}, false, errors.New("current Admin authority row not locked")
		}
	}
	r.checked = true
	return r.PgAdminSessionRepository.RevokeSessionCAS(ctx, id, version, reason, at)
}
func TestAdminSPAPostgreSQLRefreshReusePersistsFamilyRevocationAndOwnLogoutLocks(t *testing.T) {
	ctx := context.Background()
	f := adminSPAPostgres(t, 610201)
	first, _, _ := f.login(t)
	locks := &adminSPAPGSessionLocks{PgAdminSessionRepository: f.repo, pool: f.pool, account: f.account}
	f.sessions.sessions = locks
	second, err := f.sessions.Refresh(ctx, first.RefreshToken, AdminSessionMetadata{})
	if err != nil || !locks.checked {
		t.Fatal("rotation current account/session locks", err)
	}
	if _, err := f.sessions.Refresh(ctx, first.RefreshToken, AdminSessionMetadata{}); !errors.Is(err, ErrAdminRefreshReuse) {
		t.Fatal(err)
	}
	claims, err := f.sessions.jwt.VerifyAccessToken(second.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.sessions.VerifyAccess(ctx, claims.SessionID); err == nil {
		t.Fatal("reuse family revocation rolled back")
	}
	var evidence string
	if err := f.pool.QueryRow(ctx, `SELECT (SELECT coalesce(jsonb_agg(to_jsonb(a)),'[]') FROM platform_command_audits a)::text || (SELECT coalesce(jsonb_agg(to_jsonb(o)),'[]') FROM platform_outbox o)::text`).Scan(&evidence); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{first.RefreshToken, second.RefreshToken, first.AccessToken, second.AccessToken, "pg-spa-password"} {
		if strings.Contains(evidence, raw) {
			t.Fatal("raw credential persisted")
		}
	}
	locks.checked = false
	_, proof, _ := f.login(t)
	if err := f.sessions.RevokeCurrentSession(ctx, proof); err != nil || !locks.checked {
		t.Fatal("own logout current locks", err)
	}
	var typed int
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM platform_command_audits WHERE command_code='identity.admin_session.logout' AND actor_type='admin' AND actor_id=$1`, f.account).Scan(&typed); err != nil || typed != 1 {
		t.Fatal("required typed logout evidence", err)
	}
}
func TestAdminSPAPostgreSQLMFAAndSessionRequiredEvidenceFailureRollbackAll(t *testing.T) {
	ctx := context.Background()
	seed := 610300
	for _, op := range []string{"login", "refresh", "logout", "enroll", "confirm", "step_up"} {
		for _, failure := range []string{"audit", "outbox"} {
			seed++
			t.Run(op+"/"+failure, func(t *testing.T) {
				f := adminSPAPostgres(t, seed)
				tokens, proof, actor := f.login(t)
				var secret []byte
				step := f.now.Unix() / 30
				if op == "confirm" || op == "step_up" {
					if _, err := f.mfa.StartEnrollment(ctx, actor, "CampusOS"); err != nil {
						t.Fatal(err)
					}
					m, err := f.security.GetTOTP(ctx, f.account)
					if err != nil {
						t.Fatal(err)
					}
					secret, err = f.mfa.protector.Open(m.KeyID, m.Nonce, m.Ciphertext)
					if err != nil {
						t.Fatal(err)
					}
				}
				if op == "step_up" {
					if err := f.mfa.ConfirmEnrollment(ctx, actor, totpCode(secret, step)); err != nil {
						t.Fatal(err)
					}
				}
				store := &adminSPAMFAStore{Store: reliability.NewPostgreSQLStore(f.pool), fail: true, events: failure == "outbox"}
				reliable := reliability.NewService(transaction.NewPostgreSQL(f.pool), store)
				f.sessions.SetReliability(reliable)
				f.mfa.SetReliability(reliable)
				before := f.snapshot(t)
				var err error
				switch op {
				case "login":
					var value *AdminSessionTokens
					value, err = f.sessions.Login(ctx, f.username, "pg-spa-password", AdminSessionMetadata{})
					if value != nil {
						t.Fatal("failed login returned credentials")
					}
				case "refresh":
					var value *AdminSessionTokens
					value, err = f.sessions.Refresh(ctx, tokens.RefreshToken, AdminSessionMetadata{})
					if value != nil {
						t.Fatal("failed rotation returned credentials")
					}
				case "logout":
					err = f.sessions.RevokeCurrentSession(ctx, proof)
				case "enroll":
					var value string
					value, err = f.mfa.StartEnrollment(ctx, actor, "CampusOS")
					if value != "" {
						t.Fatal("failed enrollment returned secret")
					}
				case "confirm":
					err = f.mfa.ConfirmEnrollment(ctx, actor, totpCode(secret, step))
				case "step_up":
					err = f.mfa.VerifyStepUp(ctx, actor, actor.CredentialID, totpCode(secret, step+1))
				}
				if err == nil {
					t.Fatal("required evidence failure ignored")
				}
				if f.snapshot(t) != before {
					t.Fatal("partial session/factor/audit/outbox escaped PostgreSQL rollback")
				}
			})
		}
	}
}
func TestAdminSPAPostgreSQLPasswordMFAProducesCurrentStrengthAndRejectsReplay(t *testing.T) {
	ctx := context.Background()
	f := adminSPAPostgres(t, 610401)
	_, proof, actor := f.login(t)
	if _, err := f.mfa.StartEnrollment(ctx, actor, "CampusOS"); err != nil {
		t.Fatal(err)
	}
	m, err := f.security.GetTOTP(ctx, f.account)
	if err != nil {
		t.Fatal(err)
	}
	secret, err := f.mfa.protector.Open(m.KeyID, m.Nonce, m.Ciphertext)
	if err != nil {
		t.Fatal(err)
	}
	step := f.now.Unix() / 30
	if err := f.mfa.ConfirmEnrollment(ctx, actor, totpCode(secret, step)); err != nil {
		t.Fatal(err)
	}
	if err := f.mfa.VerifyStepUp(ctx, actor, actor.CredentialID, totpCode(secret, step)); !errors.Is(err, ErrAdminMFAReplay) {
		t.Fatal("confirmation replay", err)
	}
	if err := f.mfa.VerifyStepUp(ctx, actor, actor.CredentialID, totpCode(secret, step+1)); err != nil {
		t.Fatal("actual password step up", err)
	}
	profile, err := f.self.CurrentProfile(ctx, proof)
	if err != nil || profile.AuthenticationStrength != "mfa" || !profile.MFAEnrolled {
		t.Fatal("current Admin strength", err)
	}
	if err := f.mfa.VerifyStepUp(ctx, actor, actor.CredentialID, totpCode(secret, step+1)); !errors.Is(err, ErrAdminMFAReplay) {
		t.Fatal("replay accepted", err)
	}
}
