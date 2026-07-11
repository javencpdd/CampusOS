package moderation

import (
	"context"
	"encoding/json"
	"strconv"
	"sync"
	"time"

	"github.com/campusos/CampusOS/pkg/idgen"
	"github.com/jackc/pgx/v5/pgxpool"
)

type AuditRecord struct {
	TraceID    string
	ActorID    string
	Action     string
	Resource   string
	ResourceID string
	Before     interface{}
	After      interface{}
	Metadata   map[string]interface{}
	IPAddress  string
	CreatedAt  time.Time
}

type AuditStore interface {
	Log(ctx context.Context, record AuditRecord) error
}

type PgAuditStore struct {
	pool *pgxpool.Pool
}

func NewPgAuditStore(pool *pgxpool.Pool) *PgAuditStore {
	return &PgAuditStore{pool: pool}
}

func (s *PgAuditStore) Log(ctx context.Context, record AuditRecord) error {
	actorID, err := strconv.ParseInt(record.ActorID, 10, 64)
	if err != nil {
		return err
	}
	beforeJSON, err := json.Marshal(record.Before)
	if err != nil {
		return err
	}
	afterJSON, err := json.Marshal(record.After)
	if err != nil {
		return err
	}
	metadataJSON, err := json.Marshal(record.Metadata)
	if err != nil {
		return err
	}
	if record.CreatedAt.IsZero() {
		record.CreatedAt = time.Now().UTC()
	}
	_, err = s.pool.Exec(ctx, `INSERT INTO audit_logs
		(id, trace_id, actor_id, actor_type, action, resource, resource_id, before_data, after_data, metadata, ip_address, created_at)
		VALUES ($1, $2, $3, 'user', $4, $5, $6, $7::jsonb, $8::jsonb, $9::jsonb, $10, $11)`,
		idgen.New(), record.TraceID, actorID, record.Action, record.Resource, record.ResourceID,
		string(beforeJSON), string(afterJSON), string(metadataJSON), record.IPAddress, record.CreatedAt)
	return err
}

type MemoryAuditStore struct {
	mu      sync.RWMutex
	records []AuditRecord
}

func NewMemoryAuditStore() *MemoryAuditStore {
	return &MemoryAuditStore{}
}

func (s *MemoryAuditStore) Log(_ context.Context, record AuditRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if record.CreatedAt.IsZero() {
		record.CreatedAt = time.Now().UTC()
	}
	s.records = append(s.records, record)
	return nil
}

func (s *MemoryAuditStore) Records() []AuditRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]AuditRecord, len(s.records))
	copy(result, s.records)
	return result
}
