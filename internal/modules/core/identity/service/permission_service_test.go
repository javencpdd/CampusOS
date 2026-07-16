package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/identity/domain"
	"github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
)

func TestPermissionServiceAssignRoleIsValidatedAndIdempotent(t *testing.T) {
	ctx := context.Background()
	userRepo := repository.NewMemoryUserRepository()
	if err := userRepo.Create(ctx, &domain.User{
		ID:        "1001",
		Username:  "role_test_user",
		Nickname:  "Role Test User",
		Email:     "role-test@example.test",
		Status:    domain.UserStatusActive,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}

	svc := NewPermissionService(repository.NewMemoryRoleRepository(), userRepo)

	assigned, err := svc.AssignRole(ctx, "1001", 1)
	if err != nil {
		t.Fatalf("assign admin: %v", err)
	}
	if !assigned {
		t.Fatal("expected first assignment to create a user-role mapping")
	}

	assigned, err = svc.AssignRole(ctx, "1001", 1)
	if err != nil {
		t.Fatalf("repeat assignment: %v", err)
	}
	if assigned {
		t.Fatal("expected repeat assignment to be idempotent")
	}

	roles, err := svc.GetUserRoles(ctx, "1001")
	if err != nil {
		t.Fatalf("get user roles: %v", err)
	}
	if len(roles) != 2 || roles[0].Name != "admin" || roles[1].Name != "member" {
		t.Fatalf("unexpected roles after assignment: %#v", roles)
	}

	allowed, err := svc.Check(ctx, "1001", "post", "write")
	if err != nil {
		t.Fatalf("check implicit member permission: %v", err)
	}
	if !allowed {
		t.Fatal("expected an administrator to retain implicit member post:write permission")
	}

	if _, err := svc.AssignRole(ctx, "1001", 3); !errors.Is(err, ErrProtectedRole) {
		t.Fatalf("expected member assignment to be protected, got %v", err)
	}

	revoked, err := svc.RevokeRole(ctx, "1001", 1)
	if err != nil {
		t.Fatalf("revoke moderator: %v", err)
	}
	if !revoked {
		t.Fatal("expected existing assignment to be revoked")
	}

	_, err = svc.RevokeRole(ctx, "1001", 1)
	if !errors.Is(err, ErrRoleAssignmentNotFound) {
		t.Fatalf("expected missing assignment error, got %v", err)
	}
}

func TestPermissionServiceRequiresAndEnforcesModeratorCategoryScope(t *testing.T) {
	ctx := context.Background()
	userRepo := repository.NewMemoryUserRepository()
	if err := userRepo.Create(ctx, &domain.User{
		ID: "1001", Username: "scoped_moderator", Nickname: "Scoped Moderator",
		Email: "scoped-moderator@example.test", Status: domain.UserStatusActive,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}

	svc := NewPermissionService(repository.NewMemoryRoleRepository(), userRepo)
	if _, err := svc.AssignRole(ctx, "1001", 2); !errors.Is(err, ErrRoleRequiresScope) {
		t.Fatalf("expected moderator global assignment rejection, got %v", err)
	}
	changed, err := svc.ReplaceCategoryRoleScopes(ctx, "1001", "moderator", []int64{10, 20})
	if err != nil || !changed {
		t.Fatalf("replace moderator category scopes: changed=%v err=%v", changed, err)
	}
	global, err := svc.Check(ctx, "1001", "thread", "pin")
	if err != nil {
		t.Fatalf("check global permission: %v", err)
	}
	if global {
		t.Fatal("category moderator must not receive global thread:pin")
	}
	matching, err := svc.CheckScoped(ctx, "1001", "thread", "pin", "category", 10)
	if err != nil || !matching {
		t.Fatalf("expected matching category permission: allowed=%v err=%v", matching, err)
	}
	outside, err := svc.CheckScoped(ctx, "1001", "thread", "pin", "category", 30)
	if err != nil {
		t.Fatalf("check outside category: %v", err)
	}
	if outside {
		t.Fatal("category moderator must not manage an unassigned category")
	}
}

func TestPermissionServiceRejectsUnknownTargets(t *testing.T) {
	ctx := context.Background()
	userRepo := repository.NewMemoryUserRepository()
	svc := NewPermissionService(repository.NewMemoryRoleRepository(), userRepo)

	if _, err := svc.AssignRole(ctx, "not-a-number", 2); !errors.Is(err, ErrInvalidRoleAssignment) {
		t.Fatalf("expected invalid user ID error, got %v", err)
	}
	if _, err := svc.AssignRole(ctx, "1001", 2); !errors.Is(err, repository.ErrUserNotFound) {
		t.Fatalf("expected missing user error, got %v", err)
	}

	if err := userRepo.Create(ctx, &domain.User{
		ID:        "1001",
		Username:  "role_unknown_target",
		Nickname:  "Role Unknown Target",
		Email:     "role-unknown@example.test",
		Status:    domain.UserStatusActive,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if _, err := svc.AssignRole(ctx, "1001", 999); !errors.Is(err, repository.ErrRoleNotFound) {
		t.Fatalf("expected missing role error, got %v", err)
	}
	if _, err := svc.GetUserRoles(ctx, "invalid-id"); !errors.Is(err, ErrInvalidRoleAssignment) {
		t.Fatalf("expected invalid user ID for role lookup, got %v", err)
	}
	if _, err := svc.GetUserRoles(ctx, "9999"); !errors.Is(err, repository.ErrUserNotFound) {
		t.Fatalf("expected missing user for role lookup, got %v", err)
	}
}

func TestPermissionCatalogUsesStableCodesAndPreventsPrivilegeEscalation(t *testing.T) {
	ctx := context.Background()
	users := repository.NewMemoryUserRepository()
	for _, user := range []*domain.User{
		{ID: "2001", Username: "catalog_admin", Nickname: "Catalog Admin", Email: "catalog-admin@example.test", Status: domain.UserStatusActive, CreatedAt: time.Now(), UpdatedAt: time.Now()},
		{ID: "2002", Username: "catalog_moderator", Nickname: "Catalog Moderator", Email: "catalog-moderator@example.test", Status: domain.UserStatusActive, CreatedAt: time.Now(), UpdatedAt: time.Now()},
	} {
		if err := users.Create(ctx, user); err != nil {
			t.Fatal(err)
		}
	}
	service := NewPermissionService(repository.NewMemoryRoleRepository(), users)
	if _, err := service.AssignRole(ctx, "2001", 1); err != nil {
		t.Fatal(err)
	}
	if allowed, err := service.CheckCode(ctx, "2001", "identity.user.suspend"); err != nil || !allowed {
		t.Fatalf("admin stable permission allowed=%v err=%v", allowed, err)
	}
	if changed, err := service.ReplaceCategoryRoleScopes(ctx, "2002", "moderator", []int64{12}); err != nil || !changed {
		t.Fatalf("assign moderator category: changed=%v err=%v", changed, err)
	}
	if allowed, err := service.CheckCodeScoped(ctx, "2002", "community.thread.take_down", "category", 12); err != nil || !allowed {
		t.Fatalf("scoped moderation code allowed=%v err=%v", allowed, err)
	}
	if allowed, err := service.CheckCodeScoped(ctx, "2002", "community.thread.take_down", "category", 99); err != nil || allowed {
		t.Fatalf("outside moderation scope allowed=%v err=%v", allowed, err)
	}
	if allowed, err := service.HasAnyScopedPermissionCode(ctx, "2002", "community.thread.take_down", "category"); err != nil || !allowed {
		t.Fatalf("moderator route candidate allowed=%v err=%v", allowed, err)
	}
	if allowed, err := service.HasAnyScopedPermissionCode(ctx, "2002", "community.thread.take_down", "global"); err != nil || allowed {
		t.Fatalf("moderator must not be a global route candidate allowed=%v err=%v", allowed, err)
	}
	role, err := service.CreateCustomRole(ctx, "2001", "content_reviewer", "review pending content", []string{"community.thread.review"})
	if err != nil {
		t.Fatalf("create custom role: %v", err)
	}
	if err := service.UpdateRolePermissions(ctx, "2001", role.ID, []string{"community.thread.review", "community.thread.take_down"}); err != nil {
		t.Fatalf("update role permissions: %v", err)
	}
	if err := service.UpdateRolePermissions(ctx, "2001", 1, []string{"community.thread.review"}); !errors.Is(err, ErrProtectedRole) {
		t.Fatalf("system role update must be rejected, got %v", err)
	}
	if _, err := service.CreateCustomRole(ctx, "2002", "forbidden_escalation", "must fail", []string{"identity.user.suspend"}); !errors.Is(err, ErrPermissionEscalation) {
		t.Fatalf("moderator escalation = %v, want ErrPermissionEscalation", err)
	}
	definitions, err := service.ListPermissionDefinitions(ctx)
	if err != nil || len(definitions) == 0 {
		t.Fatalf("permission definitions=%d err=%v", len(definitions), err)
	}
}

func TestPermissionCatalogFallbackIsRecordedWithoutChangingDecision(t *testing.T) {
	ctx := context.Background()
	users := repository.NewMemoryUserRepository()
	if err := users.Create(ctx, &domain.User{
		ID: "2501", Username: "legacy_catalog_admin", Nickname: "Legacy Catalog Admin",
		Email: "legacy-catalog@example.test", Status: domain.UserStatusActive,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	// Expose only the pre-catalog interface so the service takes the rolling
	// upgrade fallback even though the underlying memory adapter has a catalog.
	legacyRoles := struct{ repository.RoleRepository }{repository.NewMemoryRoleRepository()}
	service := NewPermissionService(legacyRoles, users)
	store := reliability.NewMemoryStore()
	service.SetReliability(reliability.NewService(transaction.NewMemory(), store))
	if assigned, err := legacyRoles.AssignRole(ctx, "2501", 1, "global", nil); err != nil || !assigned {
		t.Fatalf("seed administrator assignment: assigned=%v err=%v", assigned, err)
	}

	allowed, err := service.CheckCode(ctx, "2501", "identity.user.suspend")
	if err != nil || !allowed {
		t.Fatalf("legacy catalog permission result allowed=%v err=%v", allowed, err)
	}
	usages, _, err := store.ListCompatibility(ctx, reliability.PageRequest{Page: 1, PageSize: 10})
	if err != nil || len(usages) != 1 {
		t.Fatalf("compatibility usage=%#v err=%v", usages, err)
	}
	if usages[0].Key != "legacy-permission-catalog" || usages[0].Kind != "authorization" {
		t.Fatalf("unexpected compatibility usage: %#v", usages[0])
	}
}

func TestPermissionServiceActorRoleAdministrationCannotBypassServicePolicy(t *testing.T) {
	ctx := context.Background()
	users := repository.NewMemoryUserRepository()
	for _, user := range []*domain.User{
		{ID: "3001", Username: "role_admin", Nickname: "Role Admin", Email: "role-admin@example.test", Status: domain.UserStatusActive, CreatedAt: time.Now(), UpdatedAt: time.Now()},
		{ID: "3002", Username: "role_target", Nickname: "Role Target", Email: "role-target@example.test", Status: domain.UserStatusActive, CreatedAt: time.Now(), UpdatedAt: time.Now()},
		{ID: "3003", Username: "role_member", Nickname: "Role Member", Email: "role-member@example.test", Status: domain.UserStatusActive, CreatedAt: time.Now(), UpdatedAt: time.Now()},
	} {
		if err := users.Create(ctx, user); err != nil {
			t.Fatal(err)
		}
	}
	service := NewPermissionService(repository.NewMemoryRoleRepository(), users)
	if _, err := service.AssignRole(ctx, "3001", 1); err != nil {
		t.Fatalf("seed admin: %v", err)
	}

	if _, err := service.AssignRoleByActor(ctx, "3003", "3002", 1); !errors.Is(err, ErrPermissionEscalation) {
		t.Fatalf("member must not assign an administrator role, got %v", err)
	}
	if assigned, err := service.AssignRoleByActor(ctx, "3001", "3002", 1); err != nil || !assigned {
		t.Fatalf("authorized actor assignment assigned=%v err=%v", assigned, err)
	}
	if _, err := service.RevokeRoleByActor(ctx, "3001", "3001", 1); !errors.Is(err, ErrPermissionEscalation) {
		t.Fatalf("self-revocation must be denied, got %v", err)
	}
	if revoked, err := service.RevokeRoleByActor(ctx, "3001", "3002", 1); err != nil || !revoked {
		t.Fatalf("authorized actor revoke revoked=%v err=%v", revoked, err)
	}
	if changed, err := service.ReplaceCategoryRoleScopesByActor(ctx, "3001", "3002", "moderator", []int64{12}); err != nil || !changed {
		t.Fatalf("authorized actor moderator scope change changed=%v err=%v", changed, err)
	}
	if _, err := service.ReplaceCategoryRoleScopesByActor(ctx, "3003", "3002", "moderator", []int64{13}); !errors.Is(err, ErrPermissionEscalation) {
		t.Fatalf("member must not grant moderator scope, got %v", err)
	}
}

func TestProtectedGlobalRoleRevocationKeepsOneAdministratorUnderConcurrency(t *testing.T) {
	ctx := context.Background()
	repo := repository.NewMemoryRoleRepository()
	for _, userID := range []string{"4101", "4102"} {
		if assigned, err := repo.AssignRole(ctx, userID, 1, "global", nil); err != nil || !assigned {
			t.Fatalf("seed administrator %s: assigned=%v err=%v", userID, assigned, err)
		}
	}

	type result struct {
		revoked bool
		err     error
	}
	start := make(chan struct{})
	results := make(chan result, 2)
	for _, userID := range []string{"4101", "4102"} {
		userID := userID
		go func() {
			<-start
			revoked, err := repo.RevokeRoleUnlessLastGlobal(ctx, userID, 1)
			results <- result{revoked: revoked, err: err}
		}()
	}
	close(start)

	successes := 0
	protected := 0
	for range 2 {
		outcome := <-results
		if outcome.revoked && outcome.err == nil {
			successes++
		}
		if errors.Is(outcome.err, repository.ErrLastGlobalRoleAssignment) {
			protected++
		}
	}
	if successes != 1 || protected != 1 {
		t.Fatalf("expected one revoke and one protected result, successes=%d protected=%d", successes, protected)
	}
	count, err := repo.CountGlobalRoleAssignments(ctx, 1)
	if err != nil || count != 1 {
		t.Fatalf("one administrator must remain: count=%d err=%v", count, err)
	}
}
