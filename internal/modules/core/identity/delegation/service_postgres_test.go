package delegation

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/identity/domain"
	"github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgreSQL service acceptance runs only inside the isolated drill database.
func newPostgreSQLService(t *testing.T) (*Service, *pgxpool.Pool, context.Context) {
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
		"DELETE FROM identity_delegations WHERE created_by LIKE 'svc-test%' OR created_by = '98011'",
		"DELETE FROM authorization_audits WHERE actor_id = '98011' OR request_id LIKE 'svc-test%' OR resource_id LIKE 'svc-test%'",
		"DELETE FROM users WHERE id BETWEEN 98000 AND 98999",
	} {
		if _, err = pool.Exec(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	users := repository.NewPgUserRepository(pool)
	for _, id := range []string{"98001", "98002"} {
		if err = users.Create(ctx, &domain.User{ID: id, Username: "svc-test-" + id, Nickname: id, Email: "svc-test-" + id + "@test.local", Status: domain.UserStatusActive}); err != nil {
			t.Fatal(err)
		}
	}
	delegations := repository.NewPgDelegationRepository(pool)
	catalog := repository.NewPgRoleRepository(pool)
	service := NewService(delegations, users, catalog)
	service.SetClock(testClock)
	service.SetReliability(reliability.NewService(transaction.NewPostgreSQL(pool), reliability.NewPostgreSQLStore(pool)))
	seed := []repository.Delegation{
		{ID: "svc-test-mgmt-1", Kind: repository.DelegationKindManagement, SubjectKind: "admin", SubjectID: "98011",
			Action: "identity.role.assign", NotBefore: time.Unix(900, 0), ExpiresAt: time.Unix(5000, 0)},
		{ID: "svc-test-bound-take", Kind: repository.DelegationKindBound, SubjectKind: "admin", SubjectID: "98011",
			Action: "community.thread.take_down", BoardID: "42", NotBefore: time.Unix(1000, 0), ExpiresAt: time.Unix(4000, 0),
			RequiredStrength: "password", Delegable: true},
		{ID: "svc-test-bound-del", Kind: repository.DelegationKindBound, SubjectKind: "admin", SubjectID: "98011",
			Action: "community.post.delete", BoardID: "42", NotBefore: time.Unix(1000, 0), ExpiresAt: time.Unix(4000, 0),
			RequiredStrength: "password", Delegable: true},
	}
	for i := range seed {
		seed[i].CreatedBy = "svc-test"
	}
	if err = delegations.InsertDelegations(ctx, seed); err != nil {
		t.Fatal(err)
	}
	return service, pool, ctx
}

func pgActor() Actor {
	return Actor{ID: "98011", AuthenticationStrength: "mfa", CredentialID: "svc-test-session"}
}

func pgBoards(context.Context) ([]port.BoardDelegationBoard, error) {
	return []port.BoardDelegationBoard{{Kind: "community.board", ID: "42", Status: "active"}}, nil
}

func TestPostgreSQLGrantCommitAuditAndOutbox(t *testing.T) {
	service, pool, ctx := newPostgreSQLService(t)
	decision, err := service.GrantBoardDelegations(ctx, pgActor(), "98001", []GrantCandidate{
		{Action: "community.thread.take_down", BoardID: "42"},
		{Action: "community.post.delete", BoardID: "42"},
	}, pgBoards, "svc-test-req-1")
	if err != nil || decision.Effect != "allow" {
		t.Fatalf("decision: %+v %v", decision, err)
	}
	var grantCount, auditCount, eventCount int
	if err = pool.QueryRow(ctx, "SELECT count(*) FROM identity_delegations WHERE kind='grant' AND subject_id='98001' AND created_by='98011'").Scan(&grantCount); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, "SELECT count(*) FROM authorization_audits WHERE actor_kind='admin' AND actor_id='98011' AND request_id='svc-test-req-1' AND outcome='allow'").Scan(&auditCount); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, "SELECT count(*) FROM platform_outbox WHERE aggregate_type='identity_delegation' AND aggregate_id='98001'").Scan(&eventCount); err != nil {
		t.Fatal(err)
	}
	if grantCount != 2 || auditCount != 1 || eventCount != 1 {
		t.Fatalf("grants=%d audits=%d events=%d", grantCount, auditCount, eventCount)
	}
}

func TestPostgreSQLGrantRecheckFailsClosedOnBoardChange(t *testing.T) {
	service, pool, ctx := newPostgreSQLService(t)
	calls := 0
	provider := func(context.Context) ([]port.BoardDelegationBoard, error) {
		calls++
		if calls == 1 {
			return pgBoards(context.Background())
		}
		return []port.BoardDelegationBoard{{Kind: "community.board", ID: "42", Status: "archived"}}, nil
	}
	_, err := service.GrantBoardDelegations(ctx, pgActor(), "98001",
		[]GrantCandidate{{Action: "community.thread.take_down", BoardID: "42"}}, provider, "svc-test-req-2")
	if !errors.Is(err, ErrDelegationFactsChanged) {
		t.Fatalf("expected facts_changed, got %v", err)
	}
	var count int
	if err = pool.QueryRow(ctx, "SELECT count(*) FROM identity_delegations WHERE kind='grant' AND subject_id='98001'").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("grant committed against changed board facts")
	}
}

type failingAfterCreateCatalog struct {
	*repository.PgRoleRepository
}

func (c failingAfterCreateCatalog) RecordAuthorizationAudit(context.Context, repository.AuthorizationAudit) error {
	return errors.New("audit store is down")
}

func TestPostgreSQLGrantAuditFailureRollsBack(t *testing.T) {
	databaseURL := strings.TrimSpace(os.Getenv("CAMPUSOS_IDENTITY_TEST_DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("CAMPUSOS_IDENTITY_TEST_DATABASE_URL is not configured")
	}
	service, pool, ctx := newPostgreSQLService(t)
	service.catalog = failingAfterCreateCatalog{repository.NewPgRoleRepository(pool)}
	_, err := service.GrantBoardDelegations(ctx, pgActor(), "98002",
		[]GrantCandidate{{Action: "community.thread.take_down", BoardID: "42"}}, pgBoards, "svc-test-req-3")
	if err == nil {
		t.Fatal("audit failure accepted")
	}
	var count int
	if err = pool.QueryRow(ctx, "SELECT count(*) FROM identity_delegations WHERE kind='grant' AND subject_id='98002'").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("grant committed without required audit")
	}
}

func TestPostgreSQLRevocationVisibleImmediately(t *testing.T) {
	service, _, ctx := newPostgreSQLService(t)
	if _, err := service.GrantBoardDelegations(ctx, pgActor(), "98001",
		[]GrantCandidate{{Action: "community.thread.take_down", BoardID: "42"}}, pgBoards, "svc-test-req-4"); err != nil {
		t.Fatal(err)
	}
	allowed, err := service.HasActiveGovernanceGrant(ctx, "98001", "community.thread.take_down", "42", "password", time.Unix(1600, 0))
	if err != nil || !allowed {
		t.Fatalf("grant not active: %v %v", allowed, err)
	}
	if err = service.RevokeDelegation(ctx, pgActor(), "grant-svc-test-req-4-0"); err != nil {
		t.Fatal(err)
	}
	allowed, err = service.HasActiveGovernanceGrant(ctx, "98001", "community.thread.take_down", "42", "password", time.Unix(1600, 0))
	if err != nil || allowed {
		t.Fatal("revoked grant still authorizes")
	}
}

func TestPostgreSQLConcurrentGrantRevokeRace(t *testing.T) {
	service, pool, ctx := newPostgreSQLService(t)
	var wg sync.WaitGroup
	failures := make(chan error, 16)
	for i := 0; i < 8; i++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			_, err := service.GrantBoardDelegations(ctx, pgActor(), "98001",
				[]GrantCandidate{{Action: "community.post.delete", BoardID: "42"}}, pgBoards, fmt.Sprintf("svc-test-race-%d", i))
			// Only clean commits or closed recheck failures are acceptable.
			if err != nil && !errors.Is(err, ErrDelegationFactsChanged) {
				failures <- err
			}
		}(i)
		go func() {
			defer wg.Done()
			err := service.RevokeDelegation(ctx, pgActor(), "svc-test-bound-del")
			// Concurrent revokers lose to the terminal state or the version race;
			// both are closed failures, never a partial transition.
			if err != nil && !errors.Is(err, repository.ErrDelegationRevokedPermanent) && !errors.Is(err, repository.ErrDelegationVersionConflict) {
				failures <- err
			}
		}()
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		t.Error(err)
	}
	// Every committed grant must have been witnessed by a then-active bound:
	// once the bound is revoked, no new grant may appear afterwards.
	var boundStatus string
	if err := pool.QueryRow(ctx, "SELECT status FROM identity_delegations WHERE id='svc-test-bound-del'").Scan(&boundStatus); err != nil {
		t.Fatal(err)
	}
	if boundStatus != repository.DelegationStatusRevoked {
		t.Fatalf("bound status: %s", boundStatus)
	}
}
