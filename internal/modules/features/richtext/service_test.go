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
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
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

type failingArticleStore struct {
	*MemoryStore
	failCreate bool
	failUpdate bool
}

func (s *failingArticleStore) CreateArticle(ctx context.Context, article *Article) error {
	if s.failCreate {
		return errors.New("article detail write failed")
	}
	return s.MemoryStore.CreateArticle(ctx, article)
}

func (s *failingArticleStore) UpdateArticle(ctx context.Context, article *Article) error {
	if s.failUpdate {
		return errors.New("article detail update failed")
	}
	return s.MemoryStore.UpdateArticle(ctx, article)
}

func TestReliableRichTextCreateRollsBackThreadRevisionAuditAndOutbox(t *testing.T) {
	ctx := context.Background()
	threadRepo := communityrepo.NewMemoryThreadRepository()
	governance := communityrepo.NewMemoryContentGovernanceRepository()
	reliableStore := reliability.NewMemoryStore()
	reliable := reliability.NewService(transaction.NewMemory(), reliableStore)
	threadSvc := communitysvc.NewThreadService(threadRepo, nil)
	threadSvc.SetGovernanceRepository(governance)
	threadSvc.SetReliability(reliable)
	store := &failingArticleStore{MemoryStore: NewMemoryStore(), failCreate: true}
	svc := NewService(store, community.NewContentGateway(threadRepo, threadSvc))
	svc.SetReliability(reliable)

	if _, err := svc.CreateDraft(ctx, "1001", "alice", SaveArticleRequest{
		Title: "atomic richtext", CategoryID: "1", ContentHTML: `<p>body</p>`,
	}); err == nil {
		t.Fatal("expected richtext create to fail when article detail persistence fails")
	}
	threads, total, err := threadRepo.List(ctx, communitydomain.ThreadListFilter{Page: 1, PageSize: 20})
	if err != nil || total != 0 || len(threads) != 0 {
		t.Fatalf("failed richtext create left a thread behind: total=%d items=%#v err=%v", total, threads, err)
	}
	assertNoRichTextReliableEvidence(t, reliableStore)
}

func TestReliableRichTextUpdateRollsBackThreadAndArticleTogether(t *testing.T) {
	ctx := context.Background()
	threadRepo := communityrepo.NewMemoryThreadRepository()
	governance := communityrepo.NewMemoryContentGovernanceRepository()
	reliableStore := reliability.NewMemoryStore()
	reliable := reliability.NewService(transaction.NewMemory(), reliableStore)
	threadSvc := communitysvc.NewThreadService(threadRepo, nil)
	threadSvc.SetGovernanceRepository(governance)
	threadSvc.SetReliability(reliable)
	store := &failingArticleStore{MemoryStore: NewMemoryStore()}
	svc := NewService(store, community.NewContentGateway(threadRepo, threadSvc))
	svc.SetReliability(reliable)

	created, err := svc.CreateDraft(ctx, "1001", "alice", SaveArticleRequest{
		Title: "before", CategoryID: "1", ContentHTML: `<p>before body</p>`,
	})
	if err != nil {
		t.Fatalf("create draft: %v", err)
	}
	store.failUpdate = true
	if _, err := svc.UpdateDraft(ctx, created.ThreadID, "1001", SaveArticleRequest{
		Title: "after", CategoryID: "1", ContentHTML: `<p>after body</p>`,
	}); err == nil {
		t.Fatal("expected richtext update to fail when article detail persistence fails")
	}
	thread, err := threadRepo.GetByID(ctx, created.ThreadID)
	if err != nil {
		t.Fatalf("read thread after failed article update: %v", err)
	}
	if thread.Title != "before" || thread.CurrentRevision != 1 {
		t.Fatalf("failed richtext update changed thread: %#v", thread)
	}
	article, err := store.GetArticleByThreadID(ctx, created.ThreadID)
	if err != nil {
		t.Fatalf("read article after failed update: %v", err)
	}
	if article.Title != "before" || !strings.Contains(article.ContentHTML, "before body") {
		t.Fatalf("failed richtext update changed article: %#v", article)
	}
	events, _, err := reliableStore.List(ctx, reliability.EventFilter{}, reliability.PageRequest{Page: 1, PageSize: 100})
	if err != nil {
		t.Fatalf("list durable events: %v", err)
	}
	audits, _, err := reliableStore.ListCommandAudits(ctx, reliability.PageRequest{Page: 1, PageSize: 100})
	if err != nil {
		t.Fatalf("list command audits: %v", err)
	}
	if len(events) != 1 || len(audits) != 1 {
		t.Fatalf("failed update should not add outbox/audit records: events=%d audits=%d", len(events), len(audits))
	}
}

func assertNoRichTextReliableEvidence(t *testing.T, store *reliability.MemoryStore) {
	t.Helper()
	events, _, err := store.List(context.Background(), reliability.EventFilter{}, reliability.PageRequest{Page: 1, PageSize: 100})
	if err != nil {
		t.Fatalf("list durable events: %v", err)
	}
	audits, _, err := store.ListCommandAudits(context.Background(), reliability.PageRequest{Page: 1, PageSize: 100})
	if err != nil {
		t.Fatalf("list durable command audits: %v", err)
	}
	if len(events) != 0 || len(audits) != 0 {
		t.Fatalf("failed richtext command left durable evidence: events=%#v audits=%#v", events, audits)
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
