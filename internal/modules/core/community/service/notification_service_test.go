package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	"github.com/campusos/CampusOS/internal/modules/core/community/repository"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
)

func TestNotificationServiceCreatesThreadTrashMessage(t *testing.T) {
	ctx := context.Background()
	repo := repository.NewMemoryNotificationRepository()
	svc := NewNotificationService(repo)
	if err := svc.NotifyThreadTrashed(ctx, "1001", "2001", strings.Repeat("很长的标题", 30), "policy"); err != nil {
		t.Fatal(err)
	}
	result, err := svc.List(ctx, "1001", 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 1 || result.UnreadCount != 1 || len(result.Items) != 1 {
		t.Fatalf("unexpected list: %#v", result)
	}
	item := result.Items[0]
	if item.Type != domain.NotificationTypeThreadTrashed || item.ActionURL != "/threads/2001" {
		t.Fatalf("unexpected notification contract: %#v", item)
	}
	if len([]rune(item.Content)) > 150 {
		t.Fatalf("notification content was not bounded: %d runes", len([]rune(item.Content)))
	}
}

type recordingThreadNotifier struct {
	calls []string
	err   error
}

func (n *recordingThreadNotifier) NotifyThreadTrashed(_ context.Context, userID, threadID, title, _ string) error {
	n.calls = append(n.calls, userID+":"+threadID+":"+title)
	return n.err
}

func (n *recordingThreadNotifier) NotifyThreadTakenDown(_ context.Context, userID, threadID, title, _ string) error {
	n.calls = append(n.calls, "taken-down:"+userID+":"+threadID+":"+title)
	return n.err
}

func TestAdminTrashNotifiesAuthorButAuthorTrashDoesNot(t *testing.T) {
	ctx := context.Background()
	threadRepo := repository.NewMemoryThreadRepository()
	svc := NewThreadService(threadRepo, nil)
	svc.SetContentAuthorization(fakeContentAuthorization{allowedCategory: 12})
	notifier := &recordingThreadNotifier{}
	svc.SetNotificationWriter(notifier)

	adminThread, err := svc.CreateThread(ctx, "1001", "alice", domain.CreateThreadRequest{
		Title: "admin removal", Content: "body", CategoryID: "12",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.AdminDeleteThread(ctx, adminThread.ID, "9001"); err != nil {
		t.Fatal(err)
	}
	if len(notifier.calls) != 1 || !strings.HasPrefix(notifier.calls[0], "1001:"+adminThread.ID+":") {
		t.Fatalf("expected one author notification, got %#v", notifier.calls)
	}

	authorThread, err := svc.CreateThread(ctx, "1001", "alice", domain.CreateThreadRequest{
		Title: "author removal", Content: "body", CategoryID: "12",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteThread(ctx, authorThread.ID, "1001"); err != nil {
		t.Fatal(err)
	}
	if len(notifier.calls) != 1 {
		t.Fatalf("author trash must not create a governance notification, got %#v", notifier.calls)
	}
}

func TestAdminTrashRollsBackWhenNotificationCannotBeStored(t *testing.T) {
	ctx := context.Background()
	threadRepo := repository.NewMemoryThreadRepository()
	svc := NewThreadService(threadRepo, nil)
	svc.SetGovernanceRepository(repository.NewMemoryContentGovernanceRepository())
	svc.SetContentAuthorization(fakeContentAuthorization{allowedCategory: 12})
	svc.SetNotificationWriter(&recordingThreadNotifier{err: errors.New("notification store unavailable")})
	svc.SetReliability(reliability.NewService(transaction.NewMemory(), reliability.NewMemoryStore()))

	thread, err := svc.CreateThread(ctx, "1001", "alice", domain.CreateThreadRequest{
		Title: "atomic removal", Content: "body", CategoryID: "12",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.AdminDeleteThread(ctx, thread.ID, "9001"); err == nil {
		t.Fatal("expected notification failure to abort admin trash")
	}
	stored, err := threadRepo.GetByID(ctx, thread.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.DeletionStatus != domain.DeletionStatusActive || stored.CurrentRevision != 1 {
		t.Fatalf("failed notification left thread trashed: %#v", stored)
	}
}

func TestAdminTakeDownCreatesASeparateAuthorNotification(t *testing.T) {
	ctx := context.Background()
	threadRepo := repository.NewMemoryThreadRepository()
	notificationRepo := repository.NewMemoryNotificationRepository()
	svc := NewThreadService(threadRepo, nil)
	svc.SetGovernanceRepository(repository.NewMemoryContentGovernanceRepository())
	svc.SetContentAuthorization(fakeContentAuthorization{allowedCategory: 12})
	svc.SetNotificationWriter(NewNotificationService(notificationRepo))
	svc.SetReliability(reliability.NewService(transaction.NewMemory(), reliability.NewMemoryStore()))
	thread, err := svc.CreateThread(ctx, "1001", "alice", domain.CreateThreadRequest{Title: "moderated", Content: "body", CategoryID: "12"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.TakeDown(ctx, thread.ID, "9001", "policy reason"); err != nil {
		t.Fatal(err)
	}
	items, _, err := notificationRepo.ListByUser(ctx, "1001", 1, 20)
	if err != nil || len(items) != 1 || items[0].Type != domain.NotificationTypeThreadTakenDown {
		t.Fatalf("take-down notification mismatch: items=%#v err=%v", items, err)
	}
}
