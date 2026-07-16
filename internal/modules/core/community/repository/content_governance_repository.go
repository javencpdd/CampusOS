package repository

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ContentGovernanceRepository keeps immutable evidence for content revisions
// and moderation decisions. ThreadRepository remains the owner of the current
// thread state; this repository is an append-only history adapter.
type ContentGovernanceRepository interface {
	CreateRevision(context.Context, *domain.ContentRevision) error
	CreateModerationCase(context.Context, *domain.ModerationCase) error
	ResolveModerationCase(context.Context, string, string, string, time.Time) error
	CreateModerationAction(context.Context, *domain.ModerationAction) error
	LatestOpenCase(context.Context, string) (*domain.ModerationCase, error)
	ListModerationActions(context.Context, string) ([]*domain.ModerationAction, error)
}

type PgContentGovernanceRepository struct{ pool *pgxpool.Pool }

func NewPgContentGovernanceRepository(pool *pgxpool.Pool) *PgContentGovernanceRepository {
	return &PgContentGovernanceRepository{pool: pool}
}

func (r *PgContentGovernanceRepository) CreateRevision(ctx context.Context, value *domain.ContentRevision) error {
	_, err := r.pool.Exec(ctx, `INSERT INTO content_revisions
		(id, thread_id, version, title, content, content_format, tags, action, reason, created_by, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NULLIF($10, '')::bigint, $11)`,
		value.ID, value.ThreadID, value.Version, value.Title, value.Content, value.ContentFormat, value.Tags,
		value.Action, value.Reason, value.CreatedBy, value.CreatedAt)
	return err
}

func (r *PgContentGovernanceRepository) CreateModerationCase(ctx context.Context, value *domain.ModerationCase) error {
	_, err := r.pool.Exec(ctx, `INSERT INTO content_moderation_cases
		(id, thread_id, status, reason, opened_by, resolved_by, opened_at, resolved_at)
		VALUES ($1, $2, $3, $4, NULLIF($5, '')::bigint, NULLIF($6, '')::bigint, $7, $8)`,
		value.ID, value.ThreadID, value.Status, value.Reason, value.OpenedBy, value.ResolvedBy, value.OpenedAt, value.ResolvedAt)
	return err
}

func (r *PgContentGovernanceRepository) ResolveModerationCase(ctx context.Context, id, status, resolvedBy string, resolvedAt time.Time) error {
	_, err := r.pool.Exec(ctx, `UPDATE content_moderation_cases
		SET status=$1, resolved_by=NULLIF($2, '')::bigint, resolved_at=$3
		WHERE id=$4 AND resolved_at IS NULL`, status, resolvedBy, resolvedAt, id)
	return err
}

func (r *PgContentGovernanceRepository) CreateModerationAction(ctx context.Context, value *domain.ModerationAction) error {
	_, err := r.pool.Exec(ctx, `INSERT INTO content_moderation_actions
		(id, case_id, thread_id, action, reason, actor_id, before_state, after_state, created_at)
		VALUES ($1, NULLIF($2, '')::bigint, $3, $4, $5, NULLIF($6, '')::bigint, $7, $8, $9)`,
		value.ID, value.CaseID, value.ThreadID, value.Action, value.Reason, value.ActorID,
		value.BeforeState, value.AfterState, value.CreatedAt)
	return err
}

func (r *PgContentGovernanceRepository) LatestOpenCase(ctx context.Context, threadID string) (*domain.ModerationCase, error) {
	value := &domain.ModerationCase{}
	err := r.pool.QueryRow(ctx, `SELECT id, thread_id, status, reason, COALESCE(opened_by::text, ''),
		COALESCE(resolved_by::text, ''), opened_at, resolved_at
		FROM content_moderation_cases
		WHERE thread_id=$1 AND resolved_at IS NULL
		ORDER BY opened_at DESC LIMIT 1`, threadID).Scan(
		&value.ID, &value.ThreadID, &value.Status, &value.Reason, &value.OpenedBy,
		&value.ResolvedBy, &value.OpenedAt, &value.ResolvedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return value, nil
}

func (r *PgContentGovernanceRepository) ListModerationActions(ctx context.Context, threadID string) ([]*domain.ModerationAction, error) {
	rows, err := r.pool.Query(ctx, `SELECT id, COALESCE(case_id::text, ''), thread_id, action, reason,
		COALESCE(actor_id::text, ''), before_state, after_state, created_at
		FROM content_moderation_actions WHERE thread_id=$1 ORDER BY created_at DESC`, threadID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []*domain.ModerationAction{}
	for rows.Next() {
		item := &domain.ModerationAction{}
		if err := rows.Scan(&item.ID, &item.CaseID, &item.ThreadID, &item.Action, &item.Reason,
			&item.ActorID, &item.BeforeState, &item.AfterState, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

type MemoryContentGovernanceRepository struct {
	mu        sync.RWMutex
	revisions map[string][]*domain.ContentRevision
	cases     map[string][]*domain.ModerationCase
	actions   map[string][]*domain.ModerationAction
}

func NewMemoryContentGovernanceRepository() *MemoryContentGovernanceRepository {
	return &MemoryContentGovernanceRepository{
		revisions: make(map[string][]*domain.ContentRevision),
		cases:     make(map[string][]*domain.ModerationCase),
		actions:   make(map[string][]*domain.ModerationAction),
	}
}

func (r *MemoryContentGovernanceRepository) CreateRevision(_ context.Context, value *domain.ContentRevision) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	copy := *value
	copy.Tags = append([]string(nil), value.Tags...)
	r.revisions[value.ThreadID] = append(r.revisions[value.ThreadID], &copy)
	return nil
}

func (r *MemoryContentGovernanceRepository) CreateModerationCase(_ context.Context, value *domain.ModerationCase) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	copy := *value
	r.cases[value.ThreadID] = append(r.cases[value.ThreadID], &copy)
	return nil
}

func (r *MemoryContentGovernanceRepository) ResolveModerationCase(_ context.Context, id, status, resolvedBy string, resolvedAt time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, items := range r.cases {
		for _, item := range items {
			if item.ID != id || item.ResolvedAt != nil {
				continue
			}
			item.Status = status
			item.ResolvedBy = resolvedBy
			value := resolvedAt
			item.ResolvedAt = &value
			return nil
		}
	}
	return nil
}

func (r *MemoryContentGovernanceRepository) CreateModerationAction(_ context.Context, value *domain.ModerationAction) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	copy := *value
	r.actions[value.ThreadID] = append(r.actions[value.ThreadID], &copy)
	return nil
}

func (r *MemoryContentGovernanceRepository) LatestOpenCase(_ context.Context, threadID string) (*domain.ModerationCase, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	items := r.cases[threadID]
	for index := len(items) - 1; index >= 0; index-- {
		if items[index].ResolvedAt == nil {
			copy := *items[index]
			return &copy, nil
		}
	}
	return nil, nil
}

func (r *MemoryContentGovernanceRepository) ListModerationActions(_ context.Context, threadID string) ([]*domain.ModerationAction, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	items := make([]*domain.ModerationAction, 0, len(r.actions[threadID]))
	for _, value := range r.actions[threadID] {
		copy := *value
		items = append(items, &copy)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.After(items[j].CreatedAt) })
	return items, nil
}
