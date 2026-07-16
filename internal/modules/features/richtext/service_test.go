package richtext

import (
	"context"
	"errors"
	"strings"
	"testing"

	community "github.com/campusos/CampusOS/internal/modules/core/community"
	communitydomain "github.com/campusos/CampusOS/internal/modules/core/community/domain"
	communityrepo "github.com/campusos/CampusOS/internal/modules/core/community/repository"
	communitysvc "github.com/campusos/CampusOS/internal/modules/core/community/service"
)

func newTestService(enabled bool) (*Service, *communityrepo.MemoryThreadRepository) {
	threadRepo := communityrepo.NewMemoryThreadRepository()
	threadSvc := communitysvc.NewThreadService(threadRepo, nil)
	svc := NewService(NewMemoryStore(), community.NewContentGateway(threadRepo, threadSvc))
	svc.SetEnabledChecker(func() bool { return enabled })
	return svc, threadRepo
}

func TestCreateDraftAndPublishSanitizesHTML(t *testing.T) {
	svc, threadRepo := newTestService(true)
	ctx := context.Background()

	created, err := svc.CreateDraft(ctx, "1001", "alice", SaveArticleRequest{
		Title:       "图文文章",
		CategoryID:  "1",
		Summary:     "摘要",
		ContentHTML: `<h2>Hi</h2><p>正文</p><img src="/api/v1/richtext/assets/1001/a.png" onerror="bad()"><script>alert(1)</script>`,
	})
	if err != nil {
		t.Fatalf("create draft: %v", err)
	}
	if created.Status != StatusDraft {
		t.Fatalf("expected draft status, got %s", created.Status)
	}

	published, err := svc.Publish(ctx, created.ThreadID, "1001")
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if published.Status != StatusPublished {
		t.Fatalf("expected published status, got %s", published.Status)
	}
	if containsAny(published.Article.SanitizedHTML, "script", "onerror") {
		t.Fatalf("unsafe html was not removed: %s", published.Article.SanitizedHTML)
	}
	thread, err := threadRepo.GetByID(ctx, created.ThreadID)
	if err != nil {
		t.Fatalf("get thread: %v", err)
	}
	if thread.ContentFormat != ContentFormat {
		t.Fatalf("expected content format %q, got %q", ContentFormat, thread.ContentFormat)
	}
}

func TestUpdateDraftRejectsNonAuthor(t *testing.T) {
	svc, _ := newTestService(true)
	ctx := context.Background()
	created, err := svc.CreateDraft(ctx, "1001", "alice", SaveArticleRequest{
		Title:       "Draft",
		CategoryID:  "1",
		ContentHTML: `<p>正文</p>`,
	})
	if err != nil {
		t.Fatalf("create draft: %v", err)
	}
	_, err = svc.UpdateDraft(ctx, created.ThreadID, "1002", SaveArticleRequest{
		Title:       "Other",
		ContentHTML: `<p>New</p>`,
	})
	if !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("expected permission denied, got %v", err)
	}
}

func TestAdminCanOfflineRestoreAndDeleteArticle(t *testing.T) {
	svc, threadRepo := newTestService(true)
	ctx := context.Background()
	created, err := svc.CreateDraft(ctx, "1001", "alice", SaveArticleRequest{
		Title:       "Admin Managed",
		CategoryID:  "1",
		ContentHTML: `<p>正文</p>`,
	})
	if err != nil {
		t.Fatalf("create draft: %v", err)
	}
	if _, err := svc.Publish(ctx, created.ThreadID, "1001"); err != nil {
		t.Fatalf("publish: %v", err)
	}

	offlined, err := svc.AdminOffline(ctx, created.ThreadID, "9001")
	if err != nil {
		t.Fatalf("admin offline: %v", err)
	}
	if offlined.Status != StatusOffline {
		t.Fatalf("expected offline status, got %s", offlined.Status)
	}
	thread, err := threadRepo.GetByID(ctx, created.ThreadID)
	if err != nil {
		t.Fatalf("get thread: %v", err)
	}
	if thread.Status != communitydomain.ThreadStatusArchived {
		t.Fatalf("expected archived thread, got %s", thread.Status)
	}

	restored, err := svc.AdminRestore(ctx, created.ThreadID, "9001")
	if err != nil {
		t.Fatalf("admin restore: %v", err)
	}
	if restored.Status != StatusPublished {
		t.Fatalf("expected published status, got %s", restored.Status)
	}
	if err := svc.AdminDelete(ctx, created.ThreadID, "9001"); err != nil {
		t.Fatalf("admin delete: %v", err)
	}
	if _, err := svc.GetArticle(ctx, created.ThreadID, "1001"); !errors.Is(err, ErrArticleNotFound) {
		t.Fatalf("expected article not found after admin delete, got %v", err)
	}
}

func TestTakenDownRichTextArticleReturnsToReviewInsteadOfPublic(t *testing.T) {
	svc, threadRepo := newTestService(true)
	ctx := context.Background()
	created, err := svc.CreateDraft(ctx, "1001", "alice", SaveArticleRequest{
		Title:       "Review after takedown",
		CategoryID:  "1",
		ContentHTML: `<p>first version</p>`,
	})
	if err != nil {
		t.Fatalf("create draft: %v", err)
	}
	if _, err := svc.Publish(ctx, created.ThreadID, "1001"); err != nil {
		t.Fatalf("publish article: %v", err)
	}
	if _, err := svc.AdminOfflineWithReason(ctx, created.ThreadID, "9001", "policy review required"); err != nil {
		t.Fatalf("take down article: %v", err)
	}
	if _, err := svc.UpdateDraft(ctx, created.ThreadID, "1001", SaveArticleRequest{
		Title:       "Review after takedown",
		CategoryID:  "1",
		ContentHTML: `<p>corrected version</p>`,
	}); err != nil {
		t.Fatalf("save corrected draft: %v", err)
	}
	published, err := svc.Publish(ctx, created.ThreadID, "1001")
	if err != nil {
		t.Fatalf("resubmit richtext article: %v", err)
	}
	if published.Status != StatusPendingReview {
		t.Fatalf("expected pending review, got %s", published.Status)
	}
	thread, err := threadRepo.GetByID(ctx, created.ThreadID)
	if err != nil {
		t.Fatalf("get community thread: %v", err)
	}
	if thread.ModerationStatus != communitydomain.ModerationStatusPending || thread.IsPublic() {
		t.Fatalf("richtext resubmission bypassed moderation: %#v", thread)
	}
	if _, err := svc.GetArticle(ctx, created.ThreadID, ""); !errors.Is(err, ErrArticleNotFound) {
		t.Fatalf("pending article must remain hidden from public reads, got %v", err)
	}
}

func TestPluginDisabledDoesNotBlockPlainThreadService(t *testing.T) {
	svc, threadRepo := newTestService(false)
	ctx := context.Background()
	if _, err := svc.CreateDraft(ctx, "1001", "alice", SaveArticleRequest{
		Title:       "Draft",
		CategoryID:  "1",
		ContentHTML: `<p>正文</p>`,
	}); !errors.Is(err, ErrPluginDisabled) {
		t.Fatalf("expected plugin disabled, got %v", err)
	}

	threadSvc := communitysvc.NewThreadService(threadRepo, nil)
	thread, err := threadSvc.CreateThread(ctx, "1001", "alice", communitydomain.CreateThreadRequest{
		Title:      "Plain",
		Content:    "plain body",
		CategoryID: "1",
	})
	if err != nil {
		t.Fatalf("plain thread should still work: %v", err)
	}
	if thread.ContentFormat != "markdown" {
		t.Fatalf("expected markdown plain thread, got %q", thread.ContentFormat)
	}
}

func TestPluginDisabledBlocksPreviewAndRead(t *testing.T) {
	svc, _ := newTestService(false)
	ctx := context.Background()

	if _, err := svc.Preview(ctx, `<p>正文</p>`); !errors.Is(err, ErrPluginDisabled) {
		t.Fatalf("expected preview disabled, got %v", err)
	}
	if _, err := svc.GetArticle(ctx, "1001", "1001"); !errors.Is(err, ErrPluginDisabled) {
		t.Fatalf("expected read disabled, got %v", err)
	}
}

func containsAny(value string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}
