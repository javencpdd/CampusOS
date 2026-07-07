package builtin

import (
	"context"
	"fmt"
	"sync"

	"github.com/campusos/CampusOS/internal/plugin"
)

// Runtime is a no-op runtime for CampusOS features that ship as built-in
// plugin metadata and static assets instead of external plugin processes.
type Runtime struct {
	mu      sync.RWMutex
	running map[string]struct{}
}

func NewRuntime() *Runtime {
	return &Runtime{
		running: make(map[string]struct{}),
	}
}

func (r *Runtime) Type() string {
	return "builtin"
}

func (r *Runtime) Start(_ context.Context, p *plugin.Plugin) error {
	if p == nil || p.Manifest == nil {
		return fmt.Errorf("plugin is required")
	}
	if p.Manifest.Runtime != r.Type() {
		return fmt.Errorf("plugin runtime is not builtin: %s", p.Manifest.Runtime)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.running[p.Manifest.Name] = struct{}{}
	return nil
}

func (r *Runtime) Stop(_ context.Context, pluginName string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.running, pluginName)
	return nil
}

func (r *Runtime) SendEvent(_ context.Context, pluginName string, _ *plugin.EventMessage) (*plugin.PluginResponse, error) {
	if err := r.HealthCheck(context.Background(), pluginName); err != nil {
		return nil, err
	}
	return &plugin.PluginResponse{Allowed: true}, nil
}

func (r *Runtime) HealthCheck(_ context.Context, pluginName string) error {
	if !r.IsRunning(pluginName) {
		return fmt.Errorf("builtin plugin is not running: %s", pluginName)
	}
	return nil
}

func (r *Runtime) IsRunning(pluginName string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.running[pluginName]
	return ok
}
