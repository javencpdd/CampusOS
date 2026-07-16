package reliability

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/campusos/CampusOS/pkg/eventbus"
	"github.com/campusos/CampusOS/pkg/idgen"
)

// Service is the public platform port used by Core and Built-in Feature
// modules. External plugins never receive it.
type Service struct {
	transactions transaction.Manager
	store        Store
	worker       *Worker
}

func NewService(transactions transaction.Manager, store Store) *Service {
	if transactions == nil {
		transactions = transaction.NewMemory()
	}
	if memory, ok := transactions.(*transaction.Memory); ok {
		if snapshotter, supported := store.(transaction.Snapshotter); supported {
			memory.AddSnapshotters(snapshotter)
		}
	}
	return &Service{transactions: transactions, store: store, worker: NewWorker(store, WorkerConfig{})}
}

func (s *Service) Store() Store { return s.store }

// RegisterMemorySnapshotters is intentionally a no-op for PostgreSQL. Core
// modules call it while composing the memory profile so failed reliable
// commands roll back their in-memory business repositories together with the
// outbox/audit store.
func (s *Service) RegisterMemorySnapshotters(snapshotters ...transaction.Snapshotter) {
	if s == nil {
		return
	}
	if memory, ok := s.transactions.(*transaction.Memory); ok {
		memory.AddSnapshotters(snapshotters...)
	}
}

// Execute runs the action, audit record, and optional domain event in one
// transaction. Repositories must use transaction.ExecutorFor to participate.
func (s *Service) Execute(ctx context.Context, command Command, action func(context.Context) error) error {
	if s == nil || s.store == nil {
		return errors.New("reliability service is unavailable")
	}
	if action == nil {
		return errors.New("command action is required")
	}
	normalizeCommand(&command)
	if command.Code == "" {
		return errors.New("command code is required")
	}

	return s.transactions.Within(ctx, func(txCtx context.Context) error {
		if err := action(txCtx); err != nil {
			return err
		}
		var eventID string
		eventToQueue := command.Event
		if command.EventFactory != nil {
			built, err := command.EventFactory()
			if err != nil {
				return fmt.Errorf("build durable event: %w", err)
			}
			eventToQueue = &built
		}
		if eventToQueue != nil {
			event := cloneEvent(*eventToQueue)
			if event.IdempotencyKey == "" && command.IdempotencyKey != "" {
				event.IdempotencyKey = command.IdempotencyKey + ":event"
			}
			stored, err := s.store.Enqueue(txCtx, &event)
			if err != nil {
				return fmt.Errorf("enqueue durable event: %w", err)
			}
			eventID = stored.ID
		}
		audit := CommandAudit{
			ID:             fmt.Sprintf("%d", idgen.New()),
			CommandID:      command.ID,
			CommandCode:    command.Code,
			ActorID:        command.ActorID,
			ActorType:      command.ActorType,
			ResourceType:   command.ResourceType,
			ResourceID:     command.ResourceID,
			OperationCode:  command.OperationCode,
			PermissionCode: command.PermissionCode,
			RequestID:      command.RequestID,
			TraceID:        command.TraceID,
			EventID:        eventID,
			Details:        json.RawMessage(`{}`),
			CreatedAt:      time.Now().UTC(),
		}
		if err := s.store.RecordCommandAudit(txCtx, audit); err != nil {
			return fmt.Errorf("record command audit: %w", err)
		}
		return nil
	})
}

// Enqueue is for non-transactional boundary inputs such as an accepted remote
// callback. Core business changes should use Execute instead.
func (s *Service) Enqueue(ctx context.Context, event Event) (*Event, error) {
	if s == nil || s.store == nil {
		return nil, errors.New("reliability service is unavailable")
	}
	return s.store.Enqueue(ctx, &event)
}

func (s *Service) RegisterHandler(eventType string, handler EventHandler) {
	if s != nil && s.worker != nil {
		s.worker.Register(strings.TrimSpace(eventType), handler)
	}
}

// RegisterConsumer adds a separately acknowledged post-commit consumer. It is
// for built-in infrastructure only; External Plugins stay behind the public
// Host API and Gateway and never receive the reliability service.
func (s *Service) RegisterConsumer(eventType, consumer string, handler EventHandler) {
	if s != nil && s.worker != nil {
		s.worker.RegisterConsumer(strings.TrimSpace(eventType), strings.TrimSpace(consumer), handler)
	}
}

func (s *Service) SetFallbackHandler(handler EventHandler) {
	if s != nil && s.worker != nil {
		s.worker.SetFallback(handler)
	}
}

func (s *Service) Start(ctx context.Context) {
	if s != nil && s.worker != nil {
		s.worker.Start(ctx)
	}
}

func (s *Service) Stop(ctx context.Context) error {
	if s == nil || s.worker == nil {
		return nil
	}
	return s.worker.Stop(ctx)
}

func (s *Service) ProcessOnce(ctx context.Context) (int, error) {
	if s == nil || s.worker == nil {
		return 0, errors.New("reliability worker is unavailable")
	}
	return s.worker.ProcessOnce(ctx)
}

func (s *Service) Summary(ctx context.Context) (Summary, error) { return s.store.Summary(ctx) }
func (s *Service) List(ctx context.Context, filter EventFilter) ([]Event, error) {
	return s.store.List(ctx, filter)
}
func (s *Service) ListAttempts(ctx context.Context, eventID string, limit int) ([]DeliveryAttempt, error) {
	return s.store.ListAttempts(ctx, eventID, limit)
}
func (s *Service) Replay(ctx context.Context, id string) (*Event, error) {
	return s.store.Replay(ctx, id)
}

// ReplayRequest is the minimum actor context required for a manual replay.
// The raw idempotency value is hashed before persistence because it is a
// caller-controlled request header, not trusted operational metadata.
type ReplayRequest struct {
	ActorID        string
	RequestID      string
	IdempotencyKey string
}

var ErrReplayIdempotencyKeyRequired = errors.New("Idempotency-Key is required for dead-letter replay")
var ErrReplayAlreadyRequested = errors.New("dead-letter replay with this idempotency key has already been requested")

// ReplayCommand transitions a dead-letter event back to pending together with
// its required command audit. A repeated request with the same actor/event/key
// returns the current event and never resets it a second time.
func (s *Service) ReplayCommand(ctx context.Context, id string, request ReplayRequest) (*Event, error) {
	if s == nil || s.store == nil || s.transactions == nil {
		return nil, errors.New("reliability service is unavailable")
	}
	id = strings.TrimSpace(id)
	request.ActorID = strings.TrimSpace(request.ActorID)
	request.IdempotencyKey = strings.TrimSpace(request.IdempotencyKey)
	if id == "" {
		return nil, ErrEventNotFound
	}
	if request.ActorID == "" {
		return nil, errors.New("replay actor is required")
	}
	if request.IdempotencyKey == "" {
		return nil, ErrReplayIdempotencyKeyRequired
	}

	operationID := fmt.Sprintf("replay-%d", idgen.New())
	operationKey := replayOperationKey(request.ActorID, id, request.IdempotencyKey)
	var replayed *Event
	err := s.transactions.Within(ctx, func(txCtx context.Context) error {
		operation, err := s.store.StartOperation(txCtx, Operation{
			ID: operationID, Kind: "reliability.event.replay", SubjectType: "outbox_event", SubjectID: id,
			ActorID: request.ActorID, IdempotencyKey: operationKey,
			Details: json.RawMessage(`{"action":"manual_dead_letter_replay"}`),
		})
		if err != nil {
			return fmt.Errorf("start replay operation: %w", err)
		}
		if operation.ID != operationID {
			if operation.Status == OperationSucceeded {
				replayed, err = s.store.Get(txCtx, id)
				return err
			}
			return fmt.Errorf("%w: %s", ErrReplayAlreadyRequested, operation.Status)
		}
		operation.Status = OperationRunning
		if err := s.store.UpdateOperation(txCtx, *operation); err != nil {
			return fmt.Errorf("mark replay operation running: %w", err)
		}
		replayed, err = s.store.Replay(txCtx, id)
		if err != nil {
			return err
		}
		if err := s.store.RecordCommandAudit(txCtx, CommandAudit{
			ID:             fmt.Sprintf("%d", idgen.New()),
			CommandID:      operation.ID,
			CommandCode:    "platform.reliability.replay",
			ActorID:        request.ActorID,
			ActorType:      "user",
			ResourceType:   "outbox_event",
			ResourceID:     id,
			OperationCode:  "http.platform.reliability.replay",
			PermissionCode: "platform.reliability.replay",
			RequestID:      request.RequestID,
			Details:        json.RawMessage(`{"mode":"manual_dead_letter_replay"}`),
			CreatedAt:      time.Now().UTC(),
		}); err != nil {
			return fmt.Errorf("record replay command audit: %w", err)
		}
		operation.Status = OperationSucceeded
		operation.Error = ""
		return s.store.UpdateOperation(txCtx, *operation)
	})
	return replayed, err
}

func replayOperationKey(actorID, eventID, idempotencyKey string) string {
	digest := sha256.Sum256([]byte(actorID + "\x00" + eventID + "\x00" + idempotencyKey))
	return fmt.Sprintf("reliability.replay:%x", digest[:])
}
func (s *Service) ListCompatibility(ctx context.Context, limit int) ([]CompatibilityUsage, error) {
	return s.store.ListCompatibility(ctx, limit)
}
func (s *Service) ListWorkers(ctx context.Context, limit int) ([]WorkerLease, error) {
	return s.store.ListWorkers(ctx, limit)
}
func (s *Service) ListOperations(ctx context.Context, kind string, limit int) ([]Operation, error) {
	return s.store.ListOperations(ctx, kind, limit)
}

func (s *Service) ListCommandAudits(ctx context.Context, limit int) ([]CommandAudit, error) {
	return s.store.ListCommandAudits(ctx, limit)
}

// RecoverInterruptedOperations makes a process-interrupted filesystem workflow
// visible as a terminal failed operation. The caller must use its recorded
// snapshot or explicit retry path; v11 never guesses how to mutate files on
// startup.
func (s *Service) RecoverInterruptedOperations(ctx context.Context) ([]Operation, error) {
	if s == nil || s.store == nil {
		return nil, errors.New("reliability service is unavailable")
	}
	return s.store.RecoverInterruptedOperations(ctx)
}

func (s *Service) PreviewRetention(ctx context.Context, target string, before time.Time) (RetentionPreview, error) {
	return s.store.PreviewRetention(ctx, target, before)
}

func (s *Service) StartRetentionPreview(ctx context.Context, target string, before time.Time) (*RetentionRun, error) {
	preview, err := s.store.PreviewRetention(ctx, target, before)
	if err != nil {
		return nil, err
	}
	return s.store.StartRetentionRun(ctx, RetentionRun{
		Target: target, Before: preview.Before, EligibleRows: preview.EligibleRows,
		Mode: "dry-run", Status: "completed",
	})
}

func (s *Service) ListRetentionRuns(ctx context.Context, limit int) ([]RetentionRun, error) {
	return s.store.ListRetentionRuns(ctx, limit)
}

func (s *Service) RecordCompatibility(ctx context.Context, key, kind string, detail any) error {
	encoded, err := json.Marshal(detail)
	if err != nil {
		return err
	}
	return s.store.RecordCompatibility(ctx, CompatibilityUsage{Key: key, Kind: kind, Detail: encoded})
}

// TrackOperation records a compensation-aware workflow. The operation is made
// durable before work starts, then transitioned to a terminal state. Callers
// keep their existing local rollback/staging behavior and record the failure
// rather than silently losing the evidence.
func (s *Service) TrackOperation(ctx context.Context, operation Operation, action func(context.Context) error) error {
	if s == nil || s.store == nil {
		return errors.New("reliability service is unavailable")
	}
	if action == nil {
		return errors.New("operation action is required")
	}
	stored, err := s.store.StartOperation(ctx, operation)
	if err != nil {
		return err
	}
	if stored.Status == OperationSucceeded {
		return nil
	}
	stored.Status = OperationRunning
	if err := s.store.UpdateOperation(ctx, *stored); err != nil {
		return err
	}
	if err := action(ctx); err != nil {
		stored.Status = OperationFailed
		stored.Error = err.Error()
		_ = s.store.UpdateOperation(context.Background(), *stored)
		return err
	}
	stored.Status = OperationSucceeded
	stored.Error = ""
	return s.store.UpdateOperation(ctx, *stored)
}

// DefaultEventBusHandler maintains legacy subscribers. It only runs after the
// outbox row has been committed, so the in-memory/NATS bus is no longer the
// source of truth for state-changing commands.
func DefaultEventBusHandler(bus eventbus.EventBus) EventHandler {
	return func(ctx context.Context, event Event) error {
		if bus == nil {
			return errors.New("event bus is unavailable")
		}
		var payload any
		if len(event.Payload) > 0 {
			if err := json.Unmarshal(event.Payload, &payload); err != nil {
				return Permanent(fmt.Errorf("decode durable event payload: %w", err))
			}
		}
		legacy := eventbus.NewEvent(event.Type, "campusos.reliability", event.AggregateType+"."+event.AggregateID, payload)
		legacy.ID = event.ID
		return bus.Publish(ctx, legacy)
	}
}
