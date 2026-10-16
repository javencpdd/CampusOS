package reliability

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"reflect"
	"testing"
)

func TestSpaceAvatarRetentionScopedOperationsFilterBeforeLimitAndDetachedRollback(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	manager := transaction.NewMemory(store)
	owned, err := store.StartOperation(ctx, Operation{ID: "owned", Kind: "retention", SubjectType: "space", SubjectID: "1001", ActorID: "1001", Details: json.RawMessage(`{"private":"receipt"}`)})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 110; i++ {
		if _, err := store.StartOperation(ctx, Operation{ID: fmt.Sprint(i), Kind: "retention", SubjectType: "space", SubjectID: "1002", Details: json.RawMessage(`{}`)}); err != nil {
			t.Fatal(err)
		}
	}
	items, err := store.ListSubjectOperations(ctx, "retention", "space", "1001", 1)
	if err != nil || len(items) != 1 || items[0].ID != "owned" {
		t.Fatal("owner filter followed limit", items, err)
	}
	owned.Details[2] = 'X'
	items[0].Details[2] = 'Y'
	current, err := store.GetSubjectOperation(ctx, "owned", "retention", "space", "1001", false)
	if err != nil || string(current.Details) != `{"private":"receipt"}` {
		t.Fatal("operation Details alias", current, err)
	}
	if _, err := store.GetSubjectOperation(ctx, "owned", "retention", "space", "1002", false); !errors.Is(err, ErrOperationNotFound) {
		t.Fatal("other subject returned", err)
	}
	if _, err := store.GetSubjectOperation(ctx, "owned", "retention", "space", "1001", true); err == nil {
		t.Fatal("lock outside SDK accepted")
	}
	before := store.Snapshot()
	sentinel := errors.New("rollback")
	err = manager.Within(ctx, func(tx context.Context) error {
		op, err := store.GetSubjectOperation(tx, "owned", "retention", "space", "1001", true)
		if err != nil {
			return err
		}
		op.Details = json.RawMessage(`{"changed":true}`)
		op.Status = OperationSucceeded
		if err := store.UpdateOperation(tx, *op); err != nil {
			return err
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) || !reflect.DeepEqual(before, store.Snapshot()) {
		t.Fatal("operation rollback failed", err)
	}
	store.Restore(before)
	op, _ := store.GetSubjectOperation(ctx, "owned", "retention", "space", "1001", false)
	op.Details[2] = 'Z'
	store.Restore(before)
	current, err = store.GetSubjectOperation(ctx, "owned", "retention", "space", "1001", false)
	if err != nil || string(current.Details) != `{"private":"receipt"}` {
		t.Fatal("restore reused mutable snapshot", current, err)
	}
}
