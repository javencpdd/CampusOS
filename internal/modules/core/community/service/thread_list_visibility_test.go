package service

import (
	"context"
	"testing"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	"github.com/campusos/CampusOS/internal/modules/core/community/repository"
	"github.com/campusos/CampusOS/pkg/cache"
)

func TestPublicListRechecksPublicationAfterAnotherServiceChangesIt(t *testing.T) {
	ctx := context.Background()
	repo := repository.NewMemoryThreadRepository()
	if err := repo.Create(ctx, &domain.Thread{ID: "3001", Title: "private after edit", Content: "sensitive body", AuthorID: "2001", Status: domain.ThreadStatusPublished}); err != nil {
		t.Fatal(err)
	}
	shared := cache.NewMemoryCache()
	reader, writer := NewThreadService(repo, nil), NewThreadService(repo, nil)
	reader.SetCache(shared)
	writer.SetCache(shared)
	for _, size := range []int{20, 7} {
		items, total, err := reader.ListThreads(ctx, domain.ThreadListFilter{Page: 1, PageSize: size})
		if err != nil || total != 1 || len(items) != 1 {
			t.Fatalf("warm list: total=%d items=%d err=%v", total, len(items), err)
		}
	}
	private := domain.ThreadStatusPrivate
	if _, err := writer.UpdateThread(ctx, "3001", "2001", domain.UpdateThreadRequest{Status: &private}); err != nil {
		t.Fatal(err)
	}
	for _, size := range []int{20, 7} {
		items, total, err := reader.ListThreads(ctx, domain.ThreadListFilter{Page: 1, PageSize: size})
		if err != nil || total != 0 || len(items) != 0 {
			t.Fatalf("private content leaked from public list size=%d: total=%d items=%d err=%v", size, total, len(items), err)
		}
	}
}
