package space

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PgRepository struct {
	pool *pgxpool.Pool
}

func NewPgRepository(pool *pgxpool.Pool) *PgRepository {
	return &PgRepository{pool: pool}
}

func (r *PgRepository) GetByUserID(ctx context.Context, userID string) (*Space, error) {
	query := `SELECT id, user_id, title, bio, avatar, cover_image, theme, layout,
		visibility, sync_enabled, sync_categories, sync_tags, created_at, updated_at
		FROM user_spaces WHERE user_id = $1 AND deleted_at IS NULL`

	space := &Space{}
	err := r.pool.QueryRow(ctx, query, userID).Scan(
		&space.ID, &space.UserID, &space.Title, &space.Bio, &space.Avatar,
		&space.CoverImage, &space.Theme, &space.Layout, &space.Visibility,
		&space.SyncEnabled, &space.SyncCategories, &space.SyncTags,
		&space.CreatedAt, &space.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrSpaceNotFound
		}
		return nil, err
	}
	return space, nil
}

func (r *PgRepository) Upsert(ctx context.Context, space *Space) error {
	query := `INSERT INTO user_spaces (
			id, user_id, title, bio, avatar, cover_image, theme, layout,
			visibility, sync_enabled, sync_categories, sync_tags, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8,
			$9, $10, $11, $12, $13, $14
		)
		ON CONFLICT (user_id) WHERE deleted_at IS NULL DO UPDATE SET
			title = EXCLUDED.title,
			bio = EXCLUDED.bio,
			avatar = EXCLUDED.avatar,
			cover_image = EXCLUDED.cover_image,
			theme = EXCLUDED.theme,
			layout = EXCLUDED.layout,
			visibility = EXCLUDED.visibility,
			sync_enabled = EXCLUDED.sync_enabled,
			sync_categories = EXCLUDED.sync_categories,
			sync_tags = EXCLUDED.sync_tags,
			updated_at = EXCLUDED.updated_at
		RETURNING id, created_at, updated_at`

	return r.pool.QueryRow(ctx, query,
		space.ID, space.UserID, space.Title, space.Bio, space.Avatar, space.CoverImage,
		space.Theme, space.Layout, space.Visibility, space.SyncEnabled,
		space.SyncCategories, space.SyncTags, space.CreatedAt, space.UpdatedAt,
	).Scan(&space.ID, &space.CreatedAt, &space.UpdatedAt)
}

func (r *PgRepository) UpsertContent(ctx context.Context, content *SpaceContent) error {
	query := `INSERT INTO user_space_contents (
			id, user_id, thread_id, title, excerpt, author_name, category_id, tags,
			status, thread_created_at, thread_updated_at, synced_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8,
			$9, $10, $11, $12
		)
		ON CONFLICT (thread_id) WHERE deleted_at IS NULL DO UPDATE SET
			user_id = EXCLUDED.user_id,
			title = EXCLUDED.title,
			excerpt = EXCLUDED.excerpt,
			author_name = EXCLUDED.author_name,
			category_id = EXCLUDED.category_id,
			tags = EXCLUDED.tags,
			status = EXCLUDED.status,
			thread_created_at = EXCLUDED.thread_created_at,
			thread_updated_at = EXCLUDED.thread_updated_at,
			synced_at = EXCLUDED.synced_at
		RETURNING id, synced_at`

	return r.pool.QueryRow(ctx, query,
		content.ID, content.UserID, content.ThreadID, content.Title, content.Excerpt,
		content.AuthorName, content.CategoryID, content.Tags, content.Status,
		content.ThreadCreatedAt, content.ThreadUpdatedAt, content.SyncedAt,
	).Scan(&content.ID, &content.SyncedAt)
}

func (r *PgRepository) DeleteContent(ctx context.Context, threadID string) error {
	query := `UPDATE user_space_contents SET deleted_at = NOW()
		WHERE thread_id = $1 AND deleted_at IS NULL`
	_, err := r.pool.Exec(ctx, query, threadID)
	return err
}

func (r *PgRepository) ListContentsByUserID(ctx context.Context, userID string, page, pageSize int) ([]*SpaceContent, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	offset := (page - 1) * pageSize

	var total int64
	if err := r.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM user_space_contents WHERE user_id = $1 AND deleted_at IS NULL`,
		userID,
	).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count space contents: %w", err)
	}

	rows, err := r.pool.Query(ctx, `SELECT id, user_id, thread_id, title, excerpt, author_name,
			category_id, tags, status, thread_created_at, thread_updated_at, synced_at
		FROM user_space_contents
		WHERE user_id = $1 AND deleted_at IS NULL
		ORDER BY thread_created_at DESC, synced_at DESC
		LIMIT $2 OFFSET $3`, userID, pageSize, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("query space contents: %w", err)
	}
	defer rows.Close()

	contents := make([]*SpaceContent, 0)
	for rows.Next() {
		content := &SpaceContent{}
		if err := rows.Scan(
			&content.ID, &content.UserID, &content.ThreadID, &content.Title,
			&content.Excerpt, &content.AuthorName, &content.CategoryID, &content.Tags,
			&content.Status, &content.ThreadCreatedAt, &content.ThreadUpdatedAt,
			&content.SyncedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("scan space content: %w", err)
		}
		contents = append(contents, content)
	}
	return contents, total, nil
}
