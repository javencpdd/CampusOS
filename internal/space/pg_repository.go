package space

import (
	"context"
	"errors"

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
