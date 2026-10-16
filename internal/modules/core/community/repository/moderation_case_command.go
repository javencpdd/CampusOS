package repository

import (
	"context"
	"errors"
	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/jackc/pgx/v5"
	"reflect"
	"time"
)

// Cases are command-owned history dependencies, loaded after the actual Thread
// authorization lock. Their old User fields cannot hold an Admin actor.
type ModerationCaseCommandRepository interface {
	LockLatestOpenCase(context.Context, string) (*domain.ModerationCase, error)
	LockModerationCase(context.Context, string) (*domain.ModerationCase, error)
	ResolveCaseForActor(context.Context, *domain.ModerationCase, string, string, string, time.Time) (*domain.ModerationCase, error)
}

var ErrModerationCaseChanged = errors.New("moderation case changed")

func cloneCommandCase(c *domain.ModerationCase) *domain.ModerationCase {
	if c == nil {
		return nil
	}
	v := *c
	v.OpenedAt = v.OpenedAt.UTC()
	if c.ResolvedAt != nil {
		at := c.ResolvedAt.UTC()
		v.ResolvedAt = &at
	}
	return &v
}

const caseCommandColumns = `id,thread_id,status,reason,COALESCE(opened_by::text,''),COALESCE(resolved_by::text,''),opened_at,resolved_at,opened_actor_kind,opened_actor_id,resolved_actor_kind,resolved_actor_id`

func scanCommandCase(row pgx.Row) (*domain.ModerationCase, error) {
	v := &domain.ModerationCase{}
	err := row.Scan(&v.ID, &v.ThreadID, &v.Status, &v.Reason, &v.OpenedBy, &v.ResolvedBy, &v.OpenedAt, &v.ResolvedAt, &v.OpenedActorKind, &v.OpenedActorID, &v.ResolvedActorKind, &v.ResolvedActorID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return cloneCommandCase(v), nil
}
func (r *PgContentGovernanceRepository) LockLatestOpenCase(ctx context.Context, threadID string) (*domain.ModerationCase, error) {
	if _, ok := transaction.FromContext(ctx); !ok {
		return nil, ErrThreadWriteTransactionRequired
	}
	return scanCommandCase(r.db(ctx).QueryRow(ctx, `SELECT `+caseCommandColumns+` FROM content_moderation_cases WHERE thread_id=$1 AND resolved_at IS NULL ORDER BY opened_at DESC,id DESC LIMIT 1 FOR UPDATE`, threadID))
}
func (r *PgContentGovernanceRepository) LockModerationCase(ctx context.Context, id string) (*domain.ModerationCase, error) {
	if _, ok := transaction.FromContext(ctx); !ok {
		return nil, ErrThreadWriteTransactionRequired
	}
	return scanCommandCase(r.db(ctx).QueryRow(ctx, `SELECT `+caseCommandColumns+` FROM content_moderation_cases WHERE id=$1 FOR UPDATE`, id))
}
func (r *PgContentGovernanceRepository) ResolveCaseForActor(ctx context.Context, expected *domain.ModerationCase, status, kind, id string, at time.Time) (*domain.ModerationCase, error) {
	if expected == nil || expected.ResolvedAt != nil || kind != "admin" || id == "" || at.IsZero() {
		return nil, ErrModerationCaseChanged
	}
	current, err := r.LockModerationCase(ctx, expected.ID)
	if err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(cloneCommandCase(expected), current) {
		return nil, ErrModerationCaseChanged
	}
	tag, err := r.db(ctx).Exec(ctx, `UPDATE content_moderation_cases SET status=$1,resolved_by=NULL,resolved_actor_kind=$2,resolved_actor_id=$3,resolved_at=$4 WHERE id=$5 AND thread_id=$6 AND resolved_at IS NULL`, status, kind, id, at, expected.ID, expected.ThreadID)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() != 1 {
		return nil, ErrModerationCaseChanged
	}
	return r.LockModerationCase(ctx, expected.ID)
}
func (r *MemoryContentGovernanceRepository) LockLatestOpenCase(ctx context.Context, threadID string) (*domain.ModerationCase, error) {
	if !transaction.Active(ctx) {
		return nil, ErrThreadWriteTransactionRequired
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	var latest *domain.ModerationCase
	for _, v := range r.cases[threadID] {
		if v.ResolvedAt == nil && (latest == nil || v.OpenedAt.After(latest.OpenedAt) || v.OpenedAt.Equal(latest.OpenedAt) && v.ID > latest.ID) {
			latest = v
		}
	}
	return cloneCommandCase(latest), nil
}
func (r *MemoryContentGovernanceRepository) LockModerationCase(ctx context.Context, id string) (*domain.ModerationCase, error) {
	if !transaction.Active(ctx) {
		return nil, ErrThreadWriteTransactionRequired
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, rows := range r.cases {
		for _, v := range rows {
			if v.ID == id {
				return cloneCommandCase(v), nil
			}
		}
	}
	return nil, nil
}
func (r *MemoryContentGovernanceRepository) ResolveCaseForActor(ctx context.Context, expected *domain.ModerationCase, status, kind, id string, at time.Time) (*domain.ModerationCase, error) {
	if !transaction.Active(ctx) {
		return nil, ErrThreadWriteTransactionRequired
	}
	if expected == nil || expected.ResolvedAt != nil || kind != "admin" || id == "" || at.IsZero() {
		return nil, ErrModerationCaseChanged
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, v := range r.cases[expected.ThreadID] {
		if v.ID == expected.ID {
			if !reflect.DeepEqual(cloneCommandCase(v), cloneCommandCase(expected)) {
				return nil, ErrModerationCaseChanged
			}
			v.Status, v.ResolvedBy, v.ResolvedActorKind, v.ResolvedActorID = status, "", kind, id
			timestamp := at.UTC()
			v.ResolvedAt = &timestamp
			return cloneCommandCase(v), nil
		}
	}
	return nil, ErrModerationCaseChanged
}
