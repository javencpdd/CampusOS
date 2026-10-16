package service

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
)

func TestAdminSPARefreshReliableReuseRevokesFamilyAndNeverPersistsRawCredentials(t *testing.T) {
	ctx := context.Background()
	env := newAdminSessionEnv()
	bootstrapAdmin(t, ctx, env.admins)
	events := reliability.NewMemoryStore()
	env.service.SetReliability(reliability.NewService(transaction.NewMemory(events), events))
	first, err := env.service.Login(ctx, "root-admin", "initial-password", AdminSessionMetadata{})
	if err != nil {
		t.Fatal(err)
	}
	if !first.RefreshExpiresAt.After(first.ExpiresAt) {
		t.Fatal("browser cookie must use actual refresh expiry")
	}
	second, err := env.service.Refresh(ctx, first.RefreshToken, AdminSessionMetadata{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := env.service.Refresh(ctx, first.RefreshToken, AdminSessionMetadata{}); !errors.Is(err, ErrAdminRefreshReuse) {
		t.Fatal("reuse denial", err)
	}
	if _, err := env.service.VerifyAccess(ctx, sessionIDFromToken(t, second.AccessToken)); !errors.Is(err, ErrAdminSessionInvalid) {
		t.Fatal("theft family revoke rolled back", err)
	}
	if _, err := env.service.Refresh(ctx, second.RefreshToken, AdminSessionMetadata{}); !errors.Is(err, ErrAdminRefreshInvalid) {
		t.Fatal("family refresh survived", err)
	}
	family, err := env.sessions.GetSessionByID(ctx, sessionIDFromToken(t, second.AccessToken))
	if err != nil || family.RevokedAt == nil || !family.UpdatedAt.Equal(*family.RevokedAt) {
		t.Fatal("Memory family revocation must retain the same complete timestamp semantics as PostgreSQL")
	}
	raw, _ := json.Marshal(events.Snapshot())
	for _, secret := range []string{first.RefreshToken, second.RefreshToken, first.AccessToken, second.AccessToken, "initial-password"} {
		if strings.Contains(string(raw), secret) {
			t.Fatal("raw credential persisted in audit or outbox")
		}
	}
	for _, mode := range []string{"expired", "inactive"} {
		fresh := newAdminSessionEnv()
		account := bootstrapAdmin(t, ctx, fresh.admins)
		store := reliability.NewMemoryStore()
		fresh.service.SetReliability(reliability.NewService(transaction.NewMemory(store), store))
		tokens, err := fresh.service.Login(ctx, "root-admin", "initial-password", AdminSessionMetadata{})
		if err != nil {
			t.Fatal(err)
		}
		if mode == "expired" {
			fresh.service.SetClock(func() time.Time { return time.Now().Add(31 * 24 * time.Hour) })
		} else {
			if _, _, err := fresh.admins.SetAccountStatusCAS(ctx, account.ID, 1, "suspended", "test", account.ID, time.Now()); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := fresh.service.Refresh(ctx, tokens.RefreshToken, AdminSessionMetadata{}); !errors.Is(err, ErrAdminRefreshInvalid) {
			t.Fatal(mode, err)
		}
		record, err := fresh.sessions.GetSessionByID(ctx, sessionIDFromToken(t, tokens.AccessToken))
		if err != nil || record.RevokedAt == nil {
			t.Fatal("security denial revoke rolled back", mode, err)
		}
	}
}

type adminSPARotationCASFailure struct {
	*repository.MemoryAdminSessionRepository
	called bool
}

func (s *adminSPARotationCASFailure) RevokeSessionCAS(context.Context, string, int64, string, time.Time) (repository.AdminSession, bool, error) {
	s.called = true
	return repository.AdminSession{}, false, nil
}
func TestAdminSPARefreshRequiredAuditOutboxAndFalseRotationCASReturnNoCredentials(t *testing.T) {
	ctx := context.Background()
	for _, failure := range []string{"audit", "outbox", "false_cas"} {
		t.Run(failure, func(t *testing.T) {
			e := newAdminSessionEnv()
			bootstrapAdmin(t, ctx, e.admins)
			events := reliability.NewMemoryStore()
			store := &adminSPAMFAStore{Store: events, events: failure == "outbox"}
			e.service.SetReliability(reliability.NewService(transaction.NewMemory(events), store))
			tokens, err := e.service.Login(ctx, "root-admin", "initial-password", AdminSessionMetadata{})
			if err != nil {
				t.Fatal(err)
			}
			memory := e.sessions.(*repository.MemoryAdminSessionRepository)
			var failed *adminSPARotationCASFailure
			if failure == "false_cas" {
				failed = &adminSPARotationCASFailure{MemoryAdminSessionRepository: memory}
				e.service.sessions = failed
			} else {
				store.fail = true
			}
			beforeSessions, beforeEvents := memory.Snapshot(), events.Snapshot()
			value, err := e.service.Refresh(ctx, tokens.RefreshToken, AdminSessionMetadata{})
			if err == nil || value != nil || failed != nil && !failed.called {
				t.Fatal("failed rotation returned a credential", failure, err)
			}
			if !reflect.DeepEqual(beforeSessions, memory.Snapshot()) || !reflect.DeepEqual(beforeEvents, events.Snapshot()) {
				t.Fatal("failed rotation left a partial family/audit/event")
			}
		})
	}
}
