package repository

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	"github.com/campusos/CampusOS/internal/platform/transaction"
)

func TestThreadAdminPurgeMemoryTombstoneSnapshotCASAndRollback(t *testing.T) {
	ctx := context.Background()
	r := NewMemoryThreadRepository()
	row := &domain.Thread{ID: "purge.thread", AuthorID: "owner", CategoryID: "board", Title: "private title", Content: "private original", PublicationStatus: "private", ModerationStatus: "taken_down", DeletionStatus: "trashed", CurrentRevision: 1, CreatedAt: time.Now().UTC()}
	if err := r.Create(ctx, row); err != nil {
		t.Fatal(err)
	}
	if _, err := r.LockThreadGovernanceSnapshot(ctx, row.ID); !errors.Is(err, ErrThreadWriteTransactionRequired) {
		t.Fatal(err)
	}
	base := r.Snapshot()
	high := row.FactsVersion
	if err := transaction.NewMemory(r).Within(ctx, func(tx context.Context) error {
		initial, err := r.LockThreadGovernanceSnapshot(tx, row.ID)
		if err != nil {
			return err
		}
		if _, err := r.PurgeThreadIfFacts(tx, row.ID, initial.FactsVersion+1, initial.CurrentRevision); !errors.Is(err, ErrThreadRevisionConflict) {
			t.Fatal("stale nonce", err)
		}
		if _, err := r.PurgeThreadIfFacts(tx, row.ID, initial.FactsVersion, initial.CurrentRevision+1); !errors.Is(err, ErrThreadRevisionConflict) {
			t.Fatal("stale revision", err)
		}
		written, err := r.PurgeThreadIfFacts(tx, row.ID, initial.FactsVersion, initial.CurrentRevision)
		if err != nil {
			return err
		}
		high = written.FactsVersion
		if written.DeletedAt == nil || written.DeletionStatus != "purged" || written.CurrentRevision != initial.CurrentRevision+1 || written.FactsVersion <= initial.FactsVersion || written.Content != initial.Content {
			t.Fatal(written)
		}
		if _, err := r.GetByID(tx, row.ID); !errors.Is(err, ErrThreadNotFound) {
			t.Fatal("normal read exposed tombstone", err)
		}
		if items, total, err := r.List(tx, domain.ThreadListFilter{IncludeTrashed: true}); err != nil || total != 0 || len(items) != 0 {
			t.Fatal(items, total, err)
		}
		at := *written.DeletedAt
		*written.DeletedAt = at.Add(time.Hour)
		written.Content = "mutated caller"
		actual, err := r.LockThreadGovernanceSnapshot(tx, row.ID)
		if err != nil || !actual.DeletedAt.Equal(at) || actual.Content != row.Content {
			t.Fatal("tombstone alias", actual, err)
		}
		wire, _ := json.Marshal(actual)
		if string(wire) == "" || containsDeletedDate(wire) {
			t.Fatal("hidden date became wire fact", string(wire))
		}
		if _, err := r.PurgeThreadIfFacts(tx, row.ID, actual.FactsVersion, actual.CurrentRevision); !errors.Is(err, ErrThreadRevisionConflict) {
			t.Fatal("repeat purge accepted", err)
		}
		return errors.New("forced rollback")
	}); err == nil {
		t.Fatal("forced rollback ignored")
	}
	actual, err := r.GetByID(ctx, row.ID)
	if err != nil || actual.DeletedAt != nil || actual.DeletionStatus != "trashed" || actual.FactsVersion != row.FactsVersion {
		t.Fatal(actual, err)
	}
	if err := transaction.NewMemory(r).Within(ctx, func(tx context.Context) error {
		written, err := r.PurgeThreadIfFacts(tx, row.ID, actual.FactsVersion, actual.CurrentRevision)
		if err != nil {
			return err
		}
		if written.FactsVersion <= high {
			t.Fatal("purge nonce reused")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	r.Restore(base)
	if current, err := r.GetByID(ctx, row.ID); err != nil || current.DeletedAt != nil {
		t.Fatal(current, err)
	}
}

func containsDeletedDate(wire []byte) bool {
	var fields map[string]any
	if json.Unmarshal(wire, &fields) != nil {
		return true
	}
	_, exists := fields["deleted_at"]
	return exists
}
