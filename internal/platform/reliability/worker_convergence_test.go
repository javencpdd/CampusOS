package reliability

import (
	"bytes"
	"context"
	"errors"
	"log"
	"strings"
	"testing"
	"time"
)

type workerFaultStore struct {
	Store
	completeFailures int
	completeErr      error
	retryErr         error
	deadLetterErr    error
	finishAttemptErr error
	receiptErr       error
	heartbeatErr     error
	claimErr         error
}

func (s *workerFaultStore) Complete(ctx context.Context, id, owner string, generation int64) error {
	if s.completeFailures > 0 {
		s.completeFailures--
		return s.completeErr
	}
	return s.Store.Complete(ctx, id, owner, generation)
}

func (s *workerFaultStore) Retry(ctx context.Context, id, owner string, generation int64, availableAt time.Time, message string) error {
	if s.retryErr != nil {
		return s.retryErr
	}
	return s.Store.Retry(ctx, id, owner, generation, availableAt, message)
}

func (s *workerFaultStore) DeadLetter(ctx context.Context, id, owner string, generation int64, message string) error {
	if s.deadLetterErr != nil {
		return s.deadLetterErr
	}
	return s.Store.DeadLetter(ctx, id, owner, generation, message)
}

func (s *workerFaultStore) FinishAttempt(ctx context.Context, attempt DeliveryAttempt) error {
	if s.finishAttemptErr != nil {
		return s.finishAttemptErr
	}
	return s.Store.FinishAttempt(ctx, attempt)
}

func (s *workerFaultStore) RecordConsumerReceipt(ctx context.Context, receipt ConsumerReceipt) error {
	if s.receiptErr != nil {
		return s.receiptErr
	}
	return s.Store.RecordConsumerReceipt(ctx, receipt)
}

func (s *workerFaultStore) Heartbeat(ctx context.Context, workerID string, at time.Time) error {
	if s.heartbeatErr != nil {
		return s.heartbeatErr
	}
	return s.Store.Heartbeat(ctx, workerID, at)
}

func (s *workerFaultStore) Claim(ctx context.Context, owner string, limit int, lease time.Duration) ([]Event, error) {
	if s.claimErr != nil {
		return nil, s.claimErr
	}
	return s.Store.Claim(ctx, owner, limit, lease)
}

func TestWorkerPublishesAfterAllConsumersSucceed(t *testing.T) {
	store := NewMemoryStore()
	worker := NewWorker(store, WorkerConfig{ID: "worker-publish"})
	firstCalls, secondCalls := 0, 0
	worker.RegisterConsumer("test.publish", "consumer:first", func(context.Context, Event) error {
		firstCalls++
		return nil
	})
	worker.RegisterConsumer("test.publish", "consumer:second", func(context.Context, Event) error {
		secondCalls++
		return nil
	})
	event := enqueueWorkerEvent(t, store, "test.publish", 8)

	count, err := worker.ProcessOnce(t.Context())
	if err != nil || count != 1 {
		t.Fatalf("process event: count=%d err=%v", count, err)
	}
	stored, err := store.Get(t.Context(), event.ID)
	if err != nil || stored.Status != StatusPublished || stored.Attempts != 1 {
		t.Fatalf("published event=%+v err=%v", stored, err)
	}
	if firstCalls != 1 || secondCalls != 1 {
		t.Fatalf("consumer calls=%d/%d", firstCalls, secondCalls)
	}
	for _, consumer := range []string{"consumer:first", "consumer:second"} {
		acknowledged, receiptErr := store.HasConsumerReceipt(t.Context(), consumer, event.ID)
		if receiptErr != nil || !acknowledged {
			t.Fatalf("receipt %s acknowledged=%t err=%v", consumer, acknowledged, receiptErr)
		}
	}
	claimed, err := store.Claim(t.Context(), "worker-other", 1, time.Second)
	if err != nil || len(claimed) != 0 {
		t.Fatalf("published event was reclaimed: events=%+v err=%v", claimed, err)
	}
}

func TestWorkerCompletesWhenConsumerReceiptsAlreadyExist(t *testing.T) {
	store := NewMemoryStore()
	worker := NewWorker(store, WorkerConfig{ID: "worker-receipts"})
	called := 0
	for _, consumer := range []string{"consumer:first", "consumer:second"} {
		worker.RegisterConsumer("test.receipts", consumer, func(context.Context, Event) error {
			called++
			return nil
		})
	}
	event := enqueueWorkerEvent(t, store, "test.receipts", 8)
	for _, consumer := range []string{"consumer:first", "consumer:second"} {
		if err := store.RecordConsumerReceipt(t.Context(), ConsumerReceipt{ConsumerName: consumer, EventID: event.ID, Attempt: 1}); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := worker.ProcessOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	stored, _ := store.Get(t.Context(), event.ID)
	if called != 0 || stored.Status != StatusPublished || stored.Attempts != 1 {
		t.Fatalf("receipt recovery called=%d event=%+v", called, stored)
	}
	attempts, _, err := store.ListAttempts(t.Context(), event.ID, PageRequest{Page: 1, PageSize: 10})
	if err != nil || !hasAttempt(attempts, "consumer:first", "skipped") ||
		!hasAttempt(attempts, "consumer:second", "skipped") || !hasAttempt(attempts, finalizeConsumerName, "succeeded") {
		t.Fatalf("recovery attempts=%+v err=%v", attempts, err)
	}
}

func TestWorkerDoesNotRepeatSideEffectsAfterCrashWindow(t *testing.T) {
	store := NewMemoryStore()
	event := enqueueWorkerEvent(t, store, "test.crash-window", 8)
	claimed, err := store.Claim(t.Context(), "crashed-worker", 1, time.Nanosecond)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("initial claim=%+v err=%v", claimed, err)
	}
	firstCalls, secondCalls := 1, 1
	for _, consumer := range []string{"consumer:first", "consumer:second"} {
		if err := store.RecordConsumerReceipt(t.Context(), ConsumerReceipt{ConsumerName: consumer, EventID: event.ID, Attempt: 1}); err != nil {
			t.Fatal(err)
		}
	}
	time.Sleep(time.Millisecond)
	worker := NewWorker(store, WorkerConfig{ID: "recovery-worker", LeaseDuration: time.Second})
	worker.RegisterConsumer("test.crash-window", "consumer:first", func(context.Context, Event) error {
		firstCalls++
		return nil
	})
	worker.RegisterConsumer("test.crash-window", "consumer:second", func(context.Context, Event) error {
		secondCalls++
		return nil
	})

	if _, err := worker.ProcessOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	stored, _ := store.Get(t.Context(), event.ID)
	if stored.Status != StatusPublished || stored.Attempts != 2 || firstCalls != 1 || secondCalls != 1 {
		t.Fatalf("crash recovery event=%+v calls=%d/%d", stored, firstCalls, secondCalls)
	}
}

func TestCompleteFailureBecomesRetry(t *testing.T) {
	base := NewMemoryStore()
	completeErr := errors.New("complete storage unavailable")
	store := &workerFaultStore{Store: base, completeFailures: 1, completeErr: completeErr}
	worker := NewWorker(store, WorkerConfig{ID: "worker-complete-retry"})
	calls := 0
	worker.RegisterConsumer("test.complete-retry", "consumer", func(context.Context, Event) error {
		calls++
		return nil
	})
	event := enqueueWorkerEvent(t, base, "test.complete-retry", 3)

	if _, err := worker.ProcessOnce(t.Context()); !errors.Is(err, completeErr) {
		t.Fatalf("complete error=%v, want injected error", err)
	}
	stored, _ := base.Get(t.Context(), event.ID)
	if stored.Status != StatusRetry || stored.LastError != completeFailureMessage || calls != 1 {
		t.Fatalf("retry event=%+v calls=%d", stored, calls)
	}
	attempts, _, _ := base.ListAttempts(t.Context(), event.ID, PageRequest{Page: 1, PageSize: 10})
	if !hasAttempt(attempts, finalizeConsumerName, "retry") {
		t.Fatalf("finalize retry evidence is missing: %+v", attempts)
	}
	makeMemoryEventAvailable(base, event.ID)
	if _, err := worker.ProcessOnce(t.Context()); err != nil {
		t.Fatalf("recovery process: %v", err)
	}
	stored, _ = base.Get(t.Context(), event.ID)
	if stored.Status != StatusPublished || calls != 1 {
		t.Fatalf("recovered event=%+v calls=%d", stored, calls)
	}
}

func TestCompleteFailureAtMaxAttemptsBecomesDead(t *testing.T) {
	base := NewMemoryStore()
	completeErr := errors.New("complete storage unavailable")
	store := &workerFaultStore{Store: base, completeFailures: 1, completeErr: completeErr}
	worker := NewWorker(store, WorkerConfig{ID: "worker-complete-dead"})
	worker.RegisterConsumer("test.complete-dead", "consumer", func(context.Context, Event) error { return nil })
	event := enqueueWorkerEvent(t, base, "test.complete-dead", 1)

	if _, err := worker.ProcessOnce(t.Context()); !errors.Is(err, completeErr) {
		t.Fatalf("complete error=%v, want injected error", err)
	}
	stored, _ := base.Get(t.Context(), event.ID)
	if stored.Status != StatusDead || stored.Attempts != 1 || stored.DeadLetteredAt == nil {
		t.Fatalf("dead event=%+v", stored)
	}
	claimed, err := base.Claim(t.Context(), "worker-next", 1, time.Second)
	if err != nil || len(claimed) != 0 {
		t.Fatalf("max-attempt event was reclaimed: %+v err=%v", claimed, err)
	}
}

func TestClaimNeverExceedsMaxAttempts(t *testing.T) {
	store := NewMemoryStore()
	now := time.Now().UTC()
	fixtures := []Event{
		{ID: "pending-max", Type: "test.exhausted", Status: StatusPending, Attempts: 8, MaxAttempts: 8, AvailableAt: now.Add(-time.Minute)},
		{ID: "retry-max", Type: "test.exhausted", Status: StatusRetry, Attempts: 8, MaxAttempts: 8, AvailableAt: now.Add(time.Hour)},
		{ID: "processing-max", Type: "test.exhausted", Status: StatusProcessing, Attempts: 8, MaxAttempts: 8, LeaseOwner: "old", LeaseUntil: timePointer(now.Add(-time.Minute))},
		{ID: "processing-over", Type: "test.exhausted", Status: StatusProcessing, Attempts: 103, MaxAttempts: 8, LeaseOwner: "old", LeaseUntil: timePointer(now.Add(-time.Minute))},
	}
	for index := range fixtures {
		if _, err := store.Enqueue(t.Context(), &fixtures[index]); err != nil {
			t.Fatal(err)
		}
	}
	claimed, err := store.Claim(t.Context(), "worker", 10, time.Second)
	if err != nil || len(claimed) != 0 {
		t.Fatalf("exhausted claim=%+v err=%v", claimed, err)
	}
	for _, fixture := range fixtures {
		stored, _ := store.Get(t.Context(), fixture.ID)
		if stored.Status != StatusDead || stored.Attempts != fixture.Attempts || stored.DeadLetteredAt == nil {
			t.Fatalf("fixture %s did not converge: %+v", fixture.ID, stored)
		}
	}

	allowed := Event{ID: "pending-last", Type: "test.exhausted", Status: StatusPending, Attempts: 7, MaxAttempts: 8, AvailableAt: now.Add(-time.Minute)}
	if _, err := store.Enqueue(t.Context(), &allowed); err != nil {
		t.Fatal(err)
	}
	claimed, err = store.Claim(t.Context(), "worker", 1, time.Second)
	if err != nil || len(claimed) != 1 || claimed[0].Attempts != 8 {
		t.Fatalf("last allowed claim=%+v err=%v", claimed, err)
	}
}

func TestExpiredProcessingBelowMaxCanBeReclaimed(t *testing.T) {
	store := NewMemoryStore()
	now := time.Now().UTC()
	event := Event{
		ID: "expired-below-max", Type: "test.expired", Status: StatusProcessing,
		Attempts: 7, MaxAttempts: 8, LeaseOwner: "worker-old", LeaseUntil: timePointer(now.Add(-time.Minute)), LeaseGeneration: 4,
	}
	if _, err := store.Enqueue(t.Context(), &event); err != nil {
		t.Fatal(err)
	}
	claimed, err := store.Claim(t.Context(), "worker-new", 1, time.Second)
	if err != nil || len(claimed) != 1 || claimed[0].Attempts != 8 || claimed[0].LeaseGeneration != 5 {
		t.Fatalf("reclaimed event=%+v err=%v", claimed, err)
	}
	if err := store.Complete(t.Context(), event.ID, "worker-old", 4); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("stale completion error=%v", err)
	}
	if err := store.Complete(t.Context(), event.ID, "worker-new", 5); err != nil {
		t.Fatal(err)
	}
}

func TestOverLimitHistoricalEventIsDeadLettered(t *testing.T) {
	store := NewMemoryStore()
	now := time.Now().UTC()
	event := Event{
		ID: "historical-over-limit", Type: "test.historical-over-limit", Status: StatusProcessing,
		Attempts: 103, MaxAttempts: 8, LeaseOwner: "worker-old",
		LeaseUntil: timePointer(now.Add(-time.Minute)), LeaseGeneration: 103,
	}
	if _, err := store.Enqueue(t.Context(), &event); err != nil {
		t.Fatal(err)
	}
	claimed, err := store.Claim(t.Context(), "worker-new", 1, time.Second)
	if err != nil || len(claimed) != 0 {
		t.Fatalf("historical event was reclaimed: events=%+v err=%v", claimed, err)
	}
	stored, err := store.Get(t.Context(), event.ID)
	if err != nil || stored.Status != StatusDead || stored.Attempts != 103 || stored.DeadLetteredAt == nil {
		t.Fatalf("historical event did not converge: event=%+v err=%v", stored, err)
	}
}

func TestStateTransitionErrorsAreObservable(t *testing.T) {
	base := NewMemoryStore()
	completeErr := errors.New("complete failed for secret@example.test token=raw-token")
	retryErr := errors.New("retry database unavailable")
	store := &workerFaultStore{
		Store: base, completeFailures: 1, completeErr: completeErr, retryErr: retryErr,
	}
	var output bytes.Buffer
	worker := NewWorker(store, WorkerConfig{
		ID: "worker-observable", Logger: log.New(&output, "", 0),
	})
	worker.RegisterConsumer("test.observable", "consumer", func(context.Context, Event) error { return nil })
	event := enqueueWorkerEvent(t, base, "test.observable", 3)

	_, err := worker.ProcessOnce(t.Context())
	if !errors.Is(err, completeErr) || !errors.Is(err, retryErr) {
		t.Fatalf("combined transition error=%v", err)
	}
	if strings.Contains(err.Error(), "secret@example.test") || strings.Contains(err.Error(), "raw-token") {
		t.Fatalf("returned error leaked a secret: %s", err)
	}
	logOutput := output.String()
	for _, expected := range []string{`event_id="` + event.ID + `"`, `worker_id="worker-observable"`, `operation="complete"`, `operation="retry"`} {
		if !strings.Contains(logOutput, expected) {
			t.Fatalf("structured log missing %s: %s", expected, logOutput)
		}
	}
	for _, forbidden := range []string{"secret@example.test", "raw-token", `"secret"`} {
		if strings.Contains(logOutput, forbidden) {
			t.Fatalf("structured log leaked %q: %s", forbidden, logOutput)
		}
	}
	attempts, _, _ := base.ListAttempts(t.Context(), event.ID, PageRequest{Page: 1, PageSize: 10})
	if !hasAttempt(attempts, finalizeConsumerName, "failed") {
		t.Fatalf("failed finalize evidence is missing: %+v", attempts)
	}
}

func TestDeadLetterErrorIsObservable(t *testing.T) {
	base := NewMemoryStore()
	handlerErr := Permanent(errors.New("permanent consumer failure"))
	deadLetterErr := errors.New("dead-letter storage unavailable")
	store := &workerFaultStore{Store: base, deadLetterErr: deadLetterErr}
	worker := NewWorker(store, WorkerConfig{ID: "worker-dead-letter-error"})
	worker.RegisterConsumer("test.dead-letter-error", "consumer", func(context.Context, Event) error {
		return handlerErr
	})
	event := enqueueWorkerEvent(t, base, "test.dead-letter-error", 3)

	_, err := worker.ProcessOnce(t.Context())
	if !errors.Is(err, handlerErr) || !errors.Is(err, deadLetterErr) {
		t.Fatalf("dead-letter errors=%v", err)
	}
	stored, getErr := base.Get(t.Context(), event.ID)
	if getErr != nil || stored.Status != StatusProcessing {
		t.Fatalf("failed transition must not bypass lease fencing: event=%+v err=%v", stored, getErr)
	}
	attempts, _, listErr := base.ListAttempts(t.Context(), event.ID, PageRequest{Page: 1, PageSize: 10})
	if listErr != nil || !hasAttempt(attempts, "consumer", "failed") {
		t.Fatalf("failed transition evidence=%+v err=%v", attempts, listErr)
	}
}

func TestFinishAttemptAndReceiptErrorsAreObservable(t *testing.T) {
	t.Run("finish attempt", func(t *testing.T) {
		base := NewMemoryStore()
		finishErr := errors.New("attempt store unavailable")
		store := &workerFaultStore{Store: base, finishAttemptErr: finishErr}
		worker := NewWorker(store, WorkerConfig{ID: "worker-finish-error"})
		worker.RegisterConsumer("test.finish-error", "consumer", func(context.Context, Event) error { return nil })
		event := enqueueWorkerEvent(t, base, "test.finish-error", 3)
		if _, err := worker.ProcessOnce(t.Context()); !errors.Is(err, finishErr) {
			t.Fatalf("finish attempt error=%v", err)
		}
		stored, _ := base.Get(t.Context(), event.ID)
		if stored.Status != StatusPublished {
			t.Fatalf("attempt telemetry failure blocked completion: %+v", stored)
		}
	})

	t.Run("consumer receipt", func(t *testing.T) {
		base := NewMemoryStore()
		receiptErr := errors.New("receipt store unavailable")
		store := &workerFaultStore{Store: base, receiptErr: receiptErr}
		worker := NewWorker(store, WorkerConfig{ID: "worker-receipt-error"})
		worker.RegisterConsumer("test.receipt-error", "consumer", func(context.Context, Event) error { return nil })
		event := enqueueWorkerEvent(t, base, "test.receipt-error", 3)
		if _, err := worker.ProcessOnce(t.Context()); !errors.Is(err, receiptErr) {
			t.Fatalf("receipt error=%v", err)
		}
		stored, _ := base.Get(t.Context(), event.ID)
		if stored.Status != StatusRetry || stored.LastError != receiptRecordFailureMessage {
			t.Fatalf("receipt failure did not become retry: %+v", stored)
		}
	})
}

func TestHeartbeatAndClaimErrorsAreObservable(t *testing.T) {
	heartbeatErr := errors.New("heartbeat unavailable")
	worker := NewWorker(&workerFaultStore{Store: NewMemoryStore(), heartbeatErr: heartbeatErr}, WorkerConfig{ID: "worker-heartbeat"})
	if _, err := worker.ProcessOnce(t.Context()); !errors.Is(err, heartbeatErr) {
		t.Fatalf("heartbeat error=%v", err)
	}

	claimErr := errors.New("claim unavailable")
	worker = NewWorker(&workerFaultStore{Store: NewMemoryStore(), claimErr: claimErr}, WorkerConfig{ID: "worker-claim"})
	if _, err := worker.ProcessOnce(t.Context()); !errors.Is(err, claimErr) {
		t.Fatalf("claim error=%v", err)
	}
}

func enqueueWorkerEvent(t *testing.T, store Store, eventType string, maxAttempts int) Event {
	t.Helper()
	event, err := NewEvent(eventType, "test", "1", map[string]string{"secret": "must-not-be-logged"})
	if err != nil {
		t.Fatal(err)
	}
	event.MaxAttempts = maxAttempts
	if _, err := store.Enqueue(t.Context(), &event); err != nil {
		t.Fatal(err)
	}
	return event
}

func makeMemoryEventAvailable(store *MemoryStore, id string) {
	store.mu.Lock()
	defer store.mu.Unlock()
	event := store.events[id]
	event.AvailableAt = time.Now().UTC().Add(-time.Second)
	store.events[id] = event
}

func timePointer(value time.Time) *time.Time { return &value }
