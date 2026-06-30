package wasm

import (
	"context"
	"encoding/json"
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
	ErrEventCallTimeout    = errors.New("wasm event call timed out")
	ErrEventCallFailed     = errors.New("wasm event call failed")
	ErrEventPayloadABI     = errors.New("wasm event payload abi is unsupported")
)

const (
	defaultEntrypoint         = "handle_event"
	defaultEventTimeout       = 2 * time.Second
	defaultEventPayloadOffset = uint32(0)
	eventPayloadMemoryName    = "memory"
	eventHeapBaseGlobalName   = "__heap_base"
	maxUint32Value            = uint64(1<<32 - 1)
	wasm32MemoryLimit         = uint64(1) << 32
)

type eventABI int

const (
	eventABINoArgs eventABI = iota
	eventABIPayloadJSON
)

type moduleState struct {
	name          string
	modulePath    string
	entrypoint    string
	eventABI      eventABI
	payloadOffset uint32
	timeout       time.Duration
	module        api.Module
	startedAt     time.Time
}

// Runtime is the v0.3-dev Wasm runtime.
//
// The v0.3-dev event ABI supports either handle_event() for legacy examples,
// or handle_event(i32 ptr, i32 len) where ptr/len reference JSON EventMessage
// bytes written to exported guest memory. A non-zero i32/i64 return value means
// the plugin allows the event.
type Runtime struct {
	runtime wazero.Runtime

	mu      sync.RWMutex
	modules map[string]moduleState
}

func NewRuntime() *Runtime {
	return &Runtime{
		runtime: wazero.NewRuntimeWithConfig(context.Background(),
			wazero.NewRuntimeConfig().WithCloseOnContextDone(true),
		),
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
	timeout := resolveEventTimeout(p)

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
	handler := module.ExportedFunction(entrypoint)
	eventABI, err := detectEventABI(handler)
	if err != nil {
		_ = module.Close(ctx)
		return err
	}
	payloadOffset := defaultEventPayloadOffset
	if eventABI == eventABIPayloadJSON {
		if eventPayloadMemory(module) == nil {
			_ = module.Close(ctx)
			return fmt.Errorf("%w: %s requires exported memory %q", ErrEventPayloadABI, entrypoint, eventPayloadMemoryName)
		}
		payloadOffset, err = resolveEventPayloadOffset(module)
		if err != nil {
			_ = module.Close(ctx)
			return err
		}
	}

	r.modules[p.Manifest.Name] = moduleState{
		name:          p.Manifest.Name,
		modulePath:    modulePath,
		entrypoint:    entrypoint,
		eventABI:      eventABI,
		payloadOffset: payloadOffset,
		timeout:       timeout,
		module:        module,
		startedAt:     time.Now(),
	}
	return nil
}

func (r *Runtime) Stop(ctx context.Context, pluginName string) error {
	return r.closeModule(ctx, pluginName)
}

func (r *Runtime) SendEvent(ctx context.Context, pluginName string, event *plugin.EventMessage) (response *plugin.PluginResponse, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			_ = r.closeModule(context.Background(), pluginName)
			response = nil
			err = fmt.Errorf("%w: %s panic: %v", ErrEventCallFailed, pluginName, recovered)
		}
	}()

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	state, err := r.runningModule(pluginName)
	if err != nil {
		return nil, err
	}
	handler := state.module.ExportedFunction(state.entrypoint)
	if handler == nil {
		return nil, fmt.Errorf("%w: %s", ErrEventHandlerMissing, state.entrypoint)
	}

	callCtx, cancel := context.WithTimeout(ctx, state.timeout)
	defer cancel()

	params, err := r.eventHandlerParams(state, event)
	if err != nil {
		return nil, err
	}

	results, err := handler.Call(callCtx, params...)
	if err != nil {
		if errors.Is(callCtx.Err(), context.DeadlineExceeded) {
			_ = r.closeModule(context.Background(), pluginName)
			return nil, fmt.Errorf("%w: %s after %s: %v", ErrEventCallTimeout, pluginName, state.timeout, err)
		}
		_ = r.closeModule(context.Background(), pluginName)
		return nil, fmt.Errorf("%w: %s: %v", ErrEventCallFailed, pluginName, err)
	}
	if len(results) == 0 {
		return &plugin.PluginResponse{Allowed: true}, nil
	}

	allowed := results[0] != 0
	resp := &plugin.PluginResponse{Allowed: allowed}
	if !allowed {
		resp.Message = "wasm event handler rejected event"
	}
	return resp, nil
}

func (r *Runtime) eventHandlerParams(state moduleState, event *plugin.EventMessage) ([]uint64, error) {
	switch state.eventABI {
	case eventABINoArgs:
		return nil, nil
	case eventABIPayloadJSON:
		if event == nil {
			event = &plugin.EventMessage{}
		}
		payload, err := json.Marshal(event)
		if err != nil {
			return nil, fmt.Errorf("%w: marshal event: %v", ErrEventPayloadABI, err)
		}
		if uint64(len(payload)) > wasm32MemoryLimit-uint64(state.payloadOffset) {
			return nil, fmt.Errorf("%w: event payload too large", ErrEventPayloadABI)
		}
		memory := eventPayloadMemory(state.module)
		if memory == nil {
			return nil, fmt.Errorf("%w: %s requires exported memory %q", ErrEventPayloadABI, state.entrypoint, eventPayloadMemoryName)
		}
		if !memory.Write(state.payloadOffset, payload) {
			return nil, fmt.Errorf("%w: event payload does not fit guest memory", ErrEventPayloadABI)
		}
		return []uint64{uint64(state.payloadOffset), uint64(len(payload))}, nil
	default:
		return nil, fmt.Errorf("%w: unknown event ABI", ErrEventPayloadABI)
	}
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

func (r *Runtime) runningModule(pluginName string) (moduleState, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	state, ok := r.modules[pluginName]
	if !ok || state.module.IsClosed() {
		return moduleState{}, fmt.Errorf("%w: %s", ErrPluginNotRunning, pluginName)
	}
	return state, nil
}

func (r *Runtime) closeModule(ctx context.Context, pluginName string) error {
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

func resolveEventTimeout(p *plugin.Plugin) time.Duration {
	raw, ok := p.Manifest.Config["event_timeout_ms"]
	if !ok {
		return defaultEventTimeout
	}

	switch value := raw.(type) {
	case int:
		return durationFromMilliseconds(int64(value))
	case int64:
		return durationFromMilliseconds(value)
	case int32:
		return durationFromMilliseconds(int64(value))
	case float64:
		return durationFromMilliseconds(int64(value))
	case float32:
		return durationFromMilliseconds(int64(value))
	default:
		return defaultEventTimeout
	}
}

func durationFromMilliseconds(ms int64) time.Duration {
	if ms <= 0 {
		return defaultEventTimeout
	}
	return time.Duration(ms) * time.Millisecond
}

func detectEventABI(handler api.Function) (eventABI, error) {
	params := handler.Definition().ParamTypes()
	switch {
	case len(params) == 0:
		return eventABINoArgs, nil
	case len(params) == 2 && params[0] == api.ValueTypeI32 && params[1] == api.ValueTypeI32:
		return eventABIPayloadJSON, nil
	default:
		return eventABINoArgs, fmt.Errorf("%w: %s params=%v", ErrEventPayloadABI, handler.Definition().DebugName(), params)
	}
}

func resolveEventPayloadOffset(module api.Module) (uint32, error) {
	global := module.ExportedGlobal(eventHeapBaseGlobalName)
	if global == nil {
		return defaultEventPayloadOffset, nil
	}
	if global.Type() != api.ValueTypeI32 && global.Type() != api.ValueTypeI64 {
		return 0, fmt.Errorf("%w: %s must be an integer global", ErrEventPayloadABI, eventHeapBaseGlobalName)
	}
	value := global.Get()
	if value > maxUint32Value {
		return 0, fmt.Errorf("%w: %s is outside wasm32 memory", ErrEventPayloadABI, eventHeapBaseGlobalName)
	}
	return uint32(value), nil
}

func eventPayloadMemory(module api.Module) api.Memory {
	return module.ExportedMemory(eventPayloadMemoryName)
}
