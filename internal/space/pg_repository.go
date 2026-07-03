package space

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

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
		style_name, style_version, style_manifest,
		visibility, sync_enabled, sync_categories, sync_tags,
		disabled_at, disabled_by, disabled_reason, last_sync_at, last_sync_error,
		created_at, updated_at
		FROM user_spaces WHERE user_id = $1 AND deleted_at IS NULL`

	space := &Space{}
	var styleManifestJSON []byte
	err := r.pool.QueryRow(ctx, query, userID).Scan(
		&space.ID, &space.UserID, &space.Title, &space.Bio, &space.Avatar,
		&space.CoverImage, &space.Theme, &space.Layout,
		&space.StyleName, &space.StyleVersion, &styleManifestJSON,
		&space.Visibility,
		&space.SyncEnabled, &space.SyncCategories, &space.SyncTags,
		&space.DisabledAt, &space.DisabledBy, &space.DisabledReason, &space.LastSyncAt, &space.LastSyncError,
		&space.CreatedAt, &space.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrSpaceNotFound
		}
		return nil, err
	}
	if len(styleManifestJSON) > 0 && string(styleManifestJSON) != "{}" {
		var manifest StyleManifest
		if err := json.Unmarshal(styleManifestJSON, &manifest); err != nil {
			return nil, fmt.Errorf("decode style manifest: %w", err)
		}
		space.StyleManifest = &manifest
	}
	return space, nil
}

func (r *PgRepository) Upsert(ctx context.Context, space *Space) error {
	ensureDefaults(space)

	styleManifestJSON, err := json.Marshal(styleManifestForSave(space.StyleManifest))
	if err != nil {
		return err
	}

	query := `INSERT INTO user_spaces (
			id, user_id, title, bio, avatar, cover_image, theme, layout,
			style_name, style_version, style_manifest,
			visibility, sync_enabled, sync_categories, sync_tags,
			disabled_at, disabled_by, disabled_reason, last_sync_at, last_sync_error,
			created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8,
			$9, $10, $11::jsonb,
			$12, $13, $14, $15,
			$16, $17, $18, $19, $20,
			$21, $22
		)
		ON CONFLICT (user_id) WHERE deleted_at IS NULL DO UPDATE SET
			title = EXCLUDED.title,
			bio = EXCLUDED.bio,
			avatar = EXCLUDED.avatar,
			cover_image = EXCLUDED.cover_image,
			theme = EXCLUDED.theme,
			layout = EXCLUDED.layout,
			style_name = EXCLUDED.style_name,
			style_version = EXCLUDED.style_version,
			style_manifest = EXCLUDED.style_manifest,
			visibility = EXCLUDED.visibility,
			sync_enabled = EXCLUDED.sync_enabled,
			sync_categories = EXCLUDED.sync_categories,
			sync_tags = EXCLUDED.sync_tags,
			disabled_at = EXCLUDED.disabled_at,
			disabled_by = EXCLUDED.disabled_by,
			disabled_reason = EXCLUDED.disabled_reason,
			last_sync_at = EXCLUDED.last_sync_at,
			last_sync_error = EXCLUDED.last_sync_error,
			updated_at = EXCLUDED.updated_at
		RETURNING id, created_at, updated_at`

	return r.pool.QueryRow(ctx, query,
		space.ID, space.UserID, space.Title, space.Bio, space.Avatar, space.CoverImage,
		space.Theme, space.Layout, space.StyleName, space.StyleVersion, string(styleManifestJSON),
		space.Visibility, space.SyncEnabled,
		space.SyncCategories, space.SyncTags,
		space.DisabledAt, space.DisabledBy, space.DisabledReason, space.LastSyncAt, space.LastSyncError,
		space.CreatedAt, space.UpdatedAt,
	).Scan(&space.ID, &space.CreatedAt, &space.UpdatedAt)
}

func styleManifestForSave(manifest *StyleManifest) interface{} {
	if manifest == nil {
		return map[string]interface{}{}
	}
	return manifest
}

func (r *PgRepository) UpsertContent(ctx context.Context, content *SpaceContent) error {
	ensureContentDefaults(content)

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

	err := r.pool.QueryRow(ctx, query,
		content.ID, content.UserID, content.ThreadID, content.Title, content.Excerpt,
		content.AuthorName, content.CategoryID, content.Tags, content.Status,
		content.ThreadCreatedAt, content.ThreadUpdatedAt, content.SyncedAt,
	).Scan(&content.ID, &content.SyncedAt)
	if err != nil {
		return err
	}
	return r.UpdateSyncStatus(ctx, content.UserID, &content.SyncedAt, "")
}

func ensureContentDefaults(content *SpaceContent) {
	if content == nil {
		return
	}
	if content.Tags == nil {
		content.Tags = []string{}
	}
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

func (r *PgRepository) SaveStyleSnapshot(ctx context.Context, snapshot *StyleSnapshot) error {
	if snapshot.ID == "" {
		return fmt.Errorf("snapshot id is required")
	}
	if snapshot.CreatedAt.IsZero() {
		snapshot.CreatedAt = time.Now().UTC()
	}
	manifestJSON, err := json.Marshal(styleManifestForSave(snapshot.StyleManifest))
	if err != nil {
		return err
	}
	query := `INSERT INTO user_space_style_snapshots (
			id, user_id, snapshot_type, style_name, style_version, theme, layout, style_manifest, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb, $9)`
	_, err = r.pool.Exec(ctx, query,
		snapshot.ID, snapshot.UserID, snapshot.SnapshotType, snapshot.StyleName,
		snapshot.StyleVersion, snapshot.Theme, snapshot.Layout, string(manifestJSON), snapshot.CreatedAt,
	)
	return err
}

func (r *PgRepository) GetLatestStyleSnapshot(ctx context.Context, userID string) (*StyleSnapshot, error) {
	query := `SELECT id, user_id, snapshot_type, style_name, style_version, theme, layout, style_manifest, created_at
		FROM user_space_style_snapshots
		WHERE user_id = $1
		ORDER BY created_at DESC
		LIMIT 1`
	snapshot := &StyleSnapshot{}
	var styleManifestJSON []byte
	err := r.pool.QueryRow(ctx, query, userID).Scan(
		&snapshot.ID, &snapshot.UserID, &snapshot.SnapshotType, &snapshot.StyleName,
		&snapshot.StyleVersion, &snapshot.Theme, &snapshot.Layout, &styleManifestJSON, &snapshot.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrSpaceNotFound
		}
		return nil, err
	}
	if len(styleManifestJSON) > 0 && string(styleManifestJSON) != "{}" {
		var manifest StyleManifest
		if err := json.Unmarshal(styleManifestJSON, &manifest); err != nil {
			return nil, fmt.Errorf("decode style snapshot manifest: %w", err)
		}
		snapshot.StyleManifest = &manifest
	}
	return snapshot, nil
}

func (r *PgRepository) UpdateSyncStatus(ctx context.Context, userID string, syncedAt *time.Time, syncErr string) error {
	query := `UPDATE user_spaces
		SET last_sync_at = COALESCE($2, last_sync_at), last_sync_error = $3, updated_at = NOW()
		WHERE user_id = $1 AND deleted_at IS NULL`
	_, err := r.pool.Exec(ctx, query, userID, syncedAt, syncErr)
	return err
}

func (r *PgRepository) CountContentsByUserID(ctx context.Context, userID string) (int64, error) {
	var total int64
	err := r.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM user_space_contents WHERE user_id = $1 AND deleted_at IS NULL`,
		userID,
	).Scan(&total)
	return total, err
}

func (r *PgRepository) SetDisabled(ctx context.Context, userID string, disabledAt *time.Time, disabledBy, reason string) error {
	query := `UPDATE user_spaces
		SET disabled_at = $2, disabled_by = $3, disabled_reason = $4, updated_at = NOW()
		WHERE user_id = $1 AND deleted_at IS NULL`
	_, err := r.pool.Exec(ctx, query, userID, disabledAt, disabledBy, reason)
	return err
}

func (r *PgRepository) AdminSummary(ctx context.Context) (*SpaceAdminSummary, error) {
	query := `SELECT
			COUNT(*),
			COUNT(*) FILTER (WHERE visibility = 'public' AND disabled_at IS NULL),
			COUNT(*) FILTER (WHERE disabled_at IS NOT NULL),
			COUNT(*) FILTER (WHERE style_name <> ''),
			COUNT(*) FILTER (WHERE sync_enabled = TRUE),
			MAX(last_sync_at),
			COUNT(*) FILTER (WHERE last_sync_error <> '')
		FROM user_spaces
		WHERE deleted_at IS NULL`
	summary := &SpaceAdminSummary{}
	err := r.pool.QueryRow(ctx, query).Scan(
		&summary.TotalSpaces, &summary.PublicSpaces, &summary.DisabledSpaces,
		&summary.StyledSpaces, &summary.SyncEnabledSpaces, &summary.LastSyncAt,
		&summary.SyncErrorSpaces,
	)
	return summary, err
}
