package reliability

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/campusos/CampusOS/pkg/observability"
)

// EventHandler consumes one durable event. A successful return is the consumer's
// acknowledgement; the worker records it before another worker can claim the
// same event.
type EventHandler func(context.Context, Event) error

// DeliveryError lets a handler classify an expected failure without leaking a
// transport-specific error type through the platform boundary.
type DeliveryError struct {
	Err        error
	Retryable  bool
	RetryAfter time.Duration
}

func (e *DeliveryError) Error() string {
	if e == nil || e.Err == nil {
		return "event delivery failed"
	}
	return e.Err.Error()
}

func (e *DeliveryError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func Retryable(err error, after time.Duration) error {
	if err == nil {
		return nil
	}
	return &DeliveryError{Err: err, Retryable: true, RetryAfter: after}
}

func Permanent(err error) error {
	if err == nil {
		return nil
	}
	return &DeliveryError{Err: err, Retryable: false}
}

type WorkerConfig struct {
	ID            string
	PollInterval  time.Duration
	LeaseDuration time.Duration
	BatchSize     int
	Logger        *log.Logger
}

// Worker is intentionally small. It has no domain knowledge; feature modules
// register handlers by event type and the fallback dispatches compatible
// domain events to the pre-existing EventBus.
type Worker struct {
	store Store
	cfg   WorkerConfig

	mu        sync.RWMutex
	handlers  map[string][]handlerRegistration
	fallback  handlerRegistration
	cancel    context.CancelFunc
	done      chan struct{}
	logger    *log.Logger
	telemetry telemetry
}

type handlerRegistration struct {
	consumer  string
	handler   EventHandler
	exclusive bool
}

const (
	finalizeConsumerName        = "system:outbox-finalize"
	completeFailureMessage      = "complete durable event failed"
	stateTransitionFailure      = "outbox state transition failed"
	consumerFailureMessage      = "durable event consumer failed"
	receiptReadFailureMessage   = "read consumer receipt failed"
	receiptRecordFailureMessage = "record consumer receipt failed"
	attemptStartFailureMessage  = "start delivery attempt failed"
	attemptFinishFailureMessage = "finish delivery attempt failed"
)

type workerOperationError struct {
	operation string
	message   string
	cause     error
}

func (e *workerOperationError) Error() string {
	if e == nil {
		return "durable event worker failed"
	}
	return e.operation + ": " + e.message
}

func (e *workerOperationError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

func NewWorker(store Store, cfg WorkerConfig) *Worker {
	if cfg.ID == "" {
		cfg.ID = fmt.Sprintf("campusos-outbox-%d", time.Now().UTC().UnixNano())
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 500 * time.Millisecond
	}
	if cfg.LeaseDuration <= 0 {
		cfg.LeaseDuration = 30 * time.Second
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 16
	}
	logger := cfg.Logger
	if logger == nil {
		logger = log.Default()
	}
	return &Worker{store: store, cfg: cfg, handlers: make(map[string][]handlerRegistration), logger: logger}
}

func (w *Worker) SetMeter(meter observability.Meter) {
	if w == nil {
		return
	}
	w.mu.Lock()
	w.telemetry.meter = meter
	w.mu.Unlock()
}

// RegisterHandler keeps the original one-handler dispatch behavior. An
// exclusive handler suppresses the legacy EventBus fallback for this event
// type; use RegisterConsumer for additive post-commit consumers such as the
// Webhook fan-out.
func (w *Worker) Register(eventType string, handler EventHandler) {
	w.register(eventType, "event:"+eventType, handler, true)
}

// RegisterConsumer adds one independently acknowledged consumer. Each
// consumer gets its own durable receipt, so a retry after consumer B fails
// does not run consumer A's external side effect a second time.
func (w *Worker) RegisterConsumer(eventType, consumer string, handler EventHandler) {
	w.register(eventType, consumer, handler, false)
}

func (w *Worker) register(eventType, consumer string, handler EventHandler, exclusive bool) {
	eventType = strings.TrimSpace(eventType)
	consumer = strings.TrimSpace(consumer)
	if eventType == "" || consumer == "" {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	items := w.handlers[eventType]
	for index, item := range items {
		if item.consumer != consumer {
			continue
		}
		if handler == nil {
			items = append(items[:index], items[index+1:]...)
			if len(items) == 0 {
				delete(w.handlers, eventType)
			} else {
				w.handlers[eventType] = items
			}
			return
		}
		items[index] = handlerRegistration{consumer: consumer, handler: handler, exclusive: exclusive}
		w.handlers[eventType] = items
		return
	}
	if handler == nil {
		return
	}
	w.handlers[eventType] = append(items, handlerRegistration{consumer: consumer, handler: handler, exclusive: exclusive})
}

func (w *Worker) SetFallback(handler EventHandler) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.fallback = handlerRegistration{consumer: "event:fallback", handler: handler}
}

func (w *Worker) Start(parent context.Context) {
	w.mu.Lock()
	if w.cancel != nil {
		w.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(parent)
	w.cancel = cancel
	w.done = make(chan struct{})
	done := w.done
	w.mu.Unlock()

	go func() {
		defer close(done)
		ticker := time.NewTicker(w.cfg.PollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if _, err := w.ProcessOnce(ctx); err != nil {
					w.logCycleFailure("process_once", "worker cycle completed with errors")
				}
			}
		}
	}()
}

func (w *Worker) Stop(ctx context.Context) error {
	w.mu.Lock()
	cancel := w.cancel
	done := w.done
	w.cancel = nil
	w.done = nil
	w.mu.Unlock()
	if cancel == nil {
		return nil
	}
	cancel()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (w *Worker) ProcessOnce(ctx context.Context) (int, error) {
	if w.store == nil {
		return 0, errors.New("durable event store is unavailable")
	}
	if err := w.store.Heartbeat(ctx, w.cfg.ID, time.Now().UTC()); err != nil {
		return 0, w.observe(Event{}, "heartbeat", "worker heartbeat failed", err)
	}
	events, err := w.store.Claim(ctx, w.cfg.ID, w.cfg.BatchSize, w.cfg.LeaseDuration)
	if err != nil {
		w.telemetry.operation("claim", "error")
		return 0, w.observe(Event{}, "claim", "claim durable events failed", err)
	}
	w.telemetry.operation("claim", "success")
	processErrors := make([]error, 0)
	for _, event := range events {
		if err := w.process(ctx, event); err != nil {
			processErrors = append(processErrors, err)
		}
	}
	return len(events), errors.Join(processErrors...)
}

func (w *Worker) process(ctx context.Context, event Event) error {
	w.mu.RLock()
	registered := append([]handlerRegistration(nil), w.handlers[event.Type]...)
	includeFallback := w.fallback.handler != nil
	for _, item := range registered {
		if item.exclusive {
			includeFallback = false
			break
		}
	}
	if includeFallback {
		registered = append(registered, w.fallback)
	}
	w.mu.RUnlock()

	if len(registered) == 0 {
		attempt, attemptErr := w.startAttempt(ctx, event, "event:no-handler")
		observed := make([]error, 0, 3)
		if attemptErr != nil {
			observed = append(observed, w.observe(event, "start_attempt", attemptStartFailureMessage, attemptErr))
		}
		deliveryErr := errors.New("no registered durable event handler")
		observed = append(observed, w.observe(event, "dispatch", "no registered durable event handler", deliveryErr))
		observed = append(observed, w.fail(ctx, event, deliveryErr, "no registered durable event handler", attempt))
		return errors.Join(observed...)
	}
	evidenceErrors := make([]error, 0)
	for _, item := range registered {
		consumer := item.consumer
		if consumer == "" {
			consumer = "event:" + event.Type
		}
		attempt, attemptErr := w.startAttempt(ctx, event, consumer)
		if attemptErr != nil {
			evidenceErrors = append(evidenceErrors, w.observe(event, "start_attempt", attemptStartFailureMessage, attemptErr))
		}
		acknowledged, err := w.store.HasConsumerReceipt(ctx, consumer, event.ID)
		if err != nil {
			w.telemetry.operation("receipt", "error")
			failure := w.observe(event, "read_receipt", receiptReadFailureMessage, err)
			transitionErr := w.fail(ctx, event, err, receiptReadFailureMessage, attempt)
			return errors.Join(append(evidenceErrors, failure, transitionErr)...)
		}
		if acknowledged {
			w.telemetry.operation("receipt", "existing")
			if err := w.finishAttempt(ctx, attempt, "skipped", "consumer receipt already exists"); err != nil {
				evidenceErrors = append(evidenceErrors, w.observe(event, "finish_attempt", attemptFinishFailureMessage, err))
			}
			continue
		}
		consumerStarted := time.Now()
		if handlerErr := item.handler(ctx, event); handlerErr != nil {
			w.telemetry.consumer(consumer, deliveryResult(event, handlerErr), time.Since(consumerStarted))
			failure := w.observe(event, "consume", consumerFailureMessage, handlerErr)
			transitionErr := w.fail(ctx, event, handlerErr, consumerFailureMessage, attempt)
			return errors.Join(append(evidenceErrors, failure, transitionErr)...)
		}
		w.telemetry.consumer(consumer, "success", time.Since(consumerStarted))
		if receiptErr := w.store.RecordConsumerReceipt(ctx, ConsumerReceipt{
			ConsumerName: consumer, EventID: event.ID, Attempt: event.Attempts,
		}); receiptErr != nil {
			w.telemetry.operation("receipt", "error")
			failure := w.observe(event, "record_receipt", receiptRecordFailureMessage, receiptErr)
			transitionErr := w.fail(ctx, event, receiptErr, receiptRecordFailureMessage, attempt)
			return errors.Join(append(evidenceErrors, failure, transitionErr)...)
		}
		w.telemetry.operation("receipt", "recorded")
		if err := w.finishAttempt(ctx, attempt, "succeeded", ""); err != nil {
			evidenceErrors = append(evidenceErrors, w.observe(event, "finish_attempt", attemptFinishFailureMessage, err))
		}
	}
	finalizeAttempt, attemptErr := w.startAttempt(ctx, event, finalizeConsumerName)
	if attemptErr != nil {
		evidenceErrors = append(evidenceErrors, w.observe(event, "start_finalize_attempt", attemptStartFailureMessage, attemptErr))
	}
	if err := w.store.Complete(ctx, event.ID, w.cfg.ID, event.LeaseGeneration); err != nil {
		w.telemetry.operation("complete", "error")
		failure := w.observe(event, "complete", completeFailureMessage, err)
		transitionErr := w.fail(ctx, event, err, completeFailureMessage, finalizeAttempt)
		return errors.Join(append(evidenceErrors, failure, transitionErr)...)
	}
	w.telemetry.operation("complete", "success")
	if err := w.finishAttempt(ctx, finalizeAttempt, "succeeded", ""); err != nil {
		w.telemetry.operation("finalize", "error")
		evidenceErrors = append(evidenceErrors, w.observe(event, "finish_finalize_attempt", attemptFinishFailureMessage, err))
	} else {
		w.telemetry.operation("finalize", "success")
	}
	return errors.Join(evidenceErrors...)
}

func (w *Worker) startAttempt(ctx context.Context, event Event, consumer string) (*DeliveryAttempt, error) {
	attempt, err := w.store.StartAttempt(ctx, DeliveryAttempt{
		EventID: event.ID, ConsumerName: consumer, WorkerID: w.cfg.ID,
		LeaseGeneration: event.LeaseGeneration, Attempt: event.Attempts,
	})
	if err != nil {
		return nil, err
	}
	return attempt, nil
}

func (w *Worker) finishAttempt(ctx context.Context, attempt *DeliveryAttempt, status, message string) error {
	if attempt == nil {
		return nil
	}
	copyAttempt := *attempt
	copyAttempt.Status = status
	copyAttempt.Error = message
	return w.store.FinishAttempt(ctx, copyAttempt)
}

func (w *Worker) fail(ctx context.Context, event Event, err error, message string, attempt *DeliveryAttempt) error {
	var deliveryErr *DeliveryError
	retryable := true
	retryAfter := retryDelay(event.Attempts, event.ID)
	if errors.As(err, &deliveryErr) {
		retryable = deliveryErr.Retryable
		if deliveryErr.RetryAfter > 0 {
			retryAfter = deliveryErr.RetryAfter
		}
	}
	targetStatus := "retry"
	operation := "retry"
	var transitionErr error
	if !retryable || event.Attempts >= event.MaxAttempts {
		targetStatus = "dead"
		operation = "dead_letter"
		transitionErr = w.store.DeadLetter(ctx, event.ID, w.cfg.ID, event.LeaseGeneration, message)
	} else {
		transitionErr = w.store.Retry(ctx, event.ID, w.cfg.ID, event.LeaseGeneration, time.Now().UTC().Add(retryAfter), message)
	}
	if transitionErr != nil {
		w.telemetry.operation(operation, "error")
		observed := w.observe(event, operation, stateTransitionFailure, transitionErr)
		finishErr := w.finishAttempt(ctx, attempt, "failed", stateTransitionFailure)
		if finishErr != nil {
			finishErr = w.observe(event, "finish_attempt", attemptFinishFailureMessage, finishErr)
		}
		return errors.Join(observed, finishErr)
	}
	w.telemetry.operation(operation, "success")
	if finishErr := w.finishAttempt(ctx, attempt, targetStatus, message); finishErr != nil {
		return w.observe(event, "finish_attempt", attemptFinishFailureMessage, finishErr)
	}
	return nil
}

func (w *Worker) observe(event Event, operation, message string, cause error) error {
	if errors.Is(cause, ErrLeaseLost) {
		message = "lease lost while updating durable event"
		w.telemetry.operation("lease_conflict", "error")
	}
	w.logger.Printf(
		"component=reliability_worker level=error event_id=%q event_type=%q worker_id=%q lease_generation=%d attempts=%d max_attempts=%d operation=%q error=%q",
		event.ID, event.Type, w.cfg.ID, event.LeaseGeneration, event.Attempts, event.MaxAttempts, operation, message,
	)
	return &workerOperationError{operation: operation, message: message, cause: cause}
}

func deliveryResult(event Event, err error) string {
	var deliveryErr *DeliveryError
	if errors.As(err, &deliveryErr) && !deliveryErr.Retryable {
		return "dead"
	}
	if event.Attempts >= event.MaxAttempts {
		return "dead"
	}
	return "retry"
}

func (w *Worker) logCycleFailure(operation, message string) {
	w.logger.Printf(
		"component=reliability_worker level=error event_id=%q event_type=%q worker_id=%q lease_generation=%d attempts=%d max_attempts=%d operation=%q error=%q",
		"", "", w.cfg.ID, 0, 0, 0, operation, message,
	)
}

func retryDelay(attempt int, seed string) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	if attempt > 7 {
		attempt = 7
	}
	base := time.Second * time.Duration(1<<(attempt-1))
	// Deterministic bounded jitter avoids retry herds while retaining stable
	// tests and making a task's retry schedule explainable from its event ID.
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(seed))
	jitter := time.Duration(hash.Sum32()%250) * time.Millisecond
	return base + jitter
}
