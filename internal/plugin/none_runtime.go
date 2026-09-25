package plugin

import (
	"context"
	"errors"
	"sync"
)

// NoneRuntime represents an installed UI-only package. It deliberately starts
// no process and accepts no events or extension calls; all data access is
// mediated by the host-owned v4 browser bridge.
type NoneRuntime struct {
	mu      sync.RWMutex
	running map[string]bool
}

func NewNoneRuntime() *NoneRuntime { return &NoneRuntime{running: map[string]bool{}} }

func (r *NoneRuntime) Type() string { return "none" }

func (r *NoneRuntime) Start(_ context.Context, plugin *Plugin) error {
	if plugin == nil || plugin.ID == "" {
		return errors.New("none runtime requires a plugin")
	}
	r.mu.Lock()
	r.running[plugin.ID] = true
	r.mu.Unlock()
	return nil
}

func (r *NoneRuntime) Stop(_ context.Context, pluginName string) error {
	r.mu.Lock()
	delete(r.running, pluginName)
	r.mu.Unlock()
	return nil
}

func (r *NoneRuntime) SendEvent(context.Context, string, *EventMessage) (*PluginResponse, error) {
	return nil, errors.New("UI-only plugin does not accept events")
}

func (r *NoneRuntime) HealthCheck(_ context.Context, pluginName string) error {
	if !r.IsRunning(pluginName) {
		return errors.New("UI-only plugin is not running")
	}
	return nil
}

func (r *NoneRuntime) IsRunning(pluginName string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.running[pluginName]
}
