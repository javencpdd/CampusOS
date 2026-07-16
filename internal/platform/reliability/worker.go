package reliability

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"strings"
	"sync"
	"time"
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
}

// Worker is intentionally small. It has no domain knowledge; feature modules
// register handlers by event type and the fallback dispatches compatible
// domain events to the pre-existing EventBus.
type Worker struct {
	store Store
	cfg   WorkerConfig

	mu       sync.RWMutex
	handlers map[string][]handlerRegistration
	fallback handlerRegistration
	cancel   context.CancelFunc
	done     chan struct{}
}

type handlerRegistration struct {
	consumer  string
	handler   EventHandler
	exclusive bool
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
	return &Worker{store: store, cfg: cfg, handlers: make(map[string][]handlerRegistration)}
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
				_, _ = w.ProcessOnce(ctx)
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
		return 0, fmt.Errorf("outbox worker heartbeat: %w", err)
	}
	events, err := w.store.Claim(ctx, w.cfg.ID, w.cfg.BatchSize, w.cfg.LeaseDuration)
	if err != nil {
		return 0, err
	}
	for _, event := range events {
		w.process(ctx, event)
	}
	return len(events), nil
}

func (w *Worker) process(ctx context.Context, event Event) {
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
		attempt := w.startAttempt(ctx, event, "event:no-handler")
		w.fail(ctx, event, errors.New("no registered durable event handler"), attempt)
		return
	}
	for _, item := range registered {
		consumer := item.consumer
		if consumer == "" {
			consumer = "event:" + event.Type
		}
		attempt := w.startAttempt(ctx, event, consumer)
		if attempt == nil {
			w.fail(ctx, event, errors.New("record delivery attempt: attempt store unavailable"), nil)
			return
		}
		acknowledged, err := w.store.HasConsumerReceipt(ctx, consumer, event.ID)
		if err != nil {
			w.fail(ctx, event, fmt.Errorf("read consumer receipt: %w", err), attempt)
			return
		}
		if acknowledged {
			w.finishAttempt(ctx, attempt, "skipped", "consumer receipt already exists")
			continue
		}
		if handlerErr := item.handler(ctx, event); handlerErr != nil {
			w.fail(ctx, event, handlerErr, attempt)
			return
		}
		if receiptErr := w.store.RecordConsumerReceipt(ctx, ConsumerReceipt{
			ConsumerName: consumer, EventID: event.ID, Attempt: event.Attempts,
		}); receiptErr != nil {
			w.fail(ctx, event, fmt.Errorf("record consumer receipt: %w", receiptErr), attempt)
			return
		}
		w.finishAttempt(ctx, attempt, "succeeded", "")
	}
	_ = w.store.Complete(ctx, event.ID, w.cfg.ID, event.LeaseGeneration)
}

func (w *Worker) startAttempt(ctx context.Context, event Event, consumer string) *DeliveryAttempt {
	attempt, err := w.store.StartAttempt(ctx, DeliveryAttempt{
		EventID: event.ID, ConsumerName: consumer, WorkerID: w.cfg.ID,
		LeaseGeneration: event.LeaseGeneration, Attempt: event.Attempts,
	})
	if err != nil {
		return nil
	}
	return attempt
}

func (w *Worker) finishAttempt(ctx context.Context, attempt *DeliveryAttempt, status, message string) {
	if attempt == nil {
		return
	}
	copyAttempt := *attempt
	copyAttempt.Status = status
	copyAttempt.Error = message
	_ = w.store.FinishAttempt(ctx, copyAttempt)
}

func (w *Worker) fail(ctx context.Context, event Event, err error, attempt *DeliveryAttempt) {
	var deliveryErr *DeliveryError
	retryable := true
	retryAfter := retryDelay(event.Attempts, event.ID)
	if errors.As(err, &deliveryErr) {
		retryable = deliveryErr.Retryable
		if deliveryErr.RetryAfter > 0 {
			retryAfter = deliveryErr.RetryAfter
		}
	}
	if !retryable || event.Attempts >= event.MaxAttempts {
		w.finishAttempt(ctx, attempt, "dead", err.Error())
		_ = w.store.DeadLetter(ctx, event.ID, w.cfg.ID, event.LeaseGeneration, err.Error())
		return
	}
	w.finishAttempt(ctx, attempt, "retry", err.Error())
	_ = w.store.Retry(ctx, event.ID, w.cfg.ID, event.LeaseGeneration, time.Now().UTC().Add(retryAfter), err.Error())
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
