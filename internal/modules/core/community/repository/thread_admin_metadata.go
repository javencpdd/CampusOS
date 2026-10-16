package repository

import (
	"context"
	"sort"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
)

// AdminThreadMetadata contains no title, body, tags, reason or user profile.
// The hidden version covers all policy-relevant changes to its original row.
type AdminThreadMetadata struct {
	ID                string                   `json:"id"`
	AuthorID          string                   `json:"author_id"`
	CategoryID        string                   `json:"category_id"`
	ThreadType        domain.ThreadType        `json:"thread_type"`
	ContentFormat     string                   `json:"content_format"`
	PublicationStatus domain.PublicationStatus `json:"publication_status"`
	ModerationStatus  domain.ModerationStatus  `json:"moderation_status"`
	DeletionStatus    domain.DeletionStatus    `json:"deletion_status"`
	IsPinned          bool                     `json:"is_pinned"`
	IsLocked          bool                     `json:"is_locked"`
	CreatedAt         time.Time                `json:"created_at"`
	UpdatedAt         time.Time                `json:"updated_at"`
	FactsVersion      int64                    `json:"-"`
	PhysicalDeleted   bool                     `json:"-"`
}
type AdminThreadMetadataReader interface {
	ListAdminThreadMetadata(context.Context) ([]AdminThreadMetadata, error)
}

func adminThreadMetadata(row *domain.Thread) AdminThreadMetadata {
	return AdminThreadMetadata{ID: row.ID, AuthorID: row.AuthorID, CategoryID: row.CategoryID, ThreadType: row.ThreadType, ContentFormat: row.ContentFormat, PublicationStatus: row.PublicationStatus, ModerationStatus: row.ModerationStatus, DeletionStatus: row.DeletionStatus, IsPinned: row.IsPinned, IsLocked: row.IsLocked, CreatedAt: row.CreatedAt.UTC(), UpdatedAt: row.UpdatedAt.UTC(), FactsVersion: row.FactsVersion, PhysicalDeleted: row.DeletedAt != nil}
}
func (r *MemoryThreadRepository) ListAdminThreadMetadata(ctx context.Context) ([]AdminThreadMetadata, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]AdminThreadMetadata, 0, len(r.threads))
	for _, row := range r.threads {
		if row == nil || row.FactsVersion < 1 {
			return nil, ErrThreadPurgeFactsUnavailable
		}
		result = append(result, adminThreadMetadata(row))
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID > result[j].ID })
	return result, nil
}
func (r *PgThreadRepository) ListAdminThreadMetadata(ctx context.Context) ([]AdminThreadMetadata, error) {
	if err := r.requirePurgeFacts(ctx); err != nil {
		return nil, err
	}
	rows, err := r.db(ctx).Query(ctx, `SELECT id::text,author_id::text,category_id::text,thread_type,content_format,publication_status,moderation_status,deletion_status,is_pinned,is_locked,created_at,updated_at,facts_version,deleted_at IS NOT NULL FROM threads ORDER BY id::text COLLATE "C" DESC`)
	if err != nil {
		return nil, err
	}
	result := []AdminThreadMetadata{}
	for rows.Next() {
		var row AdminThreadMetadata
		if err := rows.Scan(&row.ID, &row.AuthorID, &row.CategoryID, &row.ThreadType, &row.ContentFormat, &row.PublicationStatus, &row.ModerationStatus, &row.DeletionStatus, &row.IsPinned, &row.IsLocked, &row.CreatedAt, &row.UpdatedAt, &row.FactsVersion, &row.PhysicalDeleted); err != nil {
			rows.Close()
			return nil, err
		}
		row.CreatedAt = row.CreatedAt.UTC()
		row.UpdatedAt = row.UpdatedAt.UTC()
		if row.FactsVersion < 1 {
			rows.Close()
			return nil, ErrThreadPurgeFactsUnavailable
		}
		result = append(result, row)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if err := r.requirePurgeFacts(ctx); err != nil {
		return nil, err
	}
	return result, nil
}

type AdminModerationMetadata struct {
	ID         string    `json:"id"`
	ThreadID   string    `json:"thread_id"`
	Action     string    `json:"action"`
	ActorKind  string    `json:"actor_kind"`
	ActorRefID string    `json:"actor_ref_id"`
	CreatedAt  time.Time `json:"created_at"`
}
type AdminModerationMetadataReader interface {
	ReadAdminModerationMetadata(context.Context, string) ([]AdminModerationMetadata, error)
}

func (r *MemoryContentGovernanceRepository) ReadAdminModerationMetadata(ctx context.Context, id string) ([]AdminModerationMetadata, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := []AdminModerationMetadata{}
	for _, row := range r.actions[id] {
		if row == nil {
			return nil, ErrThreadPurgeFactsUnavailable
		}
		result = append(result, AdminModerationMetadata{ID: row.ID, ThreadID: row.ThreadID, Action: row.Action, ActorKind: row.ActorKind, ActorRefID: row.ActorRefID, CreatedAt: row.CreatedAt.UTC()})
	}
	sortAdminModerationMetadata(result)
	return result, nil
}
func sortAdminModerationMetadata(rows []AdminModerationMetadata) {
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].CreatedAt.Equal(rows[j].CreatedAt) {
			return rows[i].ID > rows[j].ID
		}
		return rows[i].CreatedAt.After(rows[j].CreatedAt)
	})
}
func (r *PgContentGovernanceRepository) ReadAdminModerationMetadata(ctx context.Context, id string) ([]AdminModerationMetadata, error) {
	rows, err := r.db(ctx).Query(ctx, `SELECT id::text,thread_id::text,action,actor_kind,actor_ref_id,created_at FROM content_moderation_actions WHERE thread_id=$1 ORDER BY created_at DESC,id::text COLLATE "C" DESC`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []AdminModerationMetadata{}
	for rows.Next() {
		var row AdminModerationMetadata
		if err := rows.Scan(&row.ID, &row.ThreadID, &row.Action, &row.ActorKind, &row.ActorRefID, &row.CreatedAt); err != nil {
			return nil, err
		}
		row.CreatedAt = row.CreatedAt.UTC()
		result = append(result, row)
	}
	return result, rows.Err()
}
