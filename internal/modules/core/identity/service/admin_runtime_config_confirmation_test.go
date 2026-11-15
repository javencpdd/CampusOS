package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/campusos/CampusOS/pkg/auth"
)

func runtimeConfirmationFixture(t *testing.T) (*BoardGovernanceAuthorityService, *repository.MemoryAdminDomainRepository, identityport.SessionPrincipalResolver) {
	t.Helper()
	ctx := context.Background()
	session, resolve := governanceAdminProof(t, "password")
	repo := session.admins.(*repository.MemoryAdminDomainRepository)
	hash, _ := auth.HashPassword("configuration-fixture-password")
	if _, _, err := repo.SetCredentialCAS(ctx, "22001", hash, 0, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := repo.BindRole(ctx, repository.AdminDomainRoleBinding{ID: "runtime-confirmation-role", AccountID: "22001", RoleID: "security_admin", GrantedBy: "22001"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.ReplaceAdminRolePermissionFacts(ctx, "security_admin", []string{"platform.runtime_config.manage", "community.thread.pin"}, "fixture"); err != nil {
		t.Fatal(err)
	}
	return NewBoardGovernanceAuthorityService(nil, repo, session), repo, resolve
}

func TestRuntimeConfigPasswordConfirmationDoesNotUpgradeMFAOrOtherOperations(t *testing.T) {
	s, _, resolve := runtimeConfirmationFixture(t)
	ctx := context.Background()
	q := namedAdminQuery("platform.runtime_config.manage")
	before, err := s.LoadAdminExecutionAuthority(ctx, q, resolve, false)
	if err != nil || before.Allowed {
		t.Fatal("MFA unexpectedly satisfied before confirmation", err)
	}
	confirmed, err := s.LoadRuntimeConfigPasswordAuthority(ctx, resolve, "configuration-fixture-password", false)
	if err != nil || !confirmed.Allowed || confirmed.Query != q || confirmed.Principal.AuthenticationStrength != "password" || confirmed.Principal.AdminMFAOptional {
		t.Fatal("scoped confirmation failed", err)
	}
	raw, _ := json.Marshal(confirmed)
	if string(raw) != "{}" {
		t.Fatal("confirmation facts serialized")
	}
	for _, code := range []string{"platform.runtime_config.manage", "community.thread.pin"} {
		a, err := s.LoadAdminExecutionAuthority(ctx, namedAdminQuery(code), resolve, false)
		if err != nil || a.Allowed {
			t.Fatal("password confirmation escaped runtime operation", code, err)
		}
	}
	if _, err = s.loadAdminExecutionAuthority(ctx, namedAdminQuery("community.thread.pin"), resolve, false, true); err == nil {
		t.Fatal("scoped password flag accepted for another operation")
	}
	if _, err = s.LoadRuntimeConfigPasswordAuthority(ctx, resolve, "configuration-fixture-password", true); err == nil {
		t.Fatal("authority lock outside transaction accepted")
	}
	if err = transaction.NewMemory().Within(ctx, func(tx context.Context) error {
		current, e := s.LoadRuntimeConfigPasswordAuthority(tx, resolve, "configuration-fixture-password", true)
		if e != nil {
			return e
		}
		return identityport.CheckAdminExecutionAuthority(confirmed, current)
	}); err != nil {
		t.Fatal("locked confirmation changed", err)
	}
}
func TestRuntimeConfigPasswordConfirmationTracksCredentialsRolesAndCurrentPrincipal(t *testing.T) {
	s, repo, resolve := runtimeConfirmationFixture(t)
	ctx := context.Background()
	first, err := s.LoadRuntimeConfigPasswordAuthority(ctx, resolve, "configuration-fixture-password", false)
	if err != nil {
		t.Fatal(err)
	}
	for _, password := range []string{"", "wrong-password"} {
		if _, e := s.LoadRuntimeConfigPasswordAuthority(ctx, resolve, password, false); !errors.Is(e, identityport.ErrAdminConfirmationPassword) {
			t.Fatal("incorrect password accepted", e)
		}
	}
	if _, e := s.LoadRuntimeConfigPasswordAuthority(ctx, governanceUserProof(t), "configuration-fixture-password", false); e == nil {
		t.Fatal("User principal accepted")
	}
	hash, _ := auth.HashPassword("changed-fixture-password")
	if _, _, err = repo.SetCredentialCAS(ctx, "22001", hash, 1, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, e := s.LoadRuntimeConfigPasswordAuthority(ctx, resolve, "configuration-fixture-password", false); !errors.Is(e, identityport.ErrAdminConfirmationPassword) {
		t.Fatal("old credential accepted", e)
	}
	current, err := s.LoadRuntimeConfigPasswordAuthority(ctx, resolve, "changed-fixture-password", false)
	if err != nil {
		t.Fatal(err)
	}
	if identityport.CheckAdminExecutionAuthority(first, current) == nil {
		t.Fatal("old credential witness reused")
	}
	if err = repo.ReplaceAdminRolePermissionFacts(ctx, "security_admin", []string{"community.thread.pin"}, "revoker"); err != nil {
		t.Fatal(err)
	}
	current, err = s.LoadRuntimeConfigPasswordAuthority(ctx, resolve, "changed-fixture-password", false)
	if err != nil || current.Allowed {
		t.Fatal("revoked permission accepted", err)
	}
}
func TestRuntimeConfigPasswordConfirmationThrottlesFailedAttempts(t *testing.T) {
	s, _, resolve := runtimeConfirmationFixture(t)
	ctx := context.Background()
	now := time.Now()
	s.now = func() time.Time { return now }
	for i := 0; i < 5; i++ {
		if _, err := s.LoadRuntimeConfigPasswordAuthority(ctx, resolve, "incorrect", false); !errors.Is(err, identityport.ErrAdminConfirmationPassword) {
			t.Fatal("unexpected failure", err)
		}
	}
	if _, err := s.LoadRuntimeConfigPasswordAuthority(ctx, resolve, "configuration-fixture-password", false); !errors.Is(err, identityport.ErrAdminConfirmationThrottled) {
		t.Fatal("attempt limit missing", err)
	}
	now = now.Add(time.Minute + time.Second)
	if a, err := s.LoadRuntimeConfigPasswordAuthority(ctx, resolve, "configuration-fixture-password", false); err != nil || !a.Allowed {
		t.Fatal("confirmation did not recover after cooldown", err)
	}
}
