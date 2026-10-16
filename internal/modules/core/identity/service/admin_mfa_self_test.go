package service

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
)

func adminSPAMFASecret(t *testing.T, e *adminMFAEnv, id string) []byte {
	t.Helper()
	m, err := e.security.GetTOTP(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	value, err := e.service.protector.Open(m.KeyID, m.Nonce, m.Ciphertext)
	if err != nil {
		t.Fatal(err)
	}
	return value
}
func TestAdminSPAPasswordSelfMFAOwnSessionReplayAndActiveFactorReplacement(t *testing.T) {
	ctx := context.Background()
	e := newAdminMFAEnv(t)
	account, actor := e.bootstrapRoot(t, ctx)
	now := time.Now().UTC().Truncate(time.Microsecond)
	e.service.SetClock(func() time.Time { return now })
	if actor.MFAAuthenticated {
		t.Fatal("fixture must prove password-to-MFA authentication")
	}
	if _, err := e.service.StartEnrollment(ctx, actor, "CampusOS"); err != nil {
		t.Fatal(err)
	}
	secret := adminSPAMFASecret(t, e, account.ID)
	step := now.Unix() / 30
	if err := e.service.ConfirmEnrollment(ctx, actor, totpCode(secret, step)); err != nil {
		t.Fatal(err)
	}
	if err := e.service.VerifyStepUp(ctx, actor, actor.CredentialID, totpCode(secret, step)); !errors.Is(err, ErrAdminMFAReplay) {
		t.Fatal("confirming code replay", err)
	}
	if err := e.service.VerifyStepUp(ctx, actor, actor.CredentialID, totpCode(secret, step+1)); err != nil {
		t.Fatal("actual password session could not step up", err)
	}
	current := NewAdminSessionService(e.admins, e.sessions, nil)
	current.SetClock(func() time.Time { return now })
	proof, err := current.CurrentAdminSession(ctx, account.ID, actor.CredentialID)
	if err != nil || proof.AuthenticationStrength != "mfa" {
		t.Fatal(proof, err)
	}
	if err := e.sessions.CreateSession(ctx, repository.AdminSession{ID: "password-only", AccountID: account.ID, RefreshTokenDigest: strings.Repeat("c", 64), TokenFamilyID: "password-only", AuthenticationStrength: "password", CreatedAt: now, ExpiresAt: now.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	before := e.security.(*repository.MemoryAdminSecurityRepository).Snapshot()
	forged := AdminActor{ID: account.ID, CredentialID: "password-only", MFAAuthenticated: true}
	if _, err := e.service.StartEnrollment(ctx, forged, "CampusOS"); !errors.Is(err, ErrAdminMFAActor) {
		t.Fatal("caller bool replaced active factor", err)
	}
	wrong := actor
	wrong.ID = "adm.other"
	if err := e.service.VerifyStepUp(ctx, wrong, actor.CredentialID, totpCode(secret, step+1)); !errors.Is(err, ErrAdminMFAActor) {
		t.Fatal("cross account session", err)
	}
	if !reflect.DeepEqual(before, e.security.(*repository.MemoryAdminSecurityRepository).Snapshot()) {
		t.Fatal("denial changed factor")
	}
}

type adminSPAMFAStore struct {
	reliability.Store
	fail, events bool
}

func (s *adminSPAMFAStore) RecordCommandAudit(ctx context.Context, a reliability.CommandAudit) error {
	if s.fail && !s.events {
		return errors.New("required admin MFA audit failed")
	}
	return s.Store.RecordCommandAudit(ctx, a)
}
func (s *adminSPAMFAStore) Enqueue(ctx context.Context, e *reliability.Event) (*reliability.Event, error) {
	if s.fail && s.events {
		return nil, errors.New("required admin MFA event failed")
	}
	return s.Store.Enqueue(ctx, e)
}

type adminSPAMFASessionCASFailure struct {
	*repository.MemoryAdminSessionRepository
	called bool
}

func (s *adminSPAMFASessionCASFailure) UpgradeSessionStrengthCAS(context.Context, string, int64, string, time.Time, time.Time) (repository.AdminSession, bool, error) {
	s.called = true
	return repository.AdminSession{}, false, nil
}
func TestAdminSPAMFASelfRequiredAuditOutboxAndFailedSessionCASRollbackAll(t *testing.T) {
	ctx := context.Background()
	for _, op := range []string{"enroll", "confirm", "step_up"} {
		for _, failure := range []string{"audit", "outbox", "session_cas"} {
			if failure == "session_cas" && op != "step_up" {
				continue
			}
			t.Run(op+"/"+failure, func(t *testing.T) {
				e := newAdminMFAEnv(t)
				account, actor := e.bootstrapRoot(t, ctx)
				now := time.Now().UTC().Truncate(time.Microsecond)
				e.service.SetClock(func() time.Time { return now })
				events := reliability.NewMemoryStore()
				store := &adminSPAMFAStore{Store: events, events: failure == "outbox"}
				e.service.SetReliability(reliability.NewService(transaction.NewMemory(events), store))
				var secret []byte
				step := now.Unix() / 30
				if op != "enroll" {
					if _, err := e.service.StartEnrollment(ctx, actor, "CampusOS"); err != nil {
						t.Fatal(err)
					}
					secret = adminSPAMFASecret(t, e, account.ID)
				}
				if op == "step_up" {
					if err := e.service.ConfirmEnrollment(ctx, actor, totpCode(secret, step)); err != nil {
						t.Fatal(err)
					}
				}
				var failedCAS *adminSPAMFASessionCASFailure
				if failure == "session_cas" {
					failedCAS = &adminSPAMFASessionCASFailure{MemoryAdminSessionRepository: e.sessions.(*repository.MemoryAdminSessionRepository)}
					e.service.sessions = failedCAS
				} else {
					store.fail = true
				}
				beforeSecurity := e.security.(*repository.MemoryAdminSecurityRepository).Snapshot()
				beforeSessions := e.sessions.(*repository.MemoryAdminSessionRepository).Snapshot()
				beforeEvents := events.Snapshot()
				var err error
				switch op {
				case "enroll":
					var uri string
					uri, err = e.service.StartEnrollment(ctx, actor, "CampusOS")
					if uri != "" {
						t.Fatal("failed enrollment returned secret")
					}
				case "confirm":
					err = e.service.ConfirmEnrollment(ctx, actor, totpCode(secret, step))
				case "step_up":
					err = e.service.VerifyStepUp(ctx, actor, actor.CredentialID, totpCode(secret, step+1))
				}
				if err == nil || failedCAS != nil && !failedCAS.called {
					t.Fatal("required failure did not execute", op, failure, err)
				}
				if !reflect.DeepEqual(beforeSecurity, e.security.(*repository.MemoryAdminSecurityRepository).Snapshot()) || !reflect.DeepEqual(beforeSessions, e.sessions.(*repository.MemoryAdminSessionRepository).Snapshot()) || !reflect.DeepEqual(beforeEvents, events.Snapshot()) {
					t.Fatal("partial factor/strength/audit/event survived")
				}
			})
		}
	}
}
