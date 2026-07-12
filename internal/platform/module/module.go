package module

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

type Kind string

const (
	KindCore           Kind = "core"
	KindBuiltinFeature Kind = "builtin-feature"
)

type State string

const (
	StateRegistered State = "registered"
	StateDisabled   State = "disabled"
	StateStarting   State = "starting"
	StateRunning    State = "running"
	StateStopping   State = "stopping"
	StateStopped    State = "stopped"
	StateFailed     State = "failed"
)

type HealthStatus string

const (
	HealthUnknown   HealthStatus = "unknown"
	HealthHealthy   HealthStatus = "healthy"
	HealthDegraded  HealthStatus = "degraded"
	HealthUnhealthy HealthStatus = "unhealthy"
)

type Health struct {
	Status  HealthStatus `json:"status"`
	Message string       `json:"message,omitempty"`
}

type Module interface {
	ID() string
	Dependencies() []string
	Register(*AppContext) error
	Start(context.Context) error
	Stop(context.Context) error
	Health(context.Context) Health
}

// AppContext contains explicitly named module ports. External plugins never
// receive this context; they continue to use Host API and Extension Gateway.
type AppContext struct {
	mu     sync.RWMutex
	values map[string]interface{}
}

func NewAppContext() *AppContext {
	return &AppContext{values: make(map[string]interface{})}
}

func (c *AppContext) Provide(name string, value interface{}) error {
	name = strings.TrimSpace(name)
	if name == "" || value == nil {
		return errors.New("module port name and value are required")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, exists := c.values[name]; exists {
		return fmt.Errorf("module port %q is already registered", name)
	}
	c.values[name] = value
	return nil
}

func (c *AppContext) Lookup(name string) (interface{}, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	value, ok := c.values[name]
	return value, ok
}

type Snapshot struct {
	ID           string   `json:"id"`
	Kind         Kind     `json:"kind"`
	Enabled      bool     `json:"enabled"`
	State        State    `json:"state"`
	Dependencies []string `json:"dependencies,omitempty"`
	Health       Health   `json:"health"`
	Error        string   `json:"error,omitempty"`
}

type entry struct {
	module  Module
	kind    Kind
	enabled bool
	state   State
	err     error
}

type Registry struct {
	mu         sync.RWMutex
	app        *AppContext
	entries    map[string]*entry
	order      []string
	registered bool
	started    bool
}

func NewRegistry(app *AppContext) *Registry {
	if app == nil {
		app = NewAppContext()
	}
	return &Registry{app: app, entries: make(map[string]*entry)}
}

func (r *Registry) Add(mod Module, kind Kind, enabled bool) error {
	if mod == nil {
		return errors.New("module is required")
	}
	id := strings.TrimSpace(mod.ID())
	if id == "" {
		return errors.New("module ID is required")
	}
	if kind != KindCore && kind != KindBuiltinFeature {
		return fmt.Errorf("module %q has unsupported kind %q", id, kind)
	}
	if kind == KindCore && !enabled {
		return fmt.Errorf("core module %q cannot be disabled", id)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.registered || r.started {
		return errors.New("modules cannot be added after registry initialization")
	}
	if _, exists := r.entries[id]; exists {
		return fmt.Errorf("module %q is already registered", id)
	}
	state := StateRegistered
	if !enabled {
		state = StateDisabled
	}
	r.entries[id] = &entry{module: mod, kind: kind, enabled: enabled, state: state}
	return nil
}

func (r *Registry) RegisterAll() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.registered {
		return nil
	}
	order, err := r.resolveOrderLocked()
	if err != nil {
		return err
	}
	for _, id := range order {
		item := r.entries[id]
		if !item.enabled {
			continue
		}
		if err := item.module.Register(r.app); err != nil {
			item.state = StateFailed
			item.err = err
			return fmt.Errorf("register module %q: %w", id, err)
		}
	}
	r.order = order
	r.registered = true
	return nil
}

func (r *Registry) StartAll(ctx context.Context) error {
	if err := r.RegisterAll(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.started {
		return nil
	}
	started := make([]string, 0, len(r.order))
	for _, id := range r.order {
		item := r.entries[id]
		if !item.enabled {
			continue
		}
		item.state = StateStarting
		if err := item.module.Start(ctx); err != nil {
			item.state = StateFailed
			item.err = err
			rollbackErr := r.stopLocked(ctx, started)
			return errors.Join(fmt.Errorf("start module %q: %w", id, err), rollbackErr)
		}
		item.state = StateRunning
		item.err = nil
		started = append(started, id)
	}
	r.started = true
	return nil
}

func (r *Registry) StopAll(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.registered {
		return nil
	}
	err := r.stopLocked(ctx, r.order)
	r.started = false
	return err
}

func (r *Registry) stopLocked(ctx context.Context, ids []string) error {
	var stopErrors []error
	for i := len(ids) - 1; i >= 0; i-- {
		item := r.entries[ids[i]]
		if item == nil || !item.enabled || (item.state != StateRunning && item.state != StateStarting) {
			continue
		}
		item.state = StateStopping
		if err := item.module.Stop(ctx); err != nil {
			item.state = StateFailed
			item.err = err
			stopErrors = append(stopErrors, fmt.Errorf("stop module %q: %w", ids[i], err))
			continue
		}
		item.state = StateStopped
	}
	return errors.Join(stopErrors...)
}

func (r *Registry) Snapshots(ctx context.Context) []Snapshot {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ids := make([]string, 0, len(r.entries))
	for id := range r.entries {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	result := make([]Snapshot, 0, len(ids))
	for _, id := range ids {
		item := r.entries[id]
		health := Health{Status: HealthUnknown}
		if item.enabled && item.state == StateRunning {
			health = item.module.Health(ctx)
		}
		snapshot := Snapshot{
			ID:           id,
			Kind:         item.kind,
			Enabled:      item.enabled,
			State:        item.state,
			Dependencies: append([]string(nil), item.module.Dependencies()...),
			Health:       health,
		}
		if item.err != nil {
			snapshot.Error = item.err.Error()
		}
		result = append(result, snapshot)
	}
	return result
}

func (r *Registry) resolveOrderLocked() ([]string, error) {
	indegree := make(map[string]int, len(r.entries))
	dependents := make(map[string][]string, len(r.entries))
	for id, item := range r.entries {
		indegree[id] = 0
		seen := map[string]struct{}{}
		for _, rawDependency := range item.module.Dependencies() {
			dependency := strings.TrimSpace(rawDependency)
			if dependency == "" || dependency == id {
				return nil, fmt.Errorf("module %q has invalid dependency %q", id, rawDependency)
			}
			if _, duplicate := seen[dependency]; duplicate {
				continue
			}
			seen[dependency] = struct{}{}
			provider, exists := r.entries[dependency]
			if !exists {
				return nil, fmt.Errorf("module %q depends on missing module %q", id, dependency)
			}
			if item.enabled && !provider.enabled {
				return nil, fmt.Errorf("enabled module %q depends on disabled module %q", id, dependency)
			}
			indegree[id]++
			dependents[dependency] = append(dependents[dependency], id)
		}
	}
	ready := make([]string, 0, len(indegree))
	for id, degree := range indegree {
		if degree == 0 {
			ready = append(ready, id)
		}
	}
	sort.Strings(ready)
	order := make([]string, 0, len(r.entries))
	for len(ready) > 0 {
		id := ready[0]
		ready = ready[1:]
		order = append(order, id)
		sort.Strings(dependents[id])
		for _, dependent := range dependents[id] {
			indegree[dependent]--
			if indegree[dependent] == 0 {
				ready = append(ready, dependent)
				sort.Strings(ready)
			}
		}
	}
	if len(order) != len(r.entries) {
		return nil, errors.New("module dependency graph contains a cycle")
	}
	return order, nil
}
