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
	mu       sync.RWMutex
	running  map[string]struct{}
	handlers map[string]func(context.Context, *plugin.ExtensionRequest) (*plugin.ExtensionResponse, error)
}

func NewRuntime() *Runtime {
	return &Runtime{
		running:  make(map[string]struct{}),
		handlers: make(map[string]func(context.Context, *plugin.ExtensionRequest) (*plugin.ExtensionResponse, error)),
	}
}

func (r *Runtime) RegisterExtension(pluginName string, handler func(context.Context, *plugin.ExtensionRequest) (*plugin.ExtensionResponse, error)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if handler == nil {
		delete(r.handlers, pluginName)
		return
	}
	r.handlers[pluginName] = handler
}

func (r *Runtime) DispatchExtension(ctx context.Context, pluginName string, request *plugin.ExtensionRequest) (*plugin.ExtensionResponse, error) {
	if err := r.HealthCheck(ctx, pluginName); err != nil {
		return nil, err
	}
	r.mu.RLock()
	handler := r.handlers[pluginName]
	r.mu.RUnlock()
	if handler == nil {
		return nil, fmt.Errorf("builtin plugin '%s' does not expose an extension handler", pluginName)
	}
	return handler(ctx, request)
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
