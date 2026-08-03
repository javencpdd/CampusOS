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
func (n failingPostNotifier) NotifyPostDeletedByModerator(context.Context, string, string, string, string) error {
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

func TestAdminDeletePostNotifiesAuthorWithoutSelfNotification(t *testing.T) {
	ctx := context.Background()
	threadRepo := repository.NewMemoryThreadRepository()
	thread := &domain.Thread{ID: "thread-mod", Title: "Moderated thread", Content: "body", CategoryID: "1", AuthorID: "1001", AuthorName: "alice", Status: domain.ThreadStatusPublished}
	if err := threadRepo.Create(ctx, thread); err != nil {
		t.Fatal(err)
	}
	postRepo := repository.NewMemoryPostRepository()
	notificationRepo := repository.NewMemoryNotificationRepository()
	svc := NewPostService(postRepo, nil)
	svc.SetThreadRepository(threadRepo)
	svc.SetNotificationWriter(NewNotificationService(notificationRepo))
	svc.SetReliability(reliability.NewService(transaction.NewMemory(), reliability.NewMemoryStore()))

	moderatorOwn, err := svc.CreatePost(ctx, thread.ID, "9001", "mod", domain.CreatePostRequest{Content: "moderator note"})
	if err != nil {
		t.Fatal(err)
	}
	other, err := svc.CreatePost(ctx, thread.ID, "1002", "bob", domain.CreatePostRequest{Content: "bob reply"})
	if err != nil {
		t.Fatal(err)
	}

	if err := svc.AdminDeletePost(ctx, other.ID, "9001"); err != nil {
		t.Fatalf("moderator delete: %v", err)
	}
	items, _, err := notificationRepo.ListByUser(ctx, "1002", 1, 20)
	if err != nil || len(items) != 1 {
		t.Fatalf("expected one deletion notification for bob: items=%#v err=%v", items, err)
	}
	if items[0].Type != domain.NotificationTypePostDeletedByModerator || items[0].ActionURL != "/threads/"+thread.ID {
		t.Fatalf("unexpected deletion notification contract: %#v", items[0])
	}
	if items[0].Metadata["post_id"] != other.ID || items[0].Metadata["thread_id"] != thread.ID {
		t.Fatalf("unexpected deletion notification metadata: %#v", items[0].Metadata)
	}
	stored, err := threadRepo.GetByID(ctx, thread.ID)
	if err != nil || stored.ReplyCount != 1 {
		t.Fatalf("reply count after delete: %#v err=%v", stored, err)
	}

	if err := svc.AdminDeletePost(ctx, moderatorOwn.ID, "9001"); err != nil {
		t.Fatalf("moderator self delete: %v", err)
	}
	if own, _, _ := notificationRepo.ListByUser(ctx, "9001", 1, 20); len(own) != 0 {
		t.Fatalf("moderator deleting own reply must not self-notify: %#v", own)
	}
}

func TestReplyKeepsParentFloorSnapshotAfterParentDeleted(t *testing.T) {
	ctx := context.Background()
	threadRepo := repository.NewMemoryThreadRepository()
	thread := &domain.Thread{ID: "thread-floor", Title: "Floor snapshot", Content: "body", CategoryID: "1", AuthorID: "1001", AuthorName: "alice", Status: domain.ThreadStatusPublished}
	if err := threadRepo.Create(ctx, thread); err != nil {
		t.Fatal(err)
	}
	postRepo := repository.NewMemoryPostRepository()
	svc := NewPostService(postRepo, nil)
	svc.SetThreadRepository(threadRepo)

	parent, err := svc.CreatePost(ctx, thread.ID, "1002", "bob", domain.CreatePostRequest{Content: "floor one"})
	if err != nil {
		t.Fatal(err)
	}
	reply, err := svc.CreatePost(ctx, thread.ID, "1003", "carol", domain.CreatePostRequest{Content: "quoting", ParentID: &parent.ID})
	if err != nil {
		t.Fatal(err)
	}
	if reply.ParentFloorNumber != parent.FloorNumber {
		t.Fatalf("expected parent floor snapshot %d, got %d", parent.FloorNumber, reply.ParentFloorNumber)
	}

	if err := svc.DeletePost(ctx, parent.ID, "1002"); err != nil {
		t.Fatalf("delete parent: %v", err)
	}
	posts, _, err := svc.ListByThread(ctx, thread.ID, 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(posts) != 1 || posts[0].ID != reply.ID {
		t.Fatalf("unexpected posts after parent delete: %#v", posts)
	}
	if posts[0].ParentID == nil || *posts[0].ParentID != parent.ID {
		t.Fatalf("reply no longer references the deleted parent: %#v", posts[0])
	}
	if posts[0].ParentFloorNumber != parent.FloorNumber {
		t.Fatalf("parent floor lost after parent delete: want %d got %d", parent.FloorNumber, posts[0].ParentFloorNumber)
	}
}

func TestAdminDeletePostRollsBackWhenNotificationCannotBeStored(t *testing.T) {
	ctx := context.Background()
	threadRepo := repository.NewMemoryThreadRepository()
	thread := &domain.Thread{ID: "thread-mod-atomic", Title: "Atomic moderation", Content: "body", CategoryID: "1", AuthorID: "1001", AuthorName: "alice", Status: domain.ThreadStatusPublished, ReplyCount: 1}
	if err := threadRepo.Create(ctx, thread); err != nil {
		t.Fatal(err)
	}
	postRepo := repository.NewMemoryPostRepository()
	post := &domain.Post{ID: "post-victim", ThreadID: thread.ID, AuthorID: "1002", AuthorName: "bob", Content: "reply", Status: "published"}
	if err := postRepo.Create(ctx, post); err != nil {
		t.Fatal(err)
	}
	svc := NewPostService(postRepo, nil)
	svc.SetThreadRepository(threadRepo)
	svc.SetNotificationWriter(failingPostNotifier{err: errors.New("notification store unavailable")})
	svc.SetReliability(reliability.NewService(transaction.NewMemory(), reliability.NewMemoryStore()))

	if err := svc.AdminDeletePost(ctx, post.ID, "9001"); err == nil {
		t.Fatal("expected notification failure to abort admin delete")
	}
	if _, err := postRepo.GetByID(ctx, post.ID); err != nil {
		t.Fatal("failed notification left the post deleted")
	}
	stored, err := threadRepo.GetByID(ctx, thread.ID)
	if err != nil || stored.ReplyCount != 1 {
		t.Fatalf("failed notification changed reply count: %#v err=%v", stored, err)
	}
}
