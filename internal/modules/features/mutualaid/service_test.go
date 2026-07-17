package mutualaid

import (
	"context"
	"errors"
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
}

func (s *failingStore) Create(ctx context.Context, detail *Detail) error {
	if s.failCreate {
		return errors.New("mutual aid detail write failed")
	}
	return s.MemoryStore.Create(ctx, detail)
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
