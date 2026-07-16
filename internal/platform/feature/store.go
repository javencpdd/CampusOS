package feature

import (
	"context"
	"encoding/json"
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
	Config           map[string]interface{}
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
	state.Config = cloneConfig(state.Config)
	s.states[state.FeatureID] = state
	return nil
}

type PostgreSQLStore struct{ pool *pgxpool.Pool }

func NewPostgreSQLStore(pool *pgxpool.Pool) *PostgreSQLStore { return &PostgreSQLStore{pool: pool} }

func (s *PostgreSQLStore) Get(ctx context.Context, id string) (StoredState, bool, error) {
	var state StoredState
	var rawConfig []byte
	err := s.pool.QueryRow(ctx, `SELECT feature_id, desired_enabled, effective_enabled, pending_restart, updated_at, config
		FROM builtin_feature_states WHERE feature_id = $1`, id).Scan(
		&state.FeatureID, &state.DesiredEnabled, &state.EffectiveEnabled, &state.PendingRestart, &state.UpdatedAt, &rawConfig,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return StoredState{}, false, nil
		}
		return StoredState{}, false, err
	}
	if len(rawConfig) > 0 && string(rawConfig) != "null" {
		if err := json.Unmarshal(rawConfig, &state.Config); err != nil {
			return StoredState{}, false, err
		}
	}
	return state, true, nil
}

func (s *PostgreSQLStore) Save(ctx context.Context, state StoredState) error {
	config, err := json.Marshal(state.Config)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `INSERT INTO builtin_feature_states
		(feature_id, desired_enabled, effective_enabled, pending_restart, updated_at, config)
		VALUES ($1, $2, $3, $4, NOW(), $5::jsonb)
		ON CONFLICT (feature_id) DO UPDATE SET
		desired_enabled = EXCLUDED.desired_enabled,
		effective_enabled = EXCLUDED.effective_enabled,
		pending_restart = EXCLUDED.pending_restart,
		config = EXCLUDED.config,
		updated_at = NOW()`, state.FeatureID, state.DesiredEnabled, state.EffectiveEnabled, state.PendingRestart, string(config))
	return err
}

func cloneConfig(config map[string]interface{}) map[string]interface{} {
	if config == nil {
		return map[string]interface{}{}
	}
	copy := make(map[string]interface{}, len(config))
	for key, value := range config {
		copy[key] = cloneConfigValue(value)
	}
	return copy
}

func cloneConfigValue(value interface{}) interface{} {
	switch typed := value.(type) {
	case map[string]interface{}:
		return cloneConfig(typed)
	case []interface{}:
		copy := make([]interface{}, len(typed))
		for index := range typed {
			copy[index] = cloneConfigValue(typed[index])
		}
		return copy
	case []string:
		return append([]string(nil), typed...)
	default:
		return typed
	}
}
