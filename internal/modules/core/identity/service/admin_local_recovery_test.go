package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/identity/domain"
	"github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const localAdminOldPassword = "old-local-admin-password"
const localAdminNewPassword = "new-local-admin-password"

func localAdminDigest(value string) string {
	h := sha256.Sum256([]byte(value))
	return hex.EncodeToString(h[:])
}

func seedLocalAdmin(t *testing.T, a repository.AdminDomainRepository, m repository.AdminSecurityRepository, s repository.AdminSessionRepository, id string, now time.Time) {
	t.Helper()
	ctx := context.Background()
	if err := a.CreateAccount(ctx, repository.AdminDomainAccount{ID: id, Username: "local-" + id, Email: id + "@example.test", DisplayName: "Local recovery", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	hash, err := auth.HashPassword(localAdminOldPassword)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := a.SetCredentialCAS(ctx, id, hash, 0, now); err != nil || !ok {
		t.Fatal(err)
	}
	if err := a.BindRole(ctx, repository.AdminDomainRoleBinding{ID: id + ".role", AccountID: id, RoleID: "security_admin", GrantedBy: "local-fixture", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	pending, err := m.UpsertTOTP(ctx, repository.AdminTOTPMethod{AccountID: id, Status: "pending", KeyID: "local-fixture", Nonce: strings.Repeat("n", 16), Ciphertext: strings.Repeat("x", 32), EnrollmentExpiresAt: now.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := m.ConfirmTOTPCAS(ctx, id, pending.Version, now); err != nil || !ok {
		t.Fatal(err)
	}
	for _, suffix := range []string{".session", ".second-session"} {
		if err := s.CreateSession(ctx, repository.AdminSession{ID: id + suffix, AccountID: id, RefreshTokenDigest: localAdminDigest(id + suffix + "refresh"), TokenFamilyID: localAdminDigest(id + suffix + ".family")[:32], AuthenticationStrength: "password", CreatedAt: now, UpdatedAt: now, LastActiveAt: now, ExpiresAt: now.Add(time.Hour)}); err != nil {
			t.Fatal(err)
		}
	}
	for _, recovery := range []bool{false, true} {
		kind := "mfa"
		if recovery {
			kind = "recovery"
		}
		ticket := repository.AdminSecurityTicket{ID: id + "." + kind, AccountID: id, Audience: "admin", Purpose: "login", Digest: localAdminDigest(id + kind), ExpiresAt: now.Add(time.Minute), MaxAttempts: 5}
		if recovery {
			ticket.Audience = ""
			ticket.Purpose = "password_reset"
			err = m.CreateRecoveryTicket(ctx, ticket)
		} else {
			err = m.CreateAdminTicket(ctx, ticket)
		}
		if err != nil {
			t.Fatal(err)
		}
	}
}

func exerciseLocalAdmin(t *testing.T, svc *AdminLocalRecoveryService, a repository.AdminDomainRepository, m repository.AdminSecurityRepository, s repository.AdminSessionRepository, id, action string, now time.Time) {
	t.Helper()
	ctx := context.Background()
	before, _ := a.GetAccountByID(ctx, id)
	if action == "account_restore" {
		var err error
		before, _, err = a.SetAccountStatusCAS(ctx, id, before.Version, "suspended", "fixture", "local-fixture", now)
		if err != nil {
			t.Fatal(err)
		}
	}
	credential, _ := a.GetCredential(ctx, id)
	method, _ := m.GetTOTP(ctx, id)
	bindings, _ := a.ListActiveBindings(ctx, id)
	var err error
	switch action {
	case "password_reset":
		err = svc.ResetPassword(ctx, before.Email, localAdminNewPassword, "ticket-42")
	case "totp_recovery":
		err = svc.ResetMFA(ctx, id, "ticket-42")
	case "account_restore":
		err = svc.RestoreAccount(ctx, id, "ticket-42")
	}
	if err != nil {
		t.Fatal(action, err)
	}
	after, _ := a.GetAccountByID(ctx, id)
	changed, _ := a.GetCredential(ctx, id)
	factor, _ := m.GetTOTP(ctx, id)
	roles, _ := a.ListActiveBindings(ctx, id)
	if !reflect.DeepEqual(bindings, roles) {
		t.Fatal("local recovery changed role bindings")
	}
	if action == "password_reset" {
		if changed.CredentialVersion != credential.CredentialVersion+1 || !auth.CheckPassword(localAdminNewPassword, changed.CredentialHash) || auth.CheckPassword(localAdminOldPassword, changed.CredentialHash) {
			t.Fatal("Admin password did not rotate")
		}
	} else if !reflect.DeepEqual(changed, credential) {
		t.Fatal("non-password recovery changed credentials")
	}
	if action == "totp_recovery" {
		if factor.Status != "disabled" || factor.Version != method.Version+1 {
			t.Fatal("Admin factor remained active")
		}
	} else if !reflect.DeepEqual(factor, method) {
		t.Fatal("recovery changed an unrelated MFA method")
	}
	if action == "account_restore" {
		if after.Status != "active" || after.Version != before.Version+1 || after.StatusChangedBy != "local-cli" {
			t.Fatal("suspended Admin was not restored")
		}
	} else if !reflect.DeepEqual(before, after) {
		t.Fatal("recovery changed account state")
	}
	for _, suffix := range []string{".session", ".second-session"} {
		session, err := s.GetSessionByID(ctx, id+suffix)
		if err != nil || session.RevokedAt == nil || session.RevokeReason != "local_"+action {
			t.Fatal("old Admin session survived", err)
		}
	}
	if err := svc.reliable.CheckedRead(ctx, func(tx context.Context) error {
		for _, recovery := range []bool{false, true} {
			kind := "mfa"
			get := m.GetAdminTicketForUpdate
			if recovery {
				kind = "recovery"
				get = m.GetRecoveryTicketForUpdate
			}
			ticket, err := get(tx, localAdminDigest(id+kind))
			if err != nil || ticket.ConsumedAt == nil {
				t.Fatal("old Admin ticket survived", err)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	audits, err := svc.reliable.ListCommandAudits(ctx, 100)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, audit := range audits {
		if audit.ResourceID == id && audit.CommandCode == "identity.admin_local."+action {
			found = true
			if audit.ActorType != "system" || audit.ActorID != "local-cli" || audit.EventID == "" {
				t.Fatal("missing required local audit")
			}
		}
	}
	if !found {
		t.Fatal("no recovery audit")
	}
	events, err := svc.reliable.List(ctx, reliability.EventFilter{Type: "authorization.changed", Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	found = false
	for _, event := range events {
		if event.AggregateID == id {
			found = true
			body := string(event.Payload)
			if !strings.Contains(body, "ticket-42") || strings.Contains(body, localAdminNewPassword) || strings.Contains(body, changed.CredentialHash) || strings.Contains(body, before.Email) {
				t.Fatal("invalid local recovery event metadata")
			}
		}
	}
	if !found {
		t.Fatal("no required recovery Outbox event")
	}
}

func TestAdminLocalRecoveryMemoryCommandsStayInAdminDomain(t *testing.T) {
	for _, action := range []string{"password_reset", "totp_recovery", "account_restore"} {
		t.Run(action, func(t *testing.T) {
			now := time.Now().UTC().Truncate(time.Microsecond)
			a, m, s := repository.NewMemoryAdminDomainRepository(), repository.NewMemoryAdminSecurityRepository(), repository.NewMemoryAdminSessionRepository()
			seedLocalAdmin(t, a, m, s, "461101", now)
			users := repository.NewMemoryUserRepository()
			if err := users.Create(context.Background(), &domain.User{ID: "461101", Username: "local-user", Nickname: "User", Email: "461101@example.test", Status: domain.UserStatusActive, AuthVersion: 1, CreatedAt: now, UpdatedAt: now}); err != nil {
				t.Fatal(err)
			}
			if err := users.CreateVerifiedAccount(context.Background(), "461101", "461101@example.test", "independent-user-credential"); err != nil {
				t.Fatal(err)
			}
			userBefore := users.Snapshot()
			svc := NewAdminLocalRecoveryService(a, m, s)
			svc.SetClock(func() time.Time { return now })
			svc.SetReliability(reliability.NewService(transaction.NewMemory(), reliability.NewMemoryStore()))
			exerciseLocalAdmin(t, svc, a, m, s, "461101", action, now)
			if !reflect.DeepEqual(userBefore, users.Snapshot()) {
				t.Fatal("same-valued User account changed")
			}
		})
	}
}

type localAdminFailureStore struct {
	reliability.Store
	failure string
}

func (s *localAdminFailureStore) Enqueue(ctx context.Context, e *reliability.Event) (*reliability.Event, error) {
	if s.failure == "event" {
		return nil, errors.New("owned outbox failure")
	}
	return s.Store.Enqueue(ctx, e)
}
func (s *localAdminFailureStore) RecordCommandAudit(ctx context.Context, a reliability.CommandAudit) error {
	if s.failure == "audit" {
		return errors.New("owned audit failure")
	}
	return s.Store.RecordCommandAudit(ctx, a)
}
func (s *localAdminFailureStore) Snapshot() any {
	if v, ok := s.Store.(transaction.Snapshotter); ok {
		return v.Snapshot()
	}
	return nil
}
func (s *localAdminFailureStore) Restore(v any) {
	if p, ok := s.Store.(transaction.Snapshotter); ok {
		p.Restore(v)
	}
}

type localAdminNoPrimitives struct {
	repository.AdminDomainRepository
}

// These adapters perform the actual write and then report stale or foreign
// facts. The command must reject the receipt and roll back the write itself.
type localAdminCASDomain struct {
	*repository.MemoryAdminDomainRepository
	failure string
}

func (a *localAdminCASDomain) SetCredentialCAS(ctx context.Context, id, hash string, version int64, at time.Time) (repository.AdminDomainCredential, bool, error) {
	c, changed, err := a.MemoryAdminDomainRepository.SetCredentialCAS(ctx, id, hash, version, at)
	if err == nil && changed {
		if a.failure == "cas_miss" {
			return c, false, nil
		}
		c.CredentialVersion++
	}
	return c, changed, err
}

func (a *localAdminCASDomain) SetAccountStatusCAS(ctx context.Context, id string, version int64, status, reason, by string, at time.Time) (repository.AdminDomainAccount, bool, error) {
	account, changed, err := a.MemoryAdminDomainRepository.SetAccountStatusCAS(ctx, id, version, status, reason, by, at)
	if err == nil && changed {
		if a.failure == "cas_miss" {
			return account, false, nil
		}
		account.DisplayName = "foreign receipt"
	}
	return account, changed, err
}

type localAdminCASSecurity struct {
	*repository.MemoryAdminSecurityRepository
	failure string
}

func (m *localAdminCASSecurity) DisableTOTPCAS(ctx context.Context, id string, version int64, at time.Time) (repository.AdminTOTPMethod, bool, error) {
	method, changed, err := m.MemoryAdminSecurityRepository.DisableTOTPCAS(ctx, id, version, at)
	if err == nil && changed {
		if m.failure == "cas_miss" {
			return method, false, nil
		}
		method.Version++
	}
	return method, changed, err
}

func TestAdminLocalRecoveryCASAndCurrentFactsRollBack(t *testing.T) {
	for _, action := range []string{"password_reset", "totp_recovery", "account_restore"} {
		for _, failure := range []string{"cas_miss", "foreign_receipt"} {
			t.Run(action+"/"+failure, func(t *testing.T) {
				ctx := context.Background()
				now := time.Now().UTC().Truncate(time.Microsecond)
				a, m, s := repository.NewMemoryAdminDomainRepository(), repository.NewMemoryAdminSecurityRepository(), repository.NewMemoryAdminSessionRepository()
				seedLocalAdmin(t, a, m, s, "461104", now)
				if action == "account_restore" {
					account, _ := a.GetAccountByID(ctx, "461104")
					if _, ok, err := a.SetAccountStatusCAS(ctx, account.ID, account.Version, "suspended", "fixture", "local-fixture", now); err != nil || !ok {
						t.Fatal("invalid restore fixture", err)
					}
				}
				store := reliability.NewMemoryStore()
				svc := NewAdminLocalRecoveryService(&localAdminCASDomain{a, failure}, &localAdminCASSecurity{m, failure}, s)
				svc.SetReliability(reliability.NewService(transaction.NewMemory(), store))
				before := []any{a.Snapshot(), m.Snapshot(), s.Snapshot(), store.Snapshot()}
				var err error
				switch action {
				case "password_reset":
					err = svc.ResetPassword(ctx, "461104@example.test", localAdminNewPassword, "ticket-42")
				case "totp_recovery":
					err = svc.ResetMFA(ctx, "461104", "ticket-42")
				case "account_restore":
					err = svc.RestoreAccount(ctx, "461104", "ticket-42")
				}
				if !errors.Is(err, ErrAdminLocalRecoveryConflict) {
					t.Fatal("inconsistent recovery facts were accepted", err)
				}
				if !reflect.DeepEqual(before, []any{a.Snapshot(), m.Snapshot(), s.Snapshot(), store.Snapshot()}) {
					t.Fatal("inconsistent recovery facts left partial writes")
				}
			})
		}
	}
}

func TestAdminLocalRecoveryMissingProofAndTerminalStatesClose(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)
	a, m, s := repository.NewMemoryAdminDomainRepository(), repository.NewMemoryAdminSecurityRepository(), repository.NewMemoryAdminSessionRepository()
	seedLocalAdmin(t, a, m, s, "461102", now)
	svc := NewAdminLocalRecoveryService(a, m, s)
	if !errors.Is(svc.ResetMFA(ctx, "461102", "ticket"), ErrAdminLocalRecoveryUnavailable) {
		t.Fatal("nil reliability permitted a write")
	}
	svc.SetReliability(reliability.NewService(transaction.NewMemory(), reliability.NewMemoryStore()))
	noLock := NewAdminLocalRecoveryService(&localAdminNoPrimitives{a}, m, s)
	noLock.SetReliability(svc.reliable)
	if !errors.Is(noLock.ResetMFA(ctx, "461102", "ticket"), ErrAdminLocalRecoveryUnavailable) {
		t.Fatal("missing row-lock Port accepted")
	}
	if !errors.Is(svc.RestoreAccount(ctx, "461102", "ticket"), ErrAdminLocalRecoveryDenied) {
		t.Fatal("active account restored")
	}
	for _, id := range []string{"", "user/id", " leading"} {
		if !errors.Is(svc.ResetMFA(ctx, id, "ticket"), ErrAdminLocalRecoveryInvalid) {
			t.Fatal("invalid opaque Admin id accepted")
		}
	}
	if !errors.Is(svc.ResetMFA(ctx, "461102", ""), ErrAdminLocalRecoveryInvalid) {
		t.Fatal("missing audit reason accepted")
	}
	account, _ := a.GetAccountByID(ctx, "461102")
	account, _, _ = a.SetAccountStatusCAS(ctx, account.ID, account.Version, "suspended", "fixture", "local-fixture", now)
	beforeSuspended := []any{a.Snapshot(), m.Snapshot(), s.Snapshot()}
	for _, err := range []error{svc.ResetMFA(ctx, account.ID, "ticket"), svc.ResetPassword(ctx, account.Email, localAdminNewPassword, "ticket")} {
		if !errors.Is(err, ErrAdminLocalRecoveryDenied) {
			t.Fatal("suspended Admin password or MFA was recovered", err)
		}
	}
	if !reflect.DeepEqual(beforeSuspended, []any{a.Snapshot(), m.Snapshot(), s.Snapshot()}) {
		t.Fatal("rejected suspended recovery changed an Admin store")
	}
	if _, ok, err := a.SetAccountStatusCAS(ctx, account.ID, account.Version, "revoked", "fixture", "local-fixture", now); err != nil || !ok {
		t.Fatal(err)
	}
	for _, err := range []error{svc.ResetMFA(ctx, account.ID, "ticket"), svc.RestoreAccount(ctx, account.ID, "ticket"), svc.ResetPassword(ctx, account.Email, localAdminNewPassword, "ticket")} {
		if !errors.Is(err, ErrAdminLocalRecoveryDenied) {
			t.Fatal("revoked terminal Admin was recovered", err)
		}
	}
	audits, _ := svc.reliable.ListCommandAudits(ctx, 10)
	if len(audits) != 0 {
		t.Fatal("rejected local recovery committed evidence")
	}
}

func TestAdminLocalRecoveryRequiredEvidenceRollsBackAllMemoryStores(t *testing.T) {
	for _, action := range []string{"password_reset", "totp_recovery", "account_restore"} {
		for _, failure := range []string{"event", "audit"} {
			t.Run(action+"/"+failure, func(t *testing.T) {
				ctx := context.Background()
				now := time.Now().UTC().Truncate(time.Microsecond)
				a, m, s := repository.NewMemoryAdminDomainRepository(), repository.NewMemoryAdminSecurityRepository(), repository.NewMemoryAdminSessionRepository()
				seedLocalAdmin(t, a, m, s, "461103", now)
				if action == "account_restore" {
					before, _ := a.GetAccountByID(ctx, "461103")
					_, _, _ = a.SetAccountStatusCAS(ctx, before.ID, before.Version, "suspended", "fixture", "local-fixture", now)
				}
				store := &localAdminFailureStore{Store: reliability.NewMemoryStore(), failure: failure}
				svc := NewAdminLocalRecoveryService(a, m, s)
				svc.SetReliability(reliability.NewService(transaction.NewMemory(), store))
				before := []any{a.Snapshot(), m.Snapshot(), s.Snapshot(), store.Snapshot()}
				var err error
				switch action {
				case "password_reset":
					err = svc.ResetPassword(ctx, "461103@example.test", localAdminNewPassword, "ticket-42")
				case "totp_recovery":
					err = svc.ResetMFA(ctx, "461103", "ticket-42")
				case "account_restore":
					err = svc.RestoreAccount(ctx, "461103", "ticket-42")
				}
				if !errors.Is(err, ErrAdminLocalRecoveryUnavailable) {
					t.Fatal("required evidence failure was ignored", err)
				}
				after := []any{a.Snapshot(), m.Snapshot(), s.Snapshot(), store.Snapshot()}
				if !reflect.DeepEqual(before, after) {
					t.Fatal("required evidence failure left partial recovery")
				}
			})
		}
	}
}

func TestAdminLocalRecoveryPostgreSQL(t *testing.T) {
	raw := os.Getenv("CAMPUSOS_ADMIN_LOCAL_RECOVERY_TEST_DATABASE_URL")
	if raw == "" {
		t.Skip("owned Admin local recovery PG target not configured")
	}
	cfg, err := pgxpool.ParseConfig(raw)
	if err != nil {
		t.Fatal("invalid owned target")
	}
	if cfg.ConnConfig.Database != "campusos_v12_final_recovery_unit" || (cfg.ConnConfig.Host != "127.0.0.1" && cfg.ConnConfig.Host != "localhost") {
		t.Fatal("requires owned loopback recovery unit DB")
	}
	// The row-lock probe must use a second connection while the command holds
	// the first. A caller's pool_max_conns=1 cannot turn the check into a hang.
	cfg.MaxConns = 4
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	var version int
	if err = pool.QueryRow(ctx, `SELECT max(version)::int FROM schema_migrations`).Scan(&version); err != nil || version != 35 {
		t.Fatal("full current Schema35 required", err)
	}
	a, m, s := repository.NewPgAdminDomainRepository(pool), repository.NewPgAdminSecurityRepository(pool), repository.NewPgAdminSessionRepository(pool)
	reliable := reliability.NewService(transaction.NewPostgreSQL(pool), reliability.NewPostgreSQLStore(pool))
	for i, action := range []string{"password_reset", "totp_recovery", "account_restore"} {
		t.Run(action, func(t *testing.T) {
			now := time.Now().UTC().Truncate(time.Microsecond)
			id := fmt.Sprintf("local-pg-%d-%d", time.Now().UnixNano(), i)
			seedLocalAdmin(t, a, m, s, id, now)
			svc := NewAdminLocalRecoveryService(a, m, s)
			svc.SetClock(func() time.Time { return now })
			svc.SetReliability(reliable)
			exerciseLocalAdmin(t, svc, a, m, s, id, action, now)
		})
	}
	for _, table := range []string{"platform_command_audits", "platform_outbox"} {
		for _, action := range []string{"password_reset", "totp_recovery", "account_restore"} {
			t.Run("actual_sql_failure_"+table+"/"+action, func(t *testing.T) {
				now := time.Now().UTC().Truncate(time.Microsecond)
				id := fmt.Sprintf("local-pg-fail-%d", time.Now().UnixNano())
				seedLocalAdmin(t, a, m, s, id, now)
				if action == "account_restore" {
					account, err := a.GetAccountByID(ctx, id)
					if err != nil {
						t.Fatal(err)
					}
					if _, ok, err := a.SetAccountStatusCAS(ctx, id, account.Version, "suspended", "fixture", "local-fixture", now); err != nil || !ok {
						t.Fatal("invalid restore fixture", err)
					}
				}
				before := localAdminPGSnapshot(t, pool, id)
				_, err := pool.Exec(ctx, `CREATE FUNCTION owned_local_admin_fail() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'owned required evidence failure';END $$;CREATE TRIGGER owned_local_admin_fail BEFORE INSERT ON `+table+` FOR EACH ROW EXECUTE FUNCTION owned_local_admin_fail()`)
				if err != nil {
					t.Fatal(err)
				}
				defer func() {
					if _, err := pool.Exec(context.Background(), `DROP TRIGGER IF EXISTS owned_local_admin_fail ON `+table+`;DROP FUNCTION IF EXISTS owned_local_admin_fail()`); err != nil {
						t.Error(err)
					}
				}()
				svc := NewAdminLocalRecoveryService(a, m, s)
				svc.SetReliability(reliable)
				var recoveryErr error
				switch action {
				case "password_reset":
					recoveryErr = svc.ResetPassword(ctx, id+"@example.test", localAdminNewPassword, "ticket-42")
				case "totp_recovery":
					recoveryErr = svc.ResetMFA(ctx, id, "ticket-42")
				case "account_restore":
					recoveryErr = svc.RestoreAccount(ctx, id, "ticket-42")
				}
				if !errors.Is(recoveryErr, ErrAdminLocalRecoveryUnavailable) {
					t.Fatal("actual SQL evidence failure did not reject")
				}
				if after := localAdminPGSnapshot(t, pool, id); before != after {
					t.Fatal("real PG evidence failure left partial recovery")
				}
			})
		}
	}
	t.Run("account_credential_factor_ticket_session_locks", func(t *testing.T) {
		now := time.Now().UTC().Truncate(time.Microsecond)
		id := fmt.Sprintf("local-pg-lock-%d", time.Now().UnixNano())
		seedLocalAdmin(t, a, m, s, id, now)
		err := transaction.NewPostgreSQL(pool).Within(ctx, func(tx context.Context) error {
			if _, e := a.GetAccountByIDForUpdate(tx, id); e != nil {
				return e
			}
			if _, e := a.GetCredentialForUpdate(tx, id); e != nil {
				return e
			}
			if _, e := m.GetTOTPForUpdate(tx, id); e != nil {
				return e
			}
			if e := m.InvalidateLocalRecoveryTickets(tx, id, now); e != nil {
				return e
			}
			if e := s.RevokeSessionsForAccount(tx, id, "local_lock_probe", now); e != nil {
				return e
			}
			for _, q := range []string{`SELECT id FROM admin_accounts WHERE id=$1 FOR UPDATE NOWAIT`, `SELECT account_id FROM admin_credentials WHERE account_id=$1 FOR UPDATE NOWAIT`, `SELECT account_id FROM admin_mfa_totp WHERE account_id=$1 FOR UPDATE NOWAIT`, `SELECT id FROM admin_mfa_tickets WHERE account_id=$1 FOR UPDATE NOWAIT`, `SELECT id FROM admin_recovery_tickets WHERE account_id=$1 FOR UPDATE NOWAIT`, `SELECT id FROM admin_sessions WHERE account_id=$1 FOR UPDATE NOWAIT`} {
				_, e := pool.Exec(ctx, q, id)
				var pe *pgconn.PgError
				if !errors.As(e, &pe) || pe.Code != "55P03" {
					t.Fatal("actual recovery row was not locked", q, e)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	})
}

func localAdminPGSnapshot(t *testing.T, pool *pgxpool.Pool, id string) string {
	t.Helper()
	var raw []byte
	err := pool.QueryRow(context.Background(), `SELECT jsonb_build_object('account',(SELECT to_jsonb(a) FROM admin_accounts a WHERE id=$1),'credential',(SELECT to_jsonb(c) FROM admin_credentials c WHERE account_id=$1),'factor',(SELECT to_jsonb(m) FROM admin_mfa_totp m WHERE account_id=$1),'sessions',(SELECT jsonb_agg(to_jsonb(s) ORDER BY id) FROM admin_sessions s WHERE account_id=$1),'tickets',(SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM admin_mfa_tickets t WHERE account_id=$1),'recovery',(SELECT jsonb_agg(to_jsonb(r) ORDER BY id) FROM admin_recovery_tickets r WHERE account_id=$1),'audits',(SELECT count(*) FROM platform_command_audits WHERE resource_id=$1),'events',(SELECT count(*) FROM platform_outbox WHERE aggregate_id=$1))`, id).Scan(&raw)
	if err != nil {
		t.Fatal(err)
	}
	var value any
	if json.Unmarshal(raw, &value) != nil {
		t.Fatal("invalid snapshot")
	}
	canonical, _ := json.Marshal(value)
	return string(canonical)
}
