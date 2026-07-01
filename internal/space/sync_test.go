package space

import (
	"context"
	"testing"
	"time"

	communitydomain "github.com/campusos/CampusOS/internal/community/domain"
	identitydomain "github.com/campusos/CampusOS/internal/core/identity/domain"
	"github.com/campusos/CampusOS/pkg/eventbus"
)

func TestSyncThreadUsesDefaultPublicSpace(t *testing.T) {
	repo := NewMemoryRepository()
	svc := NewService(repo, newFakeUserLookup(&identitydomain.User{
		ID:       "1001",
		Username: "alice",
		Nickname: "Alice",
	}))

	thread := testThread("1001", "1", []string{"go"})
	if err := svc.SyncThread(context.Background(), thread); err != nil {
		t.Fatalf("sync thread: %v", err)
	}

	contents, total, err := svc.ListPublicContentsByUserID(context.Background(), "1001", 1, 20)
	if err != nil {
		t.Fatalf("list public contents: %v", err)
	}
	if total != 1 || len(contents) != 1 {
		t.Fatalf("expected one synced content, total=%d len=%d", total, len(contents))
	}
	if contents[0].ThreadID != thread.ID {
		t.Fatalf("expected thread id %q, got %q", thread.ID, contents[0].ThreadID)
	}
	if contents[0].Excerpt == "" {
		t.Fatalf("expected excerpt")
	}
}

func TestSyncThreadAppliesCategoryAndTagFilters(t *testing.T) {
	repo := NewMemoryRepository()
	svc := NewService(repo, newFakeUserLookup(&identitydomain.User{
		ID:       "1001",
		Username: "alice",
		Nickname: "Alice",
	}))
	visibility := "public"
	if _, err := svc.UpsertOwnSpace(context.Background(), "1001", UpsertSpaceRequest{
		Visibility:     &visibility,
		SyncCategories: []string{"2"},
		SyncTags:       []string{"campus"},
	}); err != nil {
		t.Fatalf("upsert own space: %v", err)
	}

	if err := svc.SyncThread(context.Background(), testThread("1001", "1", []string{"campus"})); err != nil {
		t.Fatalf("sync non-matching category: %v", err)
	}
	contents, total, err := svc.ListPublicContentsByUserID(context.Background(), "1001", 1, 20)
	if err != nil {
		t.Fatalf("list contents: %v", err)
	}
	if total != 0 || len(contents) != 0 {
		t.Fatalf("expected no content after category mismatch, total=%d len=%d", total, len(contents))
	}

	if err := svc.SyncThread(context.Background(), testThread("1001", "2", []string{"other"})); err != nil {
		t.Fatalf("sync non-matching tag: %v", err)
	}
	contents, total, err = svc.ListPublicContentsByUserID(context.Background(), "1001", 1, 20)
	if err != nil {
		t.Fatalf("list contents: %v", err)
	}
	if total != 0 || len(contents) != 0 {
		t.Fatalf("expected no content after tag mismatch, total=%d len=%d", total, len(contents))
	}

	thread := testThread("1001", "2", []string{"Campus"})
	if err := svc.SyncThread(context.Background(), thread); err != nil {
		t.Fatalf("sync matching thread: %v", err)
	}
	contents, total, err = svc.ListPublicContentsByUserID(context.Background(), "1001", 1, 20)
	if err != nil {
		t.Fatalf("list contents: %v", err)
	}
	if total != 1 || contents[0].ThreadID != thread.ID {
		t.Fatalf("expected matching content, total=%d contents=%#v", total, contents)
	}
}

func TestHandleThreadEventDecodesMapPayload(t *testing.T) {
	repo := NewMemoryRepository()
	svc := NewService(repo, newFakeUserLookup(&identitydomain.User{
		ID:       "1001",
		Username: "alice",
		Nickname: "Alice",
	}))

	err := svc.HandleThreadEvent(context.Background(), eventbus.Event{
		Type: eventbus.EventThreadCreated,
		Data: map[string]interface{}{
			"id":          "2001",
			"title":       "hello",
			"content":     "hello campus",
			"author_id":   "1001",
			"author_name": "Alice",
			"category_id": "1",
			"status":      "published",
			"tags":        []interface{}{"go"},
			"created_at":  time.Now().UTC(),
			"updated_at":  time.Now().UTC(),
		},
	})
	if err != nil {
		t.Fatalf("handle thread event: %v", err)
	}

	contents, total, err := svc.ListPublicContentsByUserID(context.Background(), "1001", 1, 20)
	if err != nil {
		t.Fatalf("list contents: %v", err)
	}
	if total != 1 || contents[0].ThreadID != "2001" {
		t.Fatalf("expected decoded content, total=%d contents=%#v", total, contents)
	}
}

func testThread(authorID, categoryID string, tags []string) *communitydomain.Thread {
	now := time.Now().UTC()
	return &communitydomain.Thread{
		ID:         "2001",
		Title:      "CampusOS Sync",
		Content:    "CampusOS content sync keeps a public personal space updated.",
		AuthorID:   authorID,
		AuthorName: "Alice",
		CategoryID: categoryID,
		Status:     communitydomain.ThreadStatusPublished,
		Tags:       tags,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
}
