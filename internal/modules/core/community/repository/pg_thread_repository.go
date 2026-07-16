package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PgThreadRepository struct {
	pool *pgxpool.Pool
}

func NewPgThreadRepository(pool *pgxpool.Pool) *PgThreadRepository {
	return &PgThreadRepository{pool: pool}
}

func (r *PgThreadRepository) db(ctx context.Context) transaction.Executor {
	return transaction.ExecutorFor(ctx, r.pool)
}

func (r *PgThreadRepository) Create(ctx context.Context, thread *domain.Thread) error {
	contentFormat := thread.ContentFormat
	if contentFormat == "" {
		contentFormat = "markdown"
	}
	thread.NormalizeContentState()
	query := `INSERT INTO threads (id, title, content, content_format, author_id, author_name, category_id, status,
		publication_status, moderation_status, deletion_status, moderation_reason, moderation_by, moderation_at, current_revision,
		tags, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, NULLIF($13, '')::bigint, $14, $15, $16, $17, $18)`
	_, err := r.db(ctx).Exec(ctx, query,
		thread.ID, thread.Title, thread.Content, contentFormat,
		thread.AuthorID, thread.AuthorName, thread.CategoryID,
		thread.Status, thread.PublicationStatus, thread.ModerationStatus, thread.DeletionStatus, thread.ModerationReason,
		thread.ModerationBy, thread.ModerationAt, thread.CurrentRevision, thread.Tags, thread.CreatedAt, thread.UpdatedAt)
	return err
}

func (r *PgThreadRepository) GetByID(ctx context.Context, id string) (*domain.Thread, error) {
	t, err := scanThread(r.db(ctx).QueryRow(ctx, selectThreadSQL("id = $1"), id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrThreadNotFound
		}
		return nil, err
	}
	return t, nil
}

func (r *PgThreadRepository) IncrementViewCount(ctx context.Context, id string) (*domain.Thread, error) {
	query := `UPDATE threads
		SET view_count = view_count + 1
		WHERE id = $1 AND deleted_at IS NULL AND COALESCE(deletion_status, 'active') = 'active'
		RETURNING id, title, content, content_format, author_id, author_name, category_id, status,
			publication_status, moderation_status, deletion_status, moderation_reason, COALESCE(moderation_by::text, ''), moderation_at, current_revision,
			is_pinned, is_locked, is_highlighted, view_count, reply_count, like_count, tags, created_at, updated_at`
	t, err := scanThread(r.db(ctx).QueryRow(ctx, query, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrThreadNotFound
		}
		return nil, err
	}
	return t, nil
}

func (r *PgThreadRepository) IncrementReplyCount(ctx context.Context, id string, delta int64) error {
	query := `UPDATE threads
		SET reply_count = GREATEST(reply_count + $2, 0),
			updated_at = NOW()
		WHERE id = $1 AND deleted_at IS NULL AND COALESCE(deletion_status, 'active') = 'active'`
	tag, err := r.db(ctx).Exec(ctx, query, id, delta)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrThreadNotFound
	}
	return nil
}

func (r *PgThreadRepository) Update(ctx context.Context, thread *domain.Thread) error {
	return r.update(ctx, thread, nil)
}

func (r *PgThreadRepository) UpdateIfRevision(ctx context.Context, thread *domain.Thread, expected int) error {
	return r.update(ctx, thread, &expected)
}

func (r *PgThreadRepository) update(ctx context.Context, thread *domain.Thread, expected *int) error {
	query := `UPDATE threads
		SET title=$1,
			content=$2,
			category_id=$3,
			status=$4,
			is_pinned=$5,
			is_locked=$6,
			is_highlighted=$7,
			view_count=$8,
			reply_count=$9,
			like_count=$10,
			tags=$11,
			content_format=$12,
			publication_status=$13,
			moderation_status=$14,
			deletion_status=$15,
			moderation_reason=$16,
			moderation_by=NULLIF($17, '')::bigint,
			moderation_at=$18,
			current_revision=$19,
			updated_at=$20
		WHERE id = $21 AND deleted_at IS NULL`
	if expected != nil {
		query += " AND current_revision = $22"
	}
	contentFormat := thread.ContentFormat
	if contentFormat == "" {
		contentFormat = "markdown"
	}
	thread.NormalizeContentState()
	args := []interface{}{
		thread.Title, thread.Content, thread.CategoryID, thread.Status,
		thread.IsPinned, thread.IsLocked, thread.IsHighlighted,
		thread.ViewCount, thread.ReplyCount, thread.LikeCount, thread.Tags,
		contentFormat, thread.PublicationStatus, thread.ModerationStatus, thread.DeletionStatus, thread.ModerationReason,
		thread.ModerationBy, thread.ModerationAt, thread.CurrentRevision, time.Now().UTC(), thread.ID,
	}
	if expected != nil {
		args = append(args, *expected)
	}
	tag, err := r.db(ctx).Exec(ctx, query, args...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		if expected != nil {
			return ErrThreadRevisionConflict
		}
		return ErrThreadNotFound
	}
	return nil
}

func (r *PgThreadRepository) Delete(ctx context.Context, id string) error {
	query := `UPDATE threads
		SET deletion_status='trashed', status='archived', updated_at=$1
		WHERE id=$2 AND deleted_at IS NULL AND COALESCE(deletion_status, 'active') = 'active'`
	tag, err := r.db(ctx).Exec(ctx, query, time.Now().UTC(), id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrThreadNotFound
	}
	return nil
}

func (r *PgThreadRepository) Purge(ctx context.Context, id string) error {
	return r.purge(ctx, id, nil)
}

func (r *PgThreadRepository) PurgeIfRevision(ctx context.Context, id string, expected int) error {
	return r.purge(ctx, id, &expected)
}

func (r *PgThreadRepository) purge(ctx context.Context, id string, expected *int) error {
	query := `UPDATE threads
		SET deletion_status='purged', status='archived', deleted_at=$1, updated_at=$1
		WHERE id=$2 AND deleted_at IS NULL AND COALESCE(deletion_status, 'active') = 'trashed'`
	args := []interface{}{time.Now().UTC(), id}
	if expected != nil {
		query += " AND current_revision = $3"
		args = append(args, *expected)
	}
	tag, err := r.db(ctx).Exec(ctx, query, args...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		if expected != nil {
			return ErrThreadRevisionConflict
		}
		return ErrThreadNotFound
	}
	return nil
}

func (r *PgThreadRepository) List(ctx context.Context, filter domain.ThreadListFilter) ([]*domain.Thread, int64, error) {
	where := []string{"deleted_at IS NULL"}
	args := []interface{}{}
	argIdx := 1

	if filter.CategoryID != "" {
		where = append(where, fmt.Sprintf("category_id = $%d", argIdx))
		args = append(args, filter.CategoryID)
		argIdx++
	}
	if len(filter.CategoryIDs) > 0 {
		where = append(where, fmt.Sprintf("category_id = ANY($%d::text[])", argIdx))
		args = append(args, filter.CategoryIDs)
		argIdx++
	}
	if filter.AuthorID != "" {
		where = append(where, fmt.Sprintf("author_id = $%d", argIdx))
		args = append(args, filter.AuthorID)
		argIdx++
	}
	if filter.Status != "" {
		where = append(where, fmt.Sprintf("status = $%d", argIdx))
		args = append(args, filter.Status)
		argIdx++
	}
	if filter.ContentFormat != "" {
		where = append(where, fmt.Sprintf("content_format = $%d", argIdx))
		args = append(args, filter.ContentFormat)
		argIdx++
	}
	if filter.Tag != "" {
		where = append(where, fmt.Sprintf("tags @> ARRAY[$%d]::text[]", argIdx))
		args = append(args, filter.Tag)
		argIdx++
	}
	if len(filter.AnyTags) > 0 {
		where = append(where, fmt.Sprintf("tags && $%d::text[]", argIdx))
		args = append(args, filter.AnyTags)
		argIdx++
	}
	if filter.PublicationStatus != "" {
		where = append(where, fmt.Sprintf("publication_status = $%d", argIdx))
		args = append(args, filter.PublicationStatus)
		argIdx++
	}
	if filter.ModerationStatus != "" {
		where = append(where, fmt.Sprintf("moderation_status = $%d", argIdx))
		args = append(args, filter.ModerationStatus)
		argIdx++
	}
	if filter.DeletionStatus != "" {
		where = append(where, fmt.Sprintf("deletion_status = $%d", argIdx))
		args = append(args, filter.DeletionStatus)
		argIdx++
	} else if !filter.IncludeTrashed {
		where = append(where, "COALESCE(deletion_status, 'active') = 'active'")
	}
	if filter.Keyword != "" {
		where = append(where, fmt.Sprintf("(title ILIKE $%d OR content ILIKE $%d)", argIdx, argIdx))
		args = append(args, "%"+filter.Keyword+"%")
		argIdx++
	}

	whereStr := strings.Join(where, " AND ")

	// count
	var total int64
	err := r.db(ctx).QueryRow(ctx, "SELECT COUNT(*) FROM threads WHERE "+whereStr, args...).Scan(&total)
	if err != nil {
		return nil, 0, err
	}

	// list
	page, pageSize := filter.Page, filter.PageSize
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	offset := (page - 1) * pageSize

	query := fmt.Sprintf(`SELECT id, title, content, content_format, author_id, author_name, category_id, status,
		publication_status, moderation_status, deletion_status, moderation_reason, COALESCE(moderation_by::text, ''), moderation_at, current_revision,
		is_pinned, is_locked, is_highlighted, view_count, reply_count, like_count, tags, created_at, updated_at
		FROM threads WHERE %s ORDER BY is_pinned DESC, created_at DESC LIMIT $%d OFFSET $%d`,
		whereStr, argIdx, argIdx+1)
	args = append(args, pageSize, offset)

	rows, err := r.db(ctx).Query(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var threads []*domain.Thread
	for rows.Next() {
		t := &domain.Thread{}
		if err := rows.Scan(&t.ID, &t.Title, &t.Content, &t.ContentFormat, &t.AuthorID, &t.AuthorName, &t.CategoryID,
			&t.Status, &t.PublicationStatus, &t.ModerationStatus, &t.DeletionStatus, &t.ModerationReason, &t.ModerationBy, &t.ModerationAt, &t.CurrentRevision,
			&t.IsPinned, &t.IsLocked, &t.IsHighlighted, &t.ViewCount, &t.ReplyCount, &t.LikeCount,
			&t.Tags, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, 0, err
		}
		t.NormalizeContentState()
		threads = append(threads, t)
	}
	return threads, total, nil
}

type rowScanner interface {
	Scan(dest ...interface{}) error
}

func selectThreadSQL(where string) string {
	return `SELECT id, title, content, content_format, author_id, author_name, category_id, status,
		publication_status, moderation_status, deletion_status, moderation_reason, COALESCE(moderation_by::text, ''), moderation_at, current_revision,
		is_pinned, is_locked, is_highlighted, view_count, reply_count, like_count, tags, created_at, updated_at
		FROM threads WHERE ` + where + ` AND deleted_at IS NULL`
}

func scanThread(row rowScanner) (*domain.Thread, error) {
	t := &domain.Thread{}
	err := row.Scan(
		&t.ID, &t.Title, &t.Content, &t.ContentFormat, &t.AuthorID, &t.AuthorName, &t.CategoryID,
		&t.Status, &t.PublicationStatus, &t.ModerationStatus, &t.DeletionStatus, &t.ModerationReason, &t.ModerationBy, &t.ModerationAt, &t.CurrentRevision,
		&t.IsPinned, &t.IsLocked, &t.IsHighlighted, &t.ViewCount, &t.ReplyCount, &t.LikeCount,
		&t.Tags, &t.CreatedAt, &t.UpdatedAt,
	)
	if err == nil {
		t.NormalizeContentState()
	}
	return t, err
}
