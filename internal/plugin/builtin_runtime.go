package plugin

import (
	"context"
	"fmt"
	"sync"
)

// BuiltinRuntime represents a first-party plugin whose UI is compiled into the
// CampusOS Web application. It executes no package supplied code: lifecycle
// tracking only makes the manifest, health and authorization state visible to
// the normal Plugin Manager.
type BuiltinRuntime struct {
	mu      sync.RWMutex
	running map[string]bool
}

func NewBuiltinRuntime() *BuiltinRuntime { return &BuiltinRuntime{running: map[string]bool{}} }

func (r *BuiltinRuntime) Start(_ context.Context, p *Plugin) error {
	if p == nil || p.Manifest == nil || p.Manifest.Runtime != "builtin" {
		return fmt.Errorf("builtin runtime requires a builtin plugin manifest")
	}
	r.mu.Lock()
	r.running[p.Manifest.Name] = true
	r.mu.Unlock()
	return nil
}

func (r *BuiltinRuntime) Stop(_ context.Context, name string) error {
	r.mu.Lock()
	delete(r.running, name)
	r.mu.Unlock()
	return nil
}

func (r *BuiltinRuntime) SendEvent(_ context.Context, _ string, _ *EventMessage) (*PluginResponse, error) {
	return nil, fmt.Errorf("builtin UI plugins do not accept runtime events")
}

func (r *BuiltinRuntime) HealthCheck(_ context.Context, name string) error {
	if !r.IsRunning(name) {
		return fmt.Errorf("builtin plugin %q is not running", name)
	}
	return nil
}

func (r *BuiltinRuntime) IsRunning(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.running[name]
}

func (r *BuiltinRuntime) Type() string { return "builtin" }
