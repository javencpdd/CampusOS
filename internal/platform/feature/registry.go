package feature

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
)

type ActivationMode string

const (
	AlwaysOn ActivationMode = "always-on"
	Restart  ActivationMode = "restart"
	HotGated ActivationMode = "hot-gated"
)

type Definition struct {
	ID           string         `json:"id"`
	Mode         ActivationMode `json:"activation_mode"`
	Dependencies []string       `json:"dependencies,omitempty"`
	LegacyPlugin string         `json:"legacy_plugin,omitempty"`
}

type State struct {
	Definition
	Enabled        bool `json:"enabled"`
	DesiredEnabled bool `json:"desired_enabled"`
	PendingRestart bool `json:"pending_restart"`
}

type LegacySource func(pluginID string) bool
type LegacyConfigSource func(featureID string) map[string]interface{}

type Registry struct {
	mu           sync.RWMutex
	defs         map[string]Definition
	states       map[string]State
	persisted    map[string]bool
	legacy       LegacySource
	legacyConfig LegacyConfigSource
	store        Store
}

func NewRegistry(legacy LegacySource) *Registry {
	return NewRegistryWithStoreAndConfig(legacy, nil, NewMemoryStore())
}

func NewRegistryWithStore(legacy LegacySource, store Store) *Registry {
	return NewRegistryWithStoreAndConfig(legacy, nil, store)
}

func NewRegistryWithStoreAndConfig(legacy LegacySource, legacyConfig LegacyConfigSource, store Store) *Registry {
	if store == nil {
		store = NewMemoryStore()
	}
	return &Registry{defs: map[string]Definition{}, states: map[string]State{}, persisted: map[string]bool{}, legacy: legacy, legacyConfig: legacyConfig, store: store}
}

func (r *Registry) Register(def Definition) error {
	if def.ID == "" {
		return errors.New("feature ID is required")
	}
	if def.Mode != AlwaysOn && def.Mode != Restart && def.Mode != HotGated {
		return fmt.Errorf("feature %q has invalid activation mode", def.ID)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.defs[def.ID]; ok {
		return fmt.Errorf("feature %q already registered", def.ID)
	}
	r.defs[def.ID] = def
	stored, found, err := r.store.Get(context.Background(), def.ID)
	if err != nil {
		return fmt.Errorf("load feature state %q: %w", def.ID, err)
	}
	if found {
		r.states[def.ID] = State{Definition: def, Enabled: stored.EffectiveEnabled, DesiredEnabled: stored.DesiredEnabled, PendingRestart: stored.PendingRestart}
		r.persisted[def.ID] = true
		return nil
	}
	enabled := def.Mode == AlwaysOn
	if !enabled && def.LegacyPlugin != "" && r.legacy != nil {
		enabled = r.legacy(def.LegacyPlugin)
	}
	r.states[def.ID] = State{Definition: def, Enabled: enabled, DesiredEnabled: enabled}
	r.persisted[def.ID] = false
	return nil
}

func (r *Registry) Enabled(id string) bool {
	r.mu.RLock()
	state, ok := r.states[id]
	r.mu.RUnlock()
	if !ok {
		return false
	}
	return state.Enabled
}

func (r *Registry) Get(id string) (State, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	state, ok := r.states[id]
	return state, ok
}

// SyncLegacy captures the compatibility source at process bootstrap. Restart
// features intentionally do not follow later runtime changes until restart.
func (r *Registry) SyncLegacy() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.legacy == nil {
		return
	}
	for id, state := range r.states {
		if state.Mode != AlwaysOn && state.LegacyPlugin != "" && !r.persisted[id] {
			enabled := r.legacy(state.LegacyPlugin)
			state.Enabled = enabled
			state.DesiredEnabled = enabled
			state.PendingRestart = false
			r.states[id] = state
			config := r.legacyFeatureConfig(id)
			if err := r.store.Save(context.Background(), StoredState{FeatureID: id, DesiredEnabled: state.DesiredEnabled, EffectiveEnabled: state.Enabled, PendingRestart: state.PendingRestart, Config: config}); err == nil {
				r.persisted[id] = true
			}
			continue
		}
		if state.LegacyPlugin != "" && r.persisted[id] {
			stored, found, err := r.store.Get(context.Background(), id)
			if err != nil || !found || len(stored.Config) != 0 {
				continue
			}
			legacyConfig := r.legacyFeatureConfig(id)
			if len(legacyConfig) == 0 {
				continue
			}
			_ = r.store.Save(context.Background(), StoredState{FeatureID: id, DesiredEnabled: state.DesiredEnabled, EffectiveEnabled: state.Enabled, PendingRestart: state.PendingRestart, Config: legacyConfig})
		}
	}
}

func (r *Registry) Request(id string, enabled bool) (State, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	state, ok := r.states[id]
	if !ok {
		return State{}, fmt.Errorf("feature %q not found", id)
	}
	if state.Mode == AlwaysOn && !enabled {
		return State{}, fmt.Errorf("always-on feature %q cannot be disabled", id)
	}
	state.DesiredEnabled = enabled
	if state.Mode == HotGated {
		state.Enabled = enabled
		state.PendingRestart = false
	} else if state.Mode == Restart {
		state.PendingRestart = state.Enabled != enabled
	}
	r.states[id] = state
	stored, _, err := r.store.Get(context.Background(), id)
	if err != nil {
		return State{}, fmt.Errorf("load feature config %q: %w", id, err)
	}
	if err := r.store.Save(context.Background(), StoredState{FeatureID: id, DesiredEnabled: state.DesiredEnabled, EffectiveEnabled: state.Enabled, PendingRestart: state.PendingRestart, Config: stored.Config}); err != nil {
		return State{}, fmt.Errorf("save feature state %q: %w", id, err)
	}
	r.persisted[id] = true
	return state, nil
}

// Config returns a copy of the authoritative Built-in Feature configuration.
// Legacy plugin manifests are only used once while seeding an empty record.
func (r *Registry) Config(id string) map[string]interface{} {
	r.mu.RLock()
	_, known := r.states[id]
	r.mu.RUnlock()
	if !known {
		return map[string]interface{}{}
	}
	stored, found, err := r.store.Get(context.Background(), id)
	if err != nil || !found {
		return map[string]interface{}{}
	}
	return cloneConfig(stored.Config)
}

func (r *Registry) UpdateConfig(id string, config map[string]interface{}) error {
	r.mu.RLock()
	state, ok := r.states[id]
	r.mu.RUnlock()
	if !ok {
		return fmt.Errorf("feature %q not found", id)
	}
	if err := r.store.Save(context.Background(), StoredState{FeatureID: id, DesiredEnabled: state.DesiredEnabled, EffectiveEnabled: state.Enabled, PendingRestart: state.PendingRestart, Config: cloneConfig(config)}); err != nil {
		return fmt.Errorf("save feature config %q: %w", id, err)
	}
	return nil
}

func (r *Registry) legacyFeatureConfig(id string) map[string]interface{} {
	if r.legacyConfig == nil {
		return map[string]interface{}{}
	}
	return cloneConfig(r.legacyConfig(id))
}

func (r *Registry) List() []State {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]State, 0, len(r.states))
	for _, state := range r.states {
		result = append(result, state)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}
