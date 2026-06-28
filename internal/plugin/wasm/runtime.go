package wasm

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/campusos/CampusOS/internal/plugin"
)

var (
	ErrPluginRequired              = errors.New("plugin is required")
	ErrInvalidRuntime              = errors.New("plugin runtime is not wasm")
	ErrModuleNotFound              = errors.New("wasm module not found")
	ErrPluginNotRunning            = errors.New("wasm plugin is not running")
	ErrEventDispatchNotImplemented = errors.New("wasm event dispatch is not implemented")
)

type moduleState struct {
	name       string
	modulePath string
	startedAt  time.Time
}

// Runtime is the v0.3-dev Wasm runtime skeleton.
//
// It validates plugin metadata, tracks lifecycle state, and reserves the
// SendEvent boundary for the later wazero-backed implementation.
type Runtime struct {
	mu      sync.RWMutex
	modules map[string]moduleState
}

func NewRuntime() *Runtime {
	return &Runtime{
		modules: make(map[string]moduleState),
	}
}

func (r *Runtime) Type() string {
	return "wasm"
}

func (r *Runtime) Start(ctx context.Context, p *plugin.Plugin) error {
	if p == nil || p.Manifest == nil {
		return ErrPluginRequired
	}
	if p.Manifest.Runtime != "wasm" {
		return fmt.Errorf("%w: %s", ErrInvalidRuntime, p.Manifest.Runtime)
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	modulePath, err := resolveModulePath(p)
	if err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.modules[p.Manifest.Name] = moduleState{
		name:       p.Manifest.Name,
		modulePath: modulePath,
		startedAt:  time.Now(),
	}
	return nil
}

func (r *Runtime) Stop(_ context.Context, pluginName string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.modules, pluginName)
	return nil
}

func (r *Runtime) SendEvent(_ context.Context, pluginName string, _ *plugin.EventMessage) (*plugin.PluginResponse, error) {
	if !r.IsRunning(pluginName) {
		return nil, fmt.Errorf("%w: %s", ErrPluginNotRunning, pluginName)
	}
	return nil, ErrEventDispatchNotImplemented
}

func (r *Runtime) HealthCheck(ctx context.Context, pluginName string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !r.IsRunning(pluginName) {
		return fmt.Errorf("%w: %s", ErrPluginNotRunning, pluginName)
	}
	return nil
}

func (r *Runtime) IsRunning(pluginName string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.modules[pluginName]
	return ok
}

func resolveModulePath(p *plugin.Plugin) (string, error) {
	moduleFile := "plugin.wasm"
	if raw, ok := p.Manifest.Config["module"]; ok {
		if value, ok := raw.(string); ok && value != "" {
			moduleFile = value
		}
	}

	modulePath := moduleFile
	if !filepath.IsAbs(modulePath) {
		modulePath = filepath.Join(p.Directory, moduleFile)
	}

	info, err := os.Stat(modulePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("%w: %s", ErrModuleNotFound, modulePath)
		}
		return "", err
	}
	if info.IsDir() {
		return "", fmt.Errorf("%w: %s", ErrModuleNotFound, modulePath)
	}
	return modulePath, nil
}
