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
	"github.com/campusos/CampusOS/pkg/auth"
)

func TestAdminAdmissionSuspendRevokesSessionsAuditsAndProtectsLastActive(t *testing.T) {
	ctx := context.Background()
	admission, permissions, accounts, sessions, users, reliable := newAdminAdmissionServiceForTest(t)
	actor := createAdminAdmissionUser(t, ctx, users, permissions, "51001", "admission_actor")
	target := createAdminAdmissionUser(t, ctx, users, permissions, "51002", "admission_target")

	issued, err := sessions.Issue(ctx, target, SessionMetadata{ClientIP: "203.0.113.101"})
	if err != nil {
		t.Fatalf("issue target session: %v", err)
	}
	targetAccount, err := accounts.Get(ctx, target.ID)
	if err != nil {
		t.Fatalf("get target admission: %v", err)
	}

	updated, err := admission.Suspend(ctx, actor.ID, target.ID, AdminAdmissionCommand{
		ExpectedVersion: targetAccount.Version, Reason: "offboarding review",
	})
	if err != nil {
		t.Fatalf("suspend admission: %v", err)
	}
	if updated.Account.Status != repository.AdminAccountStatusSuspended || updated.Account.Version != targetAccount.Version+1 {
		t.Fatalf("unexpected suspended account: %#v", updated.Account)
	}
	if updated.Account.StatusReason != "offboarding review" || updated.Account.StatusChangedBy != actor.ID {
		t.Fatalf("transition evidence missing: %#v", updated.Account)
	}
	if active, err := accounts.IsActive(ctx, target.ID); err != nil || active {
		t.Fatalf("suspended admin remained admitted: active=%v err=%v", active, err)
	}
	claims, err := sessions.jwt.VerifyAccessToken(issued.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	if err := sessions.VerifyAccess(ctx, claims); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("suspended administrator session remained valid: %v", err)
	}
	audits, err := admission.ListAudits(ctx, 10)
	if err != nil || len(audits) != 1 {
		t.Fatalf("admission audit: items=%#v err=%v", audits, err)
	}
	if audits[0].PermissionCode != "identity.admin_account.suspend" || audits[0].Reason != "offboarding review" {
		t.Fatalf("unexpected authorization audit: %#v", audits[0])
	}
	commands, total, err := reliable.Store().ListCommandAudits(ctx, reliability.PageRequest{Page: 1, PageSize: 10})
	foundSuspend := false
	for _, command := range commands {
		foundSuspend = foundSuspend || command.CommandCode == "identity.admin_account.suspend"
	}
	if err != nil || total < 2 || !foundSuspend {
		t.Fatalf("reliable command audit: items=%#v total=%d err=%v", commands, total, err)
	}

	if _, err := admission.Suspend(ctx, actor.ID, target.ID, AdminAdmissionCommand{ExpectedVersion: targetAccount.Version, Reason: "stale"}); !errors.Is(err, repository.ErrAdminAccountVersionConflict) {
		t.Fatalf("stale version error=%v, want version conflict", err)
	}
	actorAccount, err := accounts.Get(ctx, actor.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := admission.Suspend(ctx, actor.ID, actor.ID, AdminAdmissionCommand{ExpectedVersion: actorAccount.Version, Reason: "self test"}); !errors.Is(err, repository.ErrLastActiveAdministrator) {
		t.Fatalf("last active administrator error=%v", err)
	}

	restored, err := admission.Restore(ctx, actor.ID, target.ID, AdminAdmissionCommand{
		ExpectedVersion: updated.Account.Version, Reason: "review complete",
	})
	if err != nil {
		t.Fatalf("restore admission: %v", err)
	}
	if restored.Account.Status != repository.AdminAccountStatusActive || restored.Account.Version != updated.Account.Version+1 {
		t.Fatalf("unexpected restored account: %#v", restored.Account)
	}
}

func TestAdminAdmissionRollbackLeavesAdmissionActiveWhenSessionRevocationFails(t *testing.T) {
	ctx := context.Background()
	users := repository.NewMemoryUserRepository()
	roles := repository.NewMemoryRoleRepository()
	accounts := repository.NewMemoryAdminAccountRepository()
	reliable := reliability.NewService(transaction.NewMemory(), reliability.NewMemoryStore())
	permissions := NewPermissionService(roles, users)
	permissions.SetAdminAccountRepository(accounts)
	permissions.SetReliability(reliable)

	actor := createAdminAdmissionUser(t, ctx, users, permissions, "52001", "rollback_actor")
	target := createAdminAdmissionUser(t, ctx, users, permissions, "52002", "rollback_target")
	admission, err := NewAdminAdmissionService(accounts, users, permissions, failingAdminAdmissionSessionRevoker{}, roles)
	if err != nil {
		t.Fatal(err)
	}
	admission.SetReliability(reliable)
	before, err := accounts.Get(ctx, target.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := admission.Suspend(ctx, actor.ID, target.ID, AdminAdmissionCommand{ExpectedVersion: before.Version, Reason: "must rollback"}); err == nil {
		t.Fatal("expected session revocation failure")
	}
	after, err := accounts.Get(ctx, target.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Status != repository.AdminAccountStatusActive || after.Version != before.Version {
		t.Fatalf("failed command changed admission state: before=%#v after=%#v", before, after)
	}
	audits, err := admission.ListAudits(ctx, 10)
	if err != nil || len(audits) != 0 {
		t.Fatalf("failed command wrote required audit: %#v err=%v", audits, err)
	}
}

func newAdminAdmissionServiceForTest(t *testing.T) (*AdminAdmissionService, *PermissionService, *repository.MemoryAdminAccountRepository, *SessionService, *repository.MemoryUserRepository, *reliability.Service) {
	t.Helper()
	users := repository.NewMemoryUserRepository()
	roles := repository.NewMemoryRoleRepository()
	accounts := repository.NewMemoryAdminAccountRepository()
	sessionStore := repository.NewMemorySessionRepository()
	jwt := auth.NewJWTManager(auth.JWTConfig{Secret: "admin-admission-test-secret", AccessTTL: time.Hour, RefreshTTL: 24 * time.Hour, Issuer: "test"})
	sessions, err := NewSessionService(sessionStore, users, jwt, SessionConfig{IPHashSecret: "admin-admission-ip-hash-secret"})
	if err != nil {
		t.Fatal(err)
	}
	reliable := reliability.NewService(transaction.NewMemory(), reliability.NewMemoryStore())
	permissions := NewPermissionService(roles, users)
	permissions.SetAdminAccountRepository(accounts)
	permissions.SetReliability(reliable)
	sessions.SetReliability(reliable)
	admission, err := NewAdminAdmissionService(accounts, users, permissions, sessions, roles)
	if err != nil {
		t.Fatal(err)
	}
	admission.SetReliability(reliable)
	return admission, permissions, accounts, sessions, users, reliable
}

func createAdminAdmissionUser(t *testing.T, ctx context.Context, users *repository.MemoryUserRepository, permissions *PermissionService, id, username string) *domain.User {
	t.Helper()
	user := &domain.User{
		ID: id, Username: username, Nickname: username, Email: username + "@example.test",
		Status: domain.UserStatusActive, AuthVersion: 1, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	if err := users.Create(ctx, user); err != nil {
		t.Fatal(err)
	}
	if assigned, err := permissions.AssignRole(ctx, user.ID, 1); err != nil || !assigned {
		t.Fatalf("assign admin role for %s: assigned=%v err=%v", user.ID, assigned, err)
	}
	return user
}

type failingAdminAdmissionSessionRevoker struct{}

func (failingAdminAdmissionSessionRevoker) RevokeAllForCommand(context.Context, string, string) (*domain.User, error) {
	return nil, errors.New("session store failed")
}
