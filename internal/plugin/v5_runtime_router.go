package plugin

import (
	"context"
	"sync"
)

// One leader and one aggregate reservation pool cover both runtime families.
// Release follows actual process termination, never a timeout of Close.
type V5RuntimeRouter struct {
	wasm, container        V5RuntimeFactory
	mu                     sync.Mutex
	memory, cpu, instances int64
}

func NewV5RuntimeRouter(wasm, container V5RuntimeFactory) *V5RuntimeRouter {
	return &V5RuntimeRouter{wasm: wasm, container: container}
}
func (f *V5RuntimeRouter) Prepare(ctx context.Context, s V5RuntimeSpec, b V5RuntimeBroker) (V5RunningRuntime, error) {
	var target V5RuntimeFactory
	switch {
	case s.Program.Digest() != "" && s.Container.PackageDigest() == "":
		target = f.wasm
	case s.Program.Digest() == "" && s.Container.PackageDigest() != "":
		target = f.container
	default:
		return nil, ErrV5RuntimeMissing
	}
	r := s.Effective
	if target == nil || ctx.Err() != nil || r.MemoryMB < 1 || r.MemoryMB > 256 || r.CPUMillis < 1 || r.CPUMillis > 1000 || r.MaxConcurrency != 1 || r.TimeoutMS < 1 || r.TimeoutMS > v5ChatMaxTimeoutMS {
		return nil, ErrV5RuntimeMissing
	}
	f.mu.Lock()
	if f.instances >= 16 || f.memory+r.MemoryMB > 1024 || f.cpu+r.CPUMillis > 4000 {
		f.mu.Unlock()
		return nil, ErrV5RuntimeMissing
	}
	f.instances++
	f.memory += r.MemoryMB
	f.cpu += r.CPUMillis
	f.mu.Unlock()
	release := func() { f.mu.Lock(); defer f.mu.Unlock(); f.instances--; f.memory -= r.MemoryMB; f.cpu -= r.CPUMillis }
	h, err := target.Prepare(ctx, s, b)
	if err != nil {
		release()
		return nil, err
	}
	go func() { <-h.Done(); release() }()
	return h, nil
}
