package repository

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
)

func ownerListSnapshotFixture(id string) *domain.Thread {
	return &domain.Thread{ID: id, AuthorID: "owner-1", CategoryID: "1", Title: "Study " + id, Content: "owner body", Tags: []string{"blue", "study"},
		ContentFormat: "markdown", ThreadType: domain.ThreadTypeMutualAid,
		PublicationStatus: domain.PublicationStatusPrivate, ModerationStatus: domain.ModerationStatusClear, DeletionStatus: domain.DeletionStatusActive,
		CreatedAt: time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC), ViewCount: 7}
}

func TestMemoryOwnerThreadListSnapshotRawStateMatrix(t *testing.T) {
	ctx := context.Background()
	repo := NewMemoryThreadRepository()
	for _, publication := range []domain.PublicationStatus{"draft", "published", "private"} {
		for _, moderation := range []domain.ModerationStatus{"clear", "pending", "rejected", "taken_down"} {
			for _, deletion := range []domain.DeletionStatus{"active", "trashed", "purged"} {
				thread := ownerListSnapshotFixture(string(publication) + "-" + string(moderation) + "-" + string(deletion))
				thread.PublicationStatus, thread.ModerationStatus, thread.DeletionStatus = publication, moderation, deletion
				if err := repo.Create(ctx, thread); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	other := ownerListSnapshotFixture("other-owner")
	other.AuthorID = "owner-2"
	if err := repo.Create(ctx, other); err != nil {
		t.Fatal(err)
	}
	// Raw invalid states cannot inherit authority from legacy Status. Directly
	// corrupt only this local fixture; production writes enforce their schema.
	for i, change := range []func(*domain.Thread){
		func(t *domain.Thread) { t.PublicationStatus = "" },
		func(t *domain.Thread) { t.PublicationStatus = "unexpected" },
		func(t *domain.Thread) { t.ModerationStatus = "" },
		func(t *domain.Thread) { t.ModerationStatus = "unexpected" },
		func(t *domain.Thread) { t.DeletionStatus = "" },
	} {
		thread := ownerListSnapshotFixture(fmt.Sprintf("invalid-%d", i))
		thread.Status = domain.ThreadStatusPublished
		change(thread)
		repo.threads[thread.ID] = thread
	}
	repo.threads["nil"] = nil
	rows, total, err := repo.ListOwnerThreadSnapshot(ctx, "owner-1", domain.ThreadListFilter{
		AuthorID: "owner-2", Status: "published", PublicationStatus: "published", ModerationStatus: "clear", DeletionStatus: "trashed", IncludeTrashed: true, PageSize: 100,
	})
	if err != nil || total != 12 || len(rows) != 12 {
		t.Fatalf("raw owner set: rows=%d total=%d err=%v", len(rows), total, err)
	}
	for _, row := range rows {
		if row.AuthorID != "owner-1" || row.DeletionStatus != domain.DeletionStatusActive || !ownerThreadSnapshotStateValid(row) || row.ViewCount != 7 {
			t.Fatalf("owner list expanded visibility or counted a view: %+v", row)
		}
	}
}

func TestMemoryOwnerThreadListSnapshotFiltersAndPagination(t *testing.T) {
	ctx := context.Background()
	repo := NewMemoryThreadRepository()
	for _, id := range []string{"10", "20", "30", "9"} {
		thread := ownerListSnapshotFixture(id)
		thread.IsPinned = id == "10"
		if err := repo.Create(ctx, thread); err != nil {
			t.Fatal(err)
		}
	}
	filter := domain.ThreadListFilter{CategoryID: "1", CategoryIDs: []string{"1"}, ContentFormat: "markdown", ThreadType: domain.ThreadTypeMutualAid,
		Keyword: "STUDY", Tag: "blue", AnyTags: []string{"study"}, PageSize: 2}
	for page, want := range map[int][]string{1: {"10", "9"}, 2: {"30", "20"}, 3: {}} {
		filter.Page = page
		rows, total, err := repo.ListOwnerThreadSnapshot(ctx, "owner-1", filter)
		if err != nil || total != 4 || rows == nil {
			t.Fatalf("page %d: rows=%v total=%d err=%v", page, rows, total, err)
		}
		got := make([]string, 0, len(rows))
		for _, row := range rows {
			got = append(got, row.ID)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("page %d: got=%v want=%v", page, got, want)
		}
	}
	for _, change := range []func(*domain.ThreadListFilter){
		func(f *domain.ThreadListFilter) { f.CategoryID = "2" },
		func(f *domain.ThreadListFilter) { f.CategoryIDs = []string{} },
		func(f *domain.ThreadListFilter) { f.ContentFormat = "safe_html" },
		func(f *domain.ThreadListFilter) { f.ThreadType = domain.ThreadTypeSecondhand },
		func(f *domain.ThreadListFilter) { f.Keyword = "no match" },
		func(f *domain.ThreadListFilter) { f.Tag = "red" },
		func(f *domain.ThreadListFilter) { f.AnyTags = []string{"red"} },
	} {
		changed := filter
		changed.Page = 1
		change(&changed)
		rows, total, err := repo.ListOwnerThreadSnapshot(ctx, "owner-1", changed)
		if err != nil || rows == nil || len(rows) != 0 || total != 0 {
			t.Fatalf("business filter ignored: %+v rows=%v total=%d err=%v", changed, rows, total, err)
		}
	}
	rows, _, err := repo.ListOwnerThreadSnapshot(ctx, "owner-1", domain.ThreadListFilter{PageSize: 20})
	if err != nil {
		t.Fatal(err)
	}
	rows[0].Tags[0] = "changed returned clone"
	stored, err := repo.GetByID(ctx, rows[0].ID)
	if err != nil || stored.Tags[0] != "blue" || stored.ViewCount != 7 {
		t.Fatalf("returned row mutated authority/counters: stored=%+v err=%v", stored, err)
	}
}

func TestMemoryOwnerThreadListSnapshotConcurrentOwnerAndCount(t *testing.T) {
	ctx := context.Background()
	repo := NewMemoryThreadRepository()
	if err := repo.Create(ctx, ownerListSnapshotFixture("concurrent")); err != nil {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	workers.Add(2)
	failures := make(chan error, 2)
	go func() {
		defer workers.Done()
		for i := 0; i < 100; i++ {
			thread, err := repo.GetByID(ctx, "concurrent")
			if err != nil {
				failures <- err
				return
			}
			thread.AuthorID, thread.Content = "owner-2", "other body"
			if i%2 == 1 {
				thread.AuthorID, thread.Content = "owner-1", "owner body"
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
			rows, total, err := repo.ListOwnerThreadSnapshot(ctx, "owner-1", domain.ThreadListFilter{PageSize: 20})
			if err != nil || int64(len(rows)) != total {
				failures <- fmt.Errorf("mixed owner page/count: total=%d rows=%d err=%v", total, len(rows), err)
				return
			}
			for _, row := range rows {
				if row.AuthorID != "owner-1" || row.Content != "owner body" {
					failures <- fmt.Errorf("other owner's content entered snapshot: %+v", row)
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

func TestOwnerThreadListSnapshotRejectsInvalidInputBeforeQuery(t *testing.T) {
	ctx := context.Background()
	for _, owner := range []string{"", " owner", "owner/1", ".owner", strings.Repeat("a", 129)} {
		for _, repo := range []OwnerThreadListSnapshotRepository{NewMemoryThreadRepository(), NewPgThreadRepository(nil)} {
			rows, total, err := repo.ListOwnerThreadSnapshot(ctx, owner, domain.ThreadListFilter{})
			if !errors.Is(err, ErrThreadOwnerListInvalid) || rows != nil || total != 0 {
				t.Fatalf("invalid owner %q: rows=%v total=%d err=%v", owner, rows, total, err)
			}
		}
	}
	// A nil PG pool makes accidental queries panic: all cases must be rejected
	// before attempting storage, including a legal actor with invalid filters.
	pg := NewPgThreadRepository(nil)
	for _, owner := range []string{"0", "-1", "owner-1", "9223372036854775808"} {
		if _, _, err := pg.ListOwnerThreadSnapshot(ctx, owner, domain.ThreadListFilter{}); err == nil {
			t.Fatalf("PG accepted invalid bigint owner %q", owner)
		}
	}
	for _, filter := range []domain.ThreadListFilter{{CategoryID: "invalid"}, {CategoryIDs: []string{"invalid"}}, {Page: int(^uint(0) >> 1), PageSize: 20}} {
		if _, _, err := pg.ListOwnerThreadSnapshot(ctx, "1", filter); err == nil {
			t.Fatalf("PG accepted invalid filter before querying: %+v", filter)
		}
	}
	maxInt := int(^uint(0) >> 1)
	if _, _, err := NewMemoryThreadRepository().ListOwnerThreadSnapshot(ctx, "owner-1", domain.ThreadListFilter{Page: maxInt, PageSize: 20}); !errors.Is(err, ErrThreadPaginationOverflow) {
		t.Fatalf("memory accepted overflow: %v", err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	for _, repo := range []OwnerThreadListSnapshotRepository{NewMemoryThreadRepository(), pg} {
		if _, _, err := repo.ListOwnerThreadSnapshot(canceled, "1", domain.ThreadListFilter{}); !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled snapshot reached query: %v", err)
		}
	}
}
