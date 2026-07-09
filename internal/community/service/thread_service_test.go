package service

import (
	"context"
	"strings"
	"testing"

	"github.com/campusos/CampusOS/internal/community/domain"
	"github.com/campusos/CampusOS/internal/community/repository"
)

func TestCreateThreadMergesCategoryDefaultTags(t *testing.T) {
	categoryRepo := repository.NewMemoryCategoryRepository()
	categorySvc := NewCategoryService(categoryRepo, nil)
	category, err := categorySvc.Create(context.Background(), domain.CreateCategoryRequest{
		Name:        "Campus Help",
		DefaultTags: []string{"help", "campus"},
	})
	if err != nil {
		t.Fatalf("create category: %v", err)
	}

	threadRepo := repository.NewMemoryThreadRepository()
	svc := NewThreadService(threadRepo, nil)
	svc.SetCategoryRepository(categoryRepo)

	thread, err := svc.CreateThread(context.Background(), "1001", "alice", domain.CreateThreadRequest{
		Title:      "How to join club?",
		Content:    "Question content",
		CategoryID: category.ID,
		Tags:       []string{"club", "help"},
	})
	if err != nil {
		t.Fatalf("create thread: %v", err)
	}

	if got := strings.Join(thread.Tags, ","); got != "help,campus,club" {
		t.Fatalf("unexpected merged tags: %#v", thread.Tags)
	}
}

func TestPrivateThreadVisibilityAndStatusUpdate(t *testing.T) {
	threadRepo := repository.NewMemoryThreadRepository()
	svc := NewThreadService(threadRepo, nil)

	thread, err := svc.CreateThread(context.Background(), "1001", "alice", domain.CreateThreadRequest{
		Title:      "private note",
		Content:    "only me",
		CategoryID: "1",
		IsPrivate:  true,
	})
	if err != nil {
		t.Fatalf("create private thread: %v", err)
	}
	if thread.Status != domain.ThreadStatusPrivate {
		t.Fatalf("expected private status, got %s", thread.Status)
	}

	if _, err := svc.GetThread(context.Background(), thread.ID); err == nil {
		t.Fatalf("expected public get to hide private thread")
	}
	if _, err := svc.GetThreadForViewer(context.Background(), thread.ID, "1002"); err == nil {
		t.Fatalf("expected non-author viewer to be denied")
	}

	visible, err := svc.GetThreadForViewer(context.Background(), thread.ID, "1001")
	if err != nil {
		t.Fatalf("author should see private thread: %v", err)
	}
	if visible.ID != thread.ID {
		t.Fatalf("unexpected visible thread: %#v", visible)
	}

	published := domain.ThreadStatusPublished
	updated, err := svc.UpdateThread(context.Background(), thread.ID, "1001", domain.UpdateThreadRequest{
		Status: &published,
	})
	if err != nil {
		t.Fatalf("publish private thread: %v", err)
	}
	if updated.Status != domain.ThreadStatusPublished {
		t.Fatalf("expected published status, got %s", updated.Status)
	}
	if _, err := svc.GetThread(context.Background(), thread.ID); err != nil {
		t.Fatalf("public get should see published thread: %v", err)
	}
}
