package storage

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type QuotaRecord struct {
	UserID     string    `json:"user_id"`
	QuotaBytes int64     `json:"quota_bytes"`
	UpdatedBy  string    `json:"updated_by,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type QuotaRepository interface {
	List(context.Context) ([]QuotaRecord, error)
	Upsert(context.Context, QuotaRecord) (QuotaRecord, error)
}

type MemoryQuotaRepository struct {
	mu    sync.RWMutex
	items map[string]QuotaRecord
}

func NewMemoryQuotaRepository() *MemoryQuotaRepository {
	return &MemoryQuotaRepository{items: make(map[string]QuotaRecord)}
}

func (r *MemoryQuotaRepository) List(context.Context) ([]QuotaRecord, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	items := make([]QuotaRecord, 0, len(r.items))
	for _, item := range r.items {
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].UserID < items[j].UserID })
	return items, nil
}

func (r *MemoryQuotaRepository) Upsert(_ context.Context, item QuotaRecord) (QuotaRecord, error) {
	now := time.Now().UTC()
	r.mu.Lock()
	defer r.mu.Unlock()
	if current, ok := r.items[item.UserID]; ok {
		item.CreatedAt = current.CreatedAt
	}
	if item.CreatedAt.IsZero() {
		item.CreatedAt = now
	}
	item.UpdatedAt = now
	r.items[item.UserID] = item
	return item, nil
}

type PgQuotaRepository struct{ pool *pgxpool.Pool }

func NewPgQuotaRepository(pool *pgxpool.Pool) *PgQuotaRepository {
	return &PgQuotaRepository{pool: pool}
}

func (r *PgQuotaRepository) List(ctx context.Context) ([]QuotaRecord, error) {
	rows, err := r.pool.Query(ctx, `SELECT user_id::text, quota_bytes,
		COALESCE(updated_by::text, ''), created_at, updated_at
		FROM user_storage_quotas ORDER BY user_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]QuotaRecord, 0)
	for rows.Next() {
		var item QuotaRecord
		if err := rows.Scan(&item.UserID, &item.QuotaBytes, &item.UpdatedBy, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PgQuotaRepository) Upsert(ctx context.Context, item QuotaRecord) (QuotaRecord, error) {
	err := r.pool.QueryRow(ctx, `INSERT INTO user_storage_quotas
		(user_id, quota_bytes, updated_by, created_at, updated_at)
		VALUES ($1, $2, NULLIF($3, '')::bigint, NOW(), NOW())
		ON CONFLICT (user_id) DO UPDATE SET
			quota_bytes = EXCLUDED.quota_bytes,
			updated_by = EXCLUDED.updated_by,
			updated_at = NOW()
		RETURNING user_id::text, quota_bytes, COALESCE(updated_by::text, ''), created_at, updated_at`,
		item.UserID, item.QuotaBytes, item.UpdatedBy,
	).Scan(&item.UserID, &item.QuotaBytes, &item.UpdatedBy, &item.CreatedAt, &item.UpdatedAt)
	return item, err
}
