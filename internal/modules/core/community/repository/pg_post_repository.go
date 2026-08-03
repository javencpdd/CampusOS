package repository

import (
	"context"
	"errors"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PgPostRepository struct {
	pool *pgxpool.Pool
}

func NewPgPostRepository(pool *pgxpool.Pool) *PgPostRepository {
	return &PgPostRepository{pool: pool}
}

func (r *PgPostRepository) db(ctx context.Context) transaction.Executor {
	return transaction.ExecutorFor(ctx, r.pool)
}

func (r *PgPostRepository) Create(ctx context.Context, post *domain.Post) error {
	query := `WITH next_floor AS (
			SELECT COALESCE(MAX(floor_number), 0) + 1 AS floor_number
			FROM posts
			WHERE thread_id = $2
		)
		INSERT INTO posts (id, thread_id, author_id, author_name, parent_id, parent_floor_number, content, content_format, status, like_count, floor_number, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10,
			CASE WHEN $11 > 0 THEN $11 ELSE (SELECT floor_number FROM next_floor) END,
			$12, $13)
		RETURNING floor_number`
	err := r.db(ctx).QueryRow(ctx, query,
		post.ID, post.ThreadID, post.AuthorID, post.AuthorName,
		post.ParentID, post.ParentFloorNumber, post.Content, "markdown", post.Status,
		post.LikeCount, post.FloorNumber, post.CreatedAt, post.UpdatedAt).Scan(&post.FloorNumber)
	return err
}

func (r *PgPostRepository) GetByID(ctx context.Context, id string) (*domain.Post, error) {
	query := `SELECT id, thread_id, author_id, author_name, parent_id, parent_floor_number, content, status, like_count, floor_number, created_at, updated_at
		FROM posts WHERE id = $1 AND deleted_at IS NULL`
	post := &domain.Post{}
	err := r.db(ctx).QueryRow(ctx, query, id).Scan(
		&post.ID, &post.ThreadID, &post.AuthorID, &post.AuthorName,
		&post.ParentID, &post.ParentFloorNumber, &post.Content, &post.Status,
		&post.LikeCount, &post.FloorNumber, &post.CreatedAt, &post.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrPostNotFound
		}
		return nil, err
	}
	return post, nil
}

func (r *PgPostRepository) ListByThread(ctx context.Context, threadID string, page, pageSize int) ([]*domain.Post, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	// count
	var total int64
	err := r.db(ctx).QueryRow(ctx, "SELECT COUNT(*) FROM posts WHERE thread_id = $1 AND deleted_at IS NULL", threadID).Scan(&total)
	if err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * pageSize
	query := `SELECT id, thread_id, author_id, author_name, parent_id, parent_floor_number, content, status, like_count, floor_number, created_at, updated_at
		FROM posts WHERE thread_id = $1 AND deleted_at IS NULL ORDER BY floor_number ASC LIMIT $2 OFFSET $3`
	rows, err := r.db(ctx).Query(ctx, query, threadID, pageSize, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var posts []*domain.Post
	for rows.Next() {
		post := &domain.Post{}
		if err := rows.Scan(
			&post.ID, &post.ThreadID, &post.AuthorID, &post.AuthorName,
			&post.ParentID, &post.ParentFloorNumber, &post.Content, &post.Status,
			&post.LikeCount, &post.FloorNumber, &post.CreatedAt, &post.UpdatedAt); err != nil {
			return nil, 0, err
		}
		posts = append(posts, post)
	}
	return posts, total, nil
}

func (r *PgPostRepository) Update(ctx context.Context, post *domain.Post) error {
	query := `UPDATE posts SET content=$1, updated_at=$2 WHERE id = $3 AND deleted_at IS NULL`
	tag, err := r.db(ctx).Exec(ctx, query, post.Content, time.Now().UTC(), post.ID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrPostNotFound
	}
	return nil
}

func (r *PgPostRepository) Delete(ctx context.Context, id string) error {
	query := `UPDATE posts SET deleted_at = $1 WHERE id = $2 AND deleted_at IS NULL`
	tag, err := r.db(ctx).Exec(ctx, query, time.Now().UTC(), id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrPostNotFound
	}
	return nil
}
