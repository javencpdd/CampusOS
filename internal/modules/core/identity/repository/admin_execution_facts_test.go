package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/campusos/CampusOS/internal/platform/transaction"
)

func TestAdminPermissionFactsMemoryVersionsAndRollback(t *testing.T) {
	ctx := context.Background()
	r := NewMemoryAdminDomainRepository()
	const account = "adm.memory.facts"
	if err := r.CreateAccount(ctx, AdminDomainAccount{ID: account, Username: "memory-facts", Email: "facts@example.test"}); err != nil {
		t.Fatal(err)
	}
	if err := r.BindRole(ctx, AdminDomainRoleBinding{ID: "binding.memory.facts", AccountID: account, RoleID: "security_admin", GrantedBy: account}); err != nil {
		t.Fatal(err)
	}
	const code = "community.thread.take_down"
	if err := r.ReplaceAdminRolePermissionFacts(ctx, "security_admin", []string{code}, "seed"); err != nil {
		t.Fatal(err)
	}
	get := func() AdminRoleExecutionFact {
		f, err := r.LoadAdminRoleExecutionFacts(ctx, account, false)
		if err != nil || len(f) != 1 {
			t.Fatal(f, err)
		}
		return f[0]
	}
	first := get()
	if err := r.ReplaceAdminRolePermissionFacts(ctx, "security_admin", []string{code}, "seed"); err != nil {
		t.Fatal(err)
	}
	if get().PermissionFactsVersion != first.PermissionFactsVersion {
		t.Fatal("no-op changed version")
	}
	if err := r.ReplaceAdminRolePermissionFacts(ctx, "security_admin", nil, "seed"); err != nil {
		t.Fatal(err)
	}
	if err := r.ReplaceAdminRolePermissionFacts(ctx, "security_admin", []string{code}, "seed"); err != nil {
		t.Fatal(err)
	}
	second := get()
	if second.PermissionFactsVersion <= first.PermissionFactsVersion {
		t.Fatal("remove/re-add reused version")
	}
	var rolledBack int64
	err := transaction.NewMemory(r).Within(ctx, func(txCtx context.Context) error {
		if err := r.ReplaceAdminRolePermissionFacts(txCtx, "security_admin", []string{code}, "new-seed"); err != nil {
			return err
		}
		f, e := r.LoadAdminRoleExecutionFacts(txCtx, account, true)
		if e != nil {
			return e
		}
		rolledBack = f[0].PermissionFactsVersion
		return errors.New("rollback")
	})
	if err == nil || get().PermissionFactsVersion != second.PermissionFactsVersion {
		t.Fatal("rollback lost original version", err)
	}
	if err := r.ReplaceAdminRolePermissionFacts(ctx, "security_admin", []string{code}, "new-seed"); err != nil {
		t.Fatal(err)
	}
	if get().PermissionFactsVersion <= rolledBack {
		t.Fatal("rollback allocation reused")
	}
	if _, err := r.LoadAdminRoleExecutionFacts(ctx, account, true); !errors.Is(err, ErrAdminAuthorityLockUnavailable) {
		t.Fatal("nontransaction lock accepted", err)
	}
}
