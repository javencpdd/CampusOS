package wasm

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/campusos/CampusOS/internal/platform/security"
	"github.com/campusos/CampusOS/internal/plugin"
)

// These fixtures are real, minimal Wasm binaries executed by wazero. Keeping
// the encoder here makes their opcodes and malicious behavior reviewable; no
// external compiler, mock runtime, loopback server or prebuilt blob is used.
func v5ULEB(value uint64) []byte {
	var out []byte
	for {
		b := byte(value & 127)
		value >>= 7
		if value != 0 {
			b |= 128
		}
		out = append(out, b)
		if value == 0 {
			return out
		}
	}
}

func v5SLEB(value int64) []byte {
	var out []byte
	for {
		b := byte(value & 127)
		value >>= 7
		done := value == 0 && b&64 == 0 || value == -1 && b&64 != 0
		if !done {
			b |= 128
		}
		out = append(out, b)
		if done {
			return out
		}
	}
}

func v5Section(id byte, body []byte) []byte {
	return append(append([]byte{id}, v5ULEB(uint64(len(body)))...), body...)
}
func v5Name(s string) []byte { return append(v5ULEB(uint64(len(s))), []byte(s)...) }

type v5WasmFixture struct {
	body            []byte
	minPages        byte
	importModule    string
	importName      string
	importKind      byte
	startLoop       bool
	noMemoryExport  bool
	noHandlerExport bool
	data            []byte
}

func v5Module(f v5WasmFixture) []byte {
	code := []byte{0, 'a', 's', 'm', 1, 0, 0, 0}
	// type0 (i32,i32)->i64; type1 ()->().
	code = append(code, v5Section(1, []byte{2, 0x60, 2, 0x7f, 0x7f, 1, 0x7e, 0x60, 0, 0})...)
	functionIndex := byte(0)
	if f.importModule != "" {
		body := append([]byte{1}, v5Name(f.importModule)...)
		body = append(body, v5Name(f.importName)...)
		body = append(body, f.importKind)
		switch f.importKind {
		case 0: // function of type0
			body = append(body, 0)
			functionIndex++
		case 1: // table, funcref, min0
			body = append(body, 0x70, 0, 0)
		case 2: // memory, min1
			body = append(body, 0, 1)
		case 3: // immutable i32 global
			body = append(body, 0x7f, 0)
		}
		code = append(code, v5Section(2, body)...)
	}
	functions := []byte{1, 0}
	if f.startLoop {
		functions = []byte{2, 0, 1}
	}
	code = append(code, v5Section(3, functions)...)
	if f.importModule == "" || f.importKind != 2 {
		code = append(code, v5Section(5, []byte{1, 0, max(f.minPages, 1)})...)
	}
	var exports []byte
	count := byte(0)
	if !f.noMemoryExport {
		exports = append(append(exports, v5Name("memory")...), 2, 0)
		count++
	}
	if !f.noHandlerExport {
		exports = append(append(exports, v5Name("campusos_invoke")...), 0, functionIndex)
		count++
	}
	code = append(code, v5Section(7, append([]byte{count}, exports...))...)
	if f.startLoop {
		code = append(code, v5Section(8, []byte{functionIndex + 1})...)
	}
	body := f.body
	if body == nil {
		body = []byte{0x20, 1, 0xad} // return input ptr0,len as packed i64
	}
	body = append(append([]byte{0}, body...), 0x0b)
	section := append([]byte{1}, v5ULEB(uint64(len(body)))...)
	section = append(section, body...)
	if f.startLoop {
		section[0] = 2
		start := []byte{0, 0x03, 0x40, 0x0c, 0, 0x0b, 0x0b}
		section = append(append(section, v5ULEB(uint64(len(start)))...), start...)
	}
	code = append(code, v5Section(10, section)...)
	if f.data != nil {
		// Active segment at byte65536, outside the reserved input page.
		data := []byte{1, 0, 0x41, 0x80, 0x80, 4, 0x0b}
		data = append(append(data, v5ULEB(uint64(len(f.data)))...), f.data...)
		code = append(code, v5Section(11, data)...)
	}
	return code
}

func v5SignedProgram(t *testing.T, code []byte) plugin.V5WasmProgram {
	t.Helper()
	sum := sha256.Sum256(code)
	var manifest plugin.PluginV5Manifest
	if err := json.Unmarshal([]byte(`{
		"api_version":"campusos.plugin/v5","publisher":"sandbox-test","key":"probe","version":"1.0.0",
		"host_contracts":["campusos.host/v1"],"runtime":"wasm",
		"backend":{"artifact":{"kind":"wasm","path":"dist/probe.wasm","digest":""},"transport":"host-call"},
		"provides":[{"id":"chat","point":"ai.chat","contract_version":"v1","input_schema":"campusos.ai.chat.request/v1","output_schema":"campusos.ai.chat.response/v1","capabilities":["text"],"data_mode":"no_host_private_data"}],
		"consumes":[],"host_resources":{"network_targets":[],"storage_bytes":1,"cpu_millis":1000,"memory_mb":32,"max_concurrency":8,"timeout_ms":5000},
		"configuration":{"contract":"campusos.plugin-config/v1","version":"v1","schema":{"type":"object","additionalProperties":false,"properties":{},"required":[]},"defaults":{},"selectors":[],"secret_names":[]},
		"data":{"config_scope":"user","private_kv_bytes":0,"private_object_bytes":0,"upgrade":{"schema_version":1,"mode":"preserve","rollback":"snapshot"}}
	}`), &manifest); err != nil {
		t.Fatal(err)
	}
	manifest.Backend.Artifact.Digest = "sha256:" + hex.EncodeToString(sum[:])
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "dist"), 0700); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "plugin.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "dist/probe.wasm"), code, 0600); err != nil {
		t.Fatal(err)
	}
	archive, _, err := plugin.BuildPluginV5Package(dir)
	if err != nil {
		t.Fatal(err)
	}
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	release, err := plugin.SignPluginV5Package(archive, "owned-ephemeral-test", key)
	if err != nil {
		t.Fatal(err)
	}
	trust, _ := json.Marshal(plugin.PluginV5TrustStore{Contract: "campusos.plugin-trust/v1", Keys: []plugin.PluginV5TrustKey{{Publisher: manifest.Publisher, KeyID: "owned-ephemeral-test", PublicKey: base64.StdEncoding.EncodeToString(pub), Status: "active"}}})
	catalog, err := plugin.NewPluginV5PackageCatalog(trust)
	if err != nil {
		t.Fatal(err)
	}
	program, err := catalog.CaptureWasmProgram(context.Background(), archive, release)
	if err != nil {
		t.Fatal(err)
	}
	return program
}

func v5Policy() V5SandboxPolicy {
	r := security.ResourceSet{NetworkTargets: []string{}, StorageBytes: 1, CPUMillis: 1000, MemoryMB: 16, MaxConcurrency: 8, TimeoutMS: 2000}
	return V5SandboxPolicy{HostCeiling: r, AdminGrant: r, DeploymentCeiling: r, GrantApproved: true, GrantExpiresAtMS: time.Now().Add(time.Minute).UnixMilli()}
}

func v5Sandbox(t *testing.T, f v5WasmFixture, policy V5SandboxPolicy) *V5Sandbox {
	t.Helper()
	s, err := NewV5Sandbox(context.Background(), v5SignedProgram(t, v5Module(f)), policy)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := s.Close(ctx); err != nil {
			t.Error("sandbox cleanup", err)
		}
	})
	return s
}

func TestV5SandboxSignedJSONRoundTripAndIsolation(t *testing.T) {
	// memory[65536] must start at zero for EACH call; retain a byte there to
	// detect accidental shared instances. A reused guest returns an empty result.
	body := []byte{0x41, 0x80, 0x80, 4, 0x2d, 0, 0, 0x04, 0x7e, 0x42, 0, 0x05,
		0x41, 0x80, 0x80, 4, 0x41, 1, 0x3a, 0, 0, 0x20, 1, 0xad, 0x0b}
	s := v5Sandbox(t, v5WasmFixture{body: body, minPages: 2}, v5Policy())
	var wg sync.WaitGroup
	for worker := 0; worker < 4; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n := 0; n < 10; n++ {
				input := []byte(`{"text":"校园🌉","request_id":"r1"}`)
				out, err := s.Invoke(context.Background(), input, 256)
				if err != nil || !bytes.Equal(out, input) {
					t.Errorf("actual isolated Wasm roundtrip: %q %v", out, err)
					return
				}
				out[0] = '!'
				if input[0] != '{' {
					t.Error("output aliases input")
				}
			}
		}()
	}
	wg.Wait()
}

func TestV5SandboxFourWayResourceLimitsAndGrantDenial(t *testing.T) {
	program := v5SignedProgram(t, v5Module(v5WasmFixture{}))
	policy := v5Policy()
	policy.HostCeiling.MemoryMB, policy.HostCeiling.MaxConcurrency, policy.HostCeiling.TimeoutMS = 12, 6, 800
	policy.AdminGrant.MemoryMB, policy.AdminGrant.MaxConcurrency, policy.AdminGrant.TimeoutMS = 8, 4, 600
	policy.DeploymentCeiling.MemoryMB, policy.DeploymentCeiling.MaxConcurrency, policy.DeploymentCeiling.TimeoutMS = 4, 2, 400
	s, err := NewV5Sandbox(context.Background(), program, policy)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close(context.Background())
	if got := s.Limits(); got != (V5SandboxLimits{4, 2, 400}) {
		t.Fatal("four-way policy not enforced", got)
	}
	for name, change := range map[string]func(*V5SandboxPolicy){
		"unapproved": func(p *V5SandboxPolicy) { p.GrantApproved = false },
		"expired":    func(p *V5SandboxPolicy) { p.GrantExpiresAtMS = time.Now().Add(-time.Second).UnixMilli() },
		"zero-limit": func(p *V5SandboxPolicy) { p.DeploymentCeiling.MemoryMB = 0 },
	} {
		t.Run(name, func(t *testing.T) {
			p := policy
			change(&p)
			if _, err := NewV5Sandbox(context.Background(), program, p); err == nil {
				t.Fatal("policy denial bypassed")
			}
		})
	}
	if _, err := NewV5Sandbox(context.Background(), plugin.V5WasmProgram{}, policy); err == nil {
		t.Fatal("uncaptured artifact")
	}
}

func TestV5SandboxRejectsAllImports(t *testing.T) {
	for _, tc := range []struct {
		module, name string
		kind         byte
	}{
		{"wasi_snapshot_preview1", "path_open", 0},
		{"wasi_snapshot_preview1", "sock_open", 0},
		{"wasi_snapshot_preview1", "environ_get", 0},
		{"wasi_snapshot_preview1", "fd_write", 0},
		{"campusos", "community.thread.public.read", 0},
		{"env", "host_secret", 3},
		{"env", "host_memory", 2},
		{"env", "host_table", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			program := v5SignedProgram(t, v5Module(v5WasmFixture{importModule: tc.module, importName: tc.name, importKind: tc.kind}))
			if _, err := NewV5Sandbox(context.Background(), program, v5Policy()); err != ErrV5SandboxImport {
				t.Fatal("import was not rejected before instantiation", err)
			}
		})
	}
}

func TestV5SandboxMemoryCeilingAndABIAdmission(t *testing.T) {
	policy := v5Policy()
	policy.DeploymentCeiling.MemoryMB = 1 // 16 Wasm pages
	for name, fixture := range map[string]v5WasmFixture{
		"initial-memory-over-limit": {minPages: 32},
		"missing-memory":            {noMemoryExport: true},
		"missing-handler":           {noHandlerExport: true},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := NewV5Sandbox(context.Background(), v5SignedProgram(t, v5Module(fixture)), policy); err == nil {
				t.Fatal("invalid memory/ABI admitted")
			}
		})
	}
	// memory.grow(17) must return -1. If it succeeds the guest returns empty.
	body := []byte{0x41, 17, 0x40, 0, 0x41, 0x7f, 0x47, 0x04, 0x7e, 0x42, 0, 0x05, 0x20, 1, 0xad, 0x0b}
	s := v5Sandbox(t, v5WasmFixture{body: body}, policy)
	if out, err := s.Invoke(context.Background(), []byte(`{"growth_denied":true}`), 64); err != nil || string(out) != `{"growth_denied":true}` {
		t.Fatal("memory growth bypassed ceiling", string(out), err)
	}
}

func TestV5SandboxInputOutputBudgetsAndTrap(t *testing.T) {
	s := v5Sandbox(t, v5WasmFixture{}, v5Policy())
	for _, input := range [][]byte{nil, []byte(`{"broken":`), {'"', 0xff, '"'}} {
		if _, err := s.Invoke(context.Background(), input, 64); err != ErrV5SandboxABI {
			t.Fatal("invalid input accepted", err)
		}
	}
	if _, err := s.Invoke(context.Background(), bytes.Repeat([]byte{' '}, V5SandboxMaxJSONBytes+1), 64); err != ErrV5SandboxBudget {
		t.Fatal("oversize input", err)
	}
	for _, limit := range []int{0, 1, V5SandboxMaxJSONBytes + 1} {
		if _, err := s.Invoke(context.Background(), []byte(`{}`), limit); err != ErrV5SandboxBudget {
			t.Fatal("output budget", err)
		}
	}
	for name, result := range map[string]uint64{"pointer-overflow": uint64(0xffffffff)<<32 | 32, "huge-size": 0xffffffff, "empty": 0} {
		t.Run(name, func(t *testing.T) {
			body := append([]byte{0x42}, v5SLEB(int64(result))...)
			guest := v5Sandbox(t, v5WasmFixture{body: body}, v5Policy())
			if out, err := guest.Invoke(context.Background(), []byte(`{}`), 64); err == nil || out != nil {
				t.Fatal("invalid result leaked", out, err)
			}
		})
	}
	for _, data := range [][]byte{[]byte(`not-json`), {'"', 0xff, '"'}} {
		packed := uint64(65536)<<32 | uint64(len(data))
		guest := v5Sandbox(t, v5WasmFixture{body: append([]byte{0x42}, v5SLEB(int64(packed))...), minPages: 2, data: data}, v5Policy())
		if out, err := guest.Invoke(context.Background(), []byte(`{}`), 64); out != nil || err != ErrV5SandboxABI {
			t.Fatal("invalid guest JSON released", out, err)
		}
	}
	trap := v5Sandbox(t, v5WasmFixture{body: []byte{0, 0x42, 0}}, v5Policy())
	if out, err := trap.Invoke(context.Background(), []byte(`{}`), 64); out != nil || err != ErrV5SandboxCall {
		t.Fatal("trap not sanitized", out, err)
	}
	if out, err := s.Invoke(context.Background(), []byte(`{"healthy":true}`), 64); err != nil || len(out) == 0 {
		t.Fatal("one guest failure affected other sandbox", err)
	}
}

func v5Loop() []byte { return []byte{0x03, 0x40, 0x0c, 0, 0x0b, 0x42, 0} }

func TestV5SandboxHardDeadlineIncludesStartSection(t *testing.T) {
	for _, start := range []bool{false, true} {
		t.Run(map[bool]string{false: "handler-loop", true: "start-section-loop"}[start], func(t *testing.T) {
			policy := v5Policy()
			policy.AdminGrant.TimeoutMS = 80
			fixture := v5WasmFixture{body: v5Loop()}
			if start {
				fixture = v5WasmFixture{startLoop: true}
			}
			s := v5Sandbox(t, fixture, policy)
			before := time.Now()
			out, err := s.Invoke(context.Background(), []byte(`{}`), 64)
			if out != nil || err != ErrV5SandboxDeadline || time.Since(before) > time.Second {
				t.Fatal("real guest loop survived hard timeout", out, err, time.Since(before))
			}
		})
	}
}

func TestV5SandboxCallerCancellationAndGrantExpiry(t *testing.T) {
	s := v5Sandbox(t, v5WasmFixture{body: v5Loop()}, v5Policy())
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	before := time.Now()
	if out, err := s.Invoke(ctx, []byte(`{}`), 64); out != nil || !errors.Is(err, context.DeadlineExceeded) || time.Since(before) > time.Second {
		t.Fatal("caller deadline did not terminate guest", out, err)
	}
	ctx2, cancel2 := context.WithCancel(context.Background())
	cancel2()
	if _, err := s.Invoke(ctx2, []byte(`{}`), 64); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled call admitted", err)
	}
	policy := v5Policy()
	policy.GrantExpiresAtMS = time.Now().Add(200 * time.Millisecond).UnixMilli()
	expiring := v5Sandbox(t, v5WasmFixture{body: v5Loop()}, policy)
	if out, err := expiring.Invoke(context.Background(), []byte(`{}`), 64); out != nil || err != security.ErrResourceGrantExpired {
		t.Fatal("grant expiry did not terminate guest", out, err)
	}
	if _, err := expiring.Invoke(context.Background(), []byte(`{}`), 64); err != security.ErrResourceGrantExpired {
		t.Fatal("expired grant reused", err)
	}
}

func TestV5SandboxConcurrencyAndCloseCancelsInflight(t *testing.T) {
	policy := v5Policy()
	policy.DeploymentCeiling.MaxConcurrency = 1
	s := v5Sandbox(t, v5WasmFixture{body: v5Loop()}, policy)
	done := make(chan error, 1)
	go func() { _, err := s.Invoke(context.Background(), []byte(`{}`), 64); done <- err }()
	until := time.Now().Add(time.Second)
	for {
		s.mu.Lock()
		active := s.active
		s.mu.Unlock()
		if active == 1 {
			break
		}
		if time.Now().After(until) {
			t.Fatal("guest was never admitted")
		}
		time.Sleep(time.Millisecond)
	}
	if _, err := s.Invoke(context.Background(), []byte(`{}`), 64); err != ErrV5SandboxBudget {
		t.Fatal("concurrency ceiling exceeded", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := s.Close(ctx); err != nil {
		t.Fatal("close did not drain actual guest", err)
	}
	if err := <-done; err != ErrV5SandboxClosed {
		t.Fatal("inflight guest result survived close", err)
	}
	if _, err := s.Invoke(context.Background(), []byte(`{}`), 64); err != ErrV5SandboxClosed {
		t.Fatal("admission survived close", err)
	}
}
