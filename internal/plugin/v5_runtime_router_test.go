package plugin

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

type routerTestFactory struct {
	calls int
	fail  bool
	runs  []*v5CoordinatorTestRun
}

func (f *routerTestFactory) Prepare(context.Context, V5RuntimeSpec, V5RuntimeBroker) (V5RunningRuntime, error) {
	f.calls++
	if f.fail {
		return nil, ErrV5RuntimeMissing
	}
	r := &v5CoordinatorTestRun{done: make(chan struct{})}
	f.runs = append(f.runs, r)
	return r, nil
}
func TestV5RuntimeRouterSharedBudgetsAndRelease(t *testing.T) {
	ctx := context.Background()
	w, c := &routerTestFactory{}, &routerTestFactory{}
	router := NewV5RuntimeRouter(w, c)
	resources := PluginV5ManifestResources{MemoryMB: 256, CPUMillis: 1000, MaxConcurrency: 1, TimeoutMS: 5000}
	wasm := V5RuntimeSpec{Program: V5WasmProgram{digest: "verified"}, Effective: resources}
	container := V5RuntimeSpec{Container: V5ContainerProgram{packageDigest: "verified"}, Effective: resources}
	broker := func(context.Context, string, string, json.RawMessage) (json.RawMessage, error) { return nil, nil }
	for i := 0; i < 2; i++ {
		if _, e := router.Prepare(ctx, wasm, broker); e != nil {
			t.Fatal(e)
		}
		if _, e := router.Prepare(ctx, container, broker); e != nil {
			t.Fatal(e)
		}
	}
	if _, e := router.Prepare(ctx, container, broker); e == nil || c.calls != 2 {
		t.Fatal("aggregate budget bypass")
	}
	w.runs[0].Abort()
	until := time.Now().Add(time.Second)
	for {
		router.mu.Lock()
		n := router.instances
		router.mu.Unlock()
		if n == 3 {
			break
		}
		if time.Now().After(until) {
			t.Fatal("reservation not released on actual Done")
		}
		time.Sleep(time.Millisecond)
	}
	if _, e := router.Prepare(ctx, container, broker); e != nil {
		t.Fatal(e)
	}
	for _, r := range append(w.runs, c.runs...) {
		r.Abort()
	}
}
func TestV5RuntimeRouterFailClosedAndFailedPreparation(t *testing.T) {
	ctx := context.Background()
	f := &routerTestFactory{fail: true}
	router := NewV5RuntimeRouter(nil, f)
	s := V5RuntimeSpec{Container: V5ContainerProgram{packageDigest: "verified"}, Effective: PluginV5ManifestResources{MemoryMB: 256, CPUMillis: 1000, MaxConcurrency: 1, TimeoutMS: 5000}}
	for i := 0; i < 10; i++ {
		if _, e := router.Prepare(ctx, s, nil); e == nil {
			t.Fatal("failed prepare admitted")
		}
	}
	if router.instances != 0 || router.memory != 0 || router.cpu != 0 {
		t.Fatal("failed preparation leaked reservation")
	}
	s.Program = V5WasmProgram{digest: "other"}
	if _, e := router.Prepare(ctx, s, nil); e == nil {
		t.Fatal("ambiguous artifact")
	}
	s.Container = V5ContainerProgram{}
	if _, e := router.Prepare(ctx, s, nil); e == nil {
		t.Fatal("disabled family admitted")
	}
}
