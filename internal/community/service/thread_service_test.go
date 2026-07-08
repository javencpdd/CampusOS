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
