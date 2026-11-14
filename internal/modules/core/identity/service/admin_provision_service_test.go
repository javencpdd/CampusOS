package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/campusos/CampusOS/pkg/auth"
)

func TestDefaultAdminPreservesChangedPasswordAndAdditionalAccounts(t *testing.T) {
	ctx := context.Background()
	admins := repository.NewMemoryAdminDomainRepository()
	catalog := repository.NewMemoryRoleRepository()
	events := reliability.NewMemoryStore()
	provision := NewAdminProvisionService(admins, catalog, reliability.NewService(transaction.NewMemory(events), events))
	var group sync.WaitGroup
	for range 4 {
		group.Add(1)
		go func() {
			defer group.Done()
			if err := provision.EnsureDefault(ctx); err != nil {
				t.Error(err)
			}
		}()
	}
	group.Wait()
	account, err := admins.GetAccountByUsername(ctx, "admin")
	if err != nil {
		t.Fatal(err)
	}
	credential, err := admins.GetCredential(ctx, account.ID)
	if err != nil || !auth.CheckPassword("admin", credential.CredentialHash) {
		t.Fatal("default credential was not initialized")
	}
	changed, _ := auth.HashPassword("changed-password")
	if _, ok, err := admins.SetCredentialCAS(ctx, account.ID, changed, credential.CredentialVersion, time.Now()); err != nil || !ok {
		t.Fatal("change fixture password", err)
	}
	if err := provision.Create(ctx, "operator", "operator@example.test", "operator-password"); err != nil {
		t.Fatal(err)
	}
	if err := provision.EnsureDefault(ctx); err != nil {
		t.Fatal(err)
	}
	if err := provision.Create(ctx, "admin", "another@example.test", "replacement-password"); err == nil {
		t.Fatal("duplicate account must not reset an existing credential")
	}
	credential, _ = admins.GetCredential(ctx, account.ID)
	if !auth.CheckPassword("changed-password", credential.CredentialHash) || auth.CheckPassword("admin", credential.CredentialHash) {
		t.Fatal("startup or provisioning replaced a changed password")
	}
	count, _ := admins.CountAdminAccounts(ctx)
	security, _ := admins.CountActiveSecurityAdmins(ctx)
	if count != 2 || security != 2 {
		t.Fatalf("accounts=%d security=%d", count, security)
	}
}

func TestDefaultAdminDoesNotAddAccountToExistingDomain(t *testing.T) {
	ctx := context.Background()
	admins := repository.NewMemoryAdminDomainRepository()
	catalog := repository.NewMemoryRoleRepository()
	events := reliability.NewMemoryStore()
	provision := NewAdminProvisionService(admins, catalog, reliability.NewService(transaction.NewMemory(events), events))
	if err := provision.Create(ctx, "existing", "existing@example.test", "existing-password"); err != nil {
		t.Fatal(err)
	}
	if err := provision.EnsureDefault(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := admins.GetAccountByUsername(ctx, "admin"); err == nil {
		t.Fatal("default account was added to an existing domain")
	}
}

func TestDefaultAdminRequiredAuditRollback(t *testing.T) {
	ctx := context.Background()
	admins := repository.NewMemoryAdminDomainRepository()
	catalog := repository.NewMemoryRoleRepository()
	events := reliability.NewMemoryStore()
	store := &adminSPAMFAStore{Store: events, fail: true}
	provision := NewAdminProvisionService(admins, catalog, reliability.NewService(transaction.NewMemory(events), store))
	if err := provision.EnsureDefault(ctx); err == nil {
		t.Fatal("default administrator committed without required audit")
	}
	if count, _ := admins.CountAdminAccounts(ctx); count != 0 {
		t.Fatal("failed startup left a partial administrator")
	}
}
