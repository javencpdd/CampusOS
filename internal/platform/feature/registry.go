package feature

import (
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

type Registry struct {
	mu     sync.RWMutex
	defs   map[string]Definition
	states map[string]State
	legacy LegacySource
}

func NewRegistry(legacy LegacySource) *Registry {
	return &Registry{defs: map[string]Definition{}, states: map[string]State{}, legacy: legacy}
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
	enabled := def.Mode == AlwaysOn
	if !enabled && def.LegacyPlugin != "" && r.legacy != nil {
		enabled = r.legacy(def.LegacyPlugin)
	}
	r.states[def.ID] = State{Definition: def, Enabled: enabled, DesiredEnabled: enabled}
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

// SyncLegacy captures the compatibility source at process bootstrap. Restart
// features intentionally do not follow later runtime changes until restart.
func (r *Registry) SyncLegacy() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.legacy == nil {
		return
	}
	for id, state := range r.states {
		if state.Mode != AlwaysOn && state.LegacyPlugin != "" {
			enabled := r.legacy(state.LegacyPlugin)
			state.Enabled = enabled
			state.DesiredEnabled = enabled
			state.PendingRestart = false
			r.states[id] = state
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
	return state, nil
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
