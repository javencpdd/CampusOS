package transaction

import (
	"context"
	"errors"
	"testing"
)

type snapshotValue struct{ value int }

func (s *snapshotValue) Snapshot() any     { return s.value }
func (s *snapshotValue) Restore(value any) { s.value = value.(int) }

func TestMemoryRestoresSnapshotsOnFailure(t *testing.T) {
	value := &snapshotValue{value: 1}
	manager := NewMemory(value)
	err := manager.Within(context.Background(), func(ctx context.Context) error {
		if !Active(ctx) {
			t.Fatal("transaction context was not marked active")
		}
		value.value = 2
		return errors.New("rollback")
	})
	if err == nil {
		t.Fatal("expected transaction error")
	}
	if value.value != 1 {
		t.Fatalf("snapshot was not restored: got %d", value.value)
	}
}

func TestMemoryRestoresSnapshotsOnPanic(t *testing.T) {
	value := &snapshotValue{value: 1}
	manager := NewMemory(value)
	func() {
		defer func() {
			if recovered := recover(); recovered == nil {
				t.Fatal("expected transaction panic to propagate")
			}
		}()
		_ = manager.Within(context.Background(), func(context.Context) error {
			value.value = 2
			panic("simulated command panic")
		})
	}()
	if value.value != 1 {
		t.Fatalf("snapshot was not restored after panic: got %d", value.value)
	}
}
