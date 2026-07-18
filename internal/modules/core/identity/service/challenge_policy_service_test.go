package service

import (
	"context"
	"errors"
	"testing"

	"github.com/campusos/CampusOS/internal/modules/core/identity/domain"
	"github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
)

func TestChallengePolicyUpdateIsVersionedAndAudited(t *testing.T) {
	store := repository.NewMemoryChallengePolicyRepository()
	service, err := NewChallengePolicyService(store)
	if err != nil {
		t.Fatal(err)
	}
	reliable := reliability.NewService(transaction.NewMemory(), reliability.NewMemoryStore())
	service.SetReliability(reliable)

	updated, err := service.UpdateChallengePolicy(context.Background(), "9001", domain.UpdateChallengePolicyRequest{
		EmailWindowMinutes: 5, EmailMaxRequests: 2, IPWindowMinutes: 30, IPMaxRequests: 20, ExpectedVersion: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Version != 2 || updated.EmailWindowMinutes != 5 || updated.EmailMaxRequests != 2 {
		t.Fatalf("unexpected updated policy: %#v", updated)
	}
	events, err := reliable.List(context.Background(), reliability.EventFilter{Type: "identity.challenge.policy.updated.v1", Limit: 10})
	if err != nil || len(events) != 1 {
		t.Fatalf("policy events=%#v err=%v", events, err)
	}

	_, err = service.UpdateChallengePolicy(context.Background(), "9001", domain.UpdateChallengePolicyRequest{
		EmailWindowMinutes: 10, EmailMaxRequests: 5, IPWindowMinutes: 60, IPMaxRequests: 10, ExpectedVersion: 1,
	})
	if !errors.Is(err, repository.ErrChallengePolicyVersionConflict) {
		t.Fatalf("stale update error=%v", err)
	}
	current, err := service.GetChallengePolicy(context.Background())
	if err != nil || current.Version != 2 || current.EmailMaxRequests != 2 {
		t.Fatalf("policy changed after conflict: policy=%#v err=%v", current, err)
	}
}

func TestChallengePolicyCannotDisableProtection(t *testing.T) {
	service, err := NewChallengePolicyService(repository.NewMemoryChallengePolicyRepository())
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.UpdateChallengePolicy(context.Background(), "9001", domain.UpdateChallengePolicyRequest{
		EmailWindowMinutes: 5, EmailMaxRequests: 0, IPWindowMinutes: 60, IPMaxRequests: 10, ExpectedVersion: 1,
	})
	if !errors.Is(err, ErrChallengePolicyInvalid) {
		t.Fatalf("disabled email protection error=%v", err)
	}
}
