package repository

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
)

func TestMemoryThreadListSnapshotPaginationAndRevocation(t *testing.T) {
	ctx := context.Background()
	repo := NewMemoryThreadRepository()
	createdAt := time.Date(2026, 10, 11, 0, 0, 0, 0, time.UTC)
	for _, id := range []string{"10", "20", "30", "9", "hidden"} {
		thread := publicSnapshotFixture()
		thread.ID, thread.CreatedAt = id, createdAt
		thread.IsPinned = id == "10"
		if id == "hidden" {
			thread.PublicationStatus = domain.PublicationStatusPrivate
		}
		if err := repo.Create(ctx, thread); err != nil {
			t.Fatal(err)
		}
	}
	filter := domain.ThreadListFilter{
		PublicationStatus: "published", ModerationStatus: "clear", DeletionStatus: "active", PageSize: 2,
	}
	check := func(page int, wantIDs []string, wantTotal int64) {
		t.Helper()
		filter.Page = page
		threads, total, err := repo.List(ctx, filter)
		if err != nil || total != wantTotal || threads == nil {
			t.Fatalf("page %d: threads=%+v total=%d err=%v", page, threads, total, err)
		}
		gotIDs := make([]string, 0, len(threads))
		for _, thread := range threads {
			gotIDs = append(gotIDs, thread.ID)
		}
		if !reflect.DeepEqual(gotIDs, wantIDs) {
			t.Fatalf("page %d: IDs=%v want=%v", page, gotIDs, wantIDs)
		}
	}
	// Same creation times must not let a map iteration change page membership.
	for i := 0; i < 20; i++ {
		check(1, []string{"10", "9"}, 4)
		check(2, []string{"30", "20"}, 4)
		check(3, []string{}, 4)
	}
	thread, err := repo.GetByID(ctx, "9")
	if err != nil {
		t.Fatal(err)
	}
	thread.ModerationStatus = domain.ModerationStatusTakenDown
	if err = repo.Update(ctx, thread); err != nil {
		t.Fatal(err)
	}
	// The public predicate is applied before both total and offset; removal
	// fills page one and moves the next page instead of leaving a sparse page.
	check(1, []string{"10", "30"}, 3)
	check(2, []string{"20"}, 3)
	check(3, []string{}, 3)
}

func TestMemoryThreadListSnapshotPaginationBoundaries(t *testing.T) {
	ctx := context.Background()
	repo := NewMemoryThreadRepository()
	if err := repo.Create(ctx, publicSnapshotFixture()); err != nil {
		t.Fatal(err)
	}
	maxInt := int(^uint(0) >> 1)
	for _, filter := range []domain.ThreadListFilter{
		{Page: -1, PageSize: -1}, {Page: 1, PageSize: maxInt},
		{Page: 2, PageSize: maxInt}, {Page: maxInt, PageSize: 1},
	} {
		threads, total, err := repo.List(ctx, filter)
		if err != nil || total != 1 || threads == nil {
			t.Fatalf("safe boundary %+v: threads=%+v total=%d err=%v", filter, threads, total, err)
		}
		wantLength := 0
		if filter.Page <= 1 {
			wantLength = 1
		}
		if len(threads) != wantLength {
			t.Fatalf("boundary %+v returned %d rows want %d", filter, len(threads), wantLength)
		}
	}
	for _, filter := range []domain.ThreadListFilter{
		{Page: 3, PageSize: maxInt}, {Page: maxInt, PageSize: 20},
	} {
		threads, total, err := repo.List(ctx, filter)
		if threads != nil || total != 0 || !errors.Is(err, ErrThreadPaginationOverflow) {
			t.Fatalf("overflow %+v: threads=%+v total=%d err=%v", filter, threads, total, err)
		}
	}
}

func TestMemoryThreadListSnapshotConcurrentCountAndRows(t *testing.T) {
	ctx := context.Background()
	repo := NewMemoryThreadRepository()
	if err := repo.Create(ctx, publicSnapshotFixture()); err != nil {
		t.Fatal(err)
	}
	filter := domain.ThreadListFilter{
		PublicationStatus: "published", ModerationStatus: "clear", DeletionStatus: "active", PageSize: 20,
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
			thread.PublicationStatus, thread.Content = domain.PublicationStatusPrivate, "private body"
			if i%2 == 1 {
				thread.PublicationStatus, thread.Content = domain.PublicationStatusPublished, "public body"
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
			threads, total, err := repo.List(ctx, filter)
			if err != nil || int64(len(threads)) != total {
				failures <- fmt.Errorf("count/page snapshot differs: total=%d rows=%d err=%v", total, len(threads), err)
				return
			}
			for _, thread := range threads {
				if thread.PublicationStatus != domain.PublicationStatusPublished || thread.Content != "public body" {
					failures <- fmt.Errorf("nonpublic row returned: %+v", thread)
					return
				}
			}
		}
	}()
	workers.Wait()
	close(failures)
	for err := range failures {
		t.Error(err)
	}
}
