package repository

import (
	"context"
	"encoding/json"
	"sort"
	"sync"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ThreadTypePolicyRepository is owned by Community. Feature modules only ask
// Community to create a typed thread; they never read or mutate this policy
// table directly.
type ThreadTypePolicyRepository interface {
	List(context.Context, string) ([]domain.CategoryThreadTypePolicy, error)
	Replace(context.Context, string, []domain.ThreadType) error
}

type MemoryThreadTypePolicyRepository struct {
	mu       sync.RWMutex
	policies map[string]map[domain.ThreadType]domain.CategoryThreadTypePolicy
}

func NewMemoryThreadTypePolicyRepository() *MemoryThreadTypePolicyRepository {
	return &MemoryThreadTypePolicyRepository{policies: map[string]map[domain.ThreadType]domain.CategoryThreadTypePolicy{}}
}

func (r *MemoryThreadTypePolicyRepository) List(_ context.Context, categoryID string) ([]domain.CategoryThreadTypePolicy, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	entries := r.policies[categoryID]
	result := make([]domain.CategoryThreadTypePolicy, 0, len(entries))
	for _, policy := range entries {
		result = append(result, policy)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ThreadType < result[j].ThreadType })
	return result, nil
}

func (r *MemoryThreadTypePolicyRepository) Replace(_ context.Context, categoryID string, allowed []domain.ThreadType) error {
	next := map[domain.ThreadType]domain.CategoryThreadTypePolicy{}
	now := time.Now().UTC()
	for _, value := range allowed {
		threadType := domain.NormalizeThreadType(value)
		if !domain.IsKnownThreadType(threadType) {
			continue
		}
		next[threadType] = domain.CategoryThreadTypePolicy{CategoryID: categoryID, ThreadType: threadType, Enabled: true, UpdatedAt: now}
	}
	r.mu.Lock()
	r.policies[categoryID] = next
	r.mu.Unlock()
	return nil
}

func (r *MemoryThreadTypePolicyRepository) Snapshot() any {
	r.mu.RLock()
	defer r.mu.RUnlock()
	payload, err := json.Marshal(r.policies)
	if err != nil {
		return []byte(nil)
	}
	return append([]byte(nil), payload...)
}

func (r *MemoryThreadTypePolicyRepository) Restore(value any) {
	payload, ok := value.([]byte)
	if !ok || len(payload) == 0 {
		return
	}
	policies := map[string]map[domain.ThreadType]domain.CategoryThreadTypePolicy{}
	if err := json.Unmarshal(payload, &policies); err != nil {
		return
	}
	r.mu.Lock()
	r.policies = policies
	r.mu.Unlock()
}

type PgThreadTypePolicyRepository struct{ pool *pgxpool.Pool }

func NewPgThreadTypePolicyRepository(pool *pgxpool.Pool) *PgThreadTypePolicyRepository {
	return &PgThreadTypePolicyRepository{pool: pool}
}

func (r *PgThreadTypePolicyRepository) db(ctx context.Context) transaction.Executor {
	return transaction.ExecutorFor(ctx, r.pool)
}

func (r *PgThreadTypePolicyRepository) List(ctx context.Context, categoryID string) ([]domain.CategoryThreadTypePolicy, error) {
	rows, err := r.db(ctx).Query(ctx, `SELECT category_id::text, thread_type, enabled, updated_at
		FROM category_thread_type_policies WHERE category_id = $1 ORDER BY thread_type`, categoryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]domain.CategoryThreadTypePolicy, 0)
	for rows.Next() {
		var policy domain.CategoryThreadTypePolicy
		if err := rows.Scan(&policy.CategoryID, &policy.ThreadType, &policy.Enabled, &policy.UpdatedAt); err != nil {
			return nil, err
		}
		policy.ThreadType = domain.NormalizeThreadType(policy.ThreadType)
		result = append(result, policy)
	}
	return result, rows.Err()
}

func (r *PgThreadTypePolicyRepository) Replace(ctx context.Context, categoryID string, allowed []domain.ThreadType) error {
	if _, err := r.db(ctx).Exec(ctx, `DELETE FROM category_thread_type_policies WHERE category_id = $1`, categoryID); err != nil {
		return err
	}
	seen := map[domain.ThreadType]bool{}
	for _, value := range allowed {
		threadType := domain.NormalizeThreadType(value)
		if !domain.IsKnownThreadType(threadType) || seen[threadType] {
			continue
		}
		seen[threadType] = true
		if _, err := r.db(ctx).Exec(ctx, `INSERT INTO category_thread_type_policies
			(category_id, thread_type, enabled, updated_at) VALUES ($1, $2, TRUE, NOW())`, categoryID, threadType); err != nil {
			return err
		}
	}
	return nil
}

var _ ThreadTypePolicyRepository = (*MemoryThreadTypePolicyRepository)(nil)
var _ ThreadTypePolicyRepository = (*PgThreadTypePolicyRepository)(nil)
