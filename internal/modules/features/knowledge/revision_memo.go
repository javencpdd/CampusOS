package knowledge

import (
	"context"
	"reflect"
	"sync"
	"sync/atomic"
)

// Only the pure digest of an exact value is memoized. Every caller still loads
// and validates current origin/G1/Feature facts BEFORE looking here. A changed
// byte, epoch or relation changes the key. No candidate, allow or error is cached.
type revisionMemoKey struct{}
type revisionMemo struct {
	mu           sync.Mutex
	values       map[revisionMemoSlot]revisionMemoValue
	count, bytes int
	active       atomic.Bool
}

type revisionMemoSlot struct{ kind, id string }

type revisionMemoValue struct {
	bound  any
	digest string
}

func withRevisionMemo(ctx context.Context) (context.Context, func()) {
	m := &revisionMemo{}
	m.active.Store(true)
	return context.WithValue(ctx, revisionMemoKey{}, m), func() { m.active.Store(false) }
}
func memoRevision(ctx context.Context, slot revisionMemoSlot, key any, weight int, compute func() (string, error)) (string, error) {
	m, ok := ctx.Value(revisionMemoKey{}).(*revisionMemo)
	if !ok || !m.active.Load() || (key == nil || !reflect.TypeOf(key).Comparable()) {
		return compute()
	}
	// The small slot locates a value; exact bound equality still decides reuse.
	// This avoids hashing a 64KiB body again just to find its digest. A changed
	// body with the same claimed ID/epoch still compares unequal and recomputes.
	m.mu.Lock()
	entry, found := m.values[slot]
	valid := m.active.Load() && found && entry.bound == key
	m.mu.Unlock()
	if valid {
		return entry.digest, nil
	}
	v, e := compute()
	if e != nil {
		return "", e
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.active.Load() && m.count < MaxMembers*4 && m.bytes+weight+512 <= MaxSearchTextBytes {
		if m.values == nil {
			m.values = make(map[revisionMemoSlot]revisionMemoValue)
		}
		m.count++
		m.bytes += weight + 512
		m.values[slot] = revisionMemoValue{key, v}
	}
	return v, nil
}
