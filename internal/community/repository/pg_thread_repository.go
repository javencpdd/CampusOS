package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/campusos/CampusOS/internal/community/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PgThreadRepository struct {
	pool *pgxpool.Pool
}

func NewPgThreadRepository(pool *pgxpool.Pool) *PgThreadRepository {
	return &PgThreadRepository{pool: pool}
}

func (r *PgThreadRepository) Create(ctx context.Context, thread *domain.Thread) error {
	query := `INSERT INTO threads (id, title, content, content_format, author_id, author_name, category_id, status, tags, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`
	_, err := r.pool.Exec(ctx, query,
		thread.ID, thread.Title, thread.Content, "markdown",
		thread.AuthorID, thread.AuthorName, thread.CategoryID,
		thread.Status, thread.Tags, thread.CreatedAt, thread.UpdatedAt)
	return err
}

func (r *PgThreadRepository) GetByID(ctx context.Context, id string) (*domain.Thread, error) {
	query := `SELECT id, title, content, author_id, author_name, category_id, status,
		is_pinned, is_locked, view_count, reply_count, like_count, tags, created_at, updated_at
		FROM threads WHERE id = $1 AND deleted_at IS NULL`
	t := &domain.Thread{}
	err := r.pool.QueryRow(ctx, query, id).Scan(
		&t.ID, &t.Title, &t.Content, &t.AuthorID, &t.AuthorName, &t.CategoryID,
		&t.Status, &t.IsPinned, &t.IsLocked, &t.ViewCount, &t.ReplyCount, &t.LikeCount,
		&t.Tags, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrThreadNotFound
		}
		return nil, err
	}
	return t, nil
}

func (r *PgThreadRepository) Update(ctx context.Context, thread *domain.Thread) error {
	query := `UPDATE threads SET title=$1, content=$2, tags=$3, updated_at=$4
		WHERE id = $5 AND deleted_at IS NULL`
	tag, err := r.pool.Exec(ctx, query, thread.Title, thread.Content, thread.Tags, time.Now().UTC(), thread.ID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrThreadNotFound
	}
	return nil
}

func (r *PgThreadRepository) Delete(ctx context.Context, id string) error {
	query := `UPDATE threads SET deleted_at = $1 WHERE id = $2 AND deleted_at IS NULL`
	tag, err := r.pool.Exec(ctx, query, time.Now().UTC(), id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
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
	if filter.Keyword != "" {
		where = append(where, fmt.Sprintf("(title ILIKE $%d OR content ILIKE $%d)", argIdx, argIdx))
		args = append(args, "%"+filter.Keyword+"%")
		argIdx++
	}

	whereStr := strings.Join(where, " AND ")

	// count
	var total int64
	err := r.pool.QueryRow(ctx, "SELECT COUNT(*) FROM threads WHERE "+whereStr, args...).Scan(&total)
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

	query := fmt.Sprintf(`SELECT id, title, content, author_id, author_name, category_id, status,
		is_pinned, is_locked, view_count, reply_count, like_count, tags, created_at, updated_at
		FROM threads WHERE %s ORDER BY is_pinned DESC, created_at DESC LIMIT $%d OFFSET $%d`,
		whereStr, argIdx, argIdx+1)
	args = append(args, pageSize, offset)

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var threads []*domain.Thread
	for rows.Next() {
		t := &domain.Thread{}
		if err := rows.Scan(&t.ID, &t.Title, &t.Content, &t.AuthorID, &t.AuthorName, &t.CategoryID,
			&t.Status, &t.IsPinned, &t.IsLocked, &t.ViewCount, &t.ReplyCount, &t.LikeCount,
			&t.Tags, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, 0, err
		}
		threads = append(threads, t)
	}
	return threads, total, nil
}
