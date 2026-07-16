package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	"github.com/campusos/CampusOS/internal/modules/core/community/repository"
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

func TestTakenDownThreadCannotBeRepublishedWithoutReview(t *testing.T) {
	ctx := context.Background()
	threadRepo := repository.NewMemoryThreadRepository()
	svc := NewThreadService(threadRepo, nil)
	svc.SetGovernanceRepository(repository.NewMemoryContentGovernanceRepository())

	thread, err := svc.CreateThread(ctx, "1001", "alice", domain.CreateThreadRequest{
		Title:      "governed post",
		Content:    "initial",
		CategoryID: "1",
	})
	if err != nil {
		t.Fatalf("create thread: %v", err)
	}
	if _, err := svc.TakeDown(ctx, thread.ID, "9001", "missing source attribution"); err != nil {
		t.Fatalf("take down thread: %v", err)
	}
	if _, err := svc.GetThread(ctx, thread.ID); err == nil {
		t.Fatal("taken-down thread must not remain public")
	}

	published := domain.ThreadStatusPublished
	updated, err := svc.UpdateThread(ctx, thread.ID, "1001", domain.UpdateThreadRequest{
		Content: ptrString("corrected content"),
		Status:  &published,
	})
	if err != nil {
		t.Fatalf("author resubmit: %v", err)
	}
	if updated.ModerationStatus != domain.ModerationStatusPending {
		t.Fatalf("expected pending review after resubmit, got %s", updated.ModerationStatus)
	}
	if _, err := svc.GetThread(ctx, thread.ID); err == nil {
		t.Fatal("pending thread must not become public")
	}

	approved, err := svc.Approve(ctx, thread.ID, "9001", "content now meets the rule")
	if err != nil {
		t.Fatalf("approve thread: %v", err)
	}
	if !approved.IsPublic() {
		t.Fatalf("approved thread should be public: %#v", approved)
	}
	if _, err := svc.GetThread(ctx, thread.ID); err != nil {
		t.Fatalf("approved thread should be visible: %v", err)
	}
}

func TestTrashAndRestorePreserveModerationState(t *testing.T) {
	ctx := context.Background()
	threadRepo := repository.NewMemoryThreadRepository()
	svc := NewThreadService(threadRepo, nil)
	svc.SetGovernanceRepository(repository.NewMemoryContentGovernanceRepository())
	thread, err := svc.CreateThread(ctx, "1001", "alice", domain.CreateThreadRequest{
		Title:      "private draft",
		Content:    "body",
		CategoryID: "1",
		IsPrivate:  true,
	})
	if err != nil {
		t.Fatalf("create thread: %v", err)
	}
	if err := svc.DeleteThread(ctx, thread.ID, "1001"); err != nil {
		t.Fatalf("trash thread: %v", err)
	}
	if _, err := svc.GetThreadForViewer(ctx, thread.ID, "1001"); err != nil {
		t.Fatalf("author should see a recoverable trashed thread: %v", err)
	}
	restored, err := svc.RestoreFromTrash(ctx, thread.ID, "1001")
	if err != nil {
		t.Fatalf("restore thread: %v", err)
	}
	if restored.DeletionStatus != domain.DeletionStatusActive || restored.PublicationStatus != domain.PublicationStatusPrivate {
		t.Fatalf("restore must retain the original non-public intent: %#v", restored)
	}
}

func TestContentGovernanceUsesStoredCategoryForScopedPermission(t *testing.T) {
	ctx := context.Background()
	threadRepo := repository.NewMemoryThreadRepository()
	svc := NewThreadService(threadRepo, nil)
	svc.SetGovernanceRepository(repository.NewMemoryContentGovernanceRepository())
	svc.SetContentAuthorization(fakeContentAuthorization{allowedCategory: 12})
	thread, err := svc.CreateThread(ctx, "1001", "alice", domain.CreateThreadRequest{
		Title: "scoped governance", Content: "body", CategoryID: "12",
	})
	if err != nil {
		t.Fatalf("create thread: %v", err)
	}
	if _, err := svc.TakeDown(ctx, thread.ID, "moderator", "missing context"); err != nil {
		t.Fatalf("matching category should be allowed: %v", err)
	}

	other, err := svc.CreateThread(ctx, "1001", "alice", domain.CreateThreadRequest{
		Title: "outside scope", Content: "body", CategoryID: "13",
	})
	if err != nil {
		t.Fatalf("create second thread: %v", err)
	}
	if _, err := svc.TakeDown(ctx, other.ID, "moderator", "missing context"); err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("out-of-scope moderator must be denied, got %v", err)
	}
}

func TestGetPublicThreadDoesNotIncrementViews(t *testing.T) {
	ctx := context.Background()
	threadRepo := repository.NewMemoryThreadRepository()
	svc := NewThreadService(threadRepo, nil)
	thread, err := svc.CreateThread(ctx, "1001", "alice", domain.CreateThreadRequest{
		Title: "canonical read", Content: "body", CategoryID: "1",
	})
	if err != nil {
		t.Fatalf("create thread: %v", err)
	}
	if _, err := svc.GetPublicThread(ctx, thread.ID); err != nil {
		t.Fatalf("canonical public read: %v", err)
	}
	fresh, err := threadRepo.GetByID(ctx, thread.ID)
	if err != nil {
		t.Fatalf("reload thread: %v", err)
	}
	if fresh.ViewCount != 0 {
		t.Fatalf("content query must not inflate views, got %d", fresh.ViewCount)
	}
	if _, err := svc.GetThread(ctx, thread.ID); err != nil {
		t.Fatalf("interactive detail read: %v", err)
	}
	fresh, _ = threadRepo.GetByID(ctx, thread.ID)
	if fresh.ViewCount != 1 {
		t.Fatalf("interactive detail read should increment views, got %d", fresh.ViewCount)
	}
}

type fakeContentAuthorization struct{ allowedCategory int64 }

func (f fakeContentAuthorization) CheckCodeScoped(_ context.Context, _ string, _ string, scopeType string, scopeID int64) (bool, error) {
	if scopeType != "category" {
		return false, errors.New("unexpected scope type")
	}
	return scopeID == f.allowedCategory, nil
}

func ptrString(value string) *string { return &value }
