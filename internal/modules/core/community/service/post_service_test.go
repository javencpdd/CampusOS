package service

import (
	"context"
	"errors"
	"testing"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	"github.com/campusos/CampusOS/internal/modules/core/community/repository"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
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

func TestCreatePostNotifiesThreadAndParentAuthorsWithoutSelfDuplicates(t *testing.T) {
	ctx := context.Background()
	threadRepo := repository.NewMemoryThreadRepository()
	thread := &domain.Thread{ID: "thread-1", Title: "Campus question", Content: "body", CategoryID: "1", AuthorID: "1001", AuthorName: "alice", Status: domain.ThreadStatusPublished}
	if err := threadRepo.Create(ctx, thread); err != nil {
		t.Fatal(err)
	}
	postRepo := repository.NewMemoryPostRepository()
	notificationRepo := repository.NewMemoryNotificationRepository()
	svc := NewPostService(postRepo, nil)
	svc.SetThreadRepository(threadRepo)
	svc.SetNotificationWriter(NewNotificationService(notificationRepo))

	first, err := svc.CreatePost(ctx, thread.ID, "1002", "bob", domain.CreatePostRequest{Content: "first"})
	if err != nil {
		t.Fatal(err)
	}
	if alice, _, err := notificationRepo.ListByUser(ctx, "1001", 1, 20); err != nil || len(alice) != 1 || alice[0].Type != domain.NotificationTypeThreadReplied {
		t.Fatalf("thread author notification mismatch: items=%#v err=%v", alice, err)
	}

	if _, err := svc.CreatePost(ctx, thread.ID, "1003", "carol", domain.CreatePostRequest{Content: "nested", ParentID: &first.ID}); err != nil {
		t.Fatal(err)
	}
	if bob, _, err := notificationRepo.ListByUser(ctx, "1002", 1, 20); err != nil || len(bob) != 1 || bob[0].Type != domain.NotificationTypePostReplied {
		t.Fatalf("parent author notification mismatch: items=%#v err=%v", bob, err)
	}
	if alice, _, err := notificationRepo.ListByUser(ctx, "1001", 1, 20); err != nil || len(alice) != 2 {
		t.Fatalf("thread author should also receive the nested activity: items=%#v err=%v", alice, err)
	}

	if _, err := svc.CreatePost(ctx, thread.ID, "1001", "alice", domain.CreatePostRequest{Content: "owner reply"}); err != nil {
		t.Fatal(err)
	}
	if alice, _, _ := notificationRepo.ListByUser(ctx, "1001", 1, 20); len(alice) != 2 {
		t.Fatalf("self reply created a duplicate notification: %#v", alice)
	}
}

type failingPostNotifier struct{ err error }

func (n failingPostNotifier) NotifyThreadReplied(context.Context, string, string, string, string, string) error {
	return n.err
}
func (n failingPostNotifier) NotifyPostReplied(context.Context, string, string, string, string, string, string) error {
	return n.err
}

func TestCreatePostRollsBackWhenRequiredNotificationFails(t *testing.T) {
	ctx := context.Background()
	threadRepo := repository.NewMemoryThreadRepository()
	thread := &domain.Thread{ID: "thread-atomic", Title: "Atomic", Content: "body", CategoryID: "1", AuthorID: "1001", AuthorName: "alice", Status: domain.ThreadStatusPublished}
	if err := threadRepo.Create(ctx, thread); err != nil {
		t.Fatal(err)
	}
	postRepo := repository.NewMemoryPostRepository()
	svc := NewPostService(postRepo, nil)
	svc.SetThreadRepository(threadRepo)
	svc.SetNotificationWriter(failingPostNotifier{err: errors.New("notification unavailable")})
	svc.SetReliability(reliability.NewService(transaction.NewMemory(), reliability.NewMemoryStore()))

	if _, err := svc.CreatePost(ctx, thread.ID, "1002", "bob", domain.CreatePostRequest{Content: "must roll back"}); err == nil {
		t.Fatal("expected notification failure")
	}
	if posts, total, err := postRepo.ListByThread(ctx, thread.ID, 1, 20); err != nil || total != 0 || len(posts) != 0 {
		t.Fatalf("failed notification left a post behind: total=%d posts=%#v err=%v", total, posts, err)
	}
	stored, err := threadRepo.GetByID(ctx, thread.ID)
	if err != nil || stored.ReplyCount != 0 {
		t.Fatalf("failed notification changed reply count: thread=%#v err=%v", stored, err)
	}
}
