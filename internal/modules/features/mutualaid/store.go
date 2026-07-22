package mutualaid

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Store owns the structured mutual-aid attributes. Community keeps ownership
// of the base Thread, moderation state, revisions and replies.
type Store interface {
	Create(context.Context, *Detail) error
	Get(context.Context, string) (*Detail, error)
	GetMany(context.Context, []string) (map[string]*Detail, error)
	Update(context.Context, *Detail, int64) error
}

type PgStore struct {
	pool *pgxpool.Pool
}

func NewPgStore(pool *pgxpool.Pool) *PgStore {
	return &PgStore{pool: pool}
}

func (s *PgStore) db(ctx context.Context) transaction.Executor {
	return transaction.ExecutorFor(ctx, s.pool)
}

func (s *PgStore) Create(ctx context.Context, detail *Detail) error {
	_, err := s.db(ctx).Exec(ctx, `INSERT INTO mutual_aid_details (
		thread_id, aid_type, aid_status, deadline, location_scope, contact_mode,
		version, created_by, created_at, updated_at
	) VALUES (
		$1, $2, $3, $4, $5, $6, $7, $8, $9, $10
	)`,
		detail.ThreadID, detail.AidType, detail.AidStatus, detail.Deadline,
		detail.LocationScope, detail.ContactMode, detail.Version, detail.CreatedBy,
		detail.CreatedAt, detail.UpdatedAt)
	return err
}

func (s *PgStore) Get(ctx context.Context, threadID string) (*Detail, error) {
	detail := &Detail{}
	err := s.db(ctx).QueryRow(ctx, `SELECT
		thread_id, aid_type, aid_status, deadline, location_scope, contact_mode,
		version, created_by, created_at, updated_at
		FROM mutual_aid_details
		WHERE thread_id=$1`, threadID).Scan(
		&detail.ThreadID, &detail.AidType, &detail.AidStatus, &detail.Deadline,
		&detail.LocationScope, &detail.ContactMode, &detail.Version,
		&detail.CreatedBy, &detail.CreatedAt, &detail.UpdatedAt)
	if err == pgx.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return detail, nil
}

func (s *PgStore) GetMany(ctx context.Context, threadIDs []string) (map[string]*Detail, error) {
	result := make(map[string]*Detail, len(threadIDs))
	if len(threadIDs) == 0 {
		return result, nil
	}
	rows, err := s.db(ctx).Query(ctx, `SELECT
		thread_id, aid_type, aid_status, deadline, location_scope, contact_mode,
		version, created_by, created_at, updated_at
		FROM mutual_aid_details
		WHERE thread_id = ANY($1)`, threadIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		detail := &Detail{}
		if err := rows.Scan(
			&detail.ThreadID, &detail.AidType, &detail.AidStatus, &detail.Deadline,
			&detail.LocationScope, &detail.ContactMode, &detail.Version,
			&detail.CreatedBy, &detail.CreatedAt, &detail.UpdatedAt,
		); err != nil {
			return nil, err
		}
		result[detail.ThreadID] = detail
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *PgStore) Update(ctx context.Context, detail *Detail, expectedVersion int64) error {
	row := s.db(ctx).QueryRow(ctx, `UPDATE mutual_aid_details
		SET aid_type=$1,
			aid_status=$2,
			deadline=$3,
			location_scope=$4,
			contact_mode=$5,
			version=version+1,
			updated_at=$6
		WHERE thread_id=$7 AND version=$8
		RETURNING version, updated_at`,
		detail.AidType, detail.AidStatus, detail.Deadline, detail.LocationScope,
		detail.ContactMode, detail.UpdatedAt, detail.ThreadID, expectedVersion)
	if err := row.Scan(&detail.Version, &detail.UpdatedAt); err == pgx.ErrNoRows {
		return ErrVersionConflict
	} else if err != nil {
		return err
	}
	return nil
}

type MemoryStore struct {
	mu      sync.RWMutex
	details map[string]*Detail
}

type memoryStoreSnapshot struct {
	Details map[string]*Detail `json:"details"`
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{details: map[string]*Detail{}}
}

// Snapshot and Restore make failed reliable commands rollback both the base
// Community Thread and this feature-owned detail in the memory profile.
func (s *MemoryStore) Snapshot() any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	payload, err := json.Marshal(memoryStoreSnapshot{Details: s.details})
	if err != nil {
		return []byte(nil)
	}
	return append([]byte(nil), payload...)
}

func (s *MemoryStore) Restore(value any) {
	payload, ok := value.([]byte)
	if !ok || len(payload) == 0 {
		return
	}
	snapshot := memoryStoreSnapshot{}
	if err := json.Unmarshal(payload, &snapshot); err != nil {
		return
	}
	if snapshot.Details == nil {
		snapshot.Details = map[string]*Detail{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.details = snapshot.Details
}

func (s *MemoryStore) Create(_ context.Context, detail *Detail) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.details[detail.ThreadID] = cloneDetail(detail)
	return nil
}

func (s *MemoryStore) Get(_ context.Context, threadID string) (*Detail, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	detail, ok := s.details[threadID]
	if !ok {
		return nil, ErrNotFound
	}
	return cloneDetail(detail), nil
}

func (s *MemoryStore) GetMany(_ context.Context, threadIDs []string) (map[string]*Detail, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make(map[string]*Detail, len(threadIDs))
	for _, threadID := range threadIDs {
		if detail, ok := s.details[threadID]; ok {
			result[threadID] = cloneDetail(detail)
		}
	}
	return result, nil
}

func (s *MemoryStore) Update(_ context.Context, detail *Detail, expectedVersion int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.details[detail.ThreadID]
	if !ok {
		return ErrNotFound
	}
	if current.Version != expectedVersion {
		return ErrVersionConflict
	}
	next := cloneDetail(detail)
	next.Version = current.Version + 1
	if next.UpdatedAt.IsZero() {
		next.UpdatedAt = time.Now().UTC()
	}
	s.details[next.ThreadID] = next
	detail.Version = next.Version
	detail.UpdatedAt = next.UpdatedAt
	return nil
}

func cloneDetail(detail *Detail) *Detail {
	if detail == nil {
		return nil
	}
	clone := *detail
	if detail.Deadline != nil {
		deadline := detail.Deadline.UTC()
		clone.Deadline = &deadline
	}
	return &clone
}
