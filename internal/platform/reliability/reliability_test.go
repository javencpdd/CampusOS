package reliability

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/campusos/CampusOS/internal/platform/transaction"
)

type failingAuditStore struct{ *MemoryStore }

func (s failingAuditStore) RecordCommandAudit(context.Context, CommandAudit) error {
	return errors.New("required audit store unavailable")
}

type mutableSnapshot struct{ value int }

func (s *mutableSnapshot) Snapshot() any { return s.value }
func (s *mutableSnapshot) Restore(value any) {
	if stored, ok := value.(int); ok {
		s.value = stored
	}
}

func TestWorkerRetriesThenPublishes(t *testing.T) {
	store := NewMemoryStore()
	service := NewService(transaction.NewMemory(), store)
	attempts := 0
	service.RegisterHandler("test.retry", func(_ context.Context, _ Event) error {
		attempts++
		if attempts == 1 {
			return Retryable(errors.New("temporary failure"), time.Millisecond)
		}
		return nil
	})
	event, err := NewEvent("test.retry", "test", "1", map[string]string{"value": "x"})
	if err != nil {
		t.Fatal(err)
	}
	event.MaxAttempts = 3
	if _, err := service.Enqueue(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ProcessOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	items, err := service.List(context.Background(), EventFilter{Type: "test.retry"})
	if err != nil || len(items) != 1 {
		t.Fatalf("list retry event: items=%d err=%v", len(items), err)
	}
	if items[0].Status != StatusRetry {
		t.Fatalf("status after retry = %s", items[0].Status)
	}
	time.Sleep(3 * time.Millisecond)
	if _, err := service.ProcessOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	items, _ = service.List(context.Background(), EventFilter{Type: "test.retry"})
	if items[0].Status != StatusPublished || attempts != 2 {
		t.Fatalf("event was not published after retry: status=%s attempts=%d", items[0].Status, attempts)
	}
}

func TestWorkerDeadLettersPermanentFailure(t *testing.T) {
	store := NewMemoryStore()
	service := NewService(transaction.NewMemory(), store)
	service.RegisterHandler("test.dead", func(_ context.Context, _ Event) error {
		return Permanent(errors.New("invalid payload"))
	})
	event, _ := NewEvent("test.dead", "test", "2", map[string]string{})
	if _, err := service.Enqueue(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ProcessOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	summary, err := service.Summary(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if summary.Dead != 1 {
		t.Fatalf("expected one dead-letter event, got %+v", summary)
	}
}

func TestExecuteDoesNotEnqueueWhenActionFails(t *testing.T) {
	store := NewMemoryStore()
	service := NewService(transaction.NewMemory(), store)
	event, _ := NewEvent("test.command", "test", "3", map[string]string{})
	err := service.Execute(context.Background(), Command{Code: "test.command", Event: &event}, func(context.Context) error {
		return errors.New("business failure")
	})
	if err == nil {
		t.Fatal("expected action failure")
	}
	summary, err := service.Summary(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if summary.Pending != 0 || summary.Published != 0 {
		t.Fatalf("failed command enqueued an event: %+v", summary)
	}
}

func TestExecuteRollsBackMemoryStateWhenRequiredAuditFails(t *testing.T) {
	store := failingAuditStore{MemoryStore: NewMemoryStore()}
	service := NewService(transaction.NewMemory(), store)
	state := &mutableSnapshot{}
	service.RegisterMemorySnapshotters(state)
	event, _ := NewEvent("test.audit", "test", "4", map[string]string{})
	err := service.Execute(context.Background(), Command{Code: "test.audit", Event: &event}, func(context.Context) error {
		state.value = 9
		return nil
	})
	if err == nil {
		t.Fatal("expected required audit failure")
	}
	if state.value != 0 {
		t.Fatalf("memory business state was not rolled back: %d", state.value)
	}
	summary, summaryErr := service.Summary(context.Background())
	if summaryErr != nil || summary.Pending != 0 {
		t.Fatalf("failed command leaked outbox state: summary=%+v err=%v", summary, summaryErr)
	}
}

func TestStaleLeaseCannotCompleteAfterRecovery(t *testing.T) {
	store := NewMemoryStore()
	event, _ := NewEvent("test.lease", "test", "5", map[string]string{})
	if _, err := store.Enqueue(context.Background(), &event); err != nil {
		t.Fatal(err)
	}
	first, err := store.Claim(context.Background(), "worker-a", 1, time.Nanosecond)
	if err != nil || len(first) != 1 {
		t.Fatalf("first claim: events=%d err=%v", len(first), err)
	}
	time.Sleep(time.Millisecond)
	second, err := store.Claim(context.Background(), "worker-b", 1, time.Second)
	if err != nil || len(second) != 1 {
		t.Fatalf("recovery claim: events=%d err=%v", len(second), err)
	}
	if err := store.Complete(context.Background(), first[0].ID, "worker-a", first[0].LeaseGeneration); err == nil {
		t.Fatal("stale worker completed a fenced event")
	}
	if err := store.Complete(context.Background(), second[0].ID, "worker-b", second[0].LeaseGeneration); err != nil {
		t.Fatalf("current worker completion failed: %v", err)
	}
}

func TestConsumerReceiptSkipsRepeatedExternalDelivery(t *testing.T) {
	store := NewMemoryStore()
	service := NewService(transaction.NewMemory(), store)
	called := 0
	service.RegisterHandler("test.receipt", func(context.Context, Event) error {
		called++
		return nil
	})
	event, _ := NewEvent("test.receipt", "test", "6", map[string]string{})
	if _, err := service.Enqueue(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordConsumerReceipt(context.Background(), ConsumerReceipt{ConsumerName: "event:test.receipt", EventID: event.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ProcessOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if called != 0 {
		t.Fatalf("consumer ran despite receipt: %d", called)
	}
	attempts, err := service.ListAttempts(context.Background(), event.ID, 10)
	if err != nil || len(attempts) != 1 || attempts[0].Status != "skipped" {
		t.Fatalf("expected skipped attempt evidence, attempts=%+v err=%v", attempts, err)
	}
}

func TestWorkerFanoutAcknowledgesConsumersIndependently(t *testing.T) {
	store := NewMemoryStore()
	service := NewService(transaction.NewMemory(), store)
	primaryCalls, secondaryCalls := 0, 0
	service.RegisterHandler("test.fanout", func(context.Context, Event) error {
		primaryCalls++
		return nil
	})
	service.RegisterConsumer("test.fanout", "test.secondary", func(context.Context, Event) error {
		secondaryCalls++
		return nil
	})
	event, _ := NewEvent("test.fanout", "test", "8", map[string]string{})
	if _, err := service.Enqueue(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ProcessOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if primaryCalls != 1 || secondaryCalls != 1 {
		t.Fatalf("fanout calls primary=%d secondary=%d", primaryCalls, secondaryCalls)
	}
	attempts, err := service.ListAttempts(context.Background(), event.ID, 10)
	if err != nil || len(attempts) != 2 {
		t.Fatalf("fanout attempt evidence=%+v err=%v", attempts, err)
	}
	items, _ := service.List(context.Background(), EventFilter{Type: "test.fanout"})
	if len(items) != 1 || items[0].Status != StatusPublished {
		t.Fatalf("fanout event was not completed: %+v", items)
	}
}

func TestRetentionPreviewRecordsDryRunOnly(t *testing.T) {
	store := NewMemoryStore()
	service := NewService(transaction.NewMemory(), store)
	event, _ := NewEvent("test.retention", "test", "7", map[string]string{})
	if _, err := service.Enqueue(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Claim(context.Background(), "worker", 1, time.Second); err != nil {
		t.Fatal(err)
	}
	items, _ := service.List(context.Background(), EventFilter{Type: "test.retention"})
	if err := store.Complete(context.Background(), event.ID, "worker", items[0].LeaseGeneration); err != nil {
		t.Fatal(err)
	}
	run, err := service.StartRetentionPreview(context.Background(), "outbox", time.Now().UTC().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if run.Mode != "dry-run" || run.EligibleRows != 1 {
		t.Fatalf("unexpected retention run: %+v", run)
	}
	items, err = service.List(context.Background(), EventFilter{Type: "test.retention"})
	if err != nil || len(items) != 1 || items[0].Status != StatusPublished {
		t.Fatalf("dry run mutated outbox: items=%+v err=%v", items, err)
	}
}

func TestRecoverInterruptedOperationsMarksOnlyNonterminalWorkFailed(t *testing.T) {
	store := NewMemoryStore()
	service := NewService(transaction.NewMemory(), store)
	interrupted, err := store.StartOperation(context.Background(), Operation{Kind: "plugin.package.import", Status: OperationRunning})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.StartOperation(context.Background(), Operation{Kind: "appearance.space.apply", Status: OperationSucceeded}); err != nil {
		t.Fatal(err)
	}
	recovered, err := service.RecoverInterruptedOperations(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(recovered) != 1 || recovered[0].ID != interrupted.ID || recovered[0].Status != OperationFailed {
		t.Fatalf("unexpected recovery result: %+v", recovered)
	}
	if recovered[0].Error == "" {
		t.Fatal("interrupted operation did not retain recovery guidance")
	}
}

func TestCommandAuditListIsNewestFirstAndCopiesDetails(t *testing.T) {
	store := NewMemoryStore()
	oldDetails := []byte(`{"result":"old"}`)
	if err := store.RecordCommandAudit(context.Background(), CommandAudit{
		ID: "old", CommandCode: "identity.role.assign", Details: oldDetails,
		CreatedAt: time.Now().UTC().Add(-time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordCommandAudit(context.Background(), CommandAudit{
		ID: "new", CommandCode: "community.thread.pin", Details: []byte(`{"result":"new"}`),
		CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	items, _, err := store.ListCommandAudits(context.Background(), PageRequest{Page: 1, PageSize: 10})
	if err != nil || len(items) != 2 {
		t.Fatalf("list command audits: items=%d err=%v", len(items), err)
	}
	if items[0].ID != "new" || items[1].ID != "old" {
		t.Fatalf("command audits were not newest first: %+v", items)
	}
	items[1].Details[0] = 'X'
	again, _, err := store.ListCommandAudits(context.Background(), PageRequest{Page: 1, PageSize: 10})
	if err != nil || string(again[1].Details) != string(oldDetails) {
		t.Fatalf("command audit details leaked mutable memory: items=%+v err=%v", again, err)
	}
}

func TestReplayCommandRequiresDeadLetterAndIsIdempotent(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	service := NewService(transaction.NewMemory(), store)
	event, _ := NewEvent("test.replay", "test", "9", map[string]string{})
	if _, err := service.Enqueue(ctx, event); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ReplayCommand(ctx, event.ID, ReplayRequest{ActorID: "1", IdempotencyKey: "first"}); !errors.Is(err, ErrEventNotReplayable) {
		t.Fatalf("non-dead event replay error=%v, want ErrEventNotReplayable", err)
	}
	claimed, err := store.Claim(ctx, "worker", 1, time.Second)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claim event: %#v err=%v", claimed, err)
	}
	if err := store.DeadLetter(ctx, event.ID, "worker", claimed[0].LeaseGeneration, "test failure"); err != nil {
		t.Fatal(err)
	}

	first, err := service.ReplayCommand(ctx, event.ID, ReplayRequest{ActorID: "1", RequestID: "request-1", IdempotencyKey: "same"})
	if err != nil || first.Status != StatusPending {
		t.Fatalf("first replay=%+v err=%v", first, err)
	}
	if len(store.audits) != 1 {
		t.Fatalf("replay did not record one command audit: %+v", store.audits)
	}
	claimed, err = store.Claim(ctx, "worker", 1, time.Second)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claim replayed event: %#v err=%v", claimed, err)
	}
	if err := store.DeadLetter(ctx, event.ID, "worker", claimed[0].LeaseGeneration, "failed again"); err != nil {
		t.Fatal(err)
	}

	duplicate, err := service.ReplayCommand(ctx, event.ID, ReplayRequest{ActorID: "1", RequestID: "request-2", IdempotencyKey: "same"})
	if err != nil || duplicate.Status != StatusDead {
		t.Fatalf("duplicate replay=%+v err=%v", duplicate, err)
	}
	if len(store.audits) != 1 {
		t.Fatalf("duplicate replay wrote a second command audit: %+v", store.audits)
	}
	second, err := service.ReplayCommand(ctx, event.ID, ReplayRequest{ActorID: "1", IdempotencyKey: "new-key"})
	if err != nil || second.Status != StatusPending || len(store.audits) != 2 {
		t.Fatalf("new replay=%+v audits=%d err=%v", second, len(store.audits), err)
	}
}

func TestReplayCommandRollsBackWhenRequiredAuditFails(t *testing.T) {
	ctx := context.Background()
	store := failingAuditStore{MemoryStore: NewMemoryStore()}
	service := NewService(transaction.NewMemory(), store)
	event, _ := NewEvent("test.replay.audit", "test", "10", map[string]string{})
	if _, err := service.Enqueue(ctx, event); err != nil {
		t.Fatal(err)
	}
	claimed, err := store.Claim(ctx, "worker", 1, time.Second)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claim event: %#v err=%v", claimed, err)
	}
	if err := store.DeadLetter(ctx, event.ID, "worker", claimed[0].LeaseGeneration, "test failure"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ReplayCommand(ctx, event.ID, ReplayRequest{ActorID: "1", IdempotencyKey: "audit-failure"}); err == nil {
		t.Fatal("expected required replay audit failure")
	}
	stored, err := store.Get(ctx, event.ID)
	if err != nil || stored.Status != StatusDead {
		t.Fatalf("failed replay changed durable event=%+v err=%v", stored, err)
	}
}
