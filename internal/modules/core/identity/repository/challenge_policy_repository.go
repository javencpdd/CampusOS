package repository

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/identity/domain"
)

var (
	ErrChallengePolicyNotFound        = errors.New("challenge policy not found")
	ErrChallengePolicyVersionConflict = errors.New("challenge policy version conflict")
)

type ChallengePolicyRepository interface {
	GetChallengePolicy(context.Context) (*domain.ChallengePolicy, error)
	UpdateChallengePolicy(context.Context, *domain.ChallengePolicy, int64) error
}

type MemoryChallengePolicyRepository struct {
	mu     sync.RWMutex
	policy domain.ChallengePolicy
}

func NewMemoryChallengePolicyRepository() *MemoryChallengePolicyRepository {
	policy := domain.DefaultChallengePolicy()
	policy.UpdatedAt = time.Now().UTC()
	return &MemoryChallengePolicyRepository{policy: policy}
}

func (r *MemoryChallengePolicyRepository) GetChallengePolicy(context.Context) (*domain.ChallengePolicy, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	copy := r.policy
	return &copy, nil
}

func (r *MemoryChallengePolicyRepository) UpdateChallengePolicy(_ context.Context, policy *domain.ChallengePolicy, expectedVersion int64) error {
	if policy == nil || policy.ID != domain.ChallengePolicyID {
		return ErrChallengePolicyNotFound
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.policy.Version != expectedVersion {
		return ErrChallengePolicyVersionConflict
	}
	copy := *policy
	copy.Version = expectedVersion + 1
	r.policy = copy
	policy.Version = copy.Version
	return nil
}

func (r *MemoryChallengePolicyRepository) Snapshot() any {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.policy
}

func (r *MemoryChallengePolicyRepository) Restore(value any) {
	policy, ok := value.(domain.ChallengePolicy)
	if !ok {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.policy = policy
}

var _ ChallengePolicyRepository = (*MemoryChallengePolicyRepository)(nil)
