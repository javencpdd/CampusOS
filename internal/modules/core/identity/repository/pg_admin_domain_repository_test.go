package repository

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func adminDomainRepositories(t *testing.T) map[string]AdminDomainRepository {
	t.Helper()
	repos := map[string]AdminDomainRepository{"memory": NewMemoryAdminDomainRepository()}
	if databaseURL := strings.TrimSpace(os.Getenv("CAMPUSOS_IDENTITY_TEST_DATABASE_URL")); databaseURL != "" {
		ctx := context.Background()
		pool, err := pgxpool.New(ctx, databaseURL)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(pool.Close)
		for _, statement := range []string{
			"DELETE FROM admin_role_bindings WHERE granted_by = 'repo-test'",
			"DELETE FROM admin_credentials WHERE account_id LIKE 'repo-test-%'",
			"DELETE FROM admin_accounts WHERE id LIKE 'repo-test-%'",
		} {
			if _, err = pool.Exec(ctx, statement); err != nil {
				t.Fatal(err)
			}
		}
		repos["postgres"] = NewPgAdminDomainRepository(pool)
	}
	return repos
}

func adminDomainAccount(id, username string) AdminDomainAccount {
	return AdminDomainAccount{
		ID: id, Username: username, Email: username + "@example.test",
		DisplayName: username, Status: "active",
	}
}

func TestAdminDomainAccountLifecycle(t *testing.T) {
	for name, repo := range adminDomainRepositories(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			account := adminDomainAccount("repo-test-adm-1", "repo-test-admin1")
			if err := repo.CreateAccount(ctx, account); err != nil {
				t.Fatal(err)
			}
			if err := repo.CreateAccount(ctx, adminDomainAccount("repo-test-adm-2", "repo-test-admin1")); !errors.Is(err, ErrAdminDomainAccountConflict) {
				t.Fatalf("username collision accepted: %v", err)
			}
			emailCollision := adminDomainAccount("repo-test-adm-3", "repo-test-admin3")
			emailCollision.Email = "repo-test-admin1@example.test"
			if err := repo.CreateAccount(ctx, emailCollision); !errors.Is(err, ErrAdminDomainAccountConflict) {
				t.Fatalf("email collision accepted: %v", err)
			}
			loaded, err := repo.GetAccountByUsername(ctx, "Repo-Test-Admin1")
			if err != nil || loaded.ID != "repo-test-adm-1" || loaded.Status != "active" || loaded.Version != 1 {
				t.Fatalf("loaded: %+v %v", loaded, err)
			}
			if _, err = repo.GetAccountByID(ctx, "repo-test-missing"); !errors.Is(err, ErrAdminDomainAccountNotFound) {
				t.Fatalf("missing: %v", err)
			}
			if _, ok, err := repo.SetAccountStatusCAS(ctx, "repo-test-adm-1", 99, "suspended", "reason", "tester", time.Now()); err != nil || ok {
				t.Fatalf("stale version applied: %v %v", ok, err)
			}
			updated, ok, err := repo.SetAccountStatusCAS(ctx, "repo-test-adm-1", 1, "suspended", "reason", "tester", time.Now())
			if err != nil || !ok || updated.Status != "suspended" || updated.Version != 2 {
				t.Fatalf("suspend: %+v %v %v", updated, ok, err)
			}
			if _, ok, err = repo.SetAccountStatusCAS(ctx, "repo-test-adm-1", 2, "revoked", "reason", "tester", time.Now()); err != nil || !ok {
				t.Fatalf("revoke: %v %v", ok, err)
			}
			if _, ok, err = repo.SetAccountStatusCAS(ctx, "repo-test-adm-1", 3, "active", "reason", "tester", time.Now()); err != nil || ok {
				t.Fatal("revoked account transitioned")
			}
			if err = repo.CreateAccount(ctx, adminDomainAccount("repo-test-bad id", "repo-test-bad")); !errors.Is(err, ErrAdminDomainConflict) {
				t.Fatalf("invalid shape accepted: %v", err)
			}
		})
	}
}

func TestAdminDomainCredentialCAS(t *testing.T) {
	for name, repo := range adminDomainRepositories(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			if err := repo.CreateAccount(ctx, adminDomainAccount("repo-test-adm-c1", "repo-test-adminc1")); err != nil {
				t.Fatal(err)
			}
			if _, err := repo.GetCredential(ctx, "repo-test-adm-c1"); !errors.Is(err, ErrAdminDomainCredentialMissing) {
				t.Fatalf("missing credential: %v", err)
			}
			created, ok, err := repo.SetCredentialCAS(ctx, "repo-test-adm-c1", strings.Repeat("a", 64), 0, time.Now())
			if err != nil || !ok || created.CredentialVersion != 1 {
				t.Fatalf("create: %+v %v %v", created, ok, err)
			}
			if _, ok, err = repo.SetCredentialCAS(ctx, "repo-test-adm-c1", strings.Repeat("b", 64), 0, time.Now()); err != nil || ok {
				t.Fatal("zero expected version overwrote existing credential")
			}
			rotated, ok, err := repo.SetCredentialCAS(ctx, "repo-test-adm-c1", strings.Repeat("b", 64), 1, time.Now())
			if err != nil || !ok || rotated.CredentialVersion != 2 {
				t.Fatalf("rotate: %+v %v %v", rotated, ok, err)
			}
			loaded, err := repo.GetCredential(ctx, "repo-test-adm-c1")
			if err != nil || loaded.CredentialHash != strings.Repeat("b", 64) || loaded.CredentialVersion != 2 {
				t.Fatalf("loaded: %+v %v", loaded, err)
			}
		})
	}
}

func TestAdminDomainRoleBindingsAndSecurityCount(t *testing.T) {
	for name, repo := range adminDomainRepositories(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			for _, id := range []string{"repo-test-adm-r1", "repo-test-adm-r2"} {
				if err := repo.CreateAccount(ctx, adminDomainAccount(id, strings.ToLower(id))); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := repo.GetRole(ctx, "security_admin"); err != nil {
				t.Fatal(err)
			}
			if _, err := repo.GetRole(ctx, "no_such_role"); !errors.Is(err, ErrAdminDomainRoleNotFound) {
				t.Fatalf("missing role: %v", err)
			}
			binding := AdminDomainRoleBinding{ID: "repo-test-bind-1", AccountID: "repo-test-adm-r1", RoleID: "security_admin", GrantedBy: "repo-test"}
			if err := repo.BindRole(ctx, binding); err != nil {
				t.Fatal(err)
			}
			if err := repo.BindRole(ctx, AdminDomainRoleBinding{ID: "repo-test-bind-2", AccountID: "repo-test-adm-r1", RoleID: "security_admin", GrantedBy: "repo-test"}); !errors.Is(err, ErrAdminDomainBindingConflict) {
				t.Fatalf("duplicate active binding accepted: %v", err)
			}
			if err := repo.BindRole(ctx, AdminDomainRoleBinding{ID: "repo-test-bind-3", AccountID: "repo-test-adm-r2", RoleID: "security_admin", GrantedBy: "repo-test"}); err != nil {
				t.Fatal(err)
			}
			count, err := repo.CountActiveSecurityAdmins(ctx)
			if err != nil || count != 2 {
				t.Fatalf("security admins: %d %v", count, err)
			}
			// Suspending the account drops it from the protected count.
			if _, ok, err := repo.SetAccountStatusCAS(ctx, "repo-test-adm-r2", 1, "suspended", "test", "repo-test", time.Now()); err != nil || !ok {
				t.Fatal(err)
			}
			if count, err = repo.CountActiveSecurityAdmins(ctx); err != nil || count != 1 {
				t.Fatalf("after suspend: %d %v", count, err)
			}
			revoked, ok, err := repo.RevokeBindingCAS(ctx, "repo-test-bind-1", 1, time.Now())
			if err != nil || !ok || revoked.Status != "revoked" || revoked.Version != 2 {
				t.Fatalf("revoke binding: %+v %v %v", revoked, ok, err)
			}
			if _, ok, err = repo.RevokeBindingCAS(ctx, "repo-test-bind-1", 2, time.Now()); err != nil || ok {
				t.Fatal("revoked binding transitioned again")
			}
			if count, err = repo.CountActiveSecurityAdmins(ctx); err != nil || count != 0 {
				t.Fatalf("after revoke: %d %v", count, err)
			}
			items, err := repo.ListActiveBindings(ctx, "repo-test-adm-r1")
			if err != nil || len(items) != 0 {
				t.Fatalf("active bindings: %d %v", len(items), err)
			}
		})
	}
}

func TestAdminDomainConcurrentStatusCAS(t *testing.T) {
	for name, repo := range adminDomainRepositories(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			if err := repo.CreateAccount(ctx, adminDomainAccount("repo-test-adm-x1", "repo-test-adminx1")); err != nil {
				t.Fatal(err)
			}
			var wg sync.WaitGroup
			results := make(chan bool, 8)
			for i := 0; i < 8; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					_, ok, err := repo.SetAccountStatusCAS(ctx, "repo-test-adm-x1", 1, "suspended", "race", "repo-test", time.Now())
					if err != nil {
						t.Error(err)
					}
					results <- ok
				}()
			}
			wg.Wait()
			close(results)
			wins := 0
			for ok := range results {
				if ok {
					wins++
				}
			}
			if wins != 1 {
				t.Fatalf("expected exactly one CAS winner, got %d", wins)
			}
		})
	}
}
