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

func delegationBound(id, adminID, action, boardID string) Delegation {
	return Delegation{
		ID: id, Kind: DelegationKindBound, SubjectKind: "admin", SubjectID: adminID,
		Action: action, BoardID: boardID,
		NotBefore: time.Unix(1000, 0), ExpiresAt: time.Unix(2000, 0),
		RequiredStrength: "password", Delegable: true,
	}
}

func delegationGrant(id, userID, action, boardID string) Delegation {
	return Delegation{
		ID: id, Kind: DelegationKindGrant, SubjectKind: "user", SubjectID: userID,
		Action: action, BoardID: boardID,
		NotBefore: time.Unix(1000, 0), ExpiresAt: time.Unix(2000, 0),
		RequiredStrength: "password",
	}
}

func delegationRepositories(t *testing.T) map[string]DelegationRepository {
	t.Helper()
	repos := map[string]DelegationRepository{"memory": NewMemoryDelegationRepository()}
	if databaseURL := strings.TrimSpace(os.Getenv("CAMPUSOS_IDENTITY_TEST_DATABASE_URL")); databaseURL != "" {
		ctx := context.Background()
		pool, err := pgxpool.New(ctx, databaseURL)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(pool.Close)
		if _, err = pool.Exec(ctx, "DELETE FROM identity_delegations WHERE created_by LIKE 'repo-test%'"); err != nil {
			t.Fatal(err)
		}
		repos["postgres"] = NewPgDelegationRepository(pool)
	}
	return repos
}

func TestDelegationRepositoryRoundTrip(t *testing.T) {
	for name, repo := range delegationRepositories(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			seed := []Delegation{
				{ID: "repo-test-mgmt-1", Kind: DelegationKindManagement, SubjectKind: "admin", SubjectID: "admin-1",
					Action: "identity.role.assign", NotBefore: time.Unix(900, 0), ExpiresAt: time.Unix(5000, 0), CreatedBy: "repo-test"},
				delegationBound("repo-test-bound-1", "admin-1", "community.thread.take_down", "42"),
				delegationBound("repo-test-bound-2", "admin-1", "community.post.delete", "42"),
				delegationGrant("repo-test-grant-1", "user-1", "community.thread.take_down", "42"),
				delegationGrant("repo-test-grant-2", "user-1", "community.post.delete", "42"),
				delegationGrant("repo-test-grant-3", "user-2", "community.thread.take_down", "43"),
			}
			for i := range seed {
				seed[i].CreatedBy = "repo-test"
			}
			if err := repo.InsertDelegations(ctx, seed); err != nil {
				t.Fatal(err)
			}
			authority, err := repo.ListByKindSubject(ctx, DelegationKindBound, "admin-1")
			if err != nil || len(authority) != 2 {
				t.Fatalf("admin bounds: %d %v", len(authority), err)
			}
			grants, err := repo.ListByKindSubject(ctx, DelegationKindGrant, "user-1")
			if err != nil || len(grants) != 2 {
				t.Fatalf("user grants: %d %v", len(grants), err)
			}
			loaded, err := repo.GetDelegation(ctx, "repo-test-grant-1")
			if err != nil || loaded.Status != DelegationStatusActive || loaded.Version != 1 || loaded.RequiredStrength != "password" {
				t.Fatalf("loaded: %+v %v", loaded, err)
			}
			if _, err = repo.GetDelegation(ctx, "repo-test-missing"); !errors.Is(err, ErrDelegationNotFound) {
				t.Fatalf("missing: %v", err)
			}
			// Identifiers survive the round trip exactly, including dotted actions.
			if loaded.Action != "community.thread.take_down" || loaded.BoardID != "42" {
				t.Fatalf("round trip changed identifiers: %+v", loaded)
			}
		})
	}
}

func TestDelegationRepositoryRejectsInvalidShape(t *testing.T) {
	for name, repo := range delegationRepositories(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			for _, item := range []Delegation{
				{ID: "repo-test-bad-kind", Kind: "grant", SubjectKind: "admin", SubjectID: "admin-1", Action: "community.thread.take_down", BoardID: "42", NotBefore: time.Unix(1, 0), ExpiresAt: time.Unix(2, 0), RequiredStrength: "mfa"},
				{ID: "repo-test-bad-window", Kind: DelegationKindGrant, SubjectKind: "user", SubjectID: "user-9", Action: "community.thread.take_down", BoardID: "42", NotBefore: time.Unix(2, 0), ExpiresAt: time.Unix(2, 0), RequiredStrength: "mfa"},
				{ID: "repo-test-bad-action", Kind: DelegationKindGrant, SubjectKind: "user", SubjectID: "user-9", Action: "community.thread.pin", BoardID: "42", NotBefore: time.Unix(1, 0), ExpiresAt: time.Unix(2, 0), RequiredStrength: "mfa"},
				{ID: "repo-test-bad-mgmt", Kind: DelegationKindManagement, SubjectKind: "admin", SubjectID: "admin-1", Action: "community.thread.take_down", NotBefore: time.Unix(1, 0), ExpiresAt: time.Unix(2, 0)},
				{ID: "repo-test-bad-delegable", Kind: DelegationKindGrant, SubjectKind: "user", SubjectID: "user-9", Action: "community.post.delete", BoardID: "42", NotBefore: time.Unix(1, 0), ExpiresAt: time.Unix(2, 0), RequiredStrength: "mfa", Delegable: true},
				{ID: "repo-test-bad id", Kind: DelegationKindGrant, SubjectKind: "user", SubjectID: "user-9", Action: "community.post.delete", BoardID: "42", NotBefore: time.Unix(1, 0), ExpiresAt: time.Unix(2, 0), RequiredStrength: "mfa"},
			} {
				item.CreatedBy = "repo-test"
				if err := repo.InsertDelegations(ctx, []Delegation{item}); !errors.Is(err, ErrDelegationInvalidShape) {
					t.Fatalf("%s accepted: %v", item.ID, err)
				}
			}
		})
	}
}

func TestDelegationRepositoryStatusCAS(t *testing.T) {
	for name, repo := range delegationRepositories(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			grant := delegationGrant("repo-test-cas-1", "user-7", "community.thread.take_down", "42")
			grant.CreatedBy = "repo-test"
			if err := repo.InsertDelegations(ctx, []Delegation{grant}); err != nil {
				t.Fatal(err)
			}
			// Stale version loses the race.
			if _, ok, err := repo.SetStatusCAS(ctx, "repo-test-cas-1", 99, DelegationStatusSuspended, time.Unix(1500, 0)); err != nil || ok {
				t.Fatalf("stale version applied: %v %v", ok, err)
			}
			updated, ok, err := repo.SetStatusCAS(ctx, "repo-test-cas-1", 1, DelegationStatusSuspended, time.Unix(1500, 0))
			if err != nil || !ok || updated.Status != DelegationStatusSuspended || updated.Version != 2 {
				t.Fatalf("suspend: %+v %v %v", updated, ok, err)
			}
			if updated, ok, err = repo.SetStatusCAS(ctx, "repo-test-cas-1", 2, DelegationStatusActive, time.Unix(1501, 0)); err != nil || !ok || updated.Status != DelegationStatusActive {
				t.Fatalf("resume: %+v %v %v", updated, ok, err)
			}
			if updated, ok, err = repo.SetStatusCAS(ctx, "repo-test-cas-1", 3, DelegationStatusRevoked, time.Unix(1502, 0)); err != nil || !ok || updated.Status != DelegationStatusRevoked {
				t.Fatalf("revoke: %+v %v %v", updated, ok, err)
			}
			// Revoked is terminal.
			if _, ok, err = repo.SetStatusCAS(ctx, "repo-test-cas-1", 4, DelegationStatusActive, time.Unix(1503, 0)); !errors.Is(err, ErrDelegationRevokedPermanent) || ok {
				t.Fatalf("revoked row transitioned: %v %v", ok, err)
			}
			if _, ok, err = repo.SetStatusCAS(ctx, "repo-test-cas-1", 4, DelegationStatusRevoked, time.Unix(1504, 0)); !errors.Is(err, ErrDelegationRevokedPermanent) || ok {
				t.Fatalf("revoked row re-revoked: %v %v", ok, err)
			}
			if _, ok, err = repo.SetStatusCAS(ctx, "repo-test-missing", 1, DelegationStatusRevoked, time.Unix(1500, 0)); !errors.Is(err, ErrDelegationNotFound) || ok {
				t.Fatalf("missing row: %v %v", ok, err)
			}
			// Duplicate IDs conflict.
			if err = repo.InsertDelegations(ctx, []Delegation{grant}); !errors.Is(err, ErrDelegationVersionConflict) && err == nil {
				t.Fatal("duplicate insert accepted")
			}
		})
	}
}

func TestDelegationRepositoryConcurrentCAS(t *testing.T) {
	for name, repo := range delegationRepositories(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			grant := delegationGrant("repo-test-race-1", "user-8", "community.post.delete", "7")
			grant.CreatedBy = "repo-test"
			if err := repo.InsertDelegations(ctx, []Delegation{grant}); err != nil {
				t.Fatal(err)
			}
			var wg sync.WaitGroup
			wins := 0
			results := make(chan bool, 8)
			for i := 0; i < 8; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					_, ok, err := repo.SetStatusCAS(ctx, "repo-test-race-1", 1, DelegationStatusRevoked, time.Unix(1600, 0))
					// Losers either miss the version race or arrive after the
					// terminal revocation commits; both are closed failures.
					if err != nil && !errors.Is(err, ErrDelegationRevokedPermanent) {
						t.Error(err)
					}
					results <- ok
				}()
			}
			wg.Wait()
			close(results)
			for ok := range results {
				if ok {
					wins++
				}
			}
			if wins != 1 {
				t.Fatalf("expected exactly one CAS winner, got %d", wins)
			}
			loaded, err := repo.GetDelegation(ctx, "repo-test-race-1")
			if err != nil || loaded.Status != DelegationStatusRevoked || loaded.Version != 2 {
				t.Fatalf("final: %+v %v", loaded, err)
			}
		})
	}
}
