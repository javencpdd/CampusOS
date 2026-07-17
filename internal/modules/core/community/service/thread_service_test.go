package service

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	"github.com/campusos/CampusOS/internal/modules/core/community/repository"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
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

type auditingContentAuthorization struct {
	fakeContentAuthorization
	decisions []string
}

func (f *auditingContentAuthorization) RecordContentAuthorizationDecision(_ context.Context, _ string, code string, categoryID int64, outcome, _ string) error {
	f.decisions = append(f.decisions, code+":"+strconv.FormatInt(categoryID, 10)+":"+outcome)
	return nil
}

type failingGovernanceRepository struct {
	*repository.MemoryContentGovernanceRepository
}

func (r failingGovernanceRepository) CreateModerationAction(context.Context, *domain.ModerationAction) error {
	return errors.New("content governance audit write failed")
}

type failingRevisionGovernanceRepository struct {
	*repository.MemoryContentGovernanceRepository
}

func (r failingRevisionGovernanceRepository) CreateRevision(context.Context, *domain.ContentRevision) error {
	return errors.New("content revision write failed")
}

func TestGovernanceCommandRollsBackWhenRequiredEvidenceFails(t *testing.T) {
	ctx := context.Background()
	threadRepo := repository.NewMemoryThreadRepository()
	governance := failingGovernanceRepository{MemoryContentGovernanceRepository: repository.NewMemoryContentGovernanceRepository()}
	svc := NewThreadService(threadRepo, nil)
	svc.SetGovernanceRepository(governance)
	svc.SetContentAuthorization(fakeContentAuthorization{allowedCategory: 12})
	svc.SetReliability(reliability.NewService(transaction.NewMemory(), reliability.NewMemoryStore()))
	thread, err := svc.CreateThread(ctx, "1001", "alice", domain.CreateThreadRequest{Title: "atomic governance", Content: "body", CategoryID: "12"})
	if err != nil {
		t.Fatalf("create thread: %v", err)
	}
	if _, err := svc.TakeDown(ctx, thread.ID, "moderator", "required evidence failure"); err == nil {
		t.Fatal("expected governance audit failure")
	}
	stored, err := threadRepo.GetByID(ctx, thread.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.ModerationStatus != domain.ModerationStatusClear || stored.CurrentRevision != 1 {
		t.Fatalf("failed governance command changed content: %#v", stored)
	}
}

func TestGovernanceDenyAuditSurvivesReliableTransactionRollback(t *testing.T) {
	ctx := context.Background()
	threadRepo := repository.NewMemoryThreadRepository()
	svc := NewThreadService(threadRepo, nil)
	svc.SetGovernanceRepository(repository.NewMemoryContentGovernanceRepository())
	authorization := &auditingContentAuthorization{fakeContentAuthorization: fakeContentAuthorization{allowedCategory: 12}}
	svc.SetContentAuthorization(authorization)
	svc.SetReliability(reliability.NewService(transaction.NewMemory(), reliability.NewMemoryStore()))
	thread, err := svc.CreateThread(ctx, "1001", "alice", domain.CreateThreadRequest{Title: "deny audit", Content: "body", CategoryID: "13"})
	if err != nil {
		t.Fatalf("create thread: %v", err)
	}
	if _, err := svc.TakeDown(ctx, thread.ID, "moderator", "outside scope"); err == nil {
		t.Fatal("expected out-of-scope governance command to be denied")
	}
	if len(authorization.decisions) != 1 || authorization.decisions[0] != "community.thread.take_down:13:deny" {
		t.Fatalf("expected persisted deny evidence after rollback, got %#v", authorization.decisions)
	}
	stored, err := threadRepo.GetByID(ctx, thread.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.ModerationStatus != domain.ModerationStatusClear {
		t.Fatalf("denied command changed thread state: %#v", stored)
	}
}

func TestAuthorThreadCommandsRollbackWhenRevisionWriteFails(t *testing.T) {
	ctx := context.Background()
	threadRepo := repository.NewMemoryThreadRepository()
	governance := failingRevisionGovernanceRepository{MemoryContentGovernanceRepository: repository.NewMemoryContentGovernanceRepository()}
	reliableStore := reliability.NewMemoryStore()
	svc := NewThreadService(threadRepo, nil)
	svc.SetGovernanceRepository(governance)
	svc.SetReliability(reliability.NewService(transaction.NewMemory(), reliableStore))

	if _, err := svc.CreateThread(ctx, "1001", "alice", domain.CreateThreadRequest{
		Title: "must roll back", Content: "body", CategoryID: "1",
	}); err == nil {
		t.Fatal("expected create to fail when its required revision cannot be written")
	}
	if threads, total, err := threadRepo.List(ctx, domain.ThreadListFilter{Page: 1, PageSize: 20}); err != nil || total != 0 || len(threads) != 0 {
		t.Fatalf("failed create left a thread behind: total=%d items=%#v err=%v", total, threads, err)
	}
	assertNoReliableEvidence(t, reliableStore)

	seed := &domain.Thread{
		ID: "atomic-author-thread", Title: "before", Content: "before body", AuthorID: "1001", AuthorName: "alice",
		CategoryID: "1", Status: domain.ThreadStatusPublished, CurrentRevision: 1,
	}
	if err := threadRepo.Create(ctx, seed); err != nil {
		t.Fatalf("seed thread: %v", err)
	}
	if _, err := svc.UpdateThread(ctx, seed.ID, "1001", domain.UpdateThreadRequest{Title: ptrString("after")}); err == nil {
		t.Fatal("expected update to fail when its required revision cannot be written")
	}
	stored, err := threadRepo.GetByID(ctx, seed.ID)
	if err != nil {
		t.Fatalf("read seeded thread after failed update: %v", err)
	}
	if stored.Title != "before" || stored.CurrentRevision != 1 {
		t.Fatalf("failed update changed thread state: %#v", stored)
	}
	assertNoReliableEvidence(t, reliableStore)

	if err := svc.DeleteThread(ctx, seed.ID, "1001"); err == nil {
		t.Fatal("expected author trash to fail when its required revision cannot be written")
	}
	stored, err = threadRepo.GetByID(ctx, seed.ID)
	if err != nil {
		t.Fatalf("read seeded thread after failed trash: %v", err)
	}
	if stored.DeletionStatus != domain.DeletionStatusActive || stored.CurrentRevision != 1 {
		t.Fatalf("failed author trash changed thread state: %#v", stored)
	}
	assertNoReliableEvidence(t, reliableStore)
}

func TestAuthorThreadCommandsWriteOneAuditAndOutboxPerCommittedMutation(t *testing.T) {
	ctx := context.Background()
	threadRepo := repository.NewMemoryThreadRepository()
	governance := repository.NewMemoryContentGovernanceRepository()
	reliableStore := reliability.NewMemoryStore()
	svc := NewThreadService(threadRepo, nil)
	svc.SetGovernanceRepository(governance)
	svc.SetReliability(reliability.NewService(transaction.NewMemory(), reliableStore))

	thread, err := svc.CreateThread(ctx, "1001", "alice", domain.CreateThreadRequest{
		Title: "reliable author command", Content: "body", CategoryID: "1",
	})
	if err != nil {
		t.Fatalf("create thread: %v", err)
	}
	if _, err := svc.UpdateThread(ctx, thread.ID, "1001", domain.UpdateThreadRequest{Content: ptrString("edited body")}); err != nil {
		t.Fatalf("update thread: %v", err)
	}
	if err := svc.DeleteThread(ctx, thread.ID, "1001"); err != nil {
		t.Fatalf("trash thread: %v", err)
	}
	events, _, err := reliableStore.List(ctx, reliability.EventFilter{}, reliability.PageRequest{Page: 1, PageSize: 100})
	if err != nil {
		t.Fatalf("list durable events: %v", err)
	}
	audits, _, err := reliableStore.ListCommandAudits(ctx, reliability.PageRequest{Page: 1, PageSize: 100})
	if err != nil {
		t.Fatalf("list command audits: %v", err)
	}
	if len(events) != 3 || len(audits) != 3 {
		t.Fatalf("expected one durable event and audit per mutation, events=%d audits=%d", len(events), len(audits))
	}
}

type testStructuredParticipant struct {
	typeValue domain.ThreadType
	fail      bool
	threadID  string
}

func (p *testStructuredParticipant) ThreadType() domain.ThreadType { return p.typeValue }
func (p *testStructuredParticipant) PersistThreadDetail(_ context.Context, thread *domain.Thread) error {
	if p.fail {
		return errors.New("structured detail write failed")
	}
	p.threadID = thread.ID
	return nil
}

func TestStructuredThreadRequiresFeatureAndCategoryPolicy(t *testing.T) {
	ctx := context.Background()
	categoryRepo := repository.NewMemoryCategoryRepository()
	policyRepo := repository.NewMemoryThreadTypePolicyRepository()
	categorySvc := NewCategoryService(categoryRepo, nil)
	categorySvc.SetThreadTypePolicyRepository(policyRepo)
	category, err := categorySvc.Create(ctx, domain.CreateCategoryRequest{Name: "Structured board"})
	if err != nil {
		t.Fatalf("create category: %v", err)
	}
	threadSvc := NewThreadService(repository.NewMemoryThreadRepository(), nil)
	threadSvc.SetCategoryRepository(categoryRepo)
	threadSvc.SetThreadTypePolicyRepository(policyRepo)
	articleEnabled := true
	threadSvc.SetThreadTypeEnabledChecker(func(threadType domain.ThreadType) bool {
		return threadType == domain.ThreadTypeDiscussion || (threadType == domain.ThreadTypeArticle && articleEnabled)
	})
	participant := &testStructuredParticipant{typeValue: domain.ThreadTypeArticle}
	thread, err := threadSvc.CreateStructuredThreadWithOptions(ctx, "1001", "alice", domain.CreateThreadRequest{
		Title: "Article", Content: "body", CategoryID: category.ID,
	}, CreateThreadOptions{Status: domain.ThreadStatusDraft, ThreadType: domain.ThreadTypeArticle}, participant)
	if err != nil {
		t.Fatalf("create allowed article: %v", err)
	}
	if thread.ThreadType != domain.ThreadTypeArticle || participant.threadID != thread.ID {
		t.Fatalf("participant did not receive typed thread: thread=%#v participant=%#v", thread, participant)
	}
	if err := policyRepo.Replace(ctx, category.ID, []domain.ThreadType{domain.ThreadTypeDiscussion}); err != nil {
		t.Fatal(err)
	}
	if _, err := threadSvc.CreateStructuredThreadWithOptions(ctx, "1001", "alice", domain.CreateThreadRequest{
		Title: "Denied", Content: "body", CategoryID: category.ID,
	}, CreateThreadOptions{ThreadType: domain.ThreadTypeArticle}, participant); !errors.Is(err, ErrThreadTypeNotAllowed) {
		t.Fatalf("expected category policy rejection, got %v", err)
	}
	if err := policyRepo.Replace(ctx, category.ID, domain.DefaultCategoryThreadTypes()); err != nil {
		t.Fatal(err)
	}
	articleEnabled = false
	if _, err := threadSvc.CreateStructuredThreadWithOptions(ctx, "1001", "alice", domain.CreateThreadRequest{
		Title: "Disabled", Content: "body", CategoryID: category.ID,
	}, CreateThreadOptions{ThreadType: domain.ThreadTypeArticle}, participant); !errors.Is(err, ErrThreadTypeUnavailable) {
		t.Fatalf("expected feature rejection, got %v", err)
	}
}

func TestStructuredThreadParticipantFailureRollsBackBaseFactsAndEvidence(t *testing.T) {
	ctx := context.Background()
	categoryRepo := repository.NewMemoryCategoryRepository()
	policyRepo := repository.NewMemoryThreadTypePolicyRepository()
	categorySvc := NewCategoryService(categoryRepo, nil)
	categorySvc.SetThreadTypePolicyRepository(policyRepo)
	category, err := categorySvc.Create(ctx, domain.CreateCategoryRequest{Name: "Atomic structured board"})
	if err != nil {
		t.Fatal(err)
	}
	threadRepo := repository.NewMemoryThreadRepository()
	governance := repository.NewMemoryContentGovernanceRepository()
	reliableStore := reliability.NewMemoryStore()
	reliable := reliability.NewService(transaction.NewMemory(), reliableStore)
	threadSvc := NewThreadService(threadRepo, nil)
	threadSvc.SetCategoryRepository(categoryRepo)
	threadSvc.SetThreadTypePolicyRepository(policyRepo)
	threadSvc.SetThreadTypeEnabledChecker(func(domain.ThreadType) bool { return true })
	threadSvc.SetGovernanceRepository(governance)
	threadSvc.SetReliability(reliable)
	participant := &testStructuredParticipant{typeValue: domain.ThreadTypeArticle, fail: true}
	if _, err := threadSvc.CreateStructuredThreadWithOptions(ctx, "1001", "alice", domain.CreateThreadRequest{
		Title: "Atomic article", Content: "body", CategoryID: category.ID,
	}, CreateThreadOptions{ThreadType: domain.ThreadTypeArticle}, participant); err == nil {
		t.Fatal("expected participant failure")
	}
	threads, total, err := threadRepo.List(ctx, domain.ThreadListFilter{Page: 1, PageSize: 20})
	if err != nil || total != 0 || len(threads) != 0 {
		t.Fatalf("participant failure left base facts: total=%d items=%#v err=%v", total, threads, err)
	}
	assertNoReliableEvidence(t, reliableStore)
}

func assertNoReliableEvidence(t *testing.T, store *reliability.MemoryStore) {
	t.Helper()
	events, _, err := store.List(context.Background(), reliability.EventFilter{}, reliability.PageRequest{Page: 1, PageSize: 100})
	if err != nil {
		t.Fatalf("list durable events: %v", err)
	}
	audits, _, err := store.ListCommandAudits(context.Background(), reliability.PageRequest{Page: 1, PageSize: 100})
	if err != nil {
		t.Fatalf("list command audits: %v", err)
	}
	if len(events) != 0 || len(audits) != 0 {
		t.Fatalf("failed command left durable evidence: events=%#v audits=%#v", events, audits)
	}
}

func ptrString(value string) *string { return &value }
