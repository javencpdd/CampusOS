package service

import (
	"context"
	"reflect"
	"testing"
	"time"

	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
)

func TestAdminSPAOwnLogoutCurrentProofExactReceiptAndRequiredAtomicEvidence(t *testing.T) {
	for _, mode := range []string{"success", "audit", "outbox", "false_cas", "user_alias", "changed_principal", "no_reliable"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			_, env, _, proof := adminSPASelfFixture(t, true)
			events := reliability.NewMemoryStore()
			store := &adminSPAMFAStore{Store: events, events: mode == "outbox", fail: mode == "audit" || mode == "outbox"}
			env.service.SetReliability(reliability.NewService(transaction.NewMemory(events), store))
			memory := env.sessions.(*repository.MemoryAdminSessionRepository)
			if err := memory.CreateSession(ctx, repository.AdminSession{ID: "other-session", AccountID: "adm.spa.opaque", RefreshTokenDigest: "other-digest", TokenFamilyID: "other-session", AuthenticationStrength: "password", ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
				t.Fatal(err)
			}
			beforeSessions, beforeEvents := memory.Snapshot(), events.Snapshot()
			if mode == "false_cas" {
				env.service.sessions = &adminSPARotationCASFailure{MemoryAdminSessionRepository: memory}
			}
			if mode == "no_reliable" {
				env.service.reliable = nil
			}
			if mode == "user_alias" {
				original := proof
				proof = func(ctx context.Context) (identityport.PrincipalContext, error) {
					p, err := original(ctx)
					p.Actor.Kind = "user"
					p.Audience = "user"
					return p, err
				}
			}
			if mode == "changed_principal" {
				original := proof
				calls := 0
				proof = func(ctx context.Context) (identityport.PrincipalContext, error) {
					p, err := original(ctx)
					calls++
					if calls > 1 {
						p.AuthenticationStrength = "mfa"
					}
					return p, err
				}
			}
			err := env.service.RevokeCurrentSession(ctx, proof)
			if mode != "success" {
				if err == nil {
					t.Fatal("denial accepted")
				}
				if !reflect.DeepEqual(beforeSessions, memory.Snapshot()) || !reflect.DeepEqual(beforeEvents, events.Snapshot()) {
					t.Fatal("partial logout escaped rollback")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			own, err := memory.GetSessionByID(ctx, "adms.spa.opaque")
			if err != nil || own.RevokedAt == nil || own.RevokeReason != "logout" || own.Version != 2 {
				t.Fatal("own session not revoked", err)
			}
			other, err := memory.GetSessionByID(ctx, "other-session")
			if err != nil || other.RevokedAt != nil {
				t.Fatal("other session revoked", err)
			}
			if _, err := proof(ctx); err == nil {
				t.Fatal("revoked access session survived")
			}
			if reflect.DeepEqual(beforeEvents, events.Snapshot()) {
				t.Fatal("missing required command evidence")
			}
		})
	}
}
