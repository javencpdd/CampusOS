package service

import (
	"context"
	"errors"
	"testing"

	"github.com/campusos/CampusOS/internal/modules/core/identity/domain"
	"github.com/campusos/CampusOS/internal/modules/core/identity/repository"
)

// V12-02d: the admin role and management-plane admission are fully decoupled.
// Assigning or revoking the admin role never opens or closes Admin access;
// administrators are created only through the independent admin identity
// domain (AdminDomainService).
func TestAdminAccountAdmissionIsIndependentFromRoleAssignment(t *testing.T) {
	ctx := context.Background()
	users := repository.NewMemoryUserRepository()
	user := &domain.User{ID: "88001", Username: "admin_candidate", Nickname: "Admin Candidate", Email: "admin-candidate@example.test", Status: domain.UserStatusActive, AuthVersion: 1}
	if err := users.Create(ctx, user); err != nil {
		t.Fatal(err)
	}
	roles := repository.NewMemoryRoleRepository()
	adminAccounts := repository.NewMemoryAdminAccountRepository()
	permissions := NewPermissionService(roles, users)
	permissions.SetAdminAccountRepository(adminAccounts)
	access := NewAdminAccessService(adminAccounts)

	if allowed, err := access.CheckAdminAccess(ctx, user.ID); err != nil || allowed {
		t.Fatalf("unprovisioned user must not enter Admin: allowed=%v err=%v", allowed, err)
	}
	assigned, err := permissions.AssignRole(ctx, user.ID, 1)
	if err != nil || !assigned {
		t.Fatalf("assign admin role: assigned=%v err=%v", assigned, err)
	}
	// The role grant is a permission-catalog fact only; it must not provision
	// management-plane admission.
	if err := access.Require(ctx, user.ID); !errors.Is(err, ErrAdminAccessDenied) {
		t.Fatalf("role assignment opened Admin access: %v", err)
	}
	// Explicit admission provisioning still works independently of roles.
	if _, err := adminAccounts.EnsureActive(ctx, user.ID, "explicit_provision"); err != nil {
		t.Fatalf("explicit admission provisioning: %v", err)
	}
	if err := access.Require(ctx, user.ID); err != nil {
		t.Fatalf("explicitly provisioned admission denied: %v", err)
	}
	// Revoking the role leaves the explicitly provisioned admission untouched.
	revoked, err := permissions.RevokeRole(ctx, user.ID, 1)
	if err != nil || !revoked {
		t.Fatalf("revoke admin role: revoked=%v err=%v", revoked, err)
	}
	if err := access.Require(ctx, user.ID); err != nil {
		t.Fatalf("role revocation closed explicit admission: %v", err)
	}
}
