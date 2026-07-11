package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/campusos/CampusOS/internal/core/identity/domain"
	"github.com/campusos/CampusOS/internal/core/identity/repository"
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
