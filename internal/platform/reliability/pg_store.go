package reliability

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgreSQLStore is the production durable outbox adapter. Every write uses
// transaction.ExecutorFor, so a command's state change, audit, and outbox row
// are committed or rolled back together.
type PostgreSQLStore struct {
	pool *pgxpool.Pool
}

func NewPostgreSQLStore(pool *pgxpool.Pool) *PostgreSQLStore {
	return &PostgreSQLStore{pool: pool}
}

func (s *PostgreSQLStore) db(ctx context.Context) transaction.Executor {
	return transaction.ExecutorFor(ctx, s.pool)
}

func (s *PostgreSQLStore) Enqueue(ctx context.Context, event *Event) (*Event, error) {
	if event == nil {
		return nil, errors.New("durable event is required")
	}
	normalizeEvent(event)
	if event.Type == "" {
		return nil, errors.New("durable event type is required")
	}
	if event.IdempotencyKey != "" {
		if existing, err := s.eventByIdempotency(ctx, event.IdempotencyKey); err == nil {
			return existing, nil
		} else if !errors.Is(err, ErrEventNotFound) {
			return nil, err
		}
	}

	query := `INSERT INTO platform_outbox (
		id, event_type, schema_version, aggregate_type, aggregate_id, payload, headers, status,
		idempotency_key, attempts, max_attempts, available_at, lease_owner,
		lease_until, lease_generation, last_error, dead_lettered_at, created_at, updated_at
	) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NULLIF($9, ''), $10, $11, $12, NULLIF($13, ''), $14, $15, NULLIF($16, ''), $17, $18, $19)`
	_, err := s.db(ctx).Exec(ctx, query,
		event.ID, event.Type, event.SchemaVersion, event.AggregateType, event.AggregateID, event.Payload, event.Headers,
		event.Status, event.IdempotencyKey, event.Attempts, event.MaxAttempts, event.AvailableAt,
		event.LeaseOwner, event.LeaseUntil, event.LeaseGeneration, event.LastError,
		event.DeadLetteredAt, event.CreatedAt, event.UpdatedAt,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if event.IdempotencyKey != "" && errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return s.eventByIdempotency(ctx, event.IdempotencyKey)
		}
		return nil, fmt.Errorf("insert durable event: %w", err)
	}
	copyEvent := cloneEvent(*event)
	return &copyEvent, nil
}

func (s *PostgreSQLStore) eventByIdempotency(ctx context.Context, key string) (*Event, error) {
	return s.eventByQuery(ctx, `SELECT id, event_type, schema_version, aggregate_type, aggregate_id, payload, headers, status,
		idempotency_key, attempts, max_attempts, available_at, lease_owner, lease_until,
		lease_generation, last_error, dead_lettered_at, created_at, updated_at
		FROM platform_outbox WHERE idempotency_key = $1`, key)
}

func (s *PostgreSQLStore) eventByQuery(ctx context.Context, query string, args ...any) (*Event, error) {
	event := Event{}
	err := s.db(ctx).QueryRow(ctx, query, args...).Scan(
		&event.ID, &event.Type, &event.SchemaVersion, &event.AggregateType, &event.AggregateID, &event.Payload,
		&event.Headers, &event.Status, &event.IdempotencyKey, &event.Attempts, &event.MaxAttempts,
		&event.AvailableAt, &event.LeaseOwner, &event.LeaseUntil, &event.LeaseGeneration,
		&event.LastError, &event.DeadLetteredAt, &event.CreatedAt, &event.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrEventNotFound
	}
	if err != nil {
		return nil, err
	}
	return &event, nil
}

func (s *PostgreSQLStore) Claim(ctx context.Context, owner string, limit int, lease time.Duration) ([]Event, error) {
	if limit <= 0 {
		limit = 16
	}
	if lease <= 0 {
		lease = 30 * time.Second
	}
	rows, err := s.db(ctx).Query(ctx, `WITH candidates AS (
		SELECT id
		FROM platform_outbox
		WHERE (
				(status IN ('pending', 'retry') AND available_at <= NOW())
				OR (status = 'processing' AND lease_until < NOW())
		  )
		ORDER BY available_at ASC, created_at ASC, id ASC
		LIMIT $2
		FOR UPDATE SKIP LOCKED
	)
	UPDATE platform_outbox AS outbox
	SET status = 'processing', lease_owner = $1,
		lease_until = NOW() + $3::interval,
		lease_generation = lease_generation + 1,
		attempts = attempts + 1, updated_at = NOW()
	FROM candidates
	WHERE outbox.id = candidates.id
	RETURNING outbox.id, outbox.event_type, outbox.schema_version, outbox.aggregate_type, outbox.aggregate_id,
		outbox.payload, outbox.headers, outbox.status, COALESCE(outbox.idempotency_key, ''),
		outbox.attempts, outbox.max_attempts, outbox.available_at, COALESCE(outbox.lease_owner, ''),
		outbox.lease_until, outbox.lease_generation, COALESCE(outbox.last_error, ''),
		outbox.dead_lettered_at, outbox.created_at, outbox.updated_at`, owner, limit, lease.String())
	if err != nil {
		return nil, fmt.Errorf("claim durable events: %w", err)
	}
	defer rows.Close()
	items := make([]Event, 0, limit)
	for rows.Next() {
		var event Event
		if err := rows.Scan(&event.ID, &event.Type, &event.SchemaVersion, &event.AggregateType, &event.AggregateID,
			&event.Payload, &event.Headers, &event.Status, &event.IdempotencyKey,
			&event.Attempts, &event.MaxAttempts, &event.AvailableAt, &event.LeaseOwner,
			&event.LeaseUntil, &event.LeaseGeneration, &event.LastError, &event.DeadLetteredAt,
			&event.CreatedAt, &event.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, event)
	}
	return items, rows.Err()
}

func (s *PostgreSQLStore) Complete(ctx context.Context, id, owner string, generation int64) error {
	return s.finish(ctx, id, owner, generation, StatusPublished, time.Time{}, "")
}

func (s *PostgreSQLStore) Retry(ctx context.Context, id, owner string, generation int64, availableAt time.Time, message string) error {
	return s.finish(ctx, id, owner, generation, StatusRetry, availableAt, message)
}

func (s *PostgreSQLStore) DeadLetter(ctx context.Context, id, owner string, generation int64, message string) error {
	return s.finish(ctx, id, owner, generation, StatusDead, time.Time{}, message)
}

func (s *PostgreSQLStore) finish(ctx context.Context, id, owner string, generation int64, status string, availableAt time.Time, message string) error {
	query := `UPDATE platform_outbox
	SET status = $4, lease_owner = NULL, lease_until = NULL,
		available_at = CASE WHEN $4 = 'retry' THEN $5 ELSE available_at END,
		last_error = NULLIF($6, ''),
		dead_lettered_at = CASE WHEN $4 = 'dead' THEN NOW() ELSE dead_lettered_at END,
		updated_at = NOW()
	WHERE id = $1 AND status = 'processing' AND lease_owner = $2 AND lease_generation = $3`
	tag, err := s.db(ctx).Exec(ctx, query, id, owner, generation, status, availableAt, message)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("event %s lease is no longer owned by %s", id, owner)
	}
	return nil
}

func (s *PostgreSQLStore) List(ctx context.Context, filter EventFilter, page PageRequest) ([]Event, int64, error) {
	page = normalizePageRequest(page, 100, 500)
	var total int64
	if err := s.db(ctx).QueryRow(ctx, `SELECT COUNT(*) FROM platform_outbox
		WHERE ($1 = '' OR status = $1) AND ($2 = '' OR event_type = $2)`, filter.Status, filter.Type).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db(ctx).Query(ctx, `SELECT id, event_type, schema_version, aggregate_type, aggregate_id, payload, headers, status,
		COALESCE(idempotency_key, ''), attempts, max_attempts, available_at, COALESCE(lease_owner, ''),
		lease_until, lease_generation, COALESCE(last_error, ''), dead_lettered_at, created_at, updated_at
		FROM platform_outbox
		WHERE ($1 = '' OR status = $1) AND ($2 = '' OR event_type = $2)
		ORDER BY created_at DESC, id DESC LIMIT $3 OFFSET $4`, filter.Status, filter.Type, page.PageSize, page.offset())
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]Event, 0, page.PageSize)
	for rows.Next() {
		var event Event
		if err := rows.Scan(&event.ID, &event.Type, &event.SchemaVersion, &event.AggregateType, &event.AggregateID,
			&event.Payload, &event.Headers, &event.Status, &event.IdempotencyKey,
			&event.Attempts, &event.MaxAttempts, &event.AvailableAt, &event.LeaseOwner,
			&event.LeaseUntil, &event.LeaseGeneration, &event.LastError, &event.DeadLetteredAt,
			&event.CreatedAt, &event.UpdatedAt); err != nil {
			return nil, 0, err
		}
		items = append(items, event)
	}
	return items, total, rows.Err()
}

func (s *PostgreSQLStore) Get(ctx context.Context, id string) (*Event, error) {
	return s.eventByQuery(ctx, `SELECT id, event_type, schema_version, aggregate_type, aggregate_id, payload, headers, status,
		COALESCE(idempotency_key, ''), attempts, max_attempts, available_at, COALESCE(lease_owner, ''), lease_until,
		lease_generation, COALESCE(last_error, ''), dead_lettered_at, created_at, updated_at
		FROM platform_outbox WHERE id = $1`, id)
}

func (s *PostgreSQLStore) Replay(ctx context.Context, id string) (*Event, error) {
	event, err := s.eventByQuery(ctx, `UPDATE platform_outbox
		SET status = 'pending', attempts = 0, available_at = NOW(), lease_owner = NULL,
			lease_until = NULL, last_error = NULL, dead_lettered_at = NULL, updated_at = NOW()
		WHERE id = $1 AND status = 'dead'
		RETURNING id, event_type, schema_version, aggregate_type, aggregate_id, payload, headers, status,
			COALESCE(idempotency_key, ''), attempts, max_attempts, available_at,
			COALESCE(lease_owner, ''), lease_until, lease_generation, COALESCE(last_error, ''),
			dead_lettered_at, created_at, updated_at`, id)
	if errors.Is(err, ErrEventNotFound) {
		if _, getErr := s.Get(ctx, id); getErr == nil {
			return nil, ErrEventNotReplayable
		} else if !errors.Is(getErr, ErrEventNotFound) {
			return nil, getErr
		}
	}
	return event, err
}

func (s *PostgreSQLStore) Summary(ctx context.Context) (Summary, error) {
	var result Summary
	err := s.db(ctx).QueryRow(ctx, `SELECT
		COUNT(*) FILTER (WHERE status = 'pending'),
		COUNT(*) FILTER (WHERE status = 'processing'),
		COUNT(*) FILTER (WHERE status = 'published'),
		COUNT(*) FILTER (WHERE status = 'retry'),
		COUNT(*) FILTER (WHERE status = 'dead'),
		MIN(created_at) FILTER (WHERE status IN ('pending', 'retry'))
		FROM platform_outbox`).Scan(&result.Pending, &result.Processing, &result.Published,
		&result.Retry, &result.Dead, &result.OldestPending)
	return result, err
}

func (s *PostgreSQLStore) Heartbeat(ctx context.Context, workerID string, at time.Time) error {
	_, err := s.db(ctx).Exec(ctx, `INSERT INTO platform_worker_leases (worker_id, last_heartbeat_at, updated_at)
		VALUES ($1, $2, NOW())
		ON CONFLICT (worker_id) DO UPDATE SET last_heartbeat_at = EXCLUDED.last_heartbeat_at, updated_at = NOW()`, workerID, at.UTC())
	return err
}

func (s *PostgreSQLStore) ListWorkers(ctx context.Context, page PageRequest) ([]WorkerLease, int64, error) {
	page = normalizePageRequest(page, 50, 200)
	var total int64
	if err := s.db(ctx).QueryRow(ctx, `SELECT COUNT(*) FROM platform_worker_leases`).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db(ctx).Query(ctx, `SELECT worker_id, last_heartbeat_at, updated_at
		FROM platform_worker_leases ORDER BY last_heartbeat_at DESC, worker_id ASC LIMIT $1 OFFSET $2`, page.PageSize, page.offset())
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]WorkerLease, 0, page.PageSize)
	for rows.Next() {
		var item WorkerLease
		if err := rows.Scan(&item.WorkerID, &item.LastHeartbeatAt, &item.UpdatedAt); err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}

func (s *PostgreSQLStore) HasConsumerReceipt(ctx context.Context, consumer, eventID string) (bool, error) {
	var exists bool
	err := s.db(ctx).QueryRow(ctx, `SELECT EXISTS(
		SELECT 1 FROM outbox_consumer_receipts WHERE consumer_name = $1 AND event_id = $2
	)`, consumer, eventID).Scan(&exists)
	return exists, err
}

func (s *PostgreSQLStore) RecordConsumerReceipt(ctx context.Context, receipt ConsumerReceipt) error {
	if strings.TrimSpace(receipt.ConsumerName) == "" || strings.TrimSpace(receipt.EventID) == "" {
		return errors.New("consumer receipt requires consumer name and event ID")
	}
	if receipt.DeliveredAt.IsZero() {
		receipt.DeliveredAt = time.Now().UTC()
	}
	_, err := s.db(ctx).Exec(ctx, `INSERT INTO outbox_consumer_receipts
		(consumer_name, event_id, attempt, delivered_at)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (consumer_name, event_id) DO NOTHING`,
		receipt.ConsumerName, receipt.EventID, receipt.Attempt, receipt.DeliveredAt)
	return err
}

func (s *PostgreSQLStore) StartAttempt(ctx context.Context, attempt DeliveryAttempt) (*DeliveryAttempt, error) {
	if strings.TrimSpace(attempt.EventID) == "" || strings.TrimSpace(attempt.ConsumerName) == "" || strings.TrimSpace(attempt.WorkerID) == "" {
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
	_, err := s.db(ctx).Exec(ctx, `INSERT INTO platform_outbox_attempts
		(id, event_id, consumer_name, worker_id, lease_generation, attempt, status, error_message, started_at, finished_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, NULLIF($8, ''), $9, $10)`,
		attempt.ID, attempt.EventID, attempt.ConsumerName, attempt.WorkerID, attempt.LeaseGeneration,
		attempt.Attempt, attempt.Status, attempt.Error, attempt.StartedAt, attempt.FinishedAt)
	if err != nil {
		return nil, err
	}
	return &attempt, nil
}

func (s *PostgreSQLStore) FinishAttempt(ctx context.Context, attempt DeliveryAttempt) error {
	if strings.TrimSpace(attempt.ID) == "" || attempt.Status == "" || attempt.Status == "running" {
		return errors.New("delivery attempt requires ID and terminal status")
	}
	tag, err := s.db(ctx).Exec(ctx, `UPDATE platform_outbox_attempts
		SET status = $2, error_message = NULLIF($3, ''), finished_at = NOW()
		WHERE id = $1`, attempt.ID, attempt.Status, attempt.Error)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrEventNotFound
	}
	return nil
}

func (s *PostgreSQLStore) ListAttempts(ctx context.Context, eventID string, page PageRequest) ([]DeliveryAttempt, int64, error) {
	page = normalizePageRequest(page, 100, 500)
	var total int64
	if err := s.db(ctx).QueryRow(ctx, `SELECT COUNT(*) FROM platform_outbox_attempts
		WHERE ($1 = '' OR event_id = $1)`, eventID).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db(ctx).Query(ctx, `SELECT id, event_id, consumer_name, worker_id, lease_generation,
		attempt, status, COALESCE(error_message, ''), started_at, finished_at
		FROM platform_outbox_attempts
		WHERE ($1 = '' OR event_id = $1)
		ORDER BY started_at DESC, id DESC LIMIT $2 OFFSET $3`, eventID, page.PageSize, page.offset())
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]DeliveryAttempt, 0, page.PageSize)
	for rows.Next() {
		var attempt DeliveryAttempt
		if err := rows.Scan(&attempt.ID, &attempt.EventID, &attempt.ConsumerName, &attempt.WorkerID,
			&attempt.LeaseGeneration, &attempt.Attempt, &attempt.Status, &attempt.Error,
			&attempt.StartedAt, &attempt.FinishedAt); err != nil {
			return nil, 0, err
		}
		items = append(items, attempt)
	}
	return items, total, rows.Err()
}

func (s *PostgreSQLStore) RecordCommandAudit(ctx context.Context, audit CommandAudit) error {
	if audit.ID == "" {
		audit.ID = fmt.Sprintf("%d", time.Now().UTC().UnixNano())
	}
	if audit.CreatedAt.IsZero() {
		audit.CreatedAt = time.Now().UTC()
	}
	if audit.Details == nil {
		audit.Details = json.RawMessage(`{}`)
	}
	_, err := s.db(ctx).Exec(ctx, `INSERT INTO platform_command_audits
		(id, command_id, command_code, actor_id, actor_type, resource_type, resource_id,
		 operation_code, permission_code, request_id, trace_id, event_id, details, created_at)
		VALUES ($1, $2, $3, NULLIF($4, ''), NULLIF($5, ''), NULLIF($6, ''), NULLIF($7, ''),
			NULLIF($8, ''), NULLIF($9, ''), NULLIF($10, ''), NULLIF($11, ''), NULLIF($12, ''), $13, $14)`,
		audit.ID, audit.CommandID, audit.CommandCode, audit.ActorID, audit.ActorType,
		audit.ResourceType, audit.ResourceID, audit.OperationCode, audit.PermissionCode,
		audit.RequestID, audit.TraceID, audit.EventID, audit.Details, audit.CreatedAt)
	return err
}

func (s *PostgreSQLStore) ListCommandAudits(ctx context.Context, page PageRequest) ([]CommandAudit, int64, error) {
	page = normalizePageRequest(page, 50, 200)
	var total int64
	if err := s.db(ctx).QueryRow(ctx, `SELECT COUNT(*) FROM platform_command_audits`).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db(ctx).Query(ctx, `SELECT id, command_id, command_code,
		COALESCE(actor_id, ''), COALESCE(actor_type, ''), COALESCE(resource_type, ''), COALESCE(resource_id, ''),
		COALESCE(operation_code, ''), COALESCE(permission_code, ''), COALESCE(request_id, ''), COALESCE(trace_id, ''),
		COALESCE(event_id, ''), details, created_at
		FROM platform_command_audits
		ORDER BY created_at DESC, id DESC LIMIT $1 OFFSET $2`, page.PageSize, page.offset())
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]CommandAudit, 0, page.PageSize)
	for rows.Next() {
		var audit CommandAudit
		if err := rows.Scan(
			&audit.ID, &audit.CommandID, &audit.CommandCode, &audit.ActorID, &audit.ActorType,
			&audit.ResourceType, &audit.ResourceID, &audit.OperationCode, &audit.PermissionCode,
			&audit.RequestID, &audit.TraceID, &audit.EventID, &audit.Details, &audit.CreatedAt,
		); err != nil {
			return nil, 0, err
		}
		items = append(items, audit)
	}
	return items, total, rows.Err()
}

func (s *PostgreSQLStore) StartOperation(ctx context.Context, operation Operation) (*Operation, error) {
	now := time.Now().UTC()
	if operation.ID == "" {
		operation.ID = fmt.Sprintf("operation-%d", now.UnixNano())
	}
	if operation.Status == "" {
		operation.Status = OperationPending
	}
	if operation.Details == nil {
		operation.Details = json.RawMessage(`{}`)
	}
	operation.CreatedAt = now
	operation.UpdatedAt = now
	if operation.IdempotencyKey != "" {
		var existing Operation
		err := s.db(ctx).QueryRow(ctx, `SELECT id, kind, subject_type, subject_id, status,
			COALESCE(actor_id, ''), COALESCE(idempotency_key, ''), details, COALESCE(error_message, ''),
			created_at, updated_at
			FROM platform_operation_runs WHERE idempotency_key = $1`, operation.IdempotencyKey).Scan(
			&existing.ID, &existing.Kind, &existing.SubjectType, &existing.SubjectID, &existing.Status,
			&existing.ActorID, &existing.IdempotencyKey, &existing.Details, &existing.Error,
			&existing.CreatedAt, &existing.UpdatedAt)
		if err == nil {
			return &existing, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
	}
	_, err := s.db(ctx).Exec(ctx, `INSERT INTO platform_operation_runs
		(id, kind, subject_type, subject_id, status, actor_id, idempotency_key, details, error_message, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''), NULLIF($7, ''), $8, NULLIF($9, ''), $10, $11)`,
		operation.ID, operation.Kind, operation.SubjectType, operation.SubjectID, operation.Status,
		operation.ActorID, operation.IdempotencyKey, operation.Details, operation.Error,
		operation.CreatedAt, operation.UpdatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if operation.IdempotencyKey != "" && errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return s.StartOperation(ctx, Operation{IdempotencyKey: operation.IdempotencyKey})
		}
		return nil, err
	}
	return &operation, nil
}

func (s *PostgreSQLStore) UpdateOperation(ctx context.Context, operation Operation) error {
	operation.UpdatedAt = time.Now().UTC()
	tag, err := s.db(ctx).Exec(ctx, `UPDATE platform_operation_runs
		SET status = $2, details = $3, error_message = NULLIF($4, ''), updated_at = $5
		WHERE id = $1`, operation.ID, operation.Status, operation.Details, operation.Error, operation.UpdatedAt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrOperationNotFound
	}
	return nil
}

func (s *PostgreSQLStore) ListOperations(ctx context.Context, kind string, page PageRequest) ([]Operation, int64, error) {
	page = normalizePageRequest(page, 50, 200)
	var total int64
	if err := s.db(ctx).QueryRow(ctx, `SELECT COUNT(*) FROM platform_operation_runs
		WHERE ($1 = '' OR kind = $1)`, kind).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db(ctx).Query(ctx, `SELECT id, kind, subject_type, subject_id, status,
		COALESCE(actor_id, ''), COALESCE(idempotency_key, ''), details, COALESCE(error_message, ''), created_at, updated_at
		FROM platform_operation_runs WHERE ($1 = '' OR kind = $1)
		ORDER BY created_at DESC, id DESC LIMIT $2 OFFSET $3`, kind, page.PageSize, page.offset())
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]Operation, 0, page.PageSize)
	for rows.Next() {
		var operation Operation
		if err := rows.Scan(&operation.ID, &operation.Kind, &operation.SubjectType, &operation.SubjectID,
			&operation.Status, &operation.ActorID, &operation.IdempotencyKey, &operation.Details,
			&operation.Error, &operation.CreatedAt, &operation.UpdatedAt); err != nil {
			return nil, 0, err
		}
		items = append(items, operation)
	}
	return items, total, rows.Err()
}

func (s *PostgreSQLStore) RecoverInterruptedOperations(ctx context.Context) ([]Operation, error) {
	rows, err := s.db(ctx).Query(ctx, `UPDATE platform_operation_runs
		SET status = 'failed',
			error_message = 'interrupted by a previous process before a terminal state; inspect the snapshot and retry explicitly',
			updated_at = NOW()
		WHERE status IN ('pending', 'running', 'compensating')
		RETURNING id, kind, subject_type, subject_id, status,
			COALESCE(actor_id, ''), COALESCE(idempotency_key, ''), details, COALESCE(error_message, ''), created_at, updated_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]Operation, 0)
	for rows.Next() {
		var operation Operation
		if err := rows.Scan(&operation.ID, &operation.Kind, &operation.SubjectType, &operation.SubjectID,
			&operation.Status, &operation.ActorID, &operation.IdempotencyKey, &operation.Details,
			&operation.Error, &operation.CreatedAt, &operation.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, operation)
	}
	return items, rows.Err()
}

func (s *PostgreSQLStore) RecordCompatibility(ctx context.Context, usage CompatibilityUsage) error {
	usage.Key = strings.TrimSpace(usage.Key)
	usage.Kind = strings.TrimSpace(usage.Kind)
	if usage.Key == "" || usage.Kind == "" {
		return errors.New("compatibility key and kind are required")
	}
	if usage.Detail == nil {
		usage.Detail = json.RawMessage(`{}`)
	}
	_, err := s.db(ctx).Exec(ctx, `INSERT INTO platform_compatibility_usage
		(usage_key, usage_kind, detail, first_seen, last_seen, usage_count)
		VALUES ($1, $2, $3, NOW(), NOW(), 1)
		ON CONFLICT (usage_key) DO UPDATE SET usage_kind = EXCLUDED.usage_kind,
			detail = EXCLUDED.detail, last_seen = NOW(), usage_count = platform_compatibility_usage.usage_count + 1`,
		usage.Key, usage.Kind, usage.Detail)
	return err
}

func (s *PostgreSQLStore) ListCompatibility(ctx context.Context, page PageRequest) ([]CompatibilityUsage, int64, error) {
	page = normalizePageRequest(page, 50, 200)
	var total int64
	if err := s.db(ctx).QueryRow(ctx, `SELECT COUNT(*) FROM platform_compatibility_usage`).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db(ctx).Query(ctx, `SELECT usage_key, usage_kind, detail, first_seen, last_seen, usage_count
		FROM platform_compatibility_usage ORDER BY last_seen DESC, usage_key ASC LIMIT $1 OFFSET $2`, page.PageSize, page.offset())
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]CompatibilityUsage, 0, page.PageSize)
	for rows.Next() {
		var usage CompatibilityUsage
		if err := rows.Scan(&usage.Key, &usage.Kind, &usage.Detail, &usage.FirstSeen, &usage.LastSeen, &usage.Count); err != nil {
			return nil, 0, err
		}
		items = append(items, usage)
	}
	return items, total, rows.Err()
}

func (s *PostgreSQLStore) PreviewRetention(ctx context.Context, target string, before time.Time) (RetentionPreview, error) {
	preview := RetentionPreview{Target: target, Before: before.UTC(), CanDelete: false}
	query, ok := retentionPreviewQuery(target)
	if !ok {
		return preview, fmt.Errorf("unsupported retention target %q", target)
	}
	err := s.db(ctx).QueryRow(ctx, query, preview.Before).Scan(&preview.EligibleRows)
	return preview, err
}

func (s *PostgreSQLStore) StartRetentionRun(ctx context.Context, run RetentionRun) (*RetentionRun, error) {
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
	_, err := s.db(ctx).Exec(ctx, `INSERT INTO platform_retention_runs
		(id, target, before_at, eligible_rows, mode, status, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		run.ID, run.Target, run.Before.UTC(), run.EligibleRows, run.Mode, run.Status, run.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &run, nil
}

func (s *PostgreSQLStore) ListRetentionRuns(ctx context.Context, page PageRequest) ([]RetentionRun, int64, error) {
	page = normalizePageRequest(page, 50, 200)
	var total int64
	if err := s.db(ctx).QueryRow(ctx, `SELECT COUNT(*) FROM platform_retention_runs`).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db(ctx).Query(ctx, `SELECT id, target, before_at, eligible_rows, mode, status, created_at
		FROM platform_retention_runs ORDER BY created_at DESC, id DESC LIMIT $1 OFFSET $2`, page.PageSize, page.offset())
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]RetentionRun, 0, page.PageSize)
	for rows.Next() {
		var item RetentionRun
		if err := rows.Scan(&item.ID, &item.Target, &item.Before, &item.EligibleRows, &item.Mode, &item.Status, &item.CreatedAt); err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}

func retentionPreviewQuery(target string) (string, bool) {
	queries := map[string]string{
		"outbox": `SELECT COUNT(*) FROM platform_outbox
			WHERE status IN ('published', 'dead') AND updated_at < $1`,
		"authorization-audits":       `SELECT COUNT(*) FROM authorization_audits WHERE created_at < $1`,
		"webhook-deliveries":         `SELECT COUNT(*) FROM webhook_deliveries WHERE created_at < $1`,
		"content-revisions":          `SELECT COUNT(*) FROM content_revisions WHERE created_at < $1`,
		"content-moderation-actions": `SELECT COUNT(*) FROM content_moderation_actions WHERE created_at < $1`,
		"plugin-logs":                `SELECT COUNT(*) FROM plugin_logs WHERE created_at < $1`,
	}
	query, ok := queries[target]
	return query, ok
}
