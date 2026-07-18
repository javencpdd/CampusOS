package reliability

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
)

var ErrEventNotFound = errors.New("durable event not found")
var ErrEventNotReplayable = errors.New("durable event is not in the dead-letter queue")
var ErrOperationNotFound = errors.New("reliable operation not found")
var ErrLeaseLost = errors.New("durable event lease lost")

const maxAttemptsExhaustedMessage = "maximum attempts exhausted before terminal completion"

// Store is the persistence port for the transactional outbox, command audit,
// durable operations, and compatibility telemetry. Implementations must honor
// the transaction carried by the request context when one exists.
type Store interface {
	Enqueue(context.Context, *Event) (*Event, error)
	Claim(context.Context, string, int, time.Duration) ([]Event, error)
	Complete(context.Context, string, string, int64) error
	Retry(context.Context, string, string, int64, time.Time, string) error
	DeadLetter(context.Context, string, string, int64, string) error
	Get(context.Context, string) (*Event, error)
	List(context.Context, EventFilter, PageRequest) ([]Event, int64, error)
	Replay(context.Context, string) (*Event, error)
	Summary(context.Context) (Summary, error)
	Heartbeat(context.Context, string, time.Time) error
	ListWorkers(context.Context, PageRequest) ([]WorkerLease, int64, error)
	HasConsumerReceipt(context.Context, string, string) (bool, error)
	RecordConsumerReceipt(context.Context, ConsumerReceipt) error
	StartAttempt(context.Context, DeliveryAttempt) (*DeliveryAttempt, error)
	FinishAttempt(context.Context, DeliveryAttempt) error
	ListAttempts(context.Context, string, PageRequest) ([]DeliveryAttempt, int64, error)

	RecordCommandAudit(context.Context, CommandAudit) error
	ListCommandAudits(context.Context, PageRequest) ([]CommandAudit, int64, error)
	StartOperation(context.Context, Operation) (*Operation, error)
	UpdateOperation(context.Context, Operation) error
	ListOperations(context.Context, string, PageRequest) ([]Operation, int64, error)
	RecoverInterruptedOperations(context.Context) ([]Operation, error)
	RecordCompatibility(context.Context, CompatibilityUsage) error
	ListCompatibility(context.Context, PageRequest) ([]CompatibilityUsage, int64, error)
	PreviewRetention(context.Context, string, time.Time) (RetentionPreview, error)
	StartRetentionRun(context.Context, RetentionRun) (*RetentionRun, error)
	ListRetentionRuns(context.Context, PageRequest) ([]RetentionRun, int64, error)
}

// MemoryStore is a deterministic local-profile adapter. The PostgreSQL store
// is authoritative for restart-safe delivery in deployed environments.
type MemoryStore struct {
	mu            sync.Mutex
	events        map[string]Event
	idempotency   map[string]string
	audits        []CommandAudit
	operations    map[string]Operation
	compatibility map[string]CompatibilityUsage
	workers       map[string]time.Time
	receipts      map[string]ConsumerReceipt
	attempts      map[string]DeliveryAttempt
	retentionRuns []RetentionRun
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		events:        make(map[string]Event),
		idempotency:   make(map[string]string),
		operations:    make(map[string]Operation),
		compatibility: make(map[string]CompatibilityUsage),
		workers:       make(map[string]time.Time),
		receipts:      make(map[string]ConsumerReceipt),
		attempts:      make(map[string]DeliveryAttempt),
	}
}

func paginateValues[T any](items []T, page PageRequest, defaultSize, maximumSize int) ([]T, int64) {
	page = normalizePageRequest(page, defaultSize, maximumSize)
	total := int64(len(items))
	offset := page.offset()
	if offset >= len(items) {
		return []T{}, total
	}
	end := offset + page.PageSize
	if end > len(items) {
		end = len(items)
	}
	return items[offset:end], total
}

func (s *MemoryStore) Enqueue(_ context.Context, event *Event) (*Event, error) {
	if event == nil {
		return nil, errors.New("durable event is required")
	}
	normalizeEvent(event)
	if event.Type == "" {
		return nil, errors.New("durable event type is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if event.IdempotencyKey != "" {
		if id, ok := s.idempotency[event.IdempotencyKey]; ok {
			existing := cloneEvent(s.events[id])
			return &existing, nil
		}
		s.idempotency[event.IdempotencyKey] = event.ID
	}
	s.events[event.ID] = cloneEvent(*event)
	stored := cloneEvent(s.events[event.ID])
	return &stored, nil
}

func (s *MemoryStore) Claim(_ context.Context, owner string, limit int, lease time.Duration) ([]Event, error) {
	if limit <= 0 {
		limit = 16
	}
	if lease <= 0 {
		lease = 30 * time.Second
	}
	now := time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, event := range s.events {
		if !eventExhaustedForClaim(event, now) {
			continue
		}
		event.Status = StatusDead
		event.LeaseOwner = ""
		event.LeaseUntil = nil
		if event.LastError == "" {
			event.LastError = maxAttemptsExhaustedMessage
		}
		if event.DeadLetteredAt == nil {
			deadLetteredAt := now
			event.DeadLetteredAt = &deadLetteredAt
		}
		event.UpdatedAt = now
		s.events[id] = event
	}
	items := make([]Event, 0, limit)
	for _, event := range s.events {
		ready := (event.Status == StatusPending || event.Status == StatusRetry) && !event.AvailableAt.After(now)
		abandoned := event.Status == StatusProcessing && event.LeaseUntil != nil && event.LeaseUntil.Before(now)
		if event.Attempts >= event.MaxAttempts || (!ready && !abandoned) {
			continue
		}
		items = append(items, event)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].AvailableAt.Equal(items[j].AvailableAt) {
			if items[i].CreatedAt.Equal(items[j].CreatedAt) {
				return items[i].ID < items[j].ID
			}
			return items[i].CreatedAt.Before(items[j].CreatedAt)
		}
		return items[i].AvailableAt.Before(items[j].AvailableAt)
	})
	if len(items) > limit {
		items = items[:limit]
	}
	for i := range items {
		event := s.events[items[i].ID]
		until := now.Add(lease)
		event.Status = StatusProcessing
		event.LeaseOwner = owner
		event.LeaseUntil = &until
		event.LeaseGeneration++
		event.Attempts++
		event.UpdatedAt = now
		s.events[event.ID] = event
		items[i] = cloneEvent(event)
	}
	return items, nil
}

func (s *MemoryStore) Complete(_ context.Context, id, owner string, generation int64) error {
	return s.finish(id, owner, generation, StatusPublished, time.Time{}, "")
}

func (s *MemoryStore) Retry(_ context.Context, id, owner string, generation int64, availableAt time.Time, message string) error {
	return s.finish(id, owner, generation, StatusRetry, availableAt, message)
}

func (s *MemoryStore) DeadLetter(_ context.Context, id, owner string, generation int64, message string) error {
	return s.finish(id, owner, generation, StatusDead, time.Time{}, message)
}

func (s *MemoryStore) finish(id, owner string, generation int64, status string, availableAt time.Time, message string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	event, ok := s.events[id]
	if !ok {
		return ErrEventNotFound
	}
	if event.Status != StatusProcessing || event.LeaseOwner != owner || event.LeaseGeneration != generation {
		return fmt.Errorf("%w: event %s is no longer owned by %s at generation %d", ErrLeaseLost, id, owner, generation)
	}
	now := time.Now().UTC()
	event.Status = status
	event.LeaseOwner = ""
	event.LeaseUntil = nil
	event.LastError = message
	event.UpdatedAt = now
	if status == StatusRetry {
		event.AvailableAt = availableAt
	}
	if status == StatusDead {
		event.DeadLetteredAt = &now
	}
	s.events[id] = event
	return nil
}

func eventExhaustedForClaim(event Event, now time.Time) bool {
	if event.Attempts < event.MaxAttempts {
		return false
	}
	if event.Status == StatusPending || event.Status == StatusRetry {
		return true
	}
	return event.Status == StatusProcessing && (event.LeaseUntil == nil || event.LeaseUntil.Before(now))
}

func (s *MemoryStore) List(_ context.Context, filter EventFilter, page PageRequest) ([]Event, int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := make([]Event, 0, len(s.events))
	for _, event := range s.events {
		if filter.Status != "" && event.Status != filter.Status {
			continue
		}
		if filter.Type != "" && event.Type != filter.Type {
			continue
		}
		items = append(items, cloneEvent(event))
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].ID > items[j].ID
		}
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	paged, total := paginateValues(items, page, 100, 500)
	return paged, total, nil
}

func (s *MemoryStore) Get(_ context.Context, id string) (*Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	event, ok := s.events[id]
	if !ok {
		return nil, ErrEventNotFound
	}
	copyEvent := cloneEvent(event)
	return &copyEvent, nil
}

func (s *MemoryStore) Replay(_ context.Context, id string) (*Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	event, ok := s.events[id]
	if !ok {
		return nil, ErrEventNotFound
	}
	if event.Status != StatusDead {
		return nil, ErrEventNotReplayable
	}
	now := time.Now().UTC()
	event.Status = StatusPending
	event.Attempts = 0
	event.AvailableAt = now
	event.LeaseOwner = ""
	event.LeaseUntil = nil
	event.LastError = ""
	event.DeadLetteredAt = nil
	event.UpdatedAt = now
	s.events[id] = event
	copyEvent := cloneEvent(event)
	return &copyEvent, nil
}

func (s *MemoryStore) Summary(_ context.Context) (Summary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := Summary{}
	for _, event := range s.events {
		switch event.Status {
		case StatusPending:
			result.Pending++
		case StatusProcessing:
			result.Processing++
		case StatusPublished:
			result.Published++
		case StatusRetry:
			result.Retry++
		case StatusDead:
			result.Dead++
		}
		if event.Status == StatusPending || event.Status == StatusRetry {
			if result.OldestPending == nil || event.CreatedAt.Before(*result.OldestPending) {
				value := event.CreatedAt
				result.OldestPending = &value
			}
		}
	}
	return result, nil
}

func (s *MemoryStore) Heartbeat(_ context.Context, workerID string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.workers[workerID] = at.UTC()
	return nil
}

func (s *MemoryStore) ListWorkers(_ context.Context, page PageRequest) ([]WorkerLease, int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := make([]WorkerLease, 0, len(s.workers))
	for workerID, heartbeat := range s.workers {
		items = append(items, WorkerLease{WorkerID: workerID, LastHeartbeatAt: heartbeat, UpdatedAt: heartbeat})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].LastHeartbeatAt.Equal(items[j].LastHeartbeatAt) {
			return items[i].WorkerID < items[j].WorkerID
		}
		return items[i].LastHeartbeatAt.After(items[j].LastHeartbeatAt)
	})
	paged, total := paginateValues(items, page, 50, 200)
	return paged, total, nil
}

func receiptKey(consumer, eventID string) string {
	return consumer + "\x00" + eventID
}

func (s *MemoryStore) HasConsumerReceipt(_ context.Context, consumer, eventID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.receipts[receiptKey(consumer, eventID)]
	return ok, nil
}

func (s *MemoryStore) RecordConsumerReceipt(_ context.Context, receipt ConsumerReceipt) error {
	if receipt.ConsumerName == "" || receipt.EventID == "" {
		return errors.New("consumer receipt requires consumer name and event ID")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := receiptKey(receipt.ConsumerName, receipt.EventID)
	if _, exists := s.receipts[key]; exists {
		return nil
	}
	if receipt.DeliveredAt.IsZero() {
		receipt.DeliveredAt = time.Now().UTC()
	}
	s.receipts[key] = receipt
	return nil
}

func (s *MemoryStore) StartAttempt(_ context.Context, attempt DeliveryAttempt) (*DeliveryAttempt, error) {
	if attempt.EventID == "" || attempt.ConsumerName == "" || attempt.WorkerID == "" {
		return nil, errors.New("delivery attempt requires event, consumer, and worker")
	}
	if attempt.ID == "" {
		attempt.ID = fmt.Sprintf("attempt-%d", time.Now().UTC().UnixNano())
	}
	if attempt.Status == "" {
		attempt.Status = "running"
	}
	if attempt.StartedAt.IsZero() {
		attempt.StartedAt = time.Now().UTC()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.attempts[attempt.ID] = attempt
	copyAttempt := attempt
	return &copyAttempt, nil
}

func (s *MemoryStore) FinishAttempt(_ context.Context, attempt DeliveryAttempt) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.attempts[attempt.ID]
	if !ok {
		return ErrEventNotFound
	}
	if attempt.Status == "" || attempt.Status == "running" {
		return errors.New("delivery attempt requires terminal status")
	}
	now := time.Now().UTC()
	current.Status = attempt.Status
	current.Error = attempt.Error
	current.FinishedAt = &now
	s.attempts[current.ID] = current
	return nil
}

func (s *MemoryStore) ListAttempts(_ context.Context, eventID string, page PageRequest) ([]DeliveryAttempt, int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := make([]DeliveryAttempt, 0, len(s.attempts))
	for _, attempt := range s.attempts {
		if eventID == "" || attempt.EventID == eventID {
			items = append(items, attempt)
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].StartedAt.Equal(items[j].StartedAt) {
			return items[i].ID > items[j].ID
		}
		return items[i].StartedAt.After(items[j].StartedAt)
	})
	paged, total := paginateValues(items, page, 100, 500)
	return paged, total, nil
}

func (s *MemoryStore) RecordCommandAudit(_ context.Context, audit CommandAudit) error {
	if audit.ID == "" {
		audit.ID = fmt.Sprintf("audit-%d", time.Now().UTC().UnixNano())
	}
	if audit.CreatedAt.IsZero() {
		audit.CreatedAt = time.Now().UTC()
	}
	if audit.Details == nil {
		audit.Details = []byte(`{}`)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.audits = append(s.audits, cloneCommandAudit(audit))
	return nil
}

func (s *MemoryStore) ListCommandAudits(_ context.Context, page PageRequest) ([]CommandAudit, int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := make([]CommandAudit, 0, len(s.audits))
	for _, audit := range s.audits {
		items = append(items, cloneCommandAudit(audit))
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].ID > items[j].ID
		}
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	paged, total := paginateValues(items, page, 50, 200)
	return paged, total, nil
}

func (s *MemoryStore) StartOperation(_ context.Context, operation Operation) (*Operation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if operation.IdempotencyKey != "" {
		for _, existing := range s.operations {
			if existing.IdempotencyKey == operation.IdempotencyKey {
				copyOperation := existing
				return &copyOperation, nil
			}
		}
	}
	now := time.Now().UTC()
	if operation.ID == "" {
		operation.ID = fmt.Sprintf("operation-%d", now.UnixNano())
	}
	if operation.Status == "" {
		operation.Status = OperationPending
	}
	operation.CreatedAt = now
	operation.UpdatedAt = now
	s.operations[operation.ID] = operation
	copyOperation := operation
	return &copyOperation, nil
}

func (s *MemoryStore) UpdateOperation(_ context.Context, operation Operation) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.operations[operation.ID]; !ok {
		return ErrOperationNotFound
	}
	operation.UpdatedAt = time.Now().UTC()
	s.operations[operation.ID] = operation
	return nil
}

func (s *MemoryStore) ListOperations(_ context.Context, kind string, page PageRequest) ([]Operation, int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := make([]Operation, 0, len(s.operations))
	for _, operation := range s.operations {
		if kind == "" || operation.Kind == kind {
			items = append(items, operation)
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].ID > items[j].ID
		}
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	paged, total := paginateValues(items, page, 50, 200)
	return paged, total, nil
}

func (s *MemoryStore) RecoverInterruptedOperations(_ context.Context) ([]Operation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	recovered := make([]Operation, 0)
	for id, operation := range s.operations {
		if operation.Status != OperationPending && operation.Status != OperationRunning && operation.Status != OperationCompensate {
			continue
		}
		operation.Status = OperationFailed
		operation.Error = "interrupted by a previous process before a terminal state; inspect the snapshot and retry explicitly"
		operation.UpdatedAt = now
		s.operations[id] = operation
		recovered = append(recovered, operation)
	}
	sort.Slice(recovered, func(i, j int) bool { return recovered[i].CreatedAt.After(recovered[j].CreatedAt) })
	return recovered, nil
}

func (s *MemoryStore) RecordCompatibility(_ context.Context, usage CompatibilityUsage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	if existing, ok := s.compatibility[usage.Key]; ok {
		existing.LastSeen = now
		existing.Count++
		if usage.Detail != nil {
			existing.Detail = usage.Detail
		}
		s.compatibility[usage.Key] = existing
		return nil
	}
	usage.FirstSeen = now
	usage.LastSeen = now
	usage.Count = 1
	s.compatibility[usage.Key] = usage
	return nil
}

func (s *MemoryStore) ListCompatibility(_ context.Context, page PageRequest) ([]CompatibilityUsage, int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := make([]CompatibilityUsage, 0, len(s.compatibility))
	for _, usage := range s.compatibility {
		items = append(items, usage)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].LastSeen.Equal(items[j].LastSeen) {
			return items[i].Key < items[j].Key
		}
		return items[i].LastSeen.After(items[j].LastSeen)
	})
	paged, total := paginateValues(items, page, 50, 200)
	return paged, total, nil
}

func (s *MemoryStore) PreviewRetention(_ context.Context, target string, before time.Time) (RetentionPreview, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	preview := RetentionPreview{Target: target, Before: before.UTC(), CanDelete: false}
	if !supportedRetentionTarget(target) {
		return preview, fmt.Errorf("unsupported retention target %q", target)
	}
	if target == "outbox" {
		for _, event := range s.events {
			if (event.Status == StatusPublished || event.Status == StatusDead) && event.UpdatedAt.Before(before) {
				preview.EligibleRows++
			}
		}
	}
	return preview, nil
}

func (s *MemoryStore) StartRetentionRun(_ context.Context, run RetentionRun) (*RetentionRun, error) {
	if !supportedRetentionTarget(run.Target) {
		return nil, fmt.Errorf("unsupported retention target %q", run.Target)
	}
	if run.ID == "" {
		run.ID = fmt.Sprintf("retention-%d", time.Now().UTC().UnixNano())
	}
	if run.Mode == "" {
		run.Mode = "dry-run"
	}
	if run.Status == "" {
		run.Status = "completed"
	}
	if run.CreatedAt.IsZero() {
		run.CreatedAt = time.Now().UTC()
	}
	s.mu.Lock()
	s.retentionRuns = append(s.retentionRuns, run)
	s.mu.Unlock()
	copyRun := run
	return &copyRun, nil
}

func (s *MemoryStore) ListRetentionRuns(_ context.Context, page PageRequest) ([]RetentionRun, int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := append([]RetentionRun(nil), s.retentionRuns...)
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].ID > items[j].ID
		}
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	paged, total := paginateValues(items, page, 50, 200)
	return paged, total, nil
}

func supportedRetentionTarget(target string) bool {
	switch target {
	case "outbox", "authorization-audits", "webhook-deliveries", "content-revisions", "content-moderation-actions", "plugin-logs":
		return true
	default:
		return false
	}
}

type memorySnapshot struct {
	events        map[string]Event
	idempotency   map[string]string
	audits        []CommandAudit
	operations    map[string]Operation
	compatibility map[string]CompatibilityUsage
	workers       map[string]time.Time
	receipts      map[string]ConsumerReceipt
	attempts      map[string]DeliveryAttempt
	retentionRuns []RetentionRun
}

// Snapshot and Restore let the local profile verify the same command rollback
// semantics as PostgreSQL without pretending it is a database transaction.
func (s *MemoryStore) Snapshot() any {
	s.mu.Lock()
	defer s.mu.Unlock()
	snapshot := memorySnapshot{
		events:        make(map[string]Event, len(s.events)),
		idempotency:   make(map[string]string, len(s.idempotency)),
		audits:        cloneCommandAudits(s.audits),
		operations:    make(map[string]Operation, len(s.operations)),
		compatibility: make(map[string]CompatibilityUsage, len(s.compatibility)),
		workers:       make(map[string]time.Time, len(s.workers)),
		receipts:      make(map[string]ConsumerReceipt, len(s.receipts)),
		attempts:      make(map[string]DeliveryAttempt, len(s.attempts)),
		retentionRuns: append([]RetentionRun(nil), s.retentionRuns...),
	}
	for key, value := range s.events {
		snapshot.events[key] = cloneEvent(value)
	}
	for key, value := range s.idempotency {
		snapshot.idempotency[key] = value
	}
	for key, value := range s.operations {
		snapshot.operations[key] = value
	}
	for key, value := range s.compatibility {
		value.Detail = append([]byte(nil), value.Detail...)
		snapshot.compatibility[key] = value
	}
	for key, value := range s.workers {
		snapshot.workers[key] = value
	}
	for key, value := range s.receipts {
		snapshot.receipts[key] = value
	}
	for key, value := range s.attempts {
		snapshot.attempts[key] = value
	}
	return snapshot
}

func (s *MemoryStore) Restore(value any) {
	snapshot, ok := value.(memorySnapshot)
	if !ok {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = snapshot.events
	s.idempotency = snapshot.idempotency
	s.audits = cloneCommandAudits(snapshot.audits)
	s.operations = snapshot.operations
	s.compatibility = snapshot.compatibility
	s.workers = snapshot.workers
	s.receipts = snapshot.receipts
	s.attempts = snapshot.attempts
	s.retentionRuns = snapshot.retentionRuns
}
