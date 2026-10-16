package service

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/jackc/pgx/v5/pgxpool"
)

type adminMFAEnv struct {
	service  *AdminMFAService
	admins   repository.AdminDomainRepository
	security repository.AdminSecurityRepository
	sessions repository.AdminSessionRepository
}

func newAdminMFAEnv(t *testing.T) *adminMFAEnv {
	t.Helper()
	admins := repository.NewMemoryAdminDomainRepository()
	security := repository.NewMemoryAdminSecurityRepository()
	sessions := repository.NewMemoryAdminSessionRepository()
	protector, err := NewMFASecretProtector("k1", map[string]string{"k1": "0123456789abcdef0123456789abcdef"})
	if err != nil {
		t.Fatal(err)
	}
	return &adminMFAEnv{
		service:  NewAdminMFAService(admins, security, sessions, protector),
		admins:   admins,
		security: security,
		sessions: sessions,
	}
}

func (e *adminMFAEnv) bootstrapRoot(t *testing.T, ctx context.Context) (*repository.AdminDomainAccount, AdminActor) {
	t.Helper()
	config := DefaultAdminDomainConfig()
	config.BootstrapEnabled = true
	account, err := NewAdminDomainService(e.admins, nil, config).BootstrapSecurityAdmin(ctx, "root-admin", "root@example.test", "initial-password", "bootstrap")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := e.sessions.CreateSession(ctx, repository.AdminSession{ID: "session-1", AccountID: account.ID, RefreshTokenDigest: strings.Repeat("a", 64), TokenFamilyID: "session-1", AuthenticationStrength: "password", CreatedAt: now, UpdatedAt: now, LastActiveAt: now, ExpiresAt: now.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	return account, AdminActor{ID: account.ID, CredentialID: "session-1"}
}

func adminTOTPCodeForTest(t *testing.T, secret []byte, step int64) string {
	t.Helper()
	return totpCode(secret, step)
}

func TestAdminMFAEnrollmentConfirmAndStepUp(t *testing.T) {
	ctx := context.Background()
	env := newAdminMFAEnv(t)
	account, actor := env.bootstrapRoot(t, ctx)

	otpauth, err := env.service.StartEnrollment(ctx, actor, "campusos-admin")
	if err != nil || !strings.HasPrefix(otpauth, "otpauth://totp/campusos-admin:root-admin") {
		t.Fatalf("enrollment: %q %v", otpauth, err)
	}
	// Extract the sealed secret via the store to compute codes in tests.
	method, err := env.security.GetTOTP(ctx, account.ID)
	if err != nil || method.Status != "pending" {
		t.Fatalf("pending method: %+v %v", method, err)
	}
	protector, _ := NewMFASecretProtector("k1", map[string]string{"k1": "0123456789abcdef0123456789abcdef"})
	secret, err := protector.Open(method.KeyID, method.Nonce, method.Ciphertext)
	if err != nil {
		t.Fatal(err)
	}
	step := time.Now().UTC().Unix() / adminTOTPPeriod
	if err = env.service.ConfirmEnrollment(ctx, actor, adminTOTPCodeForTest(t, secret, step)); err != nil {
		t.Fatal(err)
	}
	activated, err := env.security.GetTOTP(ctx, account.ID)
	if err != nil || activated.Status != "active" || activated.LastAcceptedStep != step {
		t.Fatalf("activated: %+v %v", activated, err)
	}
	// The confirming code cannot be replayed for a step-up.
	domains := adminDomainManagers()
	sessions := NewAdminSessionService(env.admins, env.sessions, domains.Admin)
	loginTokens, err := sessions.Login(ctx, "root-admin", "initial-password", AdminSessionMetadata{})
	if err != nil {
		t.Fatal(err)
	}
	sessionID := sessionIDFromToken(t, loginTokens.AccessToken)
	actor.CredentialID = sessionID
	if err = env.service.VerifyStepUp(ctx, actor, sessionID, adminTOTPCodeForTest(t, secret, step)); !errors.Is(err, ErrAdminMFACodeInvalid) && !errors.Is(err, ErrAdminMFAReplay) {
		t.Fatalf("replay step-up accepted: %v", err)
	}
	// A fresh code upgrades the session to MFA strength atomically.
	if err = env.service.VerifyStepUp(ctx, actor, sessionID, adminTOTPCodeForTest(t, secret, step+1)); err != nil {
		t.Fatal(err)
	}
	verified, err := sessions.VerifyAccess(ctx, sessionID)
	if err != nil || verified.AuthenticationStrength != "mfa" || verified.MFAAuthenticatedAt == nil {
		t.Fatalf("strength upgrade: %+v %v", verified, err)
	}
	// Same code again is a replay.
	if err = env.service.VerifyStepUp(ctx, actor, sessionID, adminTOTPCodeForTest(t, secret, step+1)); !errors.Is(err, ErrAdminMFAReplay) {
		t.Fatalf("step replay accepted: %v", err)
	}
}

func TestAdminMFAGuards(t *testing.T) {
	ctx := context.Background()
	env := newAdminMFAEnv(t)
	account, actor := env.bootstrapRoot(t, ctx)
	_ = account
	// First enrollment is account-local self-service: the actor's own session
	// suffices, while enrolling another account requires an MFA admin actor.
	if _, err := env.service.StartEnrollment(ctx, AdminActor{ID: actor.ID, CredentialID: actor.CredentialID}, "campusos-admin"); err != nil {
		t.Fatalf("self first enrollment: %v", err)
	}
	if _, err := env.service.StartEnrollment(ctx, AdminActor{ID: "someone-else"}, "campusos-admin"); !errors.Is(err, ErrAdminMFAActor) {
		t.Fatalf("cross-account enrollment: %v", err)
	}
	fresh := newAdminMFAEnv(t)
	freshAccount, freshActor := fresh.bootstrapRoot(t, ctx)
	_ = freshAccount
	if err := fresh.service.ConfirmEnrollment(ctx, freshActor, "123456"); !errors.Is(err, ErrAdminMFANotEnabled) {
		t.Fatalf("confirm without enrollment: %v", err)
	}
	if err := env.service.ConfirmEnrollment(ctx, actor, "000000"); !errors.Is(err, ErrAdminMFACodeInvalid) {
		t.Fatalf("bad code accepted: %v", err)
	}
	if err := env.service.VerifyStepUp(ctx, actor, "missing-session", "123456"); !errors.Is(err, ErrAdminMFAActor) {
		t.Fatalf("step-up for a different session: %v", err)
	}
}

// PostgreSQL acceptance runs only inside the isolated drill database.
func newAdminMFAPostgreSQLEnv(t *testing.T) (*AdminMFAService, *pgxpool.Pool, context.Context) {
	t.Helper()
	databaseURL := strings.TrimSpace(os.Getenv("CAMPUSOS_IDENTITY_TEST_DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("CAMPUSOS_IDENTITY_TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	for _, statement := range []string{
		"DELETE FROM admin_sessions",
		"DELETE FROM admin_mfa_totp WHERE account_id LIKE 'adm-%'",
		"DELETE FROM admin_mfa_tickets WHERE account_id LIKE 'adm-%'",
		"DELETE FROM admin_recovery_tickets WHERE account_id LIKE 'adm-%'",
		"DELETE FROM admin_role_bindings WHERE granted_by = 'bootstrap'",
		"DELETE FROM admin_credentials WHERE account_id LIKE 'adm-%'",
		"DELETE FROM admin_accounts WHERE id LIKE 'adm-%'",
	} {
		if _, err = pool.Exec(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	admins := repository.NewPgAdminDomainRepository(pool)
	security := repository.NewPgAdminSecurityRepository(pool)
	sessions := repository.NewPgAdminSessionRepository(pool)
	protector, err := NewMFASecretProtector("k1", map[string]string{"k1": "0123456789abcdef0123456789abcdef"})
	if err != nil {
		t.Fatal(err)
	}
	config := DefaultAdminDomainConfig()
	config.BootstrapEnabled = true
	account, err := NewAdminDomainService(admins, nil, config).BootstrapSecurityAdmin(ctx, "svc-test-root", "svc-test-root@example.test", "initial-password", "bootstrap")
	if err != nil {
		t.Fatal(err)
	}
	service := NewAdminMFAService(admins, security, sessions, protector)
	service.SetReliability(reliability.NewService(transaction.NewPostgreSQL(pool), reliability.NewPostgreSQLStore(pool)))
	now := time.Now().UTC()
	if err := sessions.CreateSession(ctx, repository.AdminSession{ID: "svc-test-session", AccountID: account.ID, RefreshTokenDigest: strings.Repeat("b", 64), TokenFamilyID: "svc-test-session", AuthenticationStrength: "password", CreatedAt: now, UpdatedAt: now, LastActiveAt: now, ExpiresAt: now.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	service.bootstrapAccountID = account.ID
	return service, pool, ctx
}

func TestPostgreSQLAdminMFAStepUp(t *testing.T) {
	service, pool, ctx := newAdminMFAPostgreSQLEnv(t)
	actor := AdminActor{ID: service.bootstrapAccountID, CredentialID: "svc-test-session"}
	if _, err := service.StartEnrollment(ctx, actor, "campusos-admin"); err != nil {
		t.Fatal(err)
	}
	var keyID, nonce, ciphertext string
	if err := pool.QueryRow(ctx, "SELECT key_id, nonce, ciphertext FROM admin_mfa_totp WHERE account_id=$1", actor.ID).Scan(&keyID, &nonce, &ciphertext); err != nil {
		t.Fatal(err)
	}
	protector, _ := NewMFASecretProtector("k1", map[string]string{"k1": "0123456789abcdef0123456789abcdef"})
	secret, err := protector.Open(keyID, nonce, ciphertext)
	if err != nil {
		t.Fatal(err)
	}
	step := time.Now().UTC().Unix() / adminTOTPPeriod
	if err = service.ConfirmEnrollment(ctx, actor, totpCode(secret, step)); err != nil {
		t.Fatal(err)
	}
	admins := repository.NewPgAdminDomainRepository(pool)
	sessions := repository.NewPgAdminSessionRepository(pool)
	domainService := NewAdminSessionService(admins, sessions, adminDomainManagers().Admin)
	loginTokens, err := domainService.Login(ctx, "svc-test-root", "initial-password", AdminSessionMetadata{})
	if err != nil {
		t.Fatal(err)
	}
	sessionID := sessionIDFromToken(t, loginTokens.AccessToken)
	actor.CredentialID = sessionID
	if err = service.VerifyStepUp(ctx, actor, sessionID, totpCode(secret, step+1)); err != nil {
		t.Fatal(err)
	}
	var strength string
	if err = pool.QueryRow(ctx, "SELECT authentication_strength FROM admin_sessions WHERE id=$1", sessionID).Scan(&strength); err != nil || strength != "mfa" {
		t.Fatalf("strength: %s %v", strength, err)
	}
	// Replay fails closed on real PostgreSQL.
	if err = service.VerifyStepUp(ctx, actor, sessionID, totpCode(secret, step+1)); !errors.Is(err, ErrAdminMFAReplay) {
		t.Fatalf("replay accepted: %v", err)
	}
	var storedStep int64
	if err = pool.QueryRow(ctx, "SELECT last_accepted_step FROM admin_mfa_totp WHERE account_id=$1", actor.ID).Scan(&storedStep); err != nil || storedStep != step+1 {
		t.Fatalf("accepted step: %d %v", storedStep, err)
	}
}
