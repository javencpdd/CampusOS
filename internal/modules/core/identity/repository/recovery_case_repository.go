package repository

import (
	"context"
	"errors"
	"sort"
	"sync"

	"github.com/campusos/CampusOS/internal/modules/core/identity/domain"
)

var ErrRecoveryCaseNotFound = errors.New("identity recovery case not found")

// RecoveryCaseRepository owns the administrative recovery workflow state. It
// does not accept credentials, codes, or raw tickets; those remain in the
// dedicated Identity services and challenge store.
type RecoveryCaseRepository interface {
	Create(context.Context, *domain.RecoveryCase) error
	GetByPublicID(context.Context, string) (*domain.RecoveryCase, error)
	GetByPublicIDForUpdate(context.Context, string) (*domain.RecoveryCase, error)
	GetByChallengeIDForUpdate(context.Context, string) (*domain.RecoveryCase, error)
	Update(context.Context, *domain.RecoveryCase) error
	List(context.Context, int) ([]*domain.RecoveryCase, error)
}

type MemoryRecoveryCaseRepository struct {
	mu          sync.RWMutex
	byID        map[string]domain.RecoveryCase
	public      map[string]string
	byChallenge map[string]string
}

func NewMemoryRecoveryCaseRepository() *MemoryRecoveryCaseRepository {
	return &MemoryRecoveryCaseRepository{
		byID:        make(map[string]domain.RecoveryCase),
		public:      make(map[string]string),
		byChallenge: make(map[string]string),
	}
}

func (r *MemoryRecoveryCaseRepository) Create(_ context.Context, value *domain.RecoveryCase) error {
	if value == nil || value.ID == "" || value.PublicID == "" || value.UserID == "" || value.AccountID == "" || value.ChallengeID == "" {
		return errors.New("identity recovery case requires ids")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.byID[value.ID]; exists {
		return errors.New("identity recovery case id already exists")
	}
	if _, exists := r.public[value.PublicID]; exists {
		return errors.New("identity recovery case public id already exists")
	}
	if _, exists := r.byChallenge[value.ChallengeID]; exists {
		return errors.New("identity recovery case challenge is already bound")
	}
	r.byID[value.ID] = cloneRecoveryCase(*value)
	r.public[value.PublicID] = value.ID
	r.byChallenge[value.ChallengeID] = value.ID
	return nil
}

func (r *MemoryRecoveryCaseRepository) GetByPublicID(_ context.Context, publicID string) (*domain.RecoveryCase, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	id, exists := r.public[publicID]
	if !exists {
		return nil, ErrRecoveryCaseNotFound
	}
	value, exists := r.byID[id]
	if !exists {
		return nil, ErrRecoveryCaseNotFound
	}
	copy := cloneRecoveryCase(value)
	return &copy, nil
}

func (r *MemoryRecoveryCaseRepository) GetByPublicIDForUpdate(ctx context.Context, publicID string) (*domain.RecoveryCase, error) {
	return r.GetByPublicID(ctx, publicID)
}

func (r *MemoryRecoveryCaseRepository) GetByChallengeIDForUpdate(_ context.Context, challengeID string) (*domain.RecoveryCase, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	id, exists := r.byChallenge[challengeID]
	if !exists {
		return nil, ErrRecoveryCaseNotFound
	}
	value, exists := r.byID[id]
	if !exists {
		return nil, ErrRecoveryCaseNotFound
	}
	copy := cloneRecoveryCase(value)
	return &copy, nil
}

func (r *MemoryRecoveryCaseRepository) Update(_ context.Context, value *domain.RecoveryCase) error {
	if value == nil || value.ID == "" {
		return ErrRecoveryCaseNotFound
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.byID[value.ID]; !exists {
		return ErrRecoveryCaseNotFound
	}
	r.byID[value.ID] = cloneRecoveryCase(*value)
	return nil
}

func (r *MemoryRecoveryCaseRepository) List(_ context.Context, limit int) ([]*domain.RecoveryCase, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	items := make([]*domain.RecoveryCase, 0, len(r.byID))
	for _, value := range r.byID {
		copy := cloneRecoveryCase(value)
		items = append(items, &copy)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.After(items[j].CreatedAt) })
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func (r *MemoryRecoveryCaseRepository) Snapshot() any {
	r.mu.RLock()
	defer r.mu.RUnlock()
	state := memoryRecoveryCaseSnapshot{
		byID:        make(map[string]domain.RecoveryCase, len(r.byID)),
		public:      make(map[string]string, len(r.public)),
		byChallenge: make(map[string]string, len(r.byChallenge)),
	}
	for id, value := range r.byID {
		state.byID[id] = cloneRecoveryCase(value)
	}
	for publicID, id := range r.public {
		state.public[publicID] = id
	}
	for challengeID, id := range r.byChallenge {
		state.byChallenge[challengeID] = id
	}
	return state
}

func (r *MemoryRecoveryCaseRepository) Restore(snapshot any) {
	state, ok := snapshot.(memoryRecoveryCaseSnapshot)
	if !ok {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byID = make(map[string]domain.RecoveryCase, len(state.byID))
	for id, value := range state.byID {
		r.byID[id] = cloneRecoveryCase(value)
	}
	r.public = make(map[string]string, len(state.public))
	for publicID, id := range state.public {
		r.public[publicID] = id
	}
	r.byChallenge = make(map[string]string, len(state.byChallenge))
	for challengeID, id := range state.byChallenge {
		r.byChallenge[challengeID] = id
	}
}

type memoryRecoveryCaseSnapshot struct {
	byID        map[string]domain.RecoveryCase
	public      map[string]string
	byChallenge map[string]string
}

func cloneRecoveryCase(value domain.RecoveryCase) domain.RecoveryCase {
	copy := value
	if value.CompletedAt != nil {
		point := *value.CompletedAt
		copy.CompletedAt = &point
	}
	if value.CancelledAt != nil {
		point := *value.CancelledAt
		copy.CancelledAt = &point
	}
	return copy
}

var _ RecoveryCaseRepository = (*MemoryRecoveryCaseRepository)(nil)
var _ interface {
	Snapshot() any
	Restore(any)
} = (*MemoryRecoveryCaseRepository)(nil)
