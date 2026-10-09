package moderation

import (
	"context"
	"testing"
	"time"

	identitydelegation "github.com/campusos/CampusOS/internal/modules/core/identity/delegation"
	identityrepo "github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	identitysvc "github.com/campusos/CampusOS/internal/modules/core/identity/service"
)

// wireTestDelegationChain connects the delegation chain to the permission
// service and seeds the test administrator's management authority and
// delegable bounds, mirroring the migration seed and bound administration.
func wireTestDelegationChain(t *testing.T, permissionSvc *identitysvc.PermissionService, userRepo *identityrepo.MemoryUserRepository, boardIDs ...string) {
	t.Helper()
	ctx := context.Background()
	delegationRepo := identityrepo.NewMemoryDelegationRepository()
	permissionSvc.SetDelegationService(identitydelegation.NewService(delegationRepo, userRepo, nil))
	now := time.Now()
	rows := []identityrepo.Delegation{
		{ID: "test-mgmt-9001", Kind: identityrepo.DelegationKindManagement, SubjectKind: "admin", SubjectID: "9001",
			Action: "identity.role.assign", NotBefore: now.Add(-time.Hour), ExpiresAt: now.Add(24 * time.Hour), CreatedBy: "test"},
	}
	for _, board := range boardIDs {
		for _, action := range []string{"community.thread.take_down", "community.post.delete"} {
			rows = append(rows, identityrepo.Delegation{
				ID: "test-bound-9001-" + board + "-" + action, Kind: identityrepo.DelegationKindBound,
				SubjectKind: "admin", SubjectID: "9001", Action: action, BoardID: board,
				NotBefore: now.Add(-time.Hour), ExpiresAt: now.Add(24 * time.Hour),
				RequiredStrength: "password", Delegable: true, CreatedBy: "test",
			})
		}
	}
	if err := delegationRepo.InsertDelegations(ctx, rows); err != nil {
		t.Fatalf("seed delegation authority: %v", err)
	}
}

// moderatorAdminOperation carries the verified MFA proof the admin entry
// supplies after server-side admission and session checks.
func moderatorAdminOperation(traceID string) OperationContext {
	return OperationContext{TraceID: traceID, AuthenticationStrength: "mfa", CredentialID: "session-9001"}
}
