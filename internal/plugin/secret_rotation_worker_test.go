package plugin

import (
	"context"
	"errors"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/campusos/CampusOS/internal/platform/reliability"
)

type memoryRotationInventory struct{ *MemorySecretStore }

func (s memoryRotationInventory) ListActiveSecretRewrapPluginIDs(ctx context.Context, key string, after int64, limit int) ([]int64, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	found := map[int64]bool{}
	for _, row := range s.items {
		if row.PluginID > after && row.Status == "active" && row.RevokedAt == nil && row.KeyVersion != key {
			found[row.PluginID] = true
		}
	}
	ids := make([]int64, 0, len(found))
	for id := range found {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	if len(ids) > limit {
		ids = ids[:limit]
	}
	return ids, nil
}

type rotationTestLock struct {
	busy                        bool
	failAfter, checks, released int
}

func (l *rotationTestLock) TryAcquire(context.Context) (SecretRotationGuard, bool, error) {
	return l, !l.busy, nil
}
func (l *rotationTestLock) Check(context.Context) error {
	l.checks++
	if l.failAfter > 0 && l.checks >= l.failAfter {
		return errors.New("lease disappeared")
	}
	return nil
}
func (l *rotationTestLock) Release(context.Context) error { l.released++; return nil }

type rotationTestAudit struct {
	*reliability.MemoryStore
	failStart, failUpdate bool
}

func (a rotationTestAudit) StartOperation(ctx context.Context, o reliability.Operation) (*reliability.Operation, error) {
	if a.failStart {
		return nil, errors.New("audit unavailable")
	}
	return a.MemoryStore.StartOperation(ctx, o)
}
func (a rotationTestAudit) UpdateOperation(ctx context.Context, o reliability.Operation) error {
	if a.failUpdate {
		return errors.New("audit unavailable")
	}
	return a.MemoryStore.UpdateOperation(ctx, o)
}
func rotationTestConfig() SecretRotationConfig {
	return SecretRotationConfig{Enabled: true, Interval: time.Second, BatchSize: 1, MaxBatchesPerPlugin: 1, MaxPluginsPerRun: 1}
}

func TestSecretRotationWorkerDisabledAndConfiguration(t *testing.T) {
	w, err := NewSecretRotationWorker(nil, nil, nil, nil, SecretRotationConfig{})
	if err != nil {
		t.Fatal(err)
	}
	result, err := w.RunOnce(t.Context())
	if err != nil || !result.Disabled {
		t.Fatalf("disabled: %+v %v", result, err)
	}
	if _, err := NewSecretRotationWorker(nil, nil, nil, nil, rotationTestConfig()); !errors.Is(err, ErrSecretRotationConfig) {
		t.Fatal(err)
	}
}
func TestSecretRotationWorkerBoundedFairnessAndAudit(t *testing.T) {
	ctx := t.Context()
	store := NewMemorySecretStore()
	old, rotating, current := testSecretRewrapServices(t, store)
	for _, id := range []int64{1, 2, 3} {
		if _, err := old.Put(ctx, id, nil, "hidden.name", "hidden-value", nil); err != nil {
			t.Fatal(err)
		}
	}
	store.mu.Lock()
	r := store.items[memorySecretKey(1, nil, "hidden.name")]
	r.Ciphertext[0] ^= 0xff
	store.items[memorySecretKey(1, nil, "hidden.name")] = r
	store.mu.Unlock()
	audit := reliability.NewMemoryStore()
	lock := &rotationTestLock{}
	w, err := NewSecretRotationWorker(rotating, memoryRotationInventory{store}, audit, lock, rotationTestConfig())
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		got, err := w.RunOnce(ctx)
		if err != nil || got.ScannedPlugins != 1 {
			t.Fatalf("pass %d: %+v %v", i, got, err)
		}
		if i == 0 && got.FailedPlugins != 1 {
			t.Fatal("corrupt plugin must fail")
		}
		if i > 0 && got.Rewrapped != 1 {
			t.Fatal("corrupt low ID starved later plugin")
		}
	}
	for _, id := range []int64{2, 3} {
		if value, err := current.Resolve(ctx, id, nil, "hidden.name"); err != nil || value != "hidden-value" {
			t.Fatal("later plugin not readable with current key")
		}
	}
	operations, total, err := audit.ListOperations(ctx, secretRotationOperationKind, reliability.PageRequest{Page: 1, PageSize: 10})
	if err != nil || total != 3 {
		t.Fatalf("audit: %d %v", total, err)
	}
	failures := 0
	for _, op := range operations {
		if op.Status == reliability.OperationFailed {
			failures++
		}
		if strings.Contains(string(op.Details), "hidden") || strings.Contains(op.Error, "hidden") {
			t.Fatal("audit leaked payload")
		}
	}
	if failures != 1 || lock.released != 3 {
		t.Fatalf("failures=%d releases=%d", failures, lock.released)
	}
}
func TestSecretRotationWorkerLeaseAndAuditFailures(t *testing.T) {
	for _, mode := range []string{"busy", "lost", "start-audit", "update-audit"} {
		t.Run(mode, func(t *testing.T) {
			store := NewMemorySecretStore()
			old, rotating, _ := testSecretRewrapServices(t, store)
			if _, err := old.Put(t.Context(), 1, nil, "token", "value", nil); err != nil {
				t.Fatal(err)
			}
			lock := &rotationTestLock{busy: mode == "busy"}
			if mode == "lost" {
				lock.failAfter = 3
			}
			audit := rotationTestAudit{MemoryStore: reliability.NewMemoryStore(), failStart: mode == "start-audit", failUpdate: mode == "update-audit"}
			w, err := NewSecretRotationWorker(rotating, memoryRotationInventory{store}, audit, lock, rotationTestConfig())
			if err != nil {
				t.Fatal(err)
			}
			got, err := w.RunOnce(t.Context())
			row, _ := store.ActiveSecret(t.Context(), 1, nil, "token")
			switch mode {
			case "busy":
				if err != nil || !got.LeaseBusy || lock.released != 0 {
					t.Fatalf("busy: %+v %v", got, err)
				}
			case "lost":
				if !errors.Is(err, ErrSecretRotationLease) {
					t.Fatal(err)
				}
			default:
				if !errors.Is(err, ErrSecretRotationAudit) {
					t.Fatal(err)
				}
			}
			if mode != "update-audit" && row.KeyVersion != "old-v1" {
				t.Fatal("rewrote before lease/audit admission")
			}
			if mode == "update-audit" && got.Rewrapped != 1 {
				t.Fatal("partial commit count missing")
			}
		})
	}
}
func TestSecretRotationWorkerCancellationBeforeFirstTick(t *testing.T) {
	store := NewMemorySecretStore()
	old, rotating, _ := testSecretRewrapServices(t, store)
	_, _ = old.Put(t.Context(), 1, nil, "token", "value", nil)
	lock := &rotationTestLock{}
	w, err := NewSecretRotationWorker(rotating, memoryRotationInventory{store}, reliability.NewMemoryStore(), lock, rotationTestConfig())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := w.Run(ctx); err != nil || lock.checks != 0 {
		t.Fatal("cancelled worker entered rotation")
	}
}
