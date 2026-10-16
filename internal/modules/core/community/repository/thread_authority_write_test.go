package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	"github.com/campusos/CampusOS/internal/platform/transaction"
)

func TestThreadAuthorityWriteFactsCASAndCounters(t *testing.T) {
	ctx := context.Background()
	repo := NewMemoryThreadRepository()
	thread := &domain.Thread{ID: "201", AuthorID: "301", CategoryID: "401", Title: "old", Content: "body", PublicationStatus: "published", ModerationStatus: "clear", DeletionStatus: "active"}
	if err := repo.Create(ctx, thread); err != nil {
		t.Fatal(err)
	}
	kernel := transaction.NewMemory(repo)
	if _, err := repo.LockThreadForWrite(ctx, thread.ID); !errors.Is(err, ErrThreadWriteTransactionRequired) {
		t.Fatal(err)
	}
	if err := repo.UpdateIfFactsVersion(ctx, thread, thread.FactsVersion); !errors.Is(err, ErrThreadWriteTransactionRequired) {
		t.Fatal(err)
	}
	if err := kernel.Within(ctx, func(tx context.Context) error {
		current, err := repo.LockThreadForWrite(tx, thread.ID)
		if err != nil {
			return err
		}
		before := current.FactsVersion
		if _, err := repo.IncrementViewCount(ctx, thread.ID); err != nil {
			return err
		}
		if err := repo.IncrementReplyCount(ctx, thread.ID, 1); err != nil {
			return err
		}
		current.Title = "edited"
		current.CurrentRevision++
		if err := repo.UpdateIfFactsVersion(tx, current, before); err != nil {
			return err
		}
		if current.ViewCount != 1 || current.ReplyCount != 1 || current.FactsVersion <= before {
			t.Fatalf("counters or facts lost: %+v", current)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	before, _ := repo.GetByID(ctx, thread.ID)
	sentinel := errors.New("required audit failure")
	err := kernel.Within(ctx, func(tx context.Context) error {
		candidate, _ := repo.LockThreadForWrite(tx, thread.ID)
		candidate.Title = "not committed"
		candidate.CurrentRevision++
		if err := repo.UpdateIfFactsVersion(tx, candidate, candidate.FactsVersion); err != nil {
			return err
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatal(err)
	}
	restored, _ := repo.GetByID(ctx, thread.ID)
	if restored.Title != before.Title || restored.FactsVersion != before.FactsVersion {
		t.Fatal("transaction failed to restore authoritative row")
	}
}

func TestThreadAuthorityWriteRejectsScopeABAWithoutRevisionChange(t *testing.T) {
	for _, field := range []string{"owner", "board", "state", "body-ABA"} {
		t.Run(field, func(t *testing.T) {
			ctx := context.Background()
			repo := NewMemoryThreadRepository()
			row := &domain.Thread{ID: "202", AuthorID: "302", CategoryID: "402", Title: "original", Content: "body", PublicationStatus: "published", ModerationStatus: "clear", DeletionStatus: "active"}
			repo.Create(ctx, row)
			candidate, _ := repo.GetByID(ctx, row.ID)
			expected := candidate.FactsVersion
			candidate.Title = "must not commit"
			candidate.CurrentRevision++
			changed, _ := repo.GetByID(ctx, row.ID)
			switch field {
			case "owner":
				changed.AuthorID = "303"
			case "board":
				changed.CategoryID = "403"
			case "state":
				changed.DeletionStatus = "trashed"
			case "body-ABA":
				changed.Content = "other"
				repo.Update(ctx, changed)
				changed.Content = "body"
			}
			repo.Update(ctx, changed)
			err := transaction.NewMemory(repo).Within(ctx, func(tx context.Context) error { return repo.UpdateIfFactsVersion(tx, candidate, expected) })
			if !errors.Is(err, ErrThreadRevisionConflict) {
				t.Fatalf("stale %s allow: %v", field, err)
			}
			current, _ := repo.GetByID(ctx, row.ID)
			if current.Title == candidate.Title {
				t.Fatal("stale mutation persisted")
			}
		})
	}
}
