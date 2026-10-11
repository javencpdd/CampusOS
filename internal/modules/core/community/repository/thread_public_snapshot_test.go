package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
)

func publicSnapshotFixture() *domain.Thread {
	return &domain.Thread{
		ID: "thread-1", AuthorID: "user-1", CategoryID: "board-1",
		Title: "public title", Content: "public body", ContentFormat: "markdown",
		PublicationStatus: domain.PublicationStatusPublished,
		ModerationStatus:  domain.ModerationStatusClear,
		DeletionStatus:    domain.DeletionStatusActive,
		CurrentRevision:   1, Tags: []string{"public"},
	}
}

func TestMemoryPublicThreadSnapshotRejectsABAAndReplacement(t *testing.T) {
	ctx := context.Background()
	repo := NewMemoryThreadRepository()
	thread := publicSnapshotFixture()
	thread.FactsVersion = 9999 // Allocation belongs to the repository.
	if err := repo.Create(ctx, thread); err != nil {
		t.Fatal(err)
	}
	first := thread.FactsVersion
	if first <= 0 || first == 9999 {
		t.Fatalf("untrusted version survived insert: %d", first)
	}
	thread.PublicationStatus = domain.PublicationStatusPrivate
	if err := repo.Update(ctx, thread); err != nil {
		t.Fatal(err)
	}
	privateVersion := thread.FactsVersion
	thread.PublicationStatus = domain.PublicationStatusPublished
	thread.FactsVersion = first // Attempt to restore the old authorization stamp.
	if err := repo.Update(ctx, thread); err != nil {
		t.Fatal(err)
	}
	if thread.FactsVersion <= privateVersion || privateVersion <= first {
		t.Fatalf("ABA reused version: first=%d private=%d restored=%d", first, privateVersion, thread.FactsVersion)
	}
	for _, countView := range []bool{false, true} {
		if got, err := repo.RecheckPublicThread(ctx, thread.ID, first, countView); got != nil || !errors.Is(err, ErrThreadNotFound) {
			t.Fatalf("stale ABA decision returned content: %+v, %v", got, err)
		}
	}
	currentVersion := thread.FactsVersion
	if err := repo.Purge(ctx, thread.ID); err != nil {
		t.Fatal(err)
	}
	if err := repo.Create(ctx, thread); err != nil {
		t.Fatal(err)
	}
	if thread.FactsVersion <= currentVersion {
		t.Fatalf("reinsert reused version: previous=%d current=%d", currentVersion, thread.FactsVersion)
	}
	if got, err := repo.RecheckPublicThread(ctx, thread.ID, currentVersion, true); got != nil || !errors.Is(err, ErrThreadNotFound) {
		t.Fatalf("deleted incarnation decision returned replacement: %+v, %v", got, err)
	}
	current, err := repo.GetByID(ctx, thread.ID)
	if err != nil || current.ViewCount != 0 {
		t.Fatalf("denied rechecks incremented views: %+v, %v", current, err)
	}
}

func TestMemoryThreadFactsVersionRollbackPreservesHiddenFactsAndAllocation(t *testing.T) {
	ctx := context.Background()
	repo := NewMemoryThreadRepository()
	empty := repo.Snapshot()
	thread := publicSnapshotFixture()
	if err := repo.Create(ctx, thread); err != nil {
		t.Fatal(err)
	}
	rolledBackCreate := thread.FactsVersion
	repo.Restore(empty)
	if _, err := repo.GetByID(ctx, thread.ID); !errors.Is(err, ErrThreadNotFound) {
		t.Fatalf("empty rollback did not remove created thread: %v", err)
	}
	if err := repo.Create(ctx, thread); err != nil {
		t.Fatal(err)
	}
	if thread.FactsVersion <= rolledBackCreate {
		t.Fatal("rollback reused an allocated insert version")
	}
	originalVersion := thread.FactsVersion
	snapshot := repo.Snapshot()
	thread.Title, thread.Tags[0] = "rolled-back body", "rolled-back tag"
	if err := repo.Update(ctx, thread); err != nil {
		t.Fatal(err)
	}
	rolledBackUpdate := thread.FactsVersion
	repo.Restore(snapshot)
	restored, err := repo.GetByID(ctx, thread.ID)
	if err != nil || restored.FactsVersion != originalVersion || restored.Title != "public title" || restored.Tags[0] != "public" {
		t.Fatalf("snapshot failed to preserve hidden version/content: %+v, %v", restored, err)
	}
	restored.Title = "committed later"
	if err := repo.Update(ctx, restored); err != nil {
		t.Fatal(err)
	}
	if restored.FactsVersion <= rolledBackUpdate {
		t.Fatal("rollback reused an allocated update version")
	}
	raw, err := json.Marshal(restored)
	if err != nil || strings.Contains(string(raw), "facts_version") || strings.Contains(string(raw), "FactsVersion") {
		t.Fatalf("repository version leaked through client JSON: %s, %v", raw, err)
	}
}

func TestMemoryPublicThreadSnapshotCountersAndMissingState(t *testing.T) {
	ctx := context.Background()
	repo := NewMemoryThreadRepository()
	thread := publicSnapshotFixture()
	if err := repo.Create(ctx, thread); err != nil {
		t.Fatal(err)
	}
	version := thread.FactsVersion
	for _, countView := range []bool{false, true} {
		got, err := repo.RecheckPublicThread(ctx, thread.ID, version, countView)
		if err != nil || got.FactsVersion != version || got.Content != "public body" {
			t.Fatalf("current snapshot failed: %+v, %v", got, err)
		}
	}
	if err := repo.IncrementReplyCount(ctx, thread.ID, 1); err != nil {
		t.Fatal(err)
	}
	got, err := repo.IncrementViewCount(ctx, thread.ID)
	if err != nil || got.FactsVersion != version || got.ViewCount != 2 || got.ReplyCount != 1 {
		t.Fatalf("counter-only writes changed facts version: %+v, %v", got, err)
	}
	// Emulate corrupt/legacy stored facts. Reads must not promote missing state.
	repo.mu.Lock()
	repo.threads[thread.ID].PublicationStatus = ""
	repo.mu.Unlock()
	got, err = repo.GetByID(ctx, thread.ID)
	if err != nil || got.PublicationStatus != "" {
		t.Fatalf("read defaulted missing publication fact: %+v, %v", got, err)
	}
	if got, err = repo.RecheckPublicThread(ctx, thread.ID, version, true); got != nil || !errors.Is(err, ErrThreadNotFound) {
		t.Fatalf("missing fact passed public recheck: %+v, %v", got, err)
	}
}

func TestMemoryPublicThreadSnapshotConcurrentRevocation(t *testing.T) {
	ctx := context.Background()
	repo := NewMemoryThreadRepository()
	if err := repo.Create(ctx, publicSnapshotFixture()); err != nil {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	failures := make(chan error, 2)
	workers.Add(2)
	go func() {
		defer workers.Done()
		for i := 0; i < 100; i++ {
			thread, err := repo.GetByID(ctx, "thread-1")
			if err != nil {
				failures <- err
				return
			}
			thread.PublicationStatus = domain.PublicationStatusPrivate
			thread.Content = "private body"
			if i%2 == 1 {
				thread.PublicationStatus = domain.PublicationStatusPublished
				thread.Content = "public body"
			}
			if err := repo.Update(ctx, thread); err != nil {
				failures <- err
				return
			}
		}
	}()
	go func() {
		defer workers.Done()
		for i := 0; i < 200; i++ {
			initial, err := repo.GetByID(ctx, "thread-1")
			if err != nil {
				failures <- err
				return
			}
			current, err := repo.RecheckPublicThread(ctx, initial.ID, initial.FactsVersion, true)
			if errors.Is(err, ErrThreadNotFound) {
				continue
			}
			if err != nil || current.PublicationStatus != domain.PublicationStatusPublished || current.Content != "public body" || current.FactsVersion != initial.FactsVersion {
				failures <- fmt.Errorf("revocation returned unauthorized snapshot: %+v, %v", current, err)
				return
			}
		}
	}()
	workers.Wait()
	close(failures)
	for err := range failures {
		t.Error(err)
	}
}
