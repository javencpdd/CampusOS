package wasm

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/campusos/CampusOS/internal/plugin"
	"os"
	"strings"
	"testing"
	"time"
)

func TestV5NativeWorkerActualIsolation(t *testing.T) {
	path := os.Getenv("CAMPUSOS_WASM_WORKER_TEST_IMAGE")
	if path == "" {
		t.Skip("requires owned Linux static worker image")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	f, err := NewV5IsolatedFactory(path)
	if err != nil {
		t.Fatal(err)
	}
	program := v5SignedProgram(t, v5Module(v5WasmFixture{}))
	r := program.Resources()
	r.MaxConcurrency = 1
	r.CPUMillis = 500
	r.TimeoutMS = 2000
	spec := plugin.V5RuntimeSpec{Program: program, Coordinate: program.Coordinate(), Owner: "1001", Generation: 42, ExpiresAtMS: time.Now().Add(time.Minute).UnixMilli(), Effective: r}
	h, err := f.Prepare(ctx, spec, func(context.Context, string, string, json.RawMessage) (json.RawMessage, error) {
		return nil, plugin.ErrV5Denied
	})
	if err != nil {
		t.Fatal("actual Docker cgroup worker", err)
	}
	defer h.Close(ctx)
	d := plugin.V5ContributionDescriptor{V5ContributionRef: plugin.V5ContributionRef{V5Coordinate: program.Coordinate(), ID: "chat"}, InstanceGeneration: 42}
	var out bytes.Buffer
	if err := h.Invoke(ctx, plugin.V5ChatTarget{Contribution: d, Owner: "1001"}, json.RawMessage(`{"actual_worker":true}`), &out); err != nil || out.String() != `{"actual_worker":true}` {
		t.Fatal(out.String(), err)
	}
	if err := h.Close(ctx); err != nil {
		t.Fatal("worker did not exit", err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.instances != 0 || f.cpu != 0 || f.memory != 0 {
		t.Fatal("native reservation leaked")
	}
}

func TestV5WorkerResolvedImageID(t *testing.T) {
	// The input reference may identify a manifest index; Docker resolves it to
	// this platform's configuration ID before the worker is pinned and launched.
	id := "sha256:" + strings.Repeat("a", 64)
	got, err := resolveV5WorkerImage([]byte(id + "\n"))
	if err != nil || got != id {
		t.Fatal(got, err)
	}
	for _, raw := range []string{"", "worker:latest", "sha256:abc", id + "\n" + id, "sha256:" + strings.Repeat("A", 64)} {
		if got, err := resolveV5WorkerImage([]byte(raw)); err == nil || got != "" {
			t.Fatal("invalid resolved ID accepted", raw)
		}
	}
}

func nativeFixture(t *testing.T, f *V5IsolatedFactory, ctx context.Context, body []byte, generation int64) plugin.V5RunningRuntime {
	t.Helper()
	p := v5SignedProgram(t, v5Module(v5WasmFixture{body: body}))
	r := p.Resources()
	r.MaxConcurrency, r.CPUMillis, r.TimeoutMS = 1, 500, 2000
	h, err := f.Prepare(ctx, plugin.V5RuntimeSpec{Program: p, Coordinate: p.Coordinate(), Owner: "1001", Generation: generation, ExpiresAtMS: time.Now().Add(time.Minute).UnixMilli(), Effective: r}, func(context.Context, string, string, json.RawMessage) (json.RawMessage, error) {
		return nil, plugin.ErrV5Denied
	})
	if err != nil {
		t.Fatal("actual native preparation", err)
	}
	t.Cleanup(func() {
		c, cancel := context.WithTimeout(context.Background(), 6*time.Second)
		defer cancel()
		if err := h.Close(c); err != nil {
			t.Error(err)
		}
	})
	return h
}

func nativeInvoke(ctx context.Context, h plugin.V5RunningRuntime, generation int64) error {
	return h.Invoke(ctx, plugin.V5ChatTarget{Owner: "1001", Contribution: plugin.V5ContributionDescriptor{V5ContributionRef: plugin.V5ContributionRef{V5Coordinate: plugin.V5Coordinate{Publisher: "sandbox-test", Key: "probe", Version: "1.0.0"}, ID: "chat"}, InstanceGeneration: generation}}, json.RawMessage(`{"actual":true}`), &bytes.Buffer{})
}

func TestV5NativeWorkerFaultsAndBudget(t *testing.T) {
	image := os.Getenv("CAMPUSOS_WASM_WORKER_TEST_IMAGE")
	if image == "" {
		t.Skip("requires owned Linux static worker image")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Second)
	defer cancel()
	f, err := NewV5IsolatedFactory(image)
	if err != nil {
		t.Fatal(err)
	}
	healthy := nativeFixture(t, f, ctx, nil, 1)
	t.Run("kernel-controls-and-no-host-access", func(t *testing.T) {
		proof := healthy.(interface{ IsolationProof() map[string]string }).IsolationProof()
		if proof["memory.max"] != "33554432" || proof["memory.swap.max"] != "0" || proof["pids.max"] != "64" || proof["cpu.max"] != "50000 100000" {
			t.Fatal(proof)
		}
		t.Logf("actual cgroup controls: %v", proof)
		// The parent already checks no mounts/network/caps, read-only root,
		// fixed image, non-root uid, exact env and disabled logging at readiness.
		for _, fixture := range []v5WasmFixture{{importModule: "wasi_snapshot_preview1", importName: "path_open"}, {importModule: "wasi_snapshot_preview1", importName: "sock_open"}, {importModule: "env", importName: "getenv"}} {
			p := v5SignedProgram(t, v5Module(fixture))
			r := p.Resources()
			r.MaxConcurrency = 1
			r.CPUMillis = 500
			h, err := f.Prepare(ctx, plugin.V5RuntimeSpec{Program: p, Coordinate: p.Coordinate(), Effective: r, ExpiresAtMS: time.Now().Add(time.Minute).UnixMilli()}, func(context.Context, string, string, json.RawMessage) (json.RawMessage, error) {
				return nil, plugin.ErrV5Denied
			})
			if err == nil {
				_ = h.Close(ctx)
				t.Fatal("forbidden OS import ready", fixture.importName)
			}
		}
	})

	t.Run("deployment-orphan-fence-preserves-peer", func(t *testing.T) {
		other, err := NewV5IsolatedFactoryForDeployment(image, strings.Repeat("a", 64))
		if err != nil {
			t.Fatal(err)
		}
		orphan := nativeFixture(t, other, ctx, nil, 99)
		replacement, err := NewV5IsolatedFactoryForDeployment(image, strings.Repeat("a", 64))
		if err != nil {
			t.Fatal(err)
		}
		if err := replacement.FenceOrphans(ctx); err != nil {
			t.Fatal(err)
		}
		select {
		case <-orphan.Done():
		case <-time.After(6 * time.Second):
			t.Fatal("orphan was not fenced")
		}
		if err := nativeInvoke(ctx, healthy, 1); err != nil {
			t.Fatal("another deployment was reaped", err)
		}
	})
	t.Run("actual-oom-and-neighbor-survival", func(t *testing.T) {
		// Grow to the declared 32MiB and touch every byte. RSS + engine overhead
		// exceeds memory.max; a Wasm page limit alone would permit this.
		body := append([]byte{0x41}, v5SLEB(511)...)
		body = append(body, 0x40, 0, 0x1a, 0x41, 0, 0x41, 1, 0x41)
		body = append(body, v5SLEB(32<<20)...)
		body = append(body, 0xfc, 0x0b, 0, 0x20, 1, 0xad)
		h := nativeFixture(t, f, ctx, body, 2)
		if err := nativeInvoke(ctx, h, 2); err == nil {
			t.Fatal("OOM guest succeeded")
		}
		select {
		case <-h.Done():
		case <-time.After(8 * time.Second):
			t.Fatal("OOM scope not reaped")
		}
		if status := h.(interface{ ExitStatus() string }).ExitStatus(); status != "oom_killed" {
			t.Fatal("kernel OOM not observed", status)
		}
		if err := nativeInvoke(ctx, healthy, 1); err != nil {
			t.Fatal("neighbor failed", err)
		}
	})
	t.Run("deadline-kills-actual-loop", func(t *testing.T) {
		h := nativeFixture(t, f, ctx, []byte{0x03, 0x40, 0x0c, 0, 0x0b, 0x42, 0}, 3)
		call, stop := context.WithTimeout(ctx, 80*time.Millisecond)
		defer stop()
		if err := nativeInvoke(call, h, 3); err == nil {
			t.Fatal("loop did not stop")
		}
		select {
		case <-h.Done():
		case <-time.After(8 * time.Second):
			t.Fatal("canceled scope not reaped")
		}
		if err := nativeInvoke(ctx, healthy, 1); err != nil {
			t.Fatal("neighbor failed", err)
		}
	})
	t.Run("aggregate-reservation-before-launch", func(t *testing.T) {
		workers := []plugin.V5RunningRuntime{}
		for n := 0; n < 7; n++ {
			workers = append(workers, nativeFixture(t, f, ctx, nil, int64(n+10)))
		}
		if len(f.Workers()) != 8 {
			t.Fatal("expected eight real workers")
		}
		p := v5SignedProgram(t, v5Module(v5WasmFixture{}))
		r := p.Resources()
		r.MaxConcurrency = 1
		r.CPUMillis = 500
		h, err := f.Prepare(ctx, plugin.V5RuntimeSpec{Program: p, Coordinate: p.Coordinate(), Effective: r, ExpiresAtMS: time.Now().Add(time.Minute).UnixMilli()}, func(context.Context, string, string, json.RawMessage) (json.RawMessage, error) {
			return nil, plugin.ErrV5Denied
		})
		if err != ErrV5SandboxBudget {
			if h != nil {
				_ = h.Close(ctx)
			}
			t.Fatal("aggregate CPU overcommit", err)
		}
		for _, h := range workers {
			if err := h.Close(ctx); err != nil {
				t.Fatal(err)
			}
		}
		if err := nativeInvoke(ctx, healthy, 1); err != nil {
			t.Fatal(err)
		}
	})
	if err := healthy.Close(ctx); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.instances != 0 || f.memory != 0 || f.cpu != 0 || len(f.workers) != 0 {
		t.Fatal("native budget leaked")
	}
}
