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
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/jackc/pgx/v5/pgxpool"
)

type adminSessionEnv struct {
	service  *AdminSessionService
	admins   repository.AdminDomainRepository
	sessions repository.AdminSessionRepository
	userJWT  *auth.JWTManager
}

func newAdminSessionEnv() *adminSessionEnv {
	admins := repository.NewMemoryAdminDomainRepository()
	sessions := repository.NewMemoryAdminSessionRepository()
	domains := adminDomainManagers()
	service := NewAdminSessionService(admins, sessions, domains.Admin)
	return &adminSessionEnv{service: service, admins: admins, sessions: sessions, userJWT: domains.User}
}

func adminDomainManagers() *auth.DomainJWTManagers {
	return auth.NewDomainJWTManagers(
		auth.JWTConfig{Secret: "user-secret-material", Issuer: "campusos-user", Audience: "user-api", KeyID: "user-key"},
		auth.JWTConfig{Secret: "admin-secret-material", Issuer: "campusos-admin", Audience: "admin-api", KeyID: "admin-key"},
	)
}

func bootstrapAdmin(t *testing.T, ctx context.Context, admins repository.AdminDomainRepository) *repository.AdminDomainAccount {
	t.Helper()
	config := DefaultAdminDomainConfig()
	config.BootstrapEnabled = true
	lifecycle := NewAdminDomainService(admins, nil, config)
	account, err := lifecycle.BootstrapSecurityAdmin(ctx, "root-admin", "root@example.test", "initial-password", "bootstrap")
	if err != nil {
		t.Fatal(err)
	}
	return account
}

func sessionIDFromToken(t *testing.T, token string) string {
	t.Helper()
	manager := auth.NewJWTManager(auth.JWTConfig{
		Secret: "admin-secret-material", Issuer: "campusos-admin", Audience: "admin-api", KeyID: "admin-key",
	})
	claims, err := manager.VerifyAccessToken(token)
	if err != nil {
		t.Fatal(err)
	}
	return claims.SessionID
}

func TestAdminSessionLoginRefreshAndVerify(t *testing.T) {
	ctx := context.Background()
	env := newAdminSessionEnv()
	account := bootstrapAdmin(t, ctx, env.admins)
	tokens, err := env.service.Login(ctx, "root-admin", "initial-password", AdminSessionMetadata{DeviceName: "console"})
	if err != nil {
		t.Fatal(err)
	}
	// The issued access token verifies only in the admin domain.
	if _, err = env.userJWT.VerifyAccessToken(tokens.AccessToken); err == nil {
		t.Fatal("admin token accepted by user domain manager")
	}
	sessionID := sessionIDFromToken(t, tokens.AccessToken)
	session, err := env.service.VerifyAccess(ctx, sessionID)
	if err != nil || session.AuthenticationStrength != "password" || session.AccountID != account.ID {
		t.Fatalf("verify: %+v %v", session, err)
	}
	// Refresh rotates inside the family and invalidates the old credential.
	rotated, err := env.service.Refresh(ctx, tokens.RefreshToken, AdminSessionMetadata{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = env.service.Refresh(ctx, tokens.RefreshToken, AdminSessionMetadata{}); !errors.Is(err, ErrAdminRefreshReuse) {
		t.Fatalf("reuse accepted: %v", err)
	}
	// Theft detection kills the whole family, including the rotated
	// credential, so neither access nor refresh survives the reuse.
	if _, err = env.service.Refresh(ctx, rotated.RefreshToken, AdminSessionMetadata{}); !errors.Is(err, ErrAdminRefreshInvalid) {
		t.Fatalf("rotated credential survived theft detection: %v", err)
	}
	if _, err = env.service.VerifyAccess(ctx, sessionIDFromToken(t, rotated.AccessToken)); !errors.Is(err, ErrAdminSessionInvalid) {
		t.Fatalf("rotated session survived theft detection: %v", err)
	}
	// A fresh login starts a new family unaffected by the revoked one.
	fresh, err := env.service.Login(ctx, "root-admin", "initial-password", AdminSessionMetadata{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = env.service.Refresh(ctx, fresh.RefreshToken, AdminSessionMetadata{}); err != nil {
		t.Fatal(err)
	}
}

func TestAdminSessionLoginDeniesInactiveAndWrongPassword(t *testing.T) {
	ctx := context.Background()
	env := newAdminSessionEnv()
	bootstrapAdmin(t, ctx, env.admins)
	if _, err := env.service.Login(ctx, "root-admin", "wrong-password", AdminSessionMetadata{}); !errors.Is(err, ErrAdminLoginDenied) {
		t.Fatalf("wrong password: %v", err)
	}
	if _, err := env.service.Login(ctx, "no-such-admin", "initial-password", AdminSessionMetadata{}); !errors.Is(err, ErrAdminLoginDenied) {
		t.Fatalf("unknown account: %v", err)
	}
	root, err := env.admins.GetAccountByUsername(ctx, "root-admin")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := env.admins.SetAccountStatusCAS(ctx, root.ID, 1, "suspended", "test", "tester", time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err = env.service.Login(ctx, "root-admin", "initial-password", AdminSessionMetadata{}); !errors.Is(err, ErrAdminLoginDenied) {
		t.Fatalf("suspended login: %v", err)
	}
}

func TestAdminSessionRevokeCascadesToAccess(t *testing.T) {
	ctx := context.Background()
	env := newAdminSessionEnv()
	bootstrapAdmin(t, ctx, env.admins)
	tokens, err := env.service.Login(ctx, "root-admin", "initial-password", AdminSessionMetadata{})
	if err != nil {
		t.Fatal(err)
	}
	sessionID := sessionIDFromToken(t, tokens.AccessToken)
	if err = env.service.RevokeSession(ctx, sessionID, "logout"); err != nil {
		t.Fatal(err)
	}
	if _, err = env.service.VerifyAccess(ctx, sessionID); !errors.Is(err, ErrAdminSessionInvalid) {
		t.Fatalf("revoked session still valid: %v", err)
	}
	if _, err = env.service.Refresh(ctx, tokens.RefreshToken, AdminSessionMetadata{}); !errors.Is(err, ErrAdminRefreshInvalid) {
		t.Fatalf("revoked refresh accepted: %v", err)
	}
}

// PostgreSQL acceptance runs only inside the isolated drill database.
func newAdminSessionPostgreSQLEnv(t *testing.T) (*AdminSessionService, *pgxpool.Pool, context.Context) {
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
		"DELETE FROM admin_role_bindings WHERE granted_by LIKE 'svc-test%' OR granted_by = 'bootstrap'",
		"DELETE FROM admin_credentials WHERE account_id LIKE 'adm-%'",
		"DELETE FROM admin_accounts WHERE id LIKE 'adm-%'",
	} {
		if _, err = pool.Exec(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	admins := repository.NewPgAdminDomainRepository(pool)
	sessions := repository.NewPgAdminSessionRepository(pool)
	domains := adminDomainManagers()
	config := DefaultAdminDomainConfig()
	config.BootstrapEnabled = true
	lifecycle := NewAdminDomainService(admins, nil, config)
	if _, err = lifecycle.BootstrapSecurityAdmin(ctx, "svc-test-root", "svc-test-root@example.test", "initial-password", "bootstrap"); err != nil {
		t.Fatal(err)
	}
	service := NewAdminSessionService(admins, sessions, domains.Admin)
	service.SetReliability(reliability.NewService(transaction.NewPostgreSQL(pool), reliability.NewPostgreSQLStore(pool)))
	return service, pool, ctx
}

func TestPostgreSQLAdminSessionLifecycleAndReuseRace(t *testing.T) {
	service, pool, ctx := newAdminSessionPostgreSQLEnv(t)
	tokens, err := service.Login(ctx, "svc-test-root", "initial-password", AdminSessionMetadata{DeviceName: "drill"})
	if err != nil {
		t.Fatal(err)
	}
	var sessionID string
	if err = pool.QueryRow(ctx, "SELECT id FROM admin_sessions LIMIT 1").Scan(&sessionID); err != nil {
		t.Fatal(err)
	}
	rotated, err := service.Refresh(ctx, tokens.RefreshToken, AdminSessionMetadata{})
	if err != nil {
		t.Fatal(err)
	}
	var revokedReason string
	if err = pool.QueryRow(ctx, "SELECT revoke_reason FROM admin_sessions WHERE id=$1", sessionID).Scan(&revokedReason); err != nil || revokedReason != "rotated" {
		t.Fatalf("rotation revocation: %s %v", revokedReason, err)
	}
	if _, err = service.Refresh(ctx, tokens.RefreshToken, AdminSessionMetadata{}); !errors.Is(err, ErrAdminRefreshReuse) {
		t.Fatalf("reuse accepted: %v", err)
	}
	// Account suspension cascades: refresh and access both fail closed.
	admins := repository.NewPgAdminDomainRepository(pool)
	var accountID string
	if err = pool.QueryRow(ctx, "SELECT account_id FROM admin_sessions LIMIT 1").Scan(&accountID); err != nil {
		t.Fatal(err)
	}
	if _, _, err = admins.SetAccountStatusCAS(ctx, accountID, 1, "suspended", "cascade", "tester", time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err = service.Refresh(ctx, rotated.RefreshToken, AdminSessionMetadata{}); !errors.Is(err, ErrAdminRefreshInvalid) {
		t.Fatalf("suspended account refresh accepted: %v", err)
	}
	if _, err = service.VerifyAccess(ctx, sessionIDFromToken(t, rotated.AccessToken)); !errors.Is(err, ErrAdminSessionInvalid) {
		t.Fatalf("suspended account session valid: %v", err)
	}
}
