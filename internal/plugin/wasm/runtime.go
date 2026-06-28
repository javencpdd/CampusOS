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
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
)

var (
	ErrPluginRequired      = errors.New("plugin is required")
	ErrInvalidRuntime      = errors.New("plugin runtime is not wasm")
	ErrModuleNotFound      = errors.New("wasm module not found")
	ErrPluginNotRunning    = errors.New("wasm plugin is not running")
	ErrEventHandlerMissing = errors.New("wasm event handler is missing")
)

const defaultEntrypoint = "handle_event"

type moduleState struct {
	name       string
	modulePath string
	entrypoint string
	module     api.Module
	startedAt  time.Time
}

// Runtime is the v0.3-dev Wasm runtime.
//
// The current event ABI is intentionally small: SendEvent invokes an exported
// no-argument function named "handle_event" by default. A non-zero i32/i64
// return value means the plugin allows the event.
type Runtime struct {
	runtime wazero.Runtime

	mu      sync.RWMutex
	modules map[string]moduleState
}

func NewRuntime() *Runtime {
	return &Runtime{
		runtime: wazero.NewRuntime(context.Background()),
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
	moduleBytes, err := os.ReadFile(modulePath)
	if err != nil {
		return err
	}
	entrypoint := resolveEntrypoint(p)

	r.mu.Lock()
	defer r.mu.Unlock()

	if existing, ok := r.modules[p.Manifest.Name]; ok {
		if err := existing.module.Close(ctx); err != nil {
			return err
		}
		delete(r.modules, p.Manifest.Name)
	}

	module, err := r.runtime.InstantiateWithConfig(ctx, moduleBytes, wazero.NewModuleConfig().
		WithName(p.Manifest.Name).
		WithStartFunctions(),
	)
	if err != nil {
		return err
	}
	if module.ExportedFunction(entrypoint) == nil {
		_ = module.Close(ctx)
		return fmt.Errorf("%w: %s", ErrEventHandlerMissing, entrypoint)
	}

	r.modules[p.Manifest.Name] = moduleState{
		name:       p.Manifest.Name,
		modulePath: modulePath,
		entrypoint: entrypoint,
		module:     module,
		startedAt:  time.Now(),
	}
	return nil
}

func (r *Runtime) Stop(ctx context.Context, pluginName string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	state, ok := r.modules[pluginName]
	if !ok {
		return nil
	}
	err := state.module.Close(ctx)
	delete(r.modules, pluginName)
	return err
}

func (r *Runtime) SendEvent(ctx context.Context, pluginName string, _ *plugin.EventMessage) (*plugin.PluginResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	state, ok := r.modules[pluginName]
	if !ok || state.module.IsClosed() {
		return nil, fmt.Errorf("%w: %s", ErrPluginNotRunning, pluginName)
	}
	handler := state.module.ExportedFunction(state.entrypoint)
	if handler == nil {
		return nil, fmt.Errorf("%w: %s", ErrEventHandlerMissing, state.entrypoint)
	}

	results, err := handler.Call(ctx)
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return &plugin.PluginResponse{Allowed: true}, nil
	}

	allowed := results[0] != 0
	response := &plugin.PluginResponse{Allowed: allowed}
	if !allowed {
		response.Message = "wasm event handler rejected event"
	}
	return response, nil
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
	state, ok := r.modules[pluginName]
	return ok && !state.module.IsClosed()
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

func resolveEntrypoint(p *plugin.Plugin) string {
	if raw, ok := p.Manifest.Config["entrypoint"]; ok {
		if value, ok := raw.(string); ok && value != "" {
			return value
		}
	}
	return defaultEntrypoint
}
