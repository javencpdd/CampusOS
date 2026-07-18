package secondhand

import (
	"context"
	"errors"
	"testing"

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
		communitydomain.ThreadTypeSecondhand,
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
	return CreateRequest{
		Title:         "Graduation desk lamp",
		Content:       "Warm desk lamp, pickup near the library.",
		CategoryID:    "1",
		Tags:          []string{"graduation", "lamp"},
		PriceMinor:    2500,
		Currency:      "CNY",
		ItemCondition: ItemConditionGood,
		TradeMethod:   TradeMethodInPerson,
		LocationScope: "Main library",
	}
}

func TestCreateAndStatusTransitionKeepsCommunityGovernanceIndependent(t *testing.T) {
	ctx := context.Background()
	service, threads, _ := newTestService(t, true)

	created, err := service.Create(ctx, "1001", "alice", validCreateRequest())
	if err != nil {
		t.Fatalf("create secondhand: %v", err)
	}
	if created.Thread.ThreadType != communitydomain.ThreadTypeSecondhand {
		t.Fatalf("unexpected thread type: %#v", created.Thread)
	}
	if created.Detail.TradeStatus != TradeStatusAvailable || created.Detail.Version != 1 || created.Detail.Currency != currencyCNY {
		t.Fatalf("unexpected initial detail: %#v", created.Detail)
	}

	updated, err := service.UpdateStatus(ctx, created.Thread.ID, "1001", UpdateStatusRequest{
		TradeStatus: TradeStatusReserved,
		Version:     created.Detail.Version,
	})
	if err != nil {
		t.Fatalf("reserve secondhand item: %v", err)
	}
	if updated.Detail.TradeStatus != TradeStatusReserved || updated.Detail.Version != 2 {
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
		TradeStatus: TradeStatusSold,
		Version:     1,
	}); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("expected optimistic version conflict, got %v", err)
	}
	if _, err := service.UpdateStatus(ctx, created.Thread.ID, "1001", UpdateStatusRequest{
		TradeStatus: TradeStatusReserved,
		Version:     updated.Detail.Version,
	}); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("expected invalid status transition, got %v", err)
	}
}

func TestRequestValidationDefaultsCNYAndRejectsInvalidPriceOrCurrency(t *testing.T) {
	ctx := context.Background()
	service, _, _ := newTestService(t, true)
	request := validCreateRequest()
	request.Currency = ""
	created, err := service.Create(ctx, "1001", "alice", request)
	if err != nil {
		t.Fatalf("create with default CNY: %v", err)
	}
	if created.Detail.Currency != currencyCNY {
		t.Fatalf("expected default CNY, got %#v", created.Detail)
	}
	request = validCreateRequest()
	request.PriceMinor = -1
	if _, err := service.Create(ctx, "1001", "alice", request); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected negative price rejection, got %v", err)
	}
	request = validCreateRequest()
	request.Currency = "USD"
	if _, err := service.Create(ctx, "1001", "alice", request); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected non-CNY rejection, got %v", err)
	}
}

func TestDisabledSecondhandDoesNotBlockPlainThreads(t *testing.T) {
	ctx := context.Background()
	service, threads, _ := newTestService(t, false)
	if service.Status().Enabled {
		t.Fatal("disabled secondhand feature must report enabled=false")
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

func (s *failingStore) Create(ctx context.Context, detail *Detail) error {
	if s.failCreate {
		return errors.New("secondhand detail write failed")
	}
	return s.MemoryStore.Create(ctx, detail)
}

func (s *failingStore) Update(ctx context.Context, detail *Detail, expectedVersion int64) error {
	if s.failUpdate {
		return errors.New("secondhand detail update failed")
	}
	return s.MemoryStore.Update(ctx, detail, expectedVersion)
}

func TestCreateRollsBackThreadAndReliableEvidenceWhenDetailWriteFails(t *testing.T) {
	ctx := context.Background()
	threadRepo := communityrepo.NewMemoryThreadRepository()
	threadSvc := communitysvc.NewThreadService(threadRepo, nil)
	policies := communityrepo.NewMemoryThreadTypePolicyRepository()
	if err := policies.Replace(ctx, "1", []communitydomain.ThreadType{communitydomain.ThreadTypeSecondhand}); err != nil {
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
	if err := policies.Replace(ctx, "1", []communitydomain.ThreadType{communitydomain.ThreadTypeSecondhand}); err != nil {
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
		t.Fatalf("create secondhand: %v", err)
	}

	store.failUpdate = true
	if _, err := service.Update(ctx, created.Thread.ID, "1001", UpdateRequest{
		Title:         "Updated desk lamp",
		Content:       "Updated listing content.",
		Tags:          []string{"lamp", "updated"},
		PriceMinor:    3200,
		Currency:      "CNY",
		ItemCondition: ItemConditionLikeNew,
		TradeMethod:   TradeMethodCampusDropoff,
		LocationScope: "Campus gate",
		Version:       created.Detail.Version,
	}); err == nil {
		t.Fatal("expected secondhand update to fail when detail persistence fails")
	}

	thread, err := threadRepo.GetByID(ctx, created.Thread.ID)
	if err != nil {
		t.Fatalf("read thread after failed update: %v", err)
	}
	if thread.Title != created.Thread.Title || thread.Content != created.Thread.Content || thread.CurrentRevision != 1 {
		t.Fatalf("failed secondhand update changed thread: %#v", thread)
	}
	detail, err := store.Get(ctx, created.Thread.ID)
	if err != nil {
		t.Fatalf("read detail after failed update: %v", err)
	}
	if detail.PriceMinor != created.Detail.PriceMinor || detail.ItemCondition != created.Detail.ItemCondition || detail.Version != 1 {
		t.Fatalf("failed secondhand update changed detail: %#v", detail)
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
		t.Fatalf("create secondhand: %v", err)
	}
	if err := threads.Delete(ctx, created.Thread.ID); err != nil {
		t.Fatalf("trash thread: %v", err)
	}
	if _, err := service.UpdateStatus(ctx, created.Thread.ID, "1001", UpdateStatusRequest{
		TradeStatus: TradeStatusReserved,
		Version:     created.Detail.Version,
	}); !errors.Is(err, ErrThreadNotEditable) {
		t.Fatalf("trashed secondhand thread accepted status update: %v", err)
	}
	current, err := service.GetMine(ctx, created.Thread.ID, "1001")
	if err != nil {
		t.Fatalf("read trashed secondhand detail as owner: %v", err)
	}
	if current.Detail.TradeStatus != TradeStatusAvailable || current.Detail.Version != 1 {
		t.Fatalf("trashed secondhand detail changed: %#v", current.Detail)
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
