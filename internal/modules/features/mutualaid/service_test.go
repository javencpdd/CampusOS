package mutualaid

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	community "github.com/campusos/CampusOS/internal/modules/core/community"
	communitydomain "github.com/campusos/CampusOS/internal/modules/core/community/domain"
	communityrepo "github.com/campusos/CampusOS/internal/modules/core/community/repository"
	communitysvc "github.com/campusos/CampusOS/internal/modules/core/community/service"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
)

func newTestService(t *testing.T, enabled bool) (*Service, *communityrepo.MemoryThreadRepository, *reliability.MemoryStore) {
	t.Helper()
	threadRepo := communityrepo.NewMemoryThreadRepository()
	threadSvc := communitysvc.NewThreadService(threadRepo, nil)
	policies := communityrepo.NewMemoryThreadTypePolicyRepository()
	if err := policies.Replace(context.Background(), "1", []communitydomain.ThreadType{
		communitydomain.ThreadTypeDiscussion,
		communitydomain.ThreadTypeMutualAid,
	}); err != nil {
		t.Fatal(err)
	}
	threadSvc.SetThreadTypePolicyRepository(policies)

	reliableStore := reliability.NewMemoryStore()
	reliable := reliability.NewService(transaction.NewMemory(), reliableStore)
	threadSvc.SetReliability(reliable)

	service := NewService(NewMemoryStore(), community.NewContentGateway(threadRepo, threadSvc), community.NewContentQuery(threadSvc))
	service.SetReliability(reliable)
	service.SetEnabledChecker(func() bool { return enabled })
	return service, threadRepo, reliableStore
}

func validCreateRequest() CreateRequest {
	deadline := time.Now().UTC().Add(24 * time.Hour)
	return CreateRequest{
		Title:         "Need a study partner",
		Content:       "Looking for a partner for tomorrow's review session.",
		CategoryID:    "1",
		Tags:          []string{"study", "library"},
		AidType:       AidTypeRequest,
		Deadline:      &deadline,
		LocationScope: "Main library",
		ContactMode:   ContactModeComment,
	}
}

func TestCreateAndStatusTransitionKeepsCommunityGovernanceIndependent(t *testing.T) {
	ctx := context.Background()
	service, threads, _ := newTestService(t, true)

	created, err := service.Create(ctx, "1001", "alice", validCreateRequest())
	if err != nil {
		t.Fatalf("create mutual aid: %v", err)
	}
	if created.Thread.ThreadType != communitydomain.ThreadTypeMutualAid {
		t.Fatalf("unexpected thread type: %#v", created.Thread)
	}
	if created.Detail.AidStatus != AidStatusOpen || created.Detail.Version != 1 {
		t.Fatalf("unexpected initial detail: %#v", created.Detail)
	}

	updated, err := service.UpdateStatus(ctx, created.Thread.ID, "1001", UpdateStatusRequest{
		AidStatus: AidStatusInProgress,
		Version:   created.Detail.Version,
	})
	if err != nil {
		t.Fatalf("update mutual aid status: %v", err)
	}
	if updated.Detail.AidStatus != AidStatusInProgress || updated.Detail.Version != 2 {
		t.Fatalf("unexpected transitioned detail: %#v", updated.Detail)
	}
	thread, err := threads.GetByID(ctx, created.Thread.ID)
	if err != nil {
		t.Fatalf("reload thread: %v", err)
	}
	if thread.PublicationStatus != communitydomain.PublicationStatusPublished || thread.ModerationStatus != communitydomain.ModerationStatusClear {
		t.Fatalf("business status must not change Community governance: %#v", thread)
	}

	if _, err := service.UpdateStatus(ctx, created.Thread.ID, "1001", UpdateStatusRequest{
		AidStatus: AidStatusClosed,
		Version:   1,
	}); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("expected optimistic version conflict, got %v", err)
	}
	if _, err := service.UpdateStatus(ctx, created.Thread.ID, "1001", UpdateStatusRequest{
		AidStatus: AidStatusInProgress,
		Version:   updated.Detail.Version,
	}); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("expected invalid status transition, got %v", err)
	}
}

func TestMutualAidAcceptsSanitizedImageTextAndKeepsLegacyDefault(t *testing.T) {
	ctx := context.Background()
	service, _, _ := newTestService(t, true)
	request := validCreateRequest()
	request.ContentFormat = ContentFormatSafeHTML
	request.Content = `<h2>可提供帮助</h2><img src="/api/v1/content/assets/images/1001/a.png" onerror="bad()"><script>bad()</script>`
	created, err := service.Create(ctx, "1001", "alice", request)
	if err != nil {
		t.Fatal(err)
	}
	if created.Thread.ContentFormat != ContentFormatSafeHTML || strings.Contains(created.Thread.Content, "script") || strings.Contains(created.Thread.Content, "onerror") {
		t.Fatalf("unsafe image-text content persisted: %#v", created.Thread)
	}

	legacy := validCreateRequest()
	plain, err := service.Create(ctx, "1002", "bob", legacy)
	if err != nil {
		t.Fatal(err)
	}
	if plain.Thread.ContentFormat != ContentFormat {
		t.Fatalf("legacy request format changed: %#v", plain.Thread)
	}
}

func TestDisabledMutualAidDoesNotBlockPlainThreads(t *testing.T) {
	ctx := context.Background()
	service, threads, _ := newTestService(t, false)
	if service.Status().Enabled {
		t.Fatal("disabled mutual aid feature must report enabled=false")
	}
	if _, err := service.Create(ctx, "1001", "alice", validCreateRequest()); !errors.Is(err, ErrFeatureDisabled) {
		t.Fatalf("expected feature disabled, got %v", err)
	}

	threadSvc := communitysvc.NewThreadService(threads, nil)
	thread, err := threadSvc.CreateThread(ctx, "1001", "alice", communitydomain.CreateThreadRequest{
		Title: "Ordinary discussion", Content: "Still available.", CategoryID: "1",
	})
	if err != nil {
		t.Fatalf("plain thread should still work: %v", err)
	}
	if thread.ThreadType != communitydomain.ThreadTypeDiscussion {
		t.Fatalf("expected discussion thread, got %#v", thread)
	}
}

type failingStore struct {
	*MemoryStore
	failCreate bool
	failUpdate bool
}

type countingStore struct {
	*MemoryStore
	getCalls     int
	getManyCalls int
	dropID       string
}

func (s *countingStore) Get(ctx context.Context, threadID string) (*Detail, error) {
	s.getCalls++
	return s.MemoryStore.Get(ctx, threadID)
}

func (s *countingStore) GetMany(ctx context.Context, threadIDs []string) (map[string]*Detail, error) {
	s.getManyCalls++
	details, err := s.MemoryStore.GetMany(ctx, threadIDs)
	delete(details, s.dropID)
	return details, err
}

func TestListPublicUsesOneBatchAndPreservesIntegrityFailure(t *testing.T) {
	ctx := context.Background()
	threadRepo := communityrepo.NewMemoryThreadRepository()
	threadSvc := communitysvc.NewThreadService(threadRepo, nil)
	policies := communityrepo.NewMemoryThreadTypePolicyRepository()
	if err := policies.Replace(ctx, "1", []communitydomain.ThreadType{communitydomain.ThreadTypeMutualAid}); err != nil {
		t.Fatal(err)
	}
	threadSvc.SetThreadTypePolicyRepository(policies)
	store := &countingStore{MemoryStore: NewMemoryStore()}
	service := NewService(store, community.NewContentGateway(threadRepo, threadSvc), community.NewContentQuery(threadSvc))

	createdIDs := map[string]bool{}
	for index := 0; index < 5; index++ {
		created, err := service.Create(ctx, "1001", "alice", validCreateRequest())
		if err != nil {
			t.Fatal(err)
		}
		createdIDs[created.Thread.ID] = true
	}
	items, total, err := service.ListPublic(ctx, communitydomain.ThreadListFilter{Page: 1, PageSize: 100})
	if err != nil {
		t.Fatal(err)
	}
	if total != 5 || len(items) != 5 || store.getManyCalls != 1 || store.getCalls != 0 {
		t.Fatalf("unexpected batch result: total=%d items=%d getMany=%d get=%d", total, len(items), store.getManyCalls, store.getCalls)
	}
	for _, item := range items {
		if item.Detail == nil || item.Detail.ThreadID != item.Thread.ID || !createdIDs[item.Thread.ID] {
			t.Fatalf("thread/detail order or identity changed: %#v", item)
		}
	}

	store.dropID = items[0].Thread.ID
	if _, _, err := service.ListPublic(ctx, communitydomain.ThreadListFilter{Page: 1, PageSize: 100}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing detail did not fail the whole page: %v", err)
	}
}

func (s *failingStore) Create(ctx context.Context, detail *Detail) error {
	if s.failCreate {
		return errors.New("mutual aid detail write failed")
	}
	return s.MemoryStore.Create(ctx, detail)
}

func (s *failingStore) Update(ctx context.Context, detail *Detail, expectedVersion int64) error {
	if s.failUpdate {
		return errors.New("mutual aid detail update failed")
	}
	return s.MemoryStore.Update(ctx, detail, expectedVersion)
}

func TestCreateRollsBackThreadAndReliableEvidenceWhenDetailWriteFails(t *testing.T) {
	ctx := context.Background()
	threadRepo := communityrepo.NewMemoryThreadRepository()
	threadSvc := communitysvc.NewThreadService(threadRepo, nil)
	policies := communityrepo.NewMemoryThreadTypePolicyRepository()
	if err := policies.Replace(ctx, "1", []communitydomain.ThreadType{communitydomain.ThreadTypeMutualAid}); err != nil {
		t.Fatal(err)
	}
	threadSvc.SetThreadTypePolicyRepository(policies)
	reliableStore := reliability.NewMemoryStore()
	reliable := reliability.NewService(transaction.NewMemory(), reliableStore)
	threadSvc.SetReliability(reliable)

	store := &failingStore{MemoryStore: NewMemoryStore(), failCreate: true}
	service := NewService(store, community.NewContentGateway(threadRepo, threadSvc), community.NewContentQuery(threadSvc))
	service.SetReliability(reliable)
	if _, err := service.Create(ctx, "1001", "alice", validCreateRequest()); err == nil {
		t.Fatal("expected detail persistence failure")
	}
	items, total, err := threadRepo.List(ctx, communitydomain.ThreadListFilter{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatal(err)
	}
	if total != 0 || len(items) != 0 {
		t.Fatalf("failed command left a base thread: %#v", items)
	}
	events, _, err := reliableStore.List(ctx, reliability.EventFilter{}, reliability.PageRequest{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatal(err)
	}
	audits, _, err := reliableStore.ListCommandAudits(ctx, reliability.PageRequest{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 || len(audits) != 0 {
		t.Fatalf("failed command left reliable evidence: events=%#v audits=%#v", events, audits)
	}
}

func TestUpdateRollsBackThreadDetailAndReliableEvidenceWhenDetailWriteFails(t *testing.T) {
	ctx := context.Background()
	threadRepo := communityrepo.NewMemoryThreadRepository()
	threadSvc := communitysvc.NewThreadService(threadRepo, nil)
	policies := communityrepo.NewMemoryThreadTypePolicyRepository()
	if err := policies.Replace(ctx, "1", []communitydomain.ThreadType{communitydomain.ThreadTypeMutualAid}); err != nil {
		t.Fatal(err)
	}
	threadSvc.SetThreadTypePolicyRepository(policies)
	reliableStore := reliability.NewMemoryStore()
	reliable := reliability.NewService(transaction.NewMemory(), reliableStore)
	threadSvc.SetReliability(reliable)

	store := &failingStore{MemoryStore: NewMemoryStore()}
	service := NewService(store, community.NewContentGateway(threadRepo, threadSvc), community.NewContentQuery(threadSvc))
	service.SetReliability(reliable)
	created, err := service.Create(ctx, "1001", "alice", validCreateRequest())
	if err != nil {
		t.Fatalf("create mutual aid: %v", err)
	}

	deadline := time.Now().UTC().Add(48 * time.Hour)
	store.failUpdate = true
	if _, err := service.Update(ctx, created.Thread.ID, "1001", UpdateRequest{
		Title:         "Updated study partner request",
		Content:       "Updated mutual-aid content.",
		Tags:          []string{"study", "updated"},
		AidType:       AidTypeOffer,
		Deadline:      &deadline,
		LocationScope: "Teaching building",
		ContactMode:   ContactModeInApp,
		Version:       created.Detail.Version,
	}); err == nil {
		t.Fatal("expected mutual-aid update to fail when detail persistence fails")
	}

	thread, err := threadRepo.GetByID(ctx, created.Thread.ID)
	if err != nil {
		t.Fatalf("read thread after failed update: %v", err)
	}
	if thread.Title != created.Thread.Title || thread.Content != created.Thread.Content || thread.CurrentRevision != 1 {
		t.Fatalf("failed mutual-aid update changed thread: %#v", thread)
	}
	detail, err := store.Get(ctx, created.Thread.ID)
	if err != nil {
		t.Fatalf("read detail after failed update: %v", err)
	}
	if detail.AidType != created.Detail.AidType || detail.ContactMode != created.Detail.ContactMode || detail.Version != 1 {
		t.Fatalf("failed mutual-aid update changed detail: %#v", detail)
	}
	events, _, err := reliableStore.List(ctx, reliability.EventFilter{}, reliability.PageRequest{Page: 1, PageSize: 100})
	if err != nil {
		t.Fatal(err)
	}
	audits, _, err := reliableStore.ListCommandAudits(ctx, reliability.PageRequest{Page: 1, PageSize: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || len(audits) != 1 {
		t.Fatalf("failed update should not add outbox/audit records: events=%d audits=%d", len(events), len(audits))
	}
}

func TestStatusUpdateRejectsTrashedThreadWithoutNewReliableEvidence(t *testing.T) {
	ctx := context.Background()
	service, threads, reliableStore := newTestService(t, true)
	created, err := service.Create(ctx, "1001", "alice", validCreateRequest())
	if err != nil {
		t.Fatalf("create mutual aid: %v", err)
	}
	if err := threads.Delete(ctx, created.Thread.ID); err != nil {
		t.Fatalf("trash thread: %v", err)
	}
	if _, err := service.UpdateStatus(ctx, created.Thread.ID, "1001", UpdateStatusRequest{
		AidStatus: AidStatusInProgress,
		Version:   created.Detail.Version,
	}); !errors.Is(err, ErrThreadNotEditable) {
		t.Fatalf("trashed mutual-aid thread accepted status update: %v", err)
	}
	current, err := service.GetMine(ctx, created.Thread.ID, "1001")
	if err != nil {
		t.Fatalf("read trashed mutual-aid detail as owner: %v", err)
	}
	if current.Detail.AidStatus != AidStatusOpen || current.Detail.Version != 1 {
		t.Fatalf("trashed mutual-aid detail changed: %#v", current.Detail)
	}
	events, _, err := reliableStore.List(ctx, reliability.EventFilter{}, reliability.PageRequest{Page: 1, PageSize: 100})
	if err != nil {
		t.Fatal(err)
	}
	audits, _, err := reliableStore.ListCommandAudits(ctx, reliability.PageRequest{Page: 1, PageSize: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || len(audits) != 1 {
		t.Fatalf("rejected status update added reliable evidence: events=%d audits=%d", len(events), len(audits))
	}
}
