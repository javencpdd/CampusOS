package repository

import (
	"context"
	"testing"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
)

func TestMemoryNotificationRepositoryScopesOrdersAndMarksRead(t *testing.T) {
	ctx := context.Background()
	repo := NewMemoryNotificationRepository()
	base := time.Date(2026, 7, 26, 8, 0, 0, 0, time.UTC)
	for _, value := range []*domain.Notification{
		{ID: "1", UserID: "1001", Title: "older", Metadata: map[string]interface{}{}, CreatedAt: base, UpdatedAt: base},
		{ID: "2", UserID: "1001", Title: "newer", Metadata: map[string]interface{}{}, CreatedAt: base.Add(time.Minute), UpdatedAt: base.Add(time.Minute)},
		{ID: "3", UserID: "1002", Title: "other user", Metadata: map[string]interface{}{}, CreatedAt: base.Add(2 * time.Minute), UpdatedAt: base.Add(2 * time.Minute)},
	} {
		if err := repo.Create(ctx, value); err != nil {
			t.Fatal(err)
		}
	}

	items, total, err := repo.ListByUser(ctx, "1001", 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 || len(items) != 2 || items[0].ID != "2" || items[1].ID != "1" {
		t.Fatalf("unexpected scoped notification list: total=%d items=%#v", total, items)
	}
	if unread, err := repo.CountUnread(ctx, "1001"); err != nil || unread != 2 {
		t.Fatalf("unexpected unread count: count=%d err=%v", unread, err)
	}
	if err := repo.MarkRead(ctx, "1002", "2", base.Add(3*time.Minute)); err != ErrNotificationNotFound {
		t.Fatalf("cross-user mark read must be hidden as not found, got %v", err)
	}
	if err := repo.MarkRead(ctx, "1001", "2", base.Add(3*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if updated, err := repo.MarkAllRead(ctx, "1001", base.Add(4*time.Minute)); err != nil || updated != 1 {
		t.Fatalf("unexpected mark-all result: updated=%d err=%v", updated, err)
	}
	if unread, _ := repo.CountUnread(ctx, "1001"); unread != 0 {
		t.Fatalf("expected all user notifications to be read, got %d", unread)
	}
	if unread, _ := repo.CountUnread(ctx, "1002"); unread != 1 {
		t.Fatalf("other user's notification changed, unread=%d", unread)
	}
}

func TestMemoryNotificationRepositorySnapshotRestoresWrites(t *testing.T) {
	ctx := context.Background()
	repo := NewMemoryNotificationRepository()
	snapshot := repo.Snapshot()
	now := time.Now().UTC()
	if err := repo.Create(ctx, &domain.Notification{
		ID: "1", UserID: "1001", Title: "temporary", Metadata: map[string]interface{}{}, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	repo.Restore(snapshot)
	if items, total, err := repo.ListByUser(ctx, "1001", 1, 20); err != nil || total != 0 || len(items) != 0 {
		t.Fatalf("snapshot restore left notification behind: total=%d items=%#v err=%v", total, items, err)
	}
}
