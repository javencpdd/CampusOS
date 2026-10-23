package wasm

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"slices"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/campusos/CampusOS/internal/platform/security"
	"github.com/campusos/CampusOS/internal/plugin"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
)

const V5SandboxMaxJSONBytes = 64 << 10

var (
	ErrV5SandboxABI      = errors.New("plugin.wasm_abi_invalid")
	ErrV5SandboxImport   = errors.New("plugin.wasm_import_forbidden")
	ErrV5SandboxCompile  = errors.New("plugin.wasm_compile_failed")
	ErrV5SandboxCall     = errors.New("plugin.wasm_call_failed")
	ErrV5SandboxBudget   = errors.New("plugin.wasm_budget_exceeded")
	ErrV5SandboxDeadline = errors.New("plugin.wasm_deadline_exceeded")
	ErrV5SandboxClosed   = errors.New("plugin.wasm_closed")
)

// V5SandboxPolicy is supplied by the compiled host, never decoded from a guest
// request. This snapshot is not a live Grant/Consent or installation proof.
type V5SandboxPolicy struct {
	HostCeiling, AdminGrant, DeploymentCeiling security.ResourceSet
	GrantApproved                              bool
	GrantExpiresAtMS                           int64
}

// Kernel limits are independent of the native CPU/RSS controls enforced by
// V5IsolatedFactory. No filesystem/storage port is exposed to the guest.
type V5SandboxLimits struct {
	MemoryMB, MaxConcurrency, TimeoutMS int64
}

// V5Sandbox implements campusos.wasm-json/v1: one exported memory and
// campusos_invoke(i32 input_ptr, i32 input_len) -> i64 (ptr<<32 | len).
// The first 64KiB are the input area. Every call gets a fresh guest instance.
// No WASI, filesystem, environment, argv, stdio or network is bound.
// The Host variant binds only the finite, declared campusos.host_call import.
// This is an execution kernel, not the V5ManagedAdapter/lifecycle coordinator.
type V5Sandbox struct {
	runtime       wazero.Runtime
	module        wazero.CompiledModule
	limits        V5SandboxLimits
	expires       time.Time
	life          context.Context
	cancel        context.CancelFunc
	mu            sync.Mutex
	closed        bool
	active        int64
	idle          chan struct{}
	configuration []byte
}

type V5HostCall func(context.Context, string, json.RawMessage) (json.RawMessage, error)
type v5HostContextKey struct{}
type v5HostInvocation struct{ calls atomic.Int32 }

func NewV5Sandbox(ctx context.Context, program plugin.V5WasmProgram, policy V5SandboxPolicy) (*V5Sandbox, error) {
	return NewV5SandboxWithHost(ctx, program, policy, nil)
}

// Host is a compiled broker, not a guest-provided function or raw resource port.
// The default constructor continues to refuse every import.
func NewV5SandboxWithHost(ctx context.Context, program plugin.V5WasmProgram, policy V5SandboxPolicy, host V5HostCall) (*V5Sandbox, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if program.Digest() == "" {
		return nil, plugin.ErrPluginV5PackageInvalid
	}
	r := program.Resources()
	requested := security.ResourceSet{NetworkTargets: r.NetworkTargets, StorageBytes: r.StorageBytes, CPUMillis: r.CPUMillis, MemoryMB: r.MemoryMB, MaxConcurrency: r.MaxConcurrency, TimeoutMS: r.TimeoutMS}
	status := "revoked"
	if policy.GrantApproved {
		status = "granted"
	}
	decision, err := security.DecideResourcePolicy(security.ResourceDecisionInput{
		Contract: security.PluginHostResourcesContract, Requested: requested, HostCeiling: policy.HostCeiling,
		AdminGrant: policy.AdminGrant, GrantStatus: status, GrantExpiresAtMS: policy.GrantExpiresAtMS, NowMS: time.Now().UnixMilli(),
	}, policy.DeploymentCeiling)
	if err != nil {
		return nil, err
	}
	// Additional finite implementation ceilings tighten the four-way policy.
	limits := V5SandboxLimits{min(decision.Effective.MemoryMB, 256), min(decision.Effective.MaxConcurrency, 8), min(decision.Effective.TimeoutMS, 5000)}
	expires := time.UnixMilli(policy.GrantExpiresAtMS)
	declared := map[string]bool{}
	for _, c := range program.Consumes() {
		declared[c.Capability] = true
	}
	return newV5Sandbox(ctx, program.Bytes(), limits, expires, declared, host)
}

func newV5Sandbox(ctx context.Context, code []byte, limits V5SandboxLimits, expires time.Time, declared map[string]bool, host V5HostCall) (*V5Sandbox, error) {
	if len(code) > plugin.PluginV5MaxFileBytes || limits.MemoryMB < 1 || limits.MemoryMB > 256 || limits.MaxConcurrency < 1 || limits.MaxConcurrency > 8 || limits.TimeoutMS < 1 || limits.TimeoutMS > 5000 {
		return nil, ErrV5SandboxBudget
	}
	compileCtx, cancel := context.WithDeadline(ctx, minTime(time.Now().Add(time.Duration(limits.TimeoutMS)*time.Millisecond), expires))
	defer cancel()
	runtime := wazero.NewRuntimeWithConfig(compileCtx, wazero.NewRuntimeConfig().
		WithMemoryLimitPages(uint32(limits.MemoryMB*16)).WithCloseOnContextDone(true).WithDebugInfoEnabled(false))
	compiled, err := runtime.CompileModule(compileCtx, code)
	if err != nil {
		_ = runtime.Close(context.Background())
		if err := compileCtx.Err(); err != nil {
			return nil, err
		}
		return nil, ErrV5SandboxCompile
	}
	fail := func(err error) (*V5Sandbox, error) {
		_ = runtime.Close(context.Background())
		return nil, err
	}
	if v5HasImports(code) {
		imports := compiled.ImportedFunctions()
		if host == nil || len(imports) != 1 || v5ImportCount(code) != 1 || len(compiled.ImportedMemories()) != 0 {
			return fail(ErrV5SandboxImport)
		}
		module, name, ok := imports[0].Import()
		if !ok || module != "campusos" || name != "host_call" || !slices.Equal(imports[0].ParamTypes(), []api.ValueType{api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32}) || !slices.Equal(imports[0].ResultTypes(), []api.ValueType{api.ValueTypeI32}) {
			return fail(ErrV5SandboxImport)
		}
		_, err := runtime.NewHostModuleBuilder("campusos").NewFunctionBuilder().WithFunc(func(ctx context.Context, m api.Module, ptr, size, dst, capacity uint32) uint32 {
			inv, ok := ctx.Value(v5HostContextKey{}).(*v5HostInvocation)
			if !ok || inv.calls.Add(1) > 32 || size > V5SandboxMaxJSONBytes || capacity < 1 || capacity > V5SandboxMaxJSONBytes || ctx.Err() != nil {
				return ^uint32(0)
			}
			memory := m.ExportedMemory("memory")
			if memory == nil {
				return ^uint32(0)
			}
			raw, ok := memory.Read(ptr, size)
			if !ok {
				return ^uint32(0)
			}
			code, payload, err := plugin.ParseV5WasmHostRequest(bytes.Clone(raw))
			if err != nil || !declared[code] {
				return ^uint32(0)
			}
			out, err := host(ctx, code, payload)
			if err != nil || ctx.Err() != nil || len(out) > int(capacity) || !utf8.Valid(out) || !json.Valid(out) || !memory.Write(dst, out) {
				return ^uint32(0)
			}
			return uint32(len(out))
		}).Export("host_call").Instantiate(compileCtx)
		if err != nil {
			return fail(ErrV5SandboxImport)
		}
	}
	handler := compiled.ExportedFunctions()["campusos_invoke"]
	memory := compiled.ExportedMemories()["memory"]
	if handler == nil || memory == nil || memory.Min() < 1 ||
		!slices.Equal(handler.ParamTypes(), []api.ValueType{api.ValueTypeI32, api.ValueTypeI32}) ||
		!slices.Equal(handler.ResultTypes(), []api.ValueType{api.ValueTypeI64}) {
		return fail(ErrV5SandboxABI)
	}
	if event := compiled.ExportedFunctions()["campusos_event"]; event != nil && (!slices.Equal(event.ParamTypes(), []api.ValueType{api.ValueTypeI32, api.ValueTypeI32}) || !slices.Equal(event.ResultTypes(), []api.ValueType{api.ValueTypeI32})) {
		return fail(ErrV5SandboxABI)
	}
	if err := compileCtx.Err(); err != nil {
		return fail(err)
	}
	idle := make(chan struct{})
	close(idle)
	life, stop := context.WithCancel(context.Background())
	return &V5Sandbox{runtime: runtime, module: compiled, limits: limits, expires: expires, life: life, cancel: stop, idle: idle}, nil
}

// Called only after wazero validates the binary. Inspect the complete import
// section, including tables/globals that CompiledModule's public import API
// does not enumerate. No kind of import has an implicit allowlist entry.
func v5HasImports(code []byte) bool {
	return v5ImportCount(code) != 0
}

func v5ImportCount(code []byte) uint64 {
	for offset := 8; offset < len(code); {
		section := code[offset]
		offset++
		size, n := binary.Uvarint(code[offset:])
		if n < 1 || size > uint64(len(code)-offset-n) {
			return ^uint64(0)
		}
		offset += n
		if section == 2 {
			count, n := binary.Uvarint(code[offset : offset+int(size)])
			if n < 1 {
				return ^uint64(0)
			}
			return count
		}
		offset += int(size)
	}
	return 0
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

func (s *V5Sandbox) Limits() V5SandboxLimits { return s.limits }

func (s *V5Sandbox) enter() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrV5SandboxClosed
	}
	if !time.Now().Before(s.expires) {
		return security.ErrResourceGrantExpired
	}
	if s.active >= s.limits.MaxConcurrency {
		return ErrV5SandboxBudget
	}
	if s.active == 0 {
		s.idle = make(chan struct{})
	}
	s.active++
	return nil
}

func (s *V5Sandbox) leave() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.active--
	if s.active == 0 {
		close(s.idle)
	}
}

// Invoke returns a copied, bounded JSON document. Typed request/response codecs
// and current authorization belong to the caller; no guest result is authority.
// An infinite guest loop is interrupted by wazero itself, not a detached worker.
func (s *V5Sandbox) Invoke(ctx context.Context, input []byte, responseLimit int) (out []byte, err error) {
	return s.invoke(ctx, input, responseLimit, "invoke")
}

func (s *V5Sandbox) SupportsEvents() bool {
	return s.module.ExportedFunctions()["campusos_event"] != nil
}
func (s *V5Sandbox) Check(ctx context.Context) error {
	_, err := s.invoke(ctx, []byte(`{}`), 1, "check")
	return err
}
func (s *V5Sandbox) Event(ctx context.Context, input []byte) error {
	_, err := s.invoke(ctx, input, 1, "event")
	return err
}

func (s *V5Sandbox) invoke(ctx context.Context, input []byte, responseLimit int, kind string) (out []byte, err error) {
	if s == nil {
		return nil, ErrV5SandboxClosed
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(input) > V5SandboxMaxJSONBytes || responseLimit < 1 || responseLimit > V5SandboxMaxJSONBytes {
		return nil, ErrV5SandboxBudget
	}
	if !utf8.Valid(input) || !json.Valid(input) {
		return nil, ErrV5SandboxABI
	}
	if err := s.enter(); err != nil {
		return nil, err
	}
	defer s.leave()
	callCtx, cancel := context.WithDeadline(ctx, minTime(time.Now().Add(time.Duration(s.limits.TimeoutMS)*time.Millisecond), s.expires))
	if kind != "check" {
		callCtx = context.WithValue(callCtx, v5HostContextKey{}, &v5HostInvocation{})
	}
	stop := context.AfterFunc(s.life, cancel)
	defer func() {
		// Runtime.Close can interrupt the guest before AfterFunc propagates the
		// lifetime cancellation into callCtx. Never release that late result.
		if callCtx.Err() != nil || s.life.Err() != nil || !time.Now().Before(s.expires) {
			out = nil
			switch {
			case ctx.Err() != nil:
				err = ctx.Err()
			case s.life.Err() != nil:
				err = ErrV5SandboxClosed
			case !time.Now().Before(s.expires):
				err = security.ErrResourceGrantExpired
			default:
				err = ErrV5SandboxDeadline
			}
		}
		stop()
		cancel()
	}()
	guest, err := s.runtime.InstantiateModule(callCtx, s.module, wazero.NewModuleConfig().WithName("").WithStartFunctions())
	if err != nil {
		return nil, ErrV5SandboxCall
	}
	defer guest.Close(context.Background())
	if len(s.configuration) > 0 {
		fn := guest.ExportedFunction("campusos_configure")
		if fn == nil || !slices.Equal(fn.Definition().ParamTypes(), []api.ValueType{api.ValueTypeI32, api.ValueTypeI32}) || !slices.Equal(fn.Definition().ResultTypes(), []api.ValueType{api.ValueTypeI32}) || !guest.ExportedMemory("memory").Write(0, s.configuration) {
			return nil, ErrV5SandboxABI
		}
		result, err := fn.Call(callCtx, 0, uint64(len(s.configuration)))
		if err != nil || len(result) != 1 || result[0] != 0 {
			return nil, ErrV5SandboxCall
		}
	}
	if kind == "check" {
		return nil, nil
	}
	memory := guest.ExportedMemory("memory")
	if memory == nil || !memory.Write(0, input) {
		return nil, ErrV5SandboxBudget
	}
	entry := "campusos_invoke"
	if kind == "event" {
		entry = "campusos_event"
	}
	handler := guest.ExportedFunction(entry)
	if handler == nil {
		return nil, ErrV5SandboxABI
	}
	results, err := handler.Call(callCtx, 0, uint64(len(input)))
	if err != nil || len(results) != 1 {
		return nil, ErrV5SandboxCall
	}
	if kind == "event" {
		if results[0] != 0 {
			return nil, ErrV5SandboxCall
		}
		return nil, nil
	}
	ptr, size := uint32(results[0]>>32), uint32(results[0])
	if size == 0 || uint64(size) > uint64(responseLimit) {
		return nil, ErrV5SandboxBudget
	}
	raw, ok := memory.Read(ptr, size)
	if !ok || !utf8.Valid(raw) || !json.Valid(raw) {
		return nil, ErrV5SandboxABI
	}
	return bytes.Clone(raw), nil
}

// Close stops admission first, cancels actual guest execution and waits for all
// admitted calls. A completed Close cannot leave a background guest running.
func (s *V5Sandbox) Close(ctx context.Context) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	s.closed = true
	s.cancel()
	idle := s.idle
	s.mu.Unlock()
	err := s.runtime.Close(ctx)
	select {
	case <-idle:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}
