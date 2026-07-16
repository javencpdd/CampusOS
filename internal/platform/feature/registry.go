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
	ID             string                 `json:"id"`
	Mode           ActivationMode         `json:"activation_mode"`
	Dependencies   []string               `json:"dependencies,omitempty"`
	DefaultEnabled bool                   `json:"default_enabled"`
	DefaultConfig  map[string]interface{} `json:"-"`
	LegacyPlugin   string                 `json:"legacy_plugin,omitempty"`
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

// NewAuthoritativeRegistry creates the runtime registry used by CampusOS.
// Legacy plugin state is deliberately not wired into this constructor. A
// compatibility bootstrap may call SeedLegacy once after manifests have been
// discovered, but all later reads and writes go through Store.
func NewAuthoritativeRegistry(store Store) *Registry {
	return NewRegistryWithStoreAndConfig(nil, nil, store)
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
		config := stored.Config
		if len(config) == 0 && len(def.DefaultConfig) > 0 {
			config = cloneConfig(def.DefaultConfig)
		}
		state := State{Definition: def, Enabled: stored.EffectiveEnabled, DesiredEnabled: stored.DesiredEnabled, PendingRestart: stored.PendingRestart}
		switch def.Mode {
		case AlwaysOn:
			state.Enabled = true
			state.DesiredEnabled = true
			state.PendingRestart = false
		case Restart:
			// Register runs once during process bootstrap, which is exactly when a
			// staged restart feature becomes effective.
			state.Enabled = state.DesiredEnabled
			state.PendingRestart = false
		case HotGated:
			state.Enabled = state.DesiredEnabled
			state.PendingRestart = false
		}
		r.states[def.ID] = state
		r.persisted[def.ID] = true
		if state.Enabled != stored.EffectiveEnabled || state.PendingRestart != stored.PendingRestart || state.DesiredEnabled != stored.DesiredEnabled {
			if err := r.store.Save(context.Background(), StoredState{FeatureID: def.ID, DesiredEnabled: state.DesiredEnabled, EffectiveEnabled: state.Enabled, PendingRestart: state.PendingRestart, Config: config}); err != nil {
				return fmt.Errorf("activate feature state %q: %w", def.ID, err)
			}
		} else if len(stored.Config) == 0 && len(config) > 0 {
			if err := r.store.Save(context.Background(), StoredState{FeatureID: def.ID, DesiredEnabled: state.DesiredEnabled, EffectiveEnabled: state.Enabled, PendingRestart: state.PendingRestart, Config: config}); err != nil {
				return fmt.Errorf("seed feature default config %q: %w", def.ID, err)
			}
		}
		return nil
	}
	enabled := def.Mode == AlwaysOn || def.DefaultEnabled
	if !enabled && def.LegacyPlugin != "" && r.legacy != nil {
		enabled = r.legacy(def.LegacyPlugin)
	}
	r.states[def.ID] = State{Definition: def, Enabled: enabled, DesiredEnabled: enabled}
	r.persisted[def.ID] = false
	if def.Mode == AlwaysOn || def.DefaultEnabled || len(def.DefaultConfig) > 0 {
		if err := r.store.Save(context.Background(), StoredState{FeatureID: def.ID, DesiredEnabled: enabled, EffectiveEnabled: enabled, Config: cloneConfig(def.DefaultConfig)}); err != nil {
			return fmt.Errorf("save feature default state %q: %w", def.ID, err)
		}
		r.persisted[def.ID] = true
	}
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
		if state.LegacyPlugin == "" {
			continue
		}
		_ = r.seedLegacyLocked(id, r.legacy(state.LegacyPlugin), r.legacyFeatureConfig(id))
	}
}

// SeedLegacy imports one historical builtin plugin state into an empty Feature
// Store record. Existing Feature state is never overwritten; an empty config
// created by the state-only migration may be filled once.
func (r *Registry) SeedLegacy(id string, enabled bool, config map[string]interface{}) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.seedLegacyLocked(id, enabled, config)
}

func (r *Registry) seedLegacyLocked(id string, enabled bool, config map[string]interface{}) error {
	state, ok := r.states[id]
	if !ok {
		return fmt.Errorf("feature %q not found", id)
	}
	stored, found, err := r.store.Get(context.Background(), id)
	if err != nil {
		return fmt.Errorf("load feature state %q for legacy seed: %w", id, err)
	}
	if r.persisted[id] || found {
		if !found || len(stored.Config) != 0 || len(config) == 0 {
			return nil
		}
		stored.Config = cloneConfig(config)
		if err := r.store.Save(context.Background(), stored); err != nil {
			return fmt.Errorf("seed feature config %q: %w", id, err)
		}
		return nil
	}
	if state.Mode == AlwaysOn {
		enabled = true
	}
	state.Enabled = enabled
	state.DesiredEnabled = enabled
	state.PendingRestart = false
	if err := r.store.Save(context.Background(), StoredState{FeatureID: id, DesiredEnabled: enabled, EffectiveEnabled: enabled, PendingRestart: false, Config: cloneConfig(config)}); err != nil {
		return fmt.Errorf("seed feature state %q: %w", id, err)
	}
	r.states[id] = state
	r.persisted[id] = true
	return nil
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
	r.mu.Lock()
	defer r.mu.Unlock()
	state, ok := r.states[id]
	if !ok {
		return fmt.Errorf("feature %q not found", id)
	}
	if err := r.store.Save(context.Background(), StoredState{FeatureID: id, DesiredEnabled: state.DesiredEnabled, EffectiveEnabled: state.Enabled, PendingRestart: state.PendingRestart, Config: cloneConfig(config)}); err != nil {
		return fmt.Errorf("save feature config %q: %w", id, err)
	}
	r.persisted[id] = true
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
