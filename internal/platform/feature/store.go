package feature

import (
	"context"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type StoredState struct {
	FeatureID        string
	DesiredEnabled   bool
	EffectiveEnabled bool
	PendingRestart   bool
	UpdatedAt        time.Time
}

// Store persists Built-in Feature state independently from the external
// Plugin Manager. Legacy plugin records are only a bootstrap compatibility
// source while a feature has no persisted state.
type Store interface {
	Get(context.Context, string) (StoredState, bool, error)
	Save(context.Context, StoredState) error
}

type MemoryStore struct {
	mu     sync.RWMutex
	states map[string]StoredState
}

func NewMemoryStore() *MemoryStore { return &MemoryStore{states: map[string]StoredState{}} }

func (s *MemoryStore) Get(_ context.Context, id string) (StoredState, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	state, ok := s.states[id]
	return state, ok, nil
}

func (s *MemoryStore) Save(_ context.Context, state StoredState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	state.UpdatedAt = time.Now().UTC()
	s.states[state.FeatureID] = state
	return nil
}

type PostgreSQLStore struct{ pool *pgxpool.Pool }

func NewPostgreSQLStore(pool *pgxpool.Pool) *PostgreSQLStore { return &PostgreSQLStore{pool: pool} }

func (s *PostgreSQLStore) Get(ctx context.Context, id string) (StoredState, bool, error) {
	var state StoredState
	err := s.pool.QueryRow(ctx, `SELECT feature_id, desired_enabled, effective_enabled, pending_restart, updated_at
		FROM builtin_feature_states WHERE feature_id = $1`, id).Scan(
		&state.FeatureID, &state.DesiredEnabled, &state.EffectiveEnabled, &state.PendingRestart, &state.UpdatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return StoredState{}, false, nil
		}
		return StoredState{}, false, err
	}
	return state, true, nil
}

func (s *PostgreSQLStore) Save(ctx context.Context, state StoredState) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO builtin_feature_states
		(feature_id, desired_enabled, effective_enabled, pending_restart, updated_at)
		VALUES ($1, $2, $3, $4, NOW())
		ON CONFLICT (feature_id) DO UPDATE SET
		desired_enabled = EXCLUDED.desired_enabled,
		effective_enabled = EXCLUDED.effective_enabled,
		pending_restart = EXCLUDED.pending_restart,
		updated_at = NOW()`, state.FeatureID, state.DesiredEnabled, state.EffectiveEnabled, state.PendingRestart)
	return err
}
