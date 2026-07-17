package repository

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/identity/domain"
)

var ErrChallengeNotFound = errors.New("email challenge not found")

// ChallengeRepository owns only opaque challenge state. It never accepts a
// verification code or raw ticket, so those values cannot accidentally enter
// a SQL query, log record, or repository snapshot.
type ChallengeRepository interface {
	TryConsumeRate(context.Context, []domain.ChallengeRateWindow) (bool, error)
	CreateChallenge(context.Context, *domain.EmailChallenge) error
	GetChallengeForUpdate(context.Context, string) (*domain.EmailChallenge, error)
	GetChallenge(context.Context, string) (*domain.EmailChallenge, error)
	GetChallengeByID(context.Context, string) (*domain.EmailChallenge, error)
	UpdateChallenge(context.Context, *domain.EmailChallenge) error
}

type MemoryChallengeRepository struct {
	mu         sync.RWMutex
	challenges map[string]domain.EmailChallenge
	byPublicID map[string]string
	rates      map[string]int
}

func NewMemoryChallengeRepository() *MemoryChallengeRepository {
	return &MemoryChallengeRepository{
		challenges: make(map[string]domain.EmailChallenge),
		byPublicID: make(map[string]string),
		rates:      make(map[string]int),
	}
}

func (r *MemoryChallengeRepository) TryConsumeRate(_ context.Context, windows []domain.ChallengeRateWindow) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, window := range windows {
		if window.Limit <= 0 || window.Scope == "" || window.SubjectDigest == "" || window.WindowStart.IsZero() {
			return false, errors.New("invalid challenge rate window")
		}
		if r.rates[rateKey(window)] >= window.Limit {
			return false, nil
		}
	}
	for _, window := range windows {
		r.rates[rateKey(window)]++
	}
	return true, nil
}

func (r *MemoryChallengeRepository) CreateChallenge(_ context.Context, challenge *domain.EmailChallenge) error {
	if challenge == nil || challenge.ID == "" || challenge.PublicID == "" {
		return errors.New("challenge id and public id are required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.challenges[challenge.ID]; exists {
		return fmt.Errorf("challenge id already exists")
	}
	if _, exists := r.byPublicID[challenge.PublicID]; exists {
		return fmt.Errorf("challenge public id already exists")
	}
	r.challenges[challenge.ID] = cloneChallenge(*challenge)
	r.byPublicID[challenge.PublicID] = challenge.ID
	return nil
}

func (r *MemoryChallengeRepository) GetChallengeForUpdate(ctx context.Context, publicID string) (*domain.EmailChallenge, error) {
	return r.GetChallenge(ctx, publicID)
}

func (r *MemoryChallengeRepository) GetChallenge(_ context.Context, publicID string) (*domain.EmailChallenge, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	id, exists := r.byPublicID[publicID]
	if !exists {
		return nil, ErrChallengeNotFound
	}
	challenge, exists := r.challenges[id]
	if !exists {
		return nil, ErrChallengeNotFound
	}
	copy := cloneChallenge(challenge)
	return &copy, nil
}

func (r *MemoryChallengeRepository) GetChallengeByID(_ context.Context, id string) (*domain.EmailChallenge, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	challenge, exists := r.challenges[id]
	if !exists {
		return nil, ErrChallengeNotFound
	}
	copy := cloneChallenge(challenge)
	return &copy, nil
}

func (r *MemoryChallengeRepository) UpdateChallenge(_ context.Context, challenge *domain.EmailChallenge) error {
	if challenge == nil || challenge.ID == "" {
		return errors.New("challenge id is required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.challenges[challenge.ID]; !exists {
		return ErrChallengeNotFound
	}
	r.challenges[challenge.ID] = cloneChallenge(*challenge)
	return nil
}

func (r *MemoryChallengeRepository) Snapshot() any {
	r.mu.RLock()
	defer r.mu.RUnlock()
	state := memoryChallengeSnapshot{
		challenges: make(map[string]domain.EmailChallenge, len(r.challenges)),
		byPublicID: make(map[string]string, len(r.byPublicID)),
		rates:      make(map[string]int, len(r.rates)),
	}
	for id, challenge := range r.challenges {
		state.challenges[id] = cloneChallenge(challenge)
	}
	for key, value := range r.byPublicID {
		state.byPublicID[key] = value
	}
	for key, value := range r.rates {
		state.rates[key] = value
	}
	return state
}

func (r *MemoryChallengeRepository) Restore(value any) {
	state, ok := value.(memoryChallengeSnapshot)
	if !ok {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.challenges = state.challenges
	r.byPublicID = state.byPublicID
	r.rates = state.rates
}

type memoryChallengeSnapshot struct {
	challenges map[string]domain.EmailChallenge
	byPublicID map[string]string
	rates      map[string]int
}

func rateKey(window domain.ChallengeRateWindow) string {
	return window.Scope + "\x00" + window.SubjectDigest + "\x00" + window.WindowStart.UTC().Format(time.RFC3339Nano)
}

func cloneChallenge(value domain.EmailChallenge) domain.EmailChallenge {
	copy := value
	if value.VerifiedAt != nil {
		point := *value.VerifiedAt
		copy.VerifiedAt = &point
	}
	if value.TicketExpiresAt != nil {
		point := *value.TicketExpiresAt
		copy.TicketExpiresAt = &point
	}
	if value.ConsumedAt != nil {
		point := *value.ConsumedAt
		copy.ConsumedAt = &point
	}
	if value.InvalidatedAt != nil {
		point := *value.InvalidatedAt
		copy.InvalidatedAt = &point
	}
	return copy
}

// ChallengeRateSnapshot is test-only inspection without exposing challenge
// values to normal callers.
func (r *MemoryChallengeRepository) ChallengeRateSnapshot() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	keys := make([]string, 0, len(r.rates))
	for key := range r.rates {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
