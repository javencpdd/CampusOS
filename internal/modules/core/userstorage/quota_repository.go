package storage

import (
	"context"
	"errors"
	"math"
	"reflect"
	"sort"
	"sync"
	"time"

	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/jackc/pgx/v5/pgxpool"
)

type QuotaRecord struct {
	UserID           string    `json:"user_id"`
	QuotaBytes       int64     `json:"quota_bytes"`
	UpdatedBy        string    `json:"updated_by,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
	UpdatedActorKind string    `json:"-"`
	UpdatedActorID   string    `json:"-"`
	FactsVersion     int64     `json:"-"`
}

type QuotaRepository interface {
	List(context.Context) ([]QuotaRecord, error)
	Upsert(context.Context, QuotaRecord) (QuotaRecord, error)
}

type MemoryQuotaRepository struct {
	mu           sync.RWMutex
	items        map[string]QuotaRecord
	factsCounter int64
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

func (r *MemoryQuotaRepository) Upsert(ctx context.Context, item QuotaRecord) (QuotaRecord, error) {
	if err := ctx.Err(); err != nil {
		return QuotaRecord{}, err
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	r.mu.Lock()
	defer r.mu.Unlock()
	normalizeQuotaActor(&item)
	current, exists := r.items[item.UserID]
	if current, ok := r.items[item.UserID]; ok {
		item.CreatedAt = current.CreatedAt
	}
	if item.CreatedAt.IsZero() {
		item.CreatedAt = now
	}
	item.UpdatedAt = now
	if exists && sameQuotaMetadata(current, item) {
		item.FactsVersion = current.FactsVersion
	} else {
		if r.factsCounter == math.MaxInt64 {
			return QuotaRecord{}, ErrQuotaFactsUnavailable
		}
		r.factsCounter++
		item.FactsVersion = r.factsCounter
	}
	r.items[item.UserID] = item
	return item, nil
}

type PgQuotaRepository struct{ pool *pgxpool.Pool }

func NewPgQuotaRepository(pool *pgxpool.Pool) *PgQuotaRepository {
	return &PgQuotaRepository{pool: pool}
}

func (r *PgQuotaRepository) List(ctx context.Context) ([]QuotaRecord, error) {
	if err := r.CheckQuotaFacts(ctx); err != nil {
		return nil, err
	}
	rows, err := transaction.ExecutorFor(ctx, r.pool).Query(ctx, `SELECT `+quotaRecordColumns+` FROM user_storage_quotas ORDER BY user_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]QuotaRecord, 0)
	for rows.Next() {
		var item QuotaRecord
		if err := rows.Scan(quotaRecordTargets(&item)...); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PgQuotaRepository) Upsert(ctx context.Context, item QuotaRecord) (QuotaRecord, error) {
	normalizeQuotaActor(&item)
	err := transaction.NewPostgreSQL(r.pool).Within(ctx, func(tx context.Context) error {
		if err := r.CheckQuotaFacts(tx); err != nil {
			return err
		}
		db := transaction.ExecutorFor(tx, r.pool)
		if _, err := db.Exec(tx, objectQuotaAdmissionSQL, item.UserID); err != nil {
			return err
		}
		if err := db.QueryRow(tx, `INSERT INTO user_storage_quotas(user_id,quota_bytes,updated_by,created_at,updated_at,updated_actor_kind,updated_actor_id)
 VALUES($1,$2,NULLIF($3,'')::bigint,NOW(),NOW(),$4,$5) ON CONFLICT(user_id) DO UPDATE SET quota_bytes=EXCLUDED.quota_bytes,updated_by=EXCLUDED.updated_by,updated_at=NOW(),updated_actor_kind=EXCLUDED.updated_actor_kind,updated_actor_id=EXCLUDED.updated_actor_id RETURNING `+quotaRecordColumns, item.UserID, item.QuotaBytes, item.UpdatedBy, item.UpdatedActorKind, item.UpdatedActorID).Scan(quotaRecordTargets(&item)...); err != nil {
			return err
		}
		return r.CheckQuotaFacts(tx)
	})
	return item, err
}

var ErrQuotaFactsUnavailable = errors.New("current storage quota facts unavailable")

const quotaRecordColumns = "user_id::text,quota_bytes,COALESCE(updated_by::text,''),created_at,updated_at,updated_actor_kind,updated_actor_id,facts_version"

func quotaRecordTargets(item *QuotaRecord) []any {
	return []any{&item.UserID, &item.QuotaBytes, &item.UpdatedBy, &item.CreatedAt, &item.UpdatedAt, &item.UpdatedActorKind, &item.UpdatedActorID, &item.FactsVersion}
}
func normalizeQuotaActor(item *QuotaRecord) {
	if item.UpdatedActorKind == "" {
		item.UpdatedActorKind, item.UpdatedActorID = "legacy_unknown", item.UpdatedBy
		if item.UpdatedBy != "" {
			item.UpdatedActorKind = "user"
		}
	}
}
func sameQuotaMetadata(a, b QuotaRecord) bool {
	a.FactsVersion, b.FactsVersion = 0, 0
	return reflect.DeepEqual(a, b)
}
func (r *MemoryQuotaRepository) Snapshot() any {
	r.mu.RLock()
	defer r.mu.RUnlock()
	items := map[string]QuotaRecord{}
	for k, v := range r.items {
		items[k] = v
	}
	return items
}
func (r *MemoryQuotaRepository) Restore(value any) {
	items, ok := value.(map[string]QuotaRecord)
	if !ok {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.items = map[string]QuotaRecord{}
	for k, v := range items {
		r.items[k] = v
	}
	// Deliberately retain allocation highwater after rollback.
}
