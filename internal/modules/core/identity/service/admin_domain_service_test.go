package service

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/jackc/pgx/v5/pgxpool"
)

type adminDomainEnv struct {
	service *AdminDomainService
	admins  repository.AdminDomainRepository
	catalog repository.AuthorizationRepository
}

func newAdminDomainEnv(bootstrapEnabled bool) *adminDomainEnv {
	admins := repository.NewMemoryAdminDomainRepository()
	catalog := repository.NewMemoryRoleRepository()
	config := DefaultAdminDomainConfig()
	config.BootstrapEnabled = bootstrapEnabled
	return &adminDomainEnv{
		service: NewAdminDomainService(admins, catalog, config),
		admins:  admins,
		catalog: catalog,
	}
}

func adminDomainOutcome(err error) string {
	if err == nil {
		return "accepted"
	}
	var deny *port.IdentityDomainDenyError
	if errors.As(err, &deny) {
		return deny.Reason
	}
	return err.Error()
}

func TestAdminDomainBootstrapFirstSecurityAdmin(t *testing.T) {
	ctx := context.Background()
	env := newAdminDomainEnv(true)
	account, err := env.service.BootstrapSecurityAdmin(ctx, "root-admin", "root@example.test", "initial-password", "initial bootstrap")
	if err != nil || account.Status != "active" {
		t.Fatalf("bootstrap: %+v %v", account, err)
	}
	credential, err := env.admins.GetCredential(ctx, account.ID)
	if err != nil || credential.CredentialVersion != 1 {
		t.Fatalf("credential: %+v %v", credential, err)
	}
	bindings, err := env.admins.ListActiveBindings(ctx, account.ID)
	if err != nil || len(bindings) != 1 || bindings[0].RoleID != "security_admin" {
		t.Fatalf("bindings: %+v %v", bindings, err)
	}
	count, err := env.admins.CountActiveSecurityAdmins(ctx)
	if err != nil || count != 1 {
		t.Fatalf("security admins: %d %v", count, err)
	}
	// A second bootstrap is forbidden once a security admin exists.
	if _, err = env.service.BootstrapSecurityAdmin(ctx, "second", "second@example.test", "password", "again"); adminDomainOutcome(err) != "identity.admin_bootstrap_forbidden" {
		t.Fatalf("second bootstrap: %v", err)
	}
	// Bootstrap requires the controlled entry to be enabled.
	env2 := newAdminDomainEnv(false)
	if _, err = env2.service.BootstrapSecurityAdmin(ctx, "root-admin", "root@example.test", "password", "off"); adminDomainOutcome(err) != "identity.admin_bootstrap_forbidden" {
		t.Fatalf("disabled bootstrap: %v", err)
	}
}

func TestAdminDomainLifecycleGuards(t *testing.T) {
	ctx := context.Background()
	env := newAdminDomainEnv(true)
	root, err := env.service.BootstrapSecurityAdmin(ctx, "root-admin", "root@example.test", "initial-password", "bootstrap")
	if err != nil {
		t.Fatal(err)
	}
	actor := AdminActor{ID: root.ID, MFAAuthenticated: true, CredentialID: "session-1"}
	// Admin lifecycle commands require an MFA admin actor.
	if _, err = env.service.CreateAdmin(ctx, AdminActor{ID: root.ID, MFAAuthenticated: false}, "ops", "ops@example.test", "password", false, "create"); adminDomainOutcome(err) != "identity.admin_mfa_required" {
		t.Fatalf("create without mfa: %v", err)
	}
	second, err := env.service.CreateAdmin(ctx, actor, "ops", "ops@example.test", "password", true, "create second")
	if err != nil {
		t.Fatal(err)
	}
	// Self transition is always refused before evaluation.
	if err = env.service.SuspendAdmin(ctx, actor, root.ID, "self"); !errors.Is(err, ErrAdminSelfTransition) {
		t.Fatalf("self suspend: %v", err)
	}
	// With two security admins, suspending one is allowed.
	if err = env.service.SuspendAdmin(ctx, actor, second.ID, "rotate"); err != nil {
		t.Fatal(err)
	}
	if err = env.service.RestoreAdmin(ctx, actor, second.ID, "restore"); err != nil {
		t.Fatal(err)
	}
	// Suspend the second, then the last remaining security admin is protected.
	if err = env.service.SuspendAdmin(ctx, actor, second.ID, "rotate"); err != nil {
		t.Fatal(err)
	}
	if err = env.service.SuspendAdmin(ctx, AdminActor{ID: second.ID, MFAAuthenticated: true}, root.ID, "last"); adminDomainOutcome(err) != "identity.last_security_admin" {
		t.Fatalf("last admin suspend: %v", err)
	}
	if err = env.service.RevokeAdmin(ctx, AdminActor{ID: second.ID, MFAAuthenticated: true}, root.ID, "last"); adminDomainOutcome(err) != "identity.last_security_admin" {
		t.Fatalf("last admin revoke: %v", err)
	}
	// Restore still works, then revoke is terminal.
	if err = env.service.RestoreAdmin(ctx, actor, second.ID, "restore"); err != nil {
		t.Fatal(err)
	}
	if err = env.service.RevokeAdmin(ctx, actor, second.ID, "offboard"); err != nil {
		t.Fatal(err)
	}
	if err = env.service.RestoreAdmin(ctx, actor, second.ID, "back"); err == nil {
		t.Fatal("revoked account restored")
	}
}

func TestAdminDomainAuditWritesPerCommand(t *testing.T) {
	ctx := context.Background()
	env := newAdminDomainEnv(true)
	root, err := env.service.BootstrapSecurityAdmin(ctx, "root-admin", "root@example.test", "initial-password", "bootstrap reason")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = env.service.CreateAdmin(ctx, AdminActor{ID: root.ID, MFAAuthenticated: true}, "ops", "ops@example.test", "password", false, "create reason"); err != nil {
		t.Fatal(err)
	}
	audits, err := env.catalog.ListAuthorizationAudits(ctx, 20)
	if err != nil || len(audits) != 2 {
		t.Fatalf("audits: %d %v", len(audits), err)
	}
	byOperation := map[string]repository.AuthorizationAudit{}
	for _, audit := range audits {
		byOperation[audit.OperationCode] = audit
		if audit.Reason == "" || audit.ResourceType != "admin_account" || audit.Outcome != "allow" {
			t.Fatalf("audit shape: %+v", audit)
		}
	}
	if byOperation["identity.admin_domain.bootstrap"].ActorKind != "system" || byOperation["identity.admin_domain.create"].ActorKind != "admin" {
		t.Fatalf("audit actor domains: %#v", audits)
	}
}

func TestAdminDomainBootstrapRaceSingleWinner(t *testing.T) {
	ctx := context.Background()
	env := newAdminDomainEnv(true)
	env.service.SetReliability(reliability.NewService(transaction.NewMemory(), reliability.NewMemoryStore()))
	const racers = 8
	results := make(chan error, racers)
	for i := 0; i < racers; i++ {
		go func(i int) {
			_, err := env.service.BootstrapSecurityAdmin(ctx, "root-admin", "root@example.test", "password", "race bootstrap")
			results <- err
		}(i)
	}
	wins, denied := 0, 0
	for i := 0; i < racers; i++ {
		err := <-results
		switch {
		case err == nil:
			wins++
		case adminDomainOutcome(err) == "identity.admin_bootstrap_forbidden" || errors.Is(err, repository.ErrAdminDomainAccountConflict):
			denied++
		default:
			t.Errorf("unexpected race error: %v", err)
		}
	}
	if wins != 1 || wins+denied != racers {
		t.Fatalf("bootstrap race: wins=%d denied=%d", wins, denied)
	}
	count, err := env.admins.CountActiveSecurityAdmins(ctx)
	if err != nil || count != 1 {
		t.Fatalf("security admins after race: %d %v", count, err)
	}
}

// PostgreSQL acceptance runs only inside the isolated drill database.
func newAdminDomainPostgreSQLEnv(t *testing.T) (*AdminDomainService, *pgxpool.Pool, context.Context) {
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
		"DELETE FROM authorization_audits WHERE resource_type = 'admin_account'",
		"DELETE FROM admin_role_bindings WHERE granted_by LIKE 'svc-test%' OR granted_by = 'bootstrap'",
		"DELETE FROM admin_credentials WHERE account_id LIKE 'adm-%'",
		"DELETE FROM admin_accounts WHERE id LIKE 'adm-%'",
	} {
		if _, err = pool.Exec(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	admins := repository.NewPgAdminDomainRepository(pool)
	config := DefaultAdminDomainConfig()
	config.BootstrapEnabled = true
	service := NewAdminDomainService(admins, repository.NewPgRoleRepository(pool), config)
	service.SetReliability(reliability.NewService(transaction.NewPostgreSQL(pool), reliability.NewPostgreSQLStore(pool)))
	return service, pool, ctx
}

func TestPostgreSQLAdminDomainLifecycleAndRace(t *testing.T) {
	service, pool, ctx := newAdminDomainPostgreSQLEnv(t)
	root, err := service.BootstrapSecurityAdmin(ctx, "svc-test-root", "svc-test-root@example.test", "initial-password", "bootstrap")
	if err != nil {
		t.Fatal(err)
	}
	// Real concurrent bootstrap: exactly one winner.
	const racers = 6
	results := make(chan error, racers)
	for i := 0; i < racers; i++ {
		go func() {
			_, err := service.BootstrapSecurityAdmin(ctx, "svc-test-other", "svc-test-other@example.test", "password", "race")
			results <- err
		}()
	}
	wins := 0
	for i := 0; i < racers; i++ {
		if err := <-results; err == nil {
			wins++
		}
	}
	if wins != 0 {
		t.Fatalf("second bootstrap committed: %d", wins)
	}
	actor := AdminActor{ID: root.ID, MFAAuthenticated: true, CredentialID: "svc-test-session"}
	second, err := service.CreateAdmin(ctx, actor, "svc-test-ops", "svc-test-ops@example.test", "password", true, "create ops")
	if err != nil {
		t.Fatal(err)
	}
	if err = service.SuspendAdmin(ctx, actor, second.ID, "rotate"); err != nil {
		t.Fatal(err)
	}
	var status string
	if err = pool.QueryRow(ctx, "SELECT status FROM admin_accounts WHERE id=$1", second.ID).Scan(&status); err != nil || status != "suspended" {
		t.Fatalf("status: %s %v", status, err)
	}
	if err = service.RestoreAdmin(ctx, actor, second.ID, "restore"); err != nil {
		t.Fatal(err)
	}
	// Required audit rows exist for every lifecycle command.
	var auditCount int
	if err = pool.QueryRow(ctx, "SELECT count(*) FROM authorization_audits WHERE resource_type='admin_account' AND outcome='allow'").Scan(&auditCount); err != nil {
		t.Fatal(err)
	}
	if auditCount != 4 {
		t.Fatalf("audits: %d", auditCount)
	}
	// Audit failure rolls the whole command back.
	service.catalog = failingAdminCatalog{}
	if _, err = service.CreateAdmin(ctx, actor, "svc-test-third", "svc-test-third@example.test", "password", false, "create third"); err == nil {
		t.Fatal("audit failure accepted")
	}
	var exists bool
	if err = pool.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM admin_accounts WHERE username='svc-test-third')").Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Fatal("account committed without required audit")
	}
}

type failingAdminCatalog struct {
	repository.AuthorizationRepository
}

func (failingAdminCatalog) RecordAuthorizationAudit(context.Context, repository.AuthorizationAudit) error {
	return errors.New("audit store is down")
}
