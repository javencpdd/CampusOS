package service

import (
	"context"
	"testing"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	"github.com/campusos/CampusOS/internal/modules/core/community/repository"
)

func TestPrivateThreadPostsAreOnlyVisibleToAuthor(t *testing.T) {
	ctx := context.Background()
	threadRepo := repository.NewMemoryThreadRepository()
	threadSvc := NewThreadService(threadRepo, nil)
	thread, err := threadSvc.CreateThread(ctx, "1001", "alice", domain.CreateThreadRequest{
		Title:      "private note",
		Content:    "only me",
		CategoryID: "1",
		IsPrivate:  true,
	})
	if err != nil {
		t.Fatalf("create private thread: %v", err)
	}

	postSvc := NewPostService(repository.NewMemoryPostRepository(), nil)
	postSvc.SetThreadRepository(threadRepo)

	if _, err := postSvc.CreatePost(ctx, thread.ID, "1002", "bob", domain.CreatePostRequest{Content: "hello"}); err == nil {
		t.Fatalf("expected non-author reply to private thread to be rejected")
	}
	if _, err := postSvc.CreatePost(ctx, thread.ID, "1001", "alice", domain.CreatePostRequest{Content: "note"}); err != nil {
		t.Fatalf("author reply should be allowed: %v", err)
	}
	if _, _, err := postSvc.ListByThread(ctx, thread.ID, 1, 20); err == nil {
		t.Fatalf("expected public post list to hide private thread")
	}
	posts, total, err := postSvc.ListByThreadForViewer(ctx, thread.ID, "1001", 1, 20)
	if err != nil {
		t.Fatalf("author should list private thread posts: %v", err)
	}
	if total != 1 || len(posts) != 1 {
		t.Fatalf("unexpected private thread posts: total=%d len=%d", total, len(posts))
	}
}
