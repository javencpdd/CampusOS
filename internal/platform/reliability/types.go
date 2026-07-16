// Package reliability implements CampusOS's durable command and event boundary.
// It is deliberately a platform package: feature modules use its narrow port,
// while external plugins continue to use the public Host API and Gateway.
package reliability

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/campusos/CampusOS/pkg/idgen"
)

const (
	EventSchemaV1 = "v1"

	StatusPending    = "pending"
	StatusProcessing = "processing"
	StatusPublished  = "published"
	StatusRetry      = "retry"
	StatusDead       = "dead"

	OperationPending    = "pending"
	OperationRunning    = "running"
	OperationCompensate = "compensating"
	OperationSucceeded  = "succeeded"
	OperationFailed     = "failed"
)

// Event is the durable representation of a domain event. Payload and headers
// are JSON so the worker can survive process restarts without Go type coupling.
type Event struct {
	ID              string          `json:"id"`
	Type            string          `json:"type"`
	SchemaVersion   string          `json:"schema_version"`
	AggregateType   string          `json:"aggregate_type,omitempty"`
	AggregateID     string          `json:"aggregate_id,omitempty"`
	Payload         json.RawMessage `json:"payload"`
	Headers         json.RawMessage `json:"headers,omitempty"`
	Status          string          `json:"status"`
	IdempotencyKey  string          `json:"idempotency_key,omitempty"`
	Attempts        int             `json:"attempts"`
	MaxAttempts     int             `json:"max_attempts"`
	AvailableAt     time.Time       `json:"available_at"`
	LeaseOwner      string          `json:"lease_owner,omitempty"`
	LeaseUntil      *time.Time      `json:"lease_until,omitempty"`
	LeaseGeneration int64           `json:"lease_generation"`
	LastError       string          `json:"last_error,omitempty"`
	DeadLetteredAt  *time.Time      `json:"dead_lettered_at,omitempty"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
}

func NewEvent(eventType, aggregateType, aggregateID string, payload any) (Event, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return Event{}, fmt.Errorf("marshal durable event payload: %w", err)
	}
	return Event{
		ID:            fmt.Sprintf("%d", idgen.New()),
		Type:          strings.TrimSpace(eventType),
		SchemaVersion: EventSchemaV1,
		AggregateType: strings.TrimSpace(aggregateType),
		AggregateID:   strings.TrimSpace(aggregateID),
		Payload:       encoded,
		Headers:       json.RawMessage(`{}`),
		Status:        StatusPending,
		MaxAttempts:   8,
	}, nil
}

// Command describes an auditable, atomic business operation. Action is
// supplied to Service.Execute instead of being serialised.
type Command struct {
	ID             string `json:"id"`
	Code           string `json:"code"`
	ActorID        string `json:"actor_id,omitempty"`
	ActorType      string `json:"actor_type,omitempty"`
	ResourceType   string `json:"resource_type,omitempty"`
	ResourceID     string `json:"resource_id,omitempty"`
	OperationCode  string `json:"operation_code,omitempty"`
	PermissionCode string `json:"permission_code,omitempty"`
	RequestID      string `json:"request_id,omitempty"`
	TraceID        string `json:"trace_id,omitempty"`
	IdempotencyKey string `json:"idempotency_key,omitempty"`
	Event          *Event `json:"-"`
	// EventFactory runs after the business action succeeds but before commit.
	// It allows the outbox payload to carry the persisted post-transition state.
	EventFactory func() (Event, error) `json:"-"`
}

// CommandAudit is persisted only after the command action and durable event
// have both been accepted by the same transaction.
type CommandAudit struct {
	ID             string          `json:"id"`
	CommandID      string          `json:"command_id"`
	CommandCode    string          `json:"command_code"`
	ActorID        string          `json:"actor_id,omitempty"`
	ActorType      string          `json:"actor_type,omitempty"`
	ResourceType   string          `json:"resource_type,omitempty"`
	ResourceID     string          `json:"resource_id,omitempty"`
	OperationCode  string          `json:"operation_code,omitempty"`
	PermissionCode string          `json:"permission_code,omitempty"`
	RequestID      string          `json:"request_id,omitempty"`
	TraceID        string          `json:"trace_id,omitempty"`
	EventID        string          `json:"event_id,omitempty"`
	Details        json.RawMessage `json:"details,omitempty"`
	CreatedAt      time.Time       `json:"created_at"`
}

type EventFilter struct {
	Status string
	Type   string
	Limit  int
}

type Summary struct {
	Pending       int64      `json:"pending"`
	Processing    int64      `json:"processing"`
	Published     int64      `json:"published"`
	Retry         int64      `json:"retry"`
	Dead          int64      `json:"dead"`
	OldestPending *time.Time `json:"oldest_pending_at,omitempty"`
}

// WorkerLease is a read-only heartbeat record. It deliberately contains no
// task payload, user identifier, or runtime secret so it can be displayed in
// the protected operations console without leaking command details.
type WorkerLease struct {
	WorkerID        string    `json:"worker_id"`
	LastHeartbeatAt time.Time `json:"last_heartbeat_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// ConsumerReceipt prevents a successful external side effect from being
// repeated when a worker crashes after the handler returns but before the
// outbox row can be marked published. It is scoped to one named consumer.
type ConsumerReceipt struct {
	ConsumerName string    `json:"consumer_name"`
	EventID      string    `json:"event_id"`
	Attempt      int       `json:"attempt"`
	DeliveredAt  time.Time `json:"delivered_at"`
}

// DeliveryAttempt is immutable operational evidence for one consumer attempt.
// It supplements the mutable outbox row so replay and dead-letter reviews can
// distinguish retry history without placing payloads or secrets in telemetry.
type DeliveryAttempt struct {
	ID              string     `json:"id"`
	EventID         string     `json:"event_id"`
	ConsumerName    string     `json:"consumer_name"`
	WorkerID        string     `json:"worker_id"`
	LeaseGeneration int64      `json:"lease_generation"`
	Attempt         int        `json:"attempt"`
	Status          string     `json:"status"`
	Error           string     `json:"error,omitempty"`
	StartedAt       time.Time  `json:"started_at"`
	FinishedAt      *time.Time `json:"finished_at,omitempty"`
}

// Operation records an import/apply/rollback workflow without pretending that
// arbitrary filesystem work can participate in a database transaction.
type Operation struct {
	ID             string          `json:"id"`
	Kind           string          `json:"kind"`
	SubjectType    string          `json:"subject_type"`
	SubjectID      string          `json:"subject_id"`
	Status         string          `json:"status"`
	ActorID        string          `json:"actor_id,omitempty"`
	IdempotencyKey string          `json:"idempotency_key,omitempty"`
	Details        json.RawMessage `json:"details,omitempty"`
	Error          string          `json:"error,omitempty"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

type CompatibilityUsage struct {
	Key       string          `json:"key"`
	Kind      string          `json:"kind"`
	Detail    json.RawMessage `json:"detail,omitempty"`
	FirstSeen time.Time       `json:"first_seen"`
	LastSeen  time.Time       `json:"last_seen"`
	Count     int64           `json:"count"`
}

type RetentionPreview struct {
	Target       string    `json:"target"`
	Before       time.Time `json:"before"`
	EligibleRows int64     `json:"eligible_rows"`
	CanDelete    bool      `json:"can_delete"`
}

// RetentionRun records an explicitly requested dry-run. v11 intentionally
// does not delete historical data; operators use this evidence to prepare a
// later approved retention execution feature.
type RetentionRun struct {
	ID           string    `json:"id"`
	Target       string    `json:"target"`
	Before       time.Time `json:"before"`
	EligibleRows int64     `json:"eligible_rows"`
	Mode         string    `json:"mode"`
	Status       string    `json:"status"`
	CreatedAt    time.Time `json:"created_at"`
}

func normalizeEvent(event *Event) {
	now := time.Now().UTC()
	if event.ID == "" {
		event.ID = fmt.Sprintf("%d", idgen.New())
	}
	event.Type = strings.TrimSpace(event.Type)
	if event.SchemaVersion == "" {
		event.SchemaVersion = EventSchemaV1
	}
	if event.Headers == nil {
		event.Headers = json.RawMessage(`{}`)
	}
	if event.Payload == nil {
		event.Payload = json.RawMessage(`{}`)
	}
	if event.Status == "" {
		event.Status = StatusPending
	}
	if event.MaxAttempts <= 0 {
		event.MaxAttempts = 8
	}
	if event.AvailableAt.IsZero() {
		event.AvailableAt = now
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = now
	}
	event.UpdatedAt = now
}

func normalizeCommand(command *Command) {
	if command.ID == "" {
		command.ID = fmt.Sprintf("%d", idgen.New())
	}
	command.Code = strings.TrimSpace(command.Code)
	if command.ActorType == "" {
		command.ActorType = "user"
	}
}

func cloneEvent(event Event) Event {
	copyEvent := event
	copyEvent.Payload = append(json.RawMessage(nil), event.Payload...)
	copyEvent.Headers = append(json.RawMessage(nil), event.Headers...)
	if event.LeaseUntil != nil {
		value := *event.LeaseUntil
		copyEvent.LeaseUntil = &value
	}
	if event.DeadLetteredAt != nil {
		value := *event.DeadLetteredAt
		copyEvent.DeadLetteredAt = &value
	}
	return copyEvent
}

func cloneCommandAudit(audit CommandAudit) CommandAudit {
	copyAudit := audit
	copyAudit.Details = append(json.RawMessage(nil), audit.Details...)
	return copyAudit
}

func cloneCommandAudits(audits []CommandAudit) []CommandAudit {
	items := make([]CommandAudit, len(audits))
	for i, audit := range audits {
		items[i] = cloneCommandAudit(audit)
	}
	return items
}
