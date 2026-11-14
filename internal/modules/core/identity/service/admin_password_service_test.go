package service

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/campusos/CampusOS/pkg/auth"
)

func TestAdminPasswordChangeAuditAndOutboxRollbackCredentialsAndSessions(t *testing.T) {
	for _, failure := range []string{"audit", "outbox"} {
		t.Run(failure, func(t *testing.T) {
			ctx := context.Background()
			_, env, security, proof := adminSPASelfFixture(t, true)
			hash, _ := auth.HashPassword("old-password")
			if _, _, err := env.admins.SetCredentialCAS(ctx, "adm.spa.opaque", hash, 0, time.Now()); err != nil {
				t.Fatal(err)
			}
			events := reliability.NewMemoryStore()
			store := &adminSPAMFAStore{Store: events, fail: true, events: failure == "outbox"}
			env.service.SetReliability(reliability.NewService(transaction.NewMemory(events), store))
			self := NewAdminSelfService(env.service, env.admins, security)
			admins := env.admins.(*repository.MemoryAdminDomainRepository)
			sessions := env.sessions.(*repository.MemoryAdminSessionRepository)
			beforeA, beforeS, beforeM := admins.Snapshot(), sessions.Snapshot(), security.Snapshot()
			if err := self.ChangePassword(ctx, proof, "old-password", "new-password"); err == nil {
				t.Fatal("required evidence failure was accepted")
			}
			if !reflect.DeepEqual(beforeA, admins.Snapshot()) || !reflect.DeepEqual(beforeS, sessions.Snapshot()) || !reflect.DeepEqual(beforeM, security.Snapshot()) {
				t.Fatal("failed change left partial credentials or session revocations")
			}
		})
	}
}

func TestAdminPasswordChangeRespectsEnrolledMFAWithoutChangingDeploymentPolicy(t *testing.T) {
	for _, mode := range []string{"password", "recent", "stale", "optional"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			_, env, security, proof := adminSPASelfFixture(t, true)
			hash, _ := auth.HashPassword("old-password")
			if _, _, err := env.admins.SetCredentialCAS(ctx, "adm.spa.opaque", hash, 0, time.Now()); err != nil {
				t.Fatal(err)
			}
			events := reliability.NewMemoryStore()
			env.service.SetReliability(reliability.NewService(transaction.NewMemory(events), events))
			self := NewAdminSelfService(env.service, env.admins, security)
			now := time.Now().UTC()
			if _, err := security.UpsertTOTP(ctx, repository.AdminTOTPMethod{AccountID: "adm.spa.opaque", Status: "active", KeyID: "fixture", Nonce: "fixture-nonce", Ciphertext: "fixture-ciphertext", ConfirmedAt: &now}); err != nil {
				t.Fatal(err)
			}
			if mode == "recent" || mode == "stale" {
				at := now
				if mode == "stale" {
					at = at.Add(-16 * time.Minute)
				}
				if _, ok, err := env.sessions.UpgradeSessionStrengthCAS(ctx, "adms.spa.opaque", 1, "mfa", at, now); err != nil || !ok {
					t.Fatal(err)
				}
			}
			env.service.SetMFAOptional(mode == "optional")
			err := self.ChangePassword(ctx, proof, "old-password", "new-password")
			if mode == "password" || mode == "stale" {
				if !errors.Is(err, ErrAdminPasswordMFA) {
					t.Fatal("missing recent MFA accepted", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
		})
	}
}
