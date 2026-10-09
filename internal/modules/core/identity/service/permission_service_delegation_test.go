package service

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/identity/delegation"
	"github.com/campusos/CampusOS/internal/modules/core/identity/domain"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgreSQL acceptance for the replaced moderator grant path: role scopes and
// delegation grants commit in one command transaction, and the governance
// codes authorize only through current delegation facts.
func newPermissionDelegationEnv(t *testing.T) (context.Context, *PermissionService, *repository.PgDelegationRepository, *pgxpool.Pool) {
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
		"DELETE FROM identity_delegations WHERE created_by IN ('svc-test', '97001') OR subject_id IN ('97001', '97002')",
		"DELETE FROM authorization_audits WHERE actor_id IN ('97001') OR resource_id LIKE '97002%'",
		"DELETE FROM user_roles WHERE user_id IN (97001, 97002)",
		"DELETE FROM identity_admin_accounts WHERE user_id IN (97001, 97002)",
		"DELETE FROM accounts WHERE user_id IN (97001, 97002)",
		"DELETE FROM users WHERE id IN (97001, 97002)",
	} {
		if _, err = pool.Exec(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	users := repository.NewPgUserRepository(pool)
	for _, user := range []*domain.User{
		{ID: "97001", Username: "svc_test_admin", Nickname: "Svc Test Admin", Email: "svc-test-admin@example.test", Status: domain.UserStatusActive},
		{ID: "97002", Username: "svc_test_mod", Nickname: "Svc Test Mod", Email: "svc-test-mod@example.test", Status: domain.UserStatusActive},
	} {
		if err = users.Create(ctx, user); err != nil {
			t.Fatal(err)
		}
		if err = users.CreateVerifiedAccount(ctx, user.ID, user.Email, "svc-test-credential"); err != nil {
			t.Fatal(err)
		}
	}
	roles := repository.NewPgRoleRepository(pool)
	delegations := repository.NewPgDelegationRepository(pool)
	service := NewPermissionService(roles, users)
	service.SetAdminAccountRepository(repository.NewPgAdminAccountRepository(pool))
	var catalog repository.AuthorizationRepository = roles
	service.SetDelegationService(delegation.NewService(delegations, users, catalog))
	service.SetReliability(reliability.NewService(transaction.NewPostgreSQL(pool), reliability.NewPostgreSQLStore(pool)))
	if _, err = service.AssignRole(ctx, "97001", 1); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	rows := []repository.Delegation{
		{ID: "svc-test-mgmt-97001", Kind: repository.DelegationKindManagement, SubjectKind: "admin", SubjectID: "97001",
			Action: "identity.role.assign", NotBefore: now.Add(-time.Hour), ExpiresAt: now.Add(24 * time.Hour), CreatedBy: "svc-test"},
	}
	for _, board := range []string{"12", "13"} {
		for _, action := range []string{"community.thread.take_down", "community.post.delete"} {
			rows = append(rows, repository.Delegation{
				ID: "svc-test-bound-97001-" + board + "-" + action, Kind: repository.DelegationKindBound,
				SubjectKind: "admin", SubjectID: "97001", Action: action, BoardID: board,
				NotBefore: now.Add(-time.Hour), ExpiresAt: now.Add(24 * time.Hour),
				RequiredStrength: "password", Delegable: true, CreatedBy: "svc-test",
			})
		}
	}
	if err = delegations.InsertDelegations(ctx, rows); err != nil {
		t.Fatal(err)
	}
	return ctx, service, delegations, pool
}

func testBoardsProvider(context.Context) ([]identityport.BoardDelegationBoard, error) {
	return []identityport.BoardDelegationBoard{
		{Kind: "community.board", ID: "12", Status: "active"},
		{Kind: "community.board", ID: "13", Status: "active"},
	}, nil
}

func TestPostgreSQLModeratorScopeReplaceCommitsDelegationGrants(t *testing.T) {
	ctx, service, delegations, _ := newPermissionDelegationEnv(t)
	proofCtx := delegation.WithActorProof(ctx, delegation.ActorProof{AuthenticationStrength: "mfa", CredentialID: "session-97001"})
	changed, err := service.ReplaceCategoryRoleScopesByActor(proofCtx, "97001", "97002", "moderator", []int64{12}, testBoardsProvider)
	if err != nil || !changed {
		t.Fatalf("replace: %v %v", changed, err)
	}
	grants, err := delegations.ListByKindSubject(ctx, repository.DelegationKindGrant, "97002")
	if err != nil || len(grants) != 2 {
		t.Fatalf("grants: %d %v", len(grants), err)
	}
	if allowed, err := service.CheckCodeScoped(ctx, "97002", "community.thread.take_down", "category", 12); err != nil || !allowed {
		t.Fatalf("delegated governance code allowed=%v err=%v", allowed, err)
	}
	if allowed, err := service.CheckCodeScoped(ctx, "97002", "community.thread.take_down", "category", 13); err != nil || allowed {
		t.Fatalf("outside delegated board allowed=%v err=%v", allowed, err)
	}
	if allowed, err := service.CheckCodeScoped(ctx, "97002", "community.thread.pin", "category", 12); err != nil || !allowed {
		t.Fatalf("low-risk moderator code must stay on the catalog path: %v %v", allowed, err)
	}
	// The global admin keeps governance access through the catalog grant.
	if allowed, err := service.CheckCodeScoped(ctx, "97001", "community.thread.take_down", "category", 12); err != nil || !allowed {
		t.Fatalf("global admin catalog grant allowed=%v err=%v", allowed, err)
	}
	audits, err := service.ListAuthorizationAudits(ctx, 20)
	if err != nil || len(audits) != 2 {
		t.Fatalf("expected role + delegation audits, got %#v %v", audits, err)
	}
	kinds := map[string]bool{}
	for _, audit := range audits {
		kinds[audit.ActorKind] = true
	}
	if !kinds["user"] || !kinds["admin"] {
		t.Fatalf("audit actor domains incomplete: %#v", audits)
	}
}

func TestPostgreSQLModeratorScopeReplaceRevokesRemovedBoards(t *testing.T) {
	ctx, service, _, _ := newPermissionDelegationEnv(t)
	proofCtx := delegation.WithActorProof(ctx, delegation.ActorProof{AuthenticationStrength: "mfa", CredentialID: "session-97001"})
	if _, err := service.ReplaceCategoryRoleScopesByActor(proofCtx, "97001", "97002", "moderator", []int64{12, 13}, testBoardsProvider); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ReplaceCategoryRoleScopesByActor(proofCtx, "97001", "97002", "moderator", []int64{13}, testBoardsProvider); err != nil {
		t.Fatal(err)
	}
	if allowed, err := service.CheckCodeScoped(ctx, "97002", "community.thread.take_down", "category", 12); err != nil || allowed {
		t.Fatal("removed board still authorizes governance")
	}
	if allowed, err := service.CheckCodeScoped(ctx, "97002", "community.post.delete", "category", 13); err != nil || !allowed {
		t.Fatal("kept board lost governance grant")
	}
}

func TestPostgreSQLModeratorScopeReplaceFailsClosedWithoutProof(t *testing.T) {
	ctx, service, _, _ := newPermissionDelegationEnv(t)
	// No actor proof in the command context: the delegation chain must deny
	// instead of assuming an MFA administrator.
	_, err := service.ReplaceCategoryRoleScopesByActor(ctx, "97001", "97002", "moderator", []int64{12}, testBoardsProvider)
	if err == nil {
		t.Fatal("grant succeeded without an entry-verified actor proof")
	}
	var denied *delegation.DelegationDeniedError
	if !errors.As(err, &denied) && !strings.Contains(err.Error(), "delegation.") {
		t.Fatalf("unexpected error kind: %v", err)
	}
	if allowed, checkErr := service.CheckCodeScoped(ctx, "97002", "community.thread.take_down", "category", 12); checkErr != nil || allowed {
		t.Fatal("failed grant still authorizes")
	}
}
