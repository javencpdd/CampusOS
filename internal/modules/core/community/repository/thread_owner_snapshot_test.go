package repository

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
)

func TestMemoryOwnerThreadSnapshotStatesAndNoViews(t *testing.T) {
	ctx := context.Background()
	for _, publication := range []domain.PublicationStatus{"draft", "published", "private"} {
		for _, moderation := range []domain.ModerationStatus{"clear", "pending", "rejected", "taken_down"} {
			for _, deletion := range []domain.DeletionStatus{"active", "trashed", "purged"} {
				t.Run(fmt.Sprintf("%s/%s/%s", publication, moderation, deletion), func(t *testing.T) {
					repo := NewMemoryThreadRepository()
					thread := publicSnapshotFixture()
					thread.PublicationStatus, thread.ModerationStatus, thread.DeletionStatus = publication, moderation, deletion
					thread.ViewCount, thread.ReplyCount, thread.LikeCount = 17, 9, 4
					if err := repo.Create(ctx, thread); err != nil {
						t.Fatal(err)
					}
					got, err := repo.RecheckOwnerThread(ctx, thread.ID, thread.AuthorID, thread.FactsVersion)
					if deletion == domain.DeletionStatusPurged {
						if got != nil || !errors.Is(err, ErrThreadNotFound) {
							t.Fatalf("purged owner snapshot returned: %+v %v", got, err)
						}
						return
					}
					if err != nil || got == nil || got.FactsVersion != thread.FactsVersion || got.AuthorID != "user-1" ||
						got.PublicationStatus != publication || got.ModerationStatus != moderation || got.DeletionStatus != deletion ||
						got.ViewCount != 17 || got.ReplyCount != 9 || got.LikeCount != 4 || got.Content != thread.Content {
						t.Fatalf("current owner snapshot changed facts or counters: %+v %v", got, err)
					}
					stored, err := repo.GetByID(ctx, thread.ID)
					if err != nil || stored.ViewCount != 17 {
						t.Fatalf("owner read inflated views: %+v %v", stored, err)
					}
				})
			}
		}
	}
}

func TestMemoryOwnerThreadSnapshotRejectsOwnerABAAndReplacement(t *testing.T) {
	ctx := context.Background()
	repo := NewMemoryThreadRepository()
	thread := publicSnapshotFixture()
	thread.PublicationStatus = domain.PublicationStatusPrivate
	if err := repo.Create(ctx, thread); err != nil {
		t.Fatal(err)
	}
	initial := thread.FactsVersion
	for _, test := range []struct {
		id      string
		owner   string
		version int64
	}{{thread.ID, "other-owner", initial}, {thread.ID, "", initial}, {thread.ID, "user-1", 0},
		{thread.ID, "user-1", -1}, {"missing-thread", "user-1", initial}} {
		if got, err := repo.RecheckOwnerThread(ctx, test.id, test.owner, test.version); got != nil || !errors.Is(err, ErrThreadNotFound) {
			t.Fatalf("mismatched owner/id/version returned: %+v %v", got, err)
		}
	}
	thread.AuthorID, thread.Content = "other-owner", "other owner's private content"
	if err := repo.Update(ctx, thread); err != nil {
		t.Fatal(err)
	}
	changedOwner := thread.FactsVersion
	if got, err := repo.RecheckOwnerThread(ctx, thread.ID, "user-1", changedOwner); got != nil || !errors.Is(err, ErrThreadNotFound) {
		t.Fatalf("old owner read changed-owner content: %+v %v", got, err)
	}
	if got, err := repo.RecheckOwnerThread(ctx, thread.ID, "other-owner", initial); got != nil || !errors.Is(err, ErrThreadNotFound) {
		t.Fatalf("new owner reused old fact epoch: %+v %v", got, err)
	}
	if got, err := repo.RecheckOwnerThread(ctx, thread.ID, "other-owner", changedOwner); err != nil || got.Content != "other owner's private content" {
		t.Fatalf("current new-owner snapshot failed: %+v %v", got, err)
	}
	thread.AuthorID, thread.Content, thread.FactsVersion = "user-1", "public body", initial
	if err := repo.Update(ctx, thread); err != nil {
		t.Fatal(err)
	}
	if thread.FactsVersion <= changedOwner {
		t.Fatal("owner ABA reused fact epoch")
	}
	if got, err := repo.RecheckOwnerThread(ctx, thread.ID, "user-1", initial); got != nil || !errors.Is(err, ErrThreadNotFound) {
		t.Fatalf("owner ABA accepted stale decision: %+v %v", got, err)
	}
	restored := thread.FactsVersion
	if err := repo.Purge(ctx, thread.ID); err != nil {
		t.Fatal(err)
	}
	if got, err := repo.RecheckOwnerThread(ctx, thread.ID, "user-1", restored); got != nil || !errors.Is(err, ErrThreadNotFound) {
		t.Fatalf("purged owner snapshot returned: %+v %v", got, err)
	}
	if err := repo.Create(ctx, thread); err != nil {
		t.Fatal(err)
	}
	if thread.FactsVersion <= restored {
		t.Fatal("replacement reused a fact epoch")
	}
	if got, err := repo.RecheckOwnerThread(ctx, thread.ID, "user-1", restored); got != nil || !errors.Is(err, ErrThreadNotFound) {
		t.Fatalf("replacement accepted predecessor decision: %+v %v", got, err)
	}
}

func TestMemoryOwnerThreadSnapshotRawFactsFailClosed(t *testing.T) {
	mutations := []struct {
		name   string
		mutate func(*domain.Thread)
	}{
		{"publication-missing", func(t *domain.Thread) { t.PublicationStatus = "" }},
		{"publication-unknown", func(t *domain.Thread) { t.PublicationStatus = "unknown" }},
		{"moderation-missing", func(t *domain.Thread) { t.ModerationStatus = "" }},
		{"moderation-unknown", func(t *domain.Thread) { t.ModerationStatus = "unknown" }},
		{"deletion-missing", func(t *domain.Thread) { t.DeletionStatus = "" }},
		{"deletion-unknown", func(t *domain.Thread) { t.DeletionStatus = "unknown" }},
		{"deletion-purged", func(t *domain.Thread) { t.DeletionStatus = domain.DeletionStatusPurged }},
		{"version-missing", func(t *domain.Thread) { t.FactsVersion = 0 }},
		{"owner-missing", func(t *domain.Thread) { t.AuthorID = "" }},
		{"row-id-mismatch", func(t *domain.Thread) { t.ID = "other-thread" }},
	}
	ctx := context.Background()
	for _, mutation := range mutations {
		t.Run(mutation.name, func(t *testing.T) {
			repo := NewMemoryThreadRepository()
			thread := publicSnapshotFixture()
			thread.PublicationStatus = domain.PublicationStatusPrivate
			if err := repo.Create(ctx, thread); err != nil {
				t.Fatal(err)
			}
			repo.mu.Lock()
			stored := repo.threads[thread.ID]
			stored.Status = domain.ThreadStatusPublished // A legacy fallback cannot repair the missing facts.
			mutation.mutate(stored)
			repo.mu.Unlock()
			if got, err := repo.RecheckOwnerThread(ctx, thread.ID, thread.AuthorID, thread.FactsVersion); got != nil || !errors.Is(err, ErrThreadNotFound) {
				t.Fatalf("corrupt raw facts authorized: %+v %v", got, err)
			}
		})
	}
	// An optional adapter must also reject a missing row value safely.
	repo := NewMemoryThreadRepository()
	repo.threads["empty-row"] = nil
	if got, err := repo.RecheckOwnerThread(ctx, "empty-row", "opaque-owner", 1); got != nil || !errors.Is(err, ErrThreadNotFound) {
		t.Fatalf("nil raw row authorized: %+v %v", got, err)
	}
}

func TestMemoryOwnerThreadSnapshotCountersAndContent(t *testing.T) {
	ctx := context.Background()
	repo := NewMemoryThreadRepository()
	thread := publicSnapshotFixture()
	thread.PublicationStatus = domain.PublicationStatusPrivate
	if err := repo.Create(ctx, thread); err != nil {
		t.Fatal(err)
	}
	version := thread.FactsVersion
	if _, err := repo.IncrementViewCount(ctx, thread.ID); err != nil {
		t.Fatal(err)
	}
	if err := repo.IncrementReplyCount(ctx, thread.ID, 3); err != nil {
		t.Fatal(err)
	}
	got, err := repo.RecheckOwnerThread(ctx, thread.ID, thread.AuthorID, version)
	if err != nil || got.FactsVersion != version || got.ViewCount != 1 || got.ReplyCount != 3 || got.Content != thread.Content {
		t.Fatalf("counter changes invalidated or changed owner snapshot: %+v %v", got, err)
	}
	got.Content, got.Tags[0] = "mutated returned content", "mutated returned tag"
	stored, err := repo.GetByID(ctx, thread.ID)
	if err != nil || stored.Content != "public body" || stored.Tags[0] != "public" || stored.ViewCount != 1 {
		t.Fatalf("returned snapshot aliases storage or changed views: %+v %v", stored, err)
	}
	stored.Content = "new owner content"
	if err := repo.Update(ctx, stored); err != nil {
		t.Fatal(err)
	}
	if got, err := repo.RecheckOwnerThread(ctx, thread.ID, thread.AuthorID, version); got != nil || !errors.Is(err, ErrThreadNotFound) {
		t.Fatalf("content change consumed stale owner facts: %+v %v", got, err)
	}
	if got, err := repo.RecheckOwnerThread(ctx, stored.ID, stored.AuthorID, stored.FactsVersion); err != nil || got.Content != "new owner content" || got.ViewCount != 1 {
		t.Fatalf("fresh content snapshot failed or counted a view: %+v %v", got, err)
	}
}

func TestMemoryOwnerThreadSnapshotConcurrentOwnerRevocation(t *testing.T) {
	ctx := context.Background()
	repo := NewMemoryThreadRepository()
	thread := publicSnapshotFixture()
	thread.PublicationStatus, thread.Content = domain.PublicationStatusPrivate, "user-1 body"
	if err := repo.Create(ctx, thread); err != nil {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	failures := make(chan error, 2)
	workers.Add(2)
	go func() {
		defer workers.Done()
		for i := 0; i < 100; i++ {
			current, err := repo.GetByID(ctx, thread.ID)
			if err != nil {
				failures <- err
				return
			}
			current.AuthorID, current.Content = "other-owner", "other-owner body"
			if i%2 == 1 {
				current.AuthorID, current.Content = "user-1", "user-1 body"
			}
			if err := repo.Update(ctx, current); err != nil {
				failures <- err
				return
			}
		}
	}()
	go func() {
		defer workers.Done()
		for i := 0; i < 200; i++ {
			initial, err := repo.GetByID(ctx, thread.ID)
			if err != nil {
				failures <- err
				return
			}
			current, err := repo.RecheckOwnerThread(ctx, initial.ID, "user-1", initial.FactsVersion)
			if errors.Is(err, ErrThreadNotFound) {
				continue
			}
			if err != nil || current == nil || current.AuthorID != "user-1" || current.Content != "user-1 body" ||
				current.FactsVersion != initial.FactsVersion || current.ViewCount != 0 {
				failures <- fmt.Errorf("ownership revoked but content returned or counted: %+v %v", current, err)
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

func TestPostgreSQLOwnerThreadSnapshotInvalidVersionAvoidsQuery(t *testing.T) {
	// A nil pool makes an accidental query observable without provisioning a
	// database; invalid stamps must fail before touching the connection layer.
	repo := NewPgThreadRepository(nil)
	for _, version := range []int64{0, -1} {
		if got, err := repo.RecheckOwnerThread(context.Background(), "thread", "opaque-owner", version); got != nil || !errors.Is(err, ErrThreadNotFound) {
			t.Fatalf("invalid fact epoch reached database or returned content: %+v %v", got, err)
		}
	}
}
