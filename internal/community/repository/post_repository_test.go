package repository

import (
	"context"
	"testing"
	"time"

	"github.com/campusos/CampusOS/internal/community/domain"
)

func TestMemoryPostRepositoryAssignsAndSortsFloorNumbers(t *testing.T) {
	repo := NewMemoryPostRepository()
	ctx := context.Background()

	posts := []*domain.Post{
		{ID: "p2", ThreadID: "t1", Content: "second", CreatedAt: time.Now().Add(2 * time.Second)},
		{ID: "other", ThreadID: "t2", Content: "other", CreatedAt: time.Now()},
		{ID: "p1", ThreadID: "t1", Content: "first", CreatedAt: time.Now().Add(time.Second)},
	}
	for _, post := range posts {
		if err := repo.Create(ctx, post); err != nil {
			t.Fatalf("create post %s: %v", post.ID, err)
		}
	}

	got, total, err := repo.ListByThread(ctx, "t1", 1, 20)
	if err != nil {
		t.Fatalf("list posts: %v", err)
	}
	if total != 2 || len(got) != 2 {
		t.Fatalf("expected two posts, got total=%d len=%d", total, len(got))
	}
	if got[0].FloorNumber != 1 || got[1].FloorNumber != 2 {
		t.Fatalf("unexpected floor numbers: %d, %d", got[0].FloorNumber, got[1].FloorNumber)
	}
	if got[0].ID != "p2" || got[1].ID != "p1" {
		t.Fatalf("posts not sorted by assigned floor: %s, %s", got[0].ID, got[1].ID)
	}

	if err := repo.Delete(ctx, "p1"); err != nil {
		t.Fatalf("delete p1: %v", err)
	}
	next := &domain.Post{ID: "p3", ThreadID: "t1", Content: "third", CreatedAt: time.Now().Add(3 * time.Second)}
	if err := repo.Create(ctx, next); err != nil {
		t.Fatalf("create p3: %v", err)
	}
	if next.FloorNumber != 3 {
		t.Fatalf("expected deleted floor not to be reused, got floor %d", next.FloorNumber)
	}
}
