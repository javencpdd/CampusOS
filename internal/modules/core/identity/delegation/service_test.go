package delegation

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/identity/domain"
	"github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
)

var testClock = func() time.Time { return time.Unix(1500, 0) }

type failingCatalog struct{}

func (failingCatalog) ListPermissionDefinitions(context.Context) ([]repository.PermissionDefinition, error) {
	return nil, errors.New("unimplemented")
}
func (failingCatalog) ListRolePermissions(context.Context, int64) ([]repository.RolePermission, error) {
	return nil, errors.New("unimplemented")
}
func (failingCatalog) ReplaceRolePermissions(context.Context, int64, []string, string) error {
	return errors.New("unimplemented")
}
func (failingCatalog) CreateCustomRole(context.Context, repository.Role) (*repository.Role, error) {
	return nil, errors.New("unimplemented")
}
func (failingCatalog) UpdateCustomRole(context.Context, repository.Role) (*repository.Role, error) {
	return nil, errors.New("unimplemented")
}
func (failingCatalog) HasPermissionCode(context.Context, string, string) (bool, error) {
	return false, errors.New("unimplemented")
}
func (failingCatalog) HasScopedPermissionCode(context.Context, string, string, string, int64) (bool, error) {
	return false, errors.New("unimplemented")
}
func (failingCatalog) HasAnyScopedPermissionCode(context.Context, string, string, string) (bool, error) {
	return false, errors.New("unimplemented")
}
func (failingCatalog) SyncRouteOperations(context.Context, []repository.RouteOperation) error {
	return errors.New("unimplemented")
}
func (failingCatalog) ListRouteOperations(context.Context) ([]repository.RouteOperation, error) {
	return nil, errors.New("unimplemented")
}
func (failingCatalog) RecordAuthorizationAudit(context.Context, repository.AuthorizationAudit) error {
	return errors.New("audit store is down")
}
func (failingCatalog) ListAuthorizationAudits(context.Context, int) ([]repository.AuthorizationAudit, error) {
	return nil, errors.New("unimplemented")
}
func (failingCatalog) CountGlobalRoleAssignments(context.Context, int64) (int, error) {
	return 0, errors.New("unimplemented")
}

type testEnv struct {
	service     *Service
	delegations repository.DelegationRepository
	users       *repository.MemoryUserRepository
	catalog     repository.AuthorizationRepository
}

func seedDelegationAuthority(t *testing.T, ctx context.Context, repo repository.DelegationRepository) {
	t.Helper()
	rows := []repository.Delegation{
		{ID: "mgmt-admin-1", Kind: repository.DelegationKindManagement, SubjectKind: "admin", SubjectID: "admin-1",
			Action: "identity.role.assign", NotBefore: time.Unix(900, 0), ExpiresAt: time.Unix(5000, 0)},
		{ID: "bound-take-42", Kind: repository.DelegationKindBound, SubjectKind: "admin", SubjectID: "admin-1",
			Action: "community.thread.take_down", BoardID: "42", NotBefore: time.Unix(1000, 0), ExpiresAt: time.Unix(4000, 0),
			RequiredStrength: "password", Delegable: true},
		{ID: "bound-del-42", Kind: repository.DelegationKindBound, SubjectKind: "admin", SubjectID: "admin-1",
			Action: "community.post.delete", BoardID: "42", NotBefore: time.Unix(1000, 0), ExpiresAt: time.Unix(4000, 0),
			RequiredStrength: "password", Delegable: true},
		{ID: "bound-take-43-other", Kind: repository.DelegationKindBound, SubjectKind: "admin", SubjectID: "admin-2",
			Action: "community.thread.take_down", BoardID: "43", NotBefore: time.Unix(1000, 0), ExpiresAt: time.Unix(4000, 0),
			RequiredStrength: "password", Delegable: true},
	}
	for i := range rows {
		rows[i].CreatedBy = "test"
	}
	if err := repo.InsertDelegations(ctx, rows); err != nil {
		t.Fatal(err)
	}
}

func testBoards(context.Context) ([]port.BoardDelegationBoard, error) {
	return []port.BoardDelegationBoard{
		{Kind: "community.board", ID: "42", Status: "active"},
		{Kind: "community.board", ID: "43", Status: "active"},
	}, nil
}

func adminActor() Actor {
	return Actor{ID: "admin-1", AuthenticationStrength: "mfa", CredentialID: "session-1"}
}

func newMemoryService(t *testing.T, withReliability bool) testEnv {
	t.Helper()
	ctx := context.Background()
	delegations := repository.NewMemoryDelegationRepository()
	users := repository.NewMemoryUserRepository()
	catalog := repository.NewMemoryRoleRepository()
	for _, u := range []struct{ id, name string }{{"user-1", "user-one"}, {"user-2", "user-two"}} {
		if err := users.Create(ctx, &domain.User{ID: u.id, Username: u.name, Nickname: u.name, Email: u.name + "@test.local", Status: domain.UserStatusActive}); err != nil {
			t.Fatal(err)
		}
	}
	seedDelegationAuthority(t, ctx, delegations)
	service := NewService(delegations, users, catalog)
	service.SetClock(testClock)
	if withReliability {
		service.SetReliability(reliability.NewService(transaction.NewMemory(), reliability.NewMemoryStore()))
	}
	return testEnv{service: service, delegations: delegations, users: users, catalog: catalog}
}

func TestGrantBoardDelegationsCommitsAtomically(t *testing.T) {
	for _, reliable := range []bool{false, true} {
		env := newMemoryService(t, reliable)
		ctx := context.Background()
		decision, err := env.service.GrantBoardDelegations(ctx, adminActor(), "user-1", []GrantCandidate{
			{Action: "community.thread.take_down", BoardID: "42"},
			{Action: "community.post.delete", BoardID: "42"},
		}, testBoards, "req-commit-1")
		if err != nil || decision.Effect != "allow" || decision.Reason != "delegation.allowed" {
			t.Fatalf("reliable=%v decision: %+v %v", reliable, decision, err)
		}
		grants, err := env.service.ListActiveGrants(ctx, "user-1")
		if err != nil || len(grants) != 2 {
			t.Fatalf("grants: %d %v", len(grants), err)
		}
		// Host default expiry clamps to the covering bound window end.
		for _, g := range grants {
			if g.ExpiresAt.Unix() != 4000 || g.NotBefore.Unix() != 1500 || g.RequiredStrength != "password" {
				t.Fatalf("default window: %+v", g)
			}
		}
		audits, err := env.catalog.ListAuthorizationAudits(ctx, 10)
		if err != nil || len(audits) != 1 || audits[0].ActorKind != "admin" || audits[0].Outcome != "allow" {
			t.Fatalf("audit: %+v %v", audits, err)
		}
		// Witnesses bind the covering bounds in candidate order.
		if len(decision.Witnesses) != 2 || decision.Witnesses[0].BoundID != "bound-take-42" || decision.Witnesses[1].BoundID != "bound-del-42" {
			t.Fatalf("witnesses: %+v", decision.Witnesses)
		}
	}
}

func TestGrantBoardDelegationsDenies(t *testing.T) {
	cases := []struct {
		name  string
		actor Actor
		user  string
		edit  func(env testEnv)
		want  string
	}{
		{"no-management", Actor{ID: "admin-9", AuthenticationStrength: "mfa", CredentialID: "s-9"}, "user-1", nil, "delegation.management_denied"},
		{"password-actor", Actor{ID: "admin-1", AuthenticationStrength: "password", CredentialID: "s-1"}, "user-1", nil, "delegation.mfa_required"},
		{"recipient-suspended", adminActor(), "user-1", func(env testEnv) {
			u, _ := env.users.GetByID(context.Background(), "user-1")
			u.Status = domain.UserStatusSuspended
			if err := env.users.Update(context.Background(), u); err != nil {
				panic(err)
			}
		}, "delegation.recipient_denied"},
		{"board-unknown", adminActor(), "user-1", nil, "delegation.board_denied"},
		{"outside-bounds", adminActor(), "user-1", nil, "delegation.outside_bounds"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			env := newMemoryService(t, false)
			if c.edit != nil {
				c.edit(env)
			}
			boards := testBoards
			boardID := "42"
			action := "community.thread.take_down"
			if c.name == "board-unknown" {
				boardID = "99"
			}
			if c.name == "outside-bounds" {
				action = "community.post.delete"
				boardID = "43" // bound for 43 belongs to admin-2
			}
			decision, err := env.service.GrantBoardDelegations(context.Background(), c.actor, c.user,
				[]GrantCandidate{{Action: action, BoardID: boardID}}, boards, "req-"+c.name)
			if err != nil || decision.Effect != "deny" || decision.Reason != c.want {
				t.Fatalf("want %s got %+v %v", c.want, decision, err)
			}
			grants, _ := env.service.ListActiveGrants(context.Background(), c.user)
			if len(grants) != 0 {
				t.Fatal("deny wrote grants")
			}
		})
	}
}

// revokingDelegationRepository revokes the bound after the first authority
// read, modeling a revocation that commits between evaluation and the write.
type revokingDelegationRepository struct {
	repository.DelegationRepository
	reads  int
	revoke string
	now    time.Time
}

func (r *revokingDelegationRepository) ListByKindSubject(ctx context.Context, kind, subjectID string) ([]repository.Delegation, error) {
	r.reads++
	// The first buildRequest reads management and bounds (reads 1-2); revoke
	// when the in-transaction reload begins (read 3), so the original
	// evaluation was clean and only the recheck observes the revocation.
	if r.reads == 3 {
		current, err := r.GetDelegation(ctx, r.revoke)
		if err != nil {
			return nil, err
		}
		if _, _, err = r.SetStatusCAS(ctx, r.revoke, current.Version, repository.DelegationStatusRevoked, r.now); err != nil {
			return nil, err
		}
	}
	return r.DelegationRepository.ListByKindSubject(ctx, kind, subjectID)
}

func TestGrantBoardDelegationsRevocationRaceFailsClosed(t *testing.T) {
	env := newMemoryService(t, false)
	racy := &revokingDelegationRepository{DelegationRepository: env.delegations, revoke: "bound-take-42", now: time.Unix(1501, 0)}
	service := NewService(racy, env.users, env.catalog)
	service.SetClock(testClock)
	decision, err := service.GrantBoardDelegations(context.Background(), adminActor(), "user-1",
		[]GrantCandidate{{Action: "community.thread.take_down", BoardID: "42"}}, testBoards, "req-race-1")
	if decision.Effect == "allow" || !errors.Is(err, ErrDelegationFactsChanged) {
		t.Fatalf("race outcome: %+v %v", decision, err)
	}
	grants, _ := service.ListActiveGrants(context.Background(), "user-1")
	if len(grants) != 0 {
		t.Fatal("racing grant committed")
	}
}

func TestGrantBoardDelegationsAuditFailureRollsBack(t *testing.T) {
	env := newMemoryService(t, false)
	service := NewService(env.delegations, env.users, failingCatalog{})
	service.SetClock(testClock)
	service.SetReliability(reliability.NewService(transaction.NewMemory(), reliability.NewMemoryStore()))
	_, err := service.GrantBoardDelegations(context.Background(), adminActor(), "user-1",
		[]GrantCandidate{{Action: "community.thread.take_down", BoardID: "42"}}, testBoards, "req-audit-fail")
	if err == nil {
		t.Fatal("audit failure accepted")
	}
	grants, _ := service.ListActiveGrants(context.Background(), "user-1")
	if len(grants) != 0 {
		t.Fatal("grant committed without required audit")
	}
}

func TestDelegationBoundAndRevocationAdministration(t *testing.T) {
	env := newMemoryService(t, false)
	ctx := context.Background()
	bound, err := env.service.CreateDelegationBound(ctx, adminActor(), repository.Delegation{
		ID: "bound-new-44", Action: "community.thread.take_down", BoardID: "44",
		NotBefore: time.Unix(1400, 0), ExpiresAt: time.Unix(5000, 0), RequiredStrength: "mfa",
	})
	if err != nil || bound.Status != repository.DelegationStatusActive || !bound.Delegable {
		t.Fatalf("create bound: %+v %v", bound, err)
	}
	if _, err = env.service.CreateDelegationBound(ctx, Actor{ID: "admin-1", AuthenticationStrength: "password", CredentialID: "s-1"}, repository.Delegation{
		ID: "bound-no-mfa", Action: "community.thread.take_down", BoardID: "45",
		NotBefore: time.Unix(1400, 0), ExpiresAt: time.Unix(5000, 0), RequiredStrength: "mfa",
	}); err == nil || err.Error() != "delegation.mfa_required" {
		t.Fatalf("bound without mfa: %v", err)
	}
	if _, err = env.service.CreateDelegationBound(ctx, Actor{ID: "admin-9", AuthenticationStrength: "mfa", CredentialID: "s-9"}, repository.Delegation{
		ID: "bound-no-mgmt", Action: "community.thread.take_down", BoardID: "45",
		NotBefore: time.Unix(1400, 0), ExpiresAt: time.Unix(5000, 0), RequiredStrength: "mfa",
	}); err == nil || err.Error() != "delegation.management_denied" {
		t.Fatalf("bound without management: %v", err)
	}
	if err = env.service.RevokeDelegation(ctx, adminActor(), "bound-new-44"); err != nil {
		t.Fatal(err)
	}
	if err = env.service.RevokeDelegation(ctx, adminActor(), "bound-new-44"); !errors.Is(err, repository.ErrDelegationRevokedPermanent) {
		t.Fatalf("second revoke: %v", err)
	}
	if err = env.service.RevokeDelegation(ctx, adminActor(), "bound-missing"); !errors.Is(err, repository.ErrDelegationNotFound) {
		t.Fatalf("missing revoke: %v", err)
	}
}

func TestHasActiveGovernanceGrant(t *testing.T) {
	env := newMemoryService(t, false)
	ctx := context.Background()
	if _, err := env.service.GrantBoardDelegations(ctx, adminActor(), "user-1",
		[]GrantCandidate{{Action: "community.thread.take_down", BoardID: "42"}}, testBoards, "req-read-1"); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name     string
		action   string
		board    string
		strength string
		at       time.Time
		allowed  bool
	}{
		{"current-password", "community.thread.take_down", "42", "password", time.Unix(1600, 0), true},
		{"wrong-board", "community.thread.take_down", "43", "password", time.Unix(1600, 0), false},
		{"wrong-action", "community.post.delete", "42", "password", time.Unix(1600, 0), false},
		{"before-window", "community.thread.take_down", "42", "password", time.Unix(1499, 0), false},
		{"after-expiry", "community.thread.take_down", "42", "password", time.Unix(4000, 0), false},
	} {
		t.Run(c.name, func(t *testing.T) {
			allowed, err := env.service.HasActiveGovernanceGrant(ctx, "user-1", c.action, c.board, c.strength, c.at)
			if err != nil || allowed != c.allowed {
				t.Fatalf("got %v %v", allowed, err)
			}
		})
	}
	if err := env.service.RevokeDelegation(ctx, adminActor(), "grant-req-read-1-0"); err != nil {
		t.Fatal(err)
	}
	allowed, err := env.service.HasActiveGovernanceGrant(ctx, "user-1", "community.thread.take_down", "42", "password", time.Unix(1600, 0))
	if err != nil || allowed {
		t.Fatal("revoked grant still authorizes")
	}
}
