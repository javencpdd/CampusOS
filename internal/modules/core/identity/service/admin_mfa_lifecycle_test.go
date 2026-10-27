package service

import (
	"context"
	"testing"
)

func TestAdminMFAOptionalLifecycleRetainsBootstrapAndLastAdminGuards(t *testing.T) {
	ctx := context.Background()
	env := newAdminDomainEnv(true)
	config := env.service.config
	config.AdminMFAOptional = true
	s := NewAdminDomainService(env.admins, env.catalog, config)
	root, err := s.BootstrapSecurityAdmin(ctx, "optional-root", "optional-root@example.test", "test-password", "test")
	if err != nil {
		t.Fatal(err)
	}
	actor := AdminActor{ID: root.ID, CredentialID: "verified-session", MFAAuthenticated: false}
	if _, err := s.CreateAdmin(ctx, actor, "optional-ops", "optional-ops@example.test", "test-password", false, "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.BootstrapSecurityAdmin(ctx, "second-root", "second-root@example.test", "test-password", "again"); adminDomainOutcome(err) != "identity.admin_bootstrap_forbidden" {
		t.Fatal("repeat bootstrap accepted", err)
	}
	// A different verified admin still cannot remove the last security admin.
	actor.ID = "other-verified-admin"
	if err := s.RevokeAdmin(ctx, actor, root.ID, "test"); adminDomainOutcome(err) != "identity.last_security_admin" {
		t.Fatal("last-admin guard bypassed", err)
	}
}
