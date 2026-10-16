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

func adminSecurityRepositories(t *testing.T) map[string]AdminSecurityRepository {
	t.Helper()
	repos := map[string]AdminSecurityRepository{"memory": NewMemoryAdminSecurityRepository()}
	if databaseURL := strings.TrimSpace(os.Getenv("CAMPUSOS_IDENTITY_TEST_DATABASE_URL")); databaseURL != "" {
		ctx := context.Background()
		pool, err := pgxpool.New(ctx, databaseURL)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(pool.Close)
		for _, statement := range []string{
			"DELETE FROM admin_recovery_tickets WHERE account_id LIKE 'repo-test-%'",
			"DELETE FROM admin_mfa_tickets WHERE account_id LIKE 'repo-test-%'",
			"DELETE FROM admin_mfa_totp WHERE account_id LIKE 'repo-test-%'",
			"DELETE FROM admin_accounts WHERE id = 'repo-test-mfa-1'",
			"INSERT INTO admin_accounts (id, username, email) VALUES ('repo-test-mfa-1', 'repo-test-mfa1', 'mfa1@b.test') ON CONFLICT (id) DO NOTHING",
		} {
			if _, err = pool.Exec(ctx, statement); err != nil {
				t.Fatal(err)
			}
		}
		repos["postgres"] = NewPgAdminSecurityRepository(pool)
	}
	return repos
}

func adminTOTPShape(accountID, status string) AdminTOTPMethod {
	return AdminTOTPMethod{
		AccountID: accountID, Status: status, KeyID: "key-1", Nonce: strings.Repeat("n", 16),
		Ciphertext: strings.Repeat("c", 32), EnrollmentExpiresAt: time.Now().Add(time.Hour),
	}
}

func TestAdminSecurityTOTPLifecycle(t *testing.T) {
	for name, repo := range adminSecurityRepositories(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			created, err := repo.UpsertTOTP(ctx, adminTOTPShape("repo-test-mfa-1", "pending"))
			if err != nil || created.Status != "pending" || created.Version != 1 {
				t.Fatalf("upsert: %+v %v", created, err)
			}
			if _, err = repo.UpsertTOTP(ctx, adminTOTPShape("repo-test-mfa-1", "active")); !errors.Is(err, ErrAdminDomainConflict) {
				t.Fatalf("active without confirmation accepted: %v", err)
			}
			if _, ok, err := repo.ConfirmTOTPCAS(ctx, "repo-test-mfa-1", 99, time.Now()); err != nil || ok {
				t.Fatalf("stale confirm: %v %v", ok, err)
			}
			confirmed, ok, err := repo.ConfirmTOTPCAS(ctx, "repo-test-mfa-1", 1, time.Now())
			if err != nil || !ok || confirmed.Status != "active" || confirmed.ConfirmedAt == nil || confirmed.Version != 2 {
				t.Fatalf("confirm: %+v %v %v", confirmed, ok, err)
			}
			if _, ok, err = repo.ConfirmTOTPCAS(ctx, "repo-test-mfa-1", 2, time.Now()); err != nil || ok {
				t.Fatal("double confirm accepted")
			}
			disabled, ok, err := repo.DisableTOTPCAS(ctx, "repo-test-mfa-1", 2, time.Now())
			if err != nil || !ok || disabled.Status != "disabled" {
				t.Fatalf("disable: %+v %v %v", disabled, ok, err)
			}
			if _, err = repo.GetTOTP(ctx, "repo-test-missing"); !errors.Is(err, ErrAdminDomainTOTPNotFound) {
				t.Fatalf("missing: %v", err)
			}
			// Rotating the envelope bumps the version under CAS.
			rotated, err := repo.UpsertTOTP(ctx, adminTOTPShape("repo-test-mfa-1", "disabled"))
			if err != nil || rotated.Version != 4 {
				t.Fatalf("envelope rotation: %+v %v", rotated, err)
			}
		})
	}
}

func adminSecurityTicketFixture(id, accountID, purpose string) AdminSecurityTicket {
	return AdminSecurityTicket{
		ID: id, AccountID: accountID, Purpose: purpose,
		Digest: strings.Repeat("b", 62) + "01", ExpiresAt: time.Now().Add(10 * time.Minute),
		MaxAttempts: 5,
	}
}

func TestAdminSecurityTicketSingleUse(t *testing.T) {
	for name, repo := range adminSecurityRepositories(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			ticket := adminSecurityTicketFixture("repo-test-t-1", "repo-test-mfa-1", "step_up")
			if err := repo.CreateAdminTicket(ctx, ticket); err != nil {
				t.Fatal(err)
			}
			if err := repo.CreateAdminTicket(ctx, ticket); !errors.Is(err, ErrAdminDomainTicketConflict) {
				t.Fatalf("digest collision accepted: %v", err)
			}
			loaded, err := repo.GetAdminTicketForUpdate(ctx, ticket.Digest)
			if err != nil || loaded.Attempts != 0 {
				t.Fatalf("loaded: %+v %v", loaded, err)
			}
			bumped, err := repo.IncrementAdminTicketAttempts(ctx, ticket.ID, time.Now())
			if err != nil || bumped.Attempts != 1 {
				t.Fatalf("attempts: %+v %v", bumped, err)
			}
			consumed, ok, err := repo.ConsumeAdminTicket(ctx, ticket.ID, time.Now())
			if err != nil || !ok || consumed.ConsumedAt == nil {
				t.Fatalf("consume: %+v %v %v", consumed, ok, err)
			}
			if _, ok, err = repo.ConsumeAdminTicket(ctx, ticket.ID, time.Now()); err != nil || ok {
				t.Fatal("double consume accepted")
			}
			if _, err = repo.GetAdminTicketForUpdate(ctx, "missing"); !errors.Is(err, ErrAdminDomainTicketNotFound) {
				t.Fatalf("missing: %v", err)
			}
		})
	}
}

func TestAdminRecoveryTicketSingleUse(t *testing.T) {
	for name, repo := range adminSecurityRepositories(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			ticket := adminSecurityTicketFixture("repo-test-r-1", "repo-test-mfa-1", "password_reset")
			if err := repo.CreateRecoveryTicket(ctx, ticket); err != nil {
				t.Fatal(err)
			}
			loaded, err := repo.GetRecoveryTicketForUpdate(ctx, ticket.Digest)
			if err != nil || loaded.Purpose != "password_reset" {
				t.Fatalf("loaded: %+v %v", loaded, err)
			}
			if _, err = repo.IncrementRecoveryAttempts(ctx, ticket.ID, time.Now()); err != nil {
				t.Fatal(err)
			}
			if _, ok, err := repo.ConsumeRecoveryTicket(ctx, ticket.ID, time.Now()); err != nil || !ok {
				t.Fatalf("consume: %v %v", ok, err)
			}
			if _, ok, err := repo.ConsumeRecoveryTicket(ctx, ticket.ID, time.Now()); err != nil || ok {
				t.Fatal("double consume accepted")
			}
		})
	}
}

func TestAdminSecurityTicketDigestShape(t *testing.T) {
	for name, repo := range adminSecurityRepositories(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			bad := adminSecurityTicketFixture("repo-test-bad-1", "repo-test-mfa-1", "login")
			bad.Digest = "not-hex"
			if err := repo.CreateAdminTicket(ctx, bad); !errors.Is(err, ErrAdminDomainConflict) {
				t.Fatalf("bad digest accepted: %v", err)
			}
			bad.Digest = strings.Repeat("a", 64)
			bad.MaxAttempts = 0
			if err := repo.CreateRecoveryTicket(ctx, bad); !errors.Is(err, ErrAdminDomainConflict) {
				t.Fatalf("bad attempts accepted: %v", err)
			}
		})
	}
}

func TestAdminSecurityConcurrentConsume(t *testing.T) {
	for name, repo := range adminSecurityRepositories(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			ticket := adminSecurityTicketFixture("repo-test-c-1", "repo-test-mfa-1", "step_up")
			if err := repo.CreateAdminTicket(ctx, ticket); err != nil {
				t.Fatal(err)
			}
			var wg sync.WaitGroup
			results := make(chan bool, 8)
			for i := 0; i < 8; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					_, ok, err := repo.ConsumeAdminTicket(ctx, ticket.ID, time.Now())
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
				t.Fatalf("expected exactly one consume winner, got %d", wins)
			}
		})
	}
}
