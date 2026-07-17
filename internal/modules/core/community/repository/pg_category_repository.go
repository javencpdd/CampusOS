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

type PgCategoryRepository struct{ pool *pgxpool.Pool }

func NewPgCategoryRepository(pool *pgxpool.Pool) *PgCategoryRepository {
	return &PgCategoryRepository{pool: pool}
}

func (r *PgCategoryRepository) db(ctx context.Context) transaction.Executor {
	return transaction.ExecutorFor(ctx, r.pool)
}

func (r *PgCategoryRepository) Create(ctx context.Context, cat *domain.Category) error {
	cat.NormalizeHierarchy()
	_, err := r.db(ctx).Exec(ctx, `INSERT INTO categories
		(id, name, slug, description, icon, color, default_tags, parent_id, node_kind, lifecycle_status, version, sort_order, is_closed, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, NULLIF($8, '')::bigint, $9, $10, $11, $12, $13, $14, $15)`,
		cat.ID, cat.Name, cat.Slug, cat.Description, cat.Icon, cat.Color, cat.DefaultTags, parentIDValue(cat.ParentID),
		cat.NodeKind, cat.LifecycleStatus, cat.Version, cat.SortOrder, cat.IsClosed, cat.CreatedAt, cat.UpdatedAt)
	return err
}

func (r *PgCategoryRepository) GetByID(ctx context.Context, id string) (*domain.Category, error) {
	return r.get(ctx, `WHERE id = $1 AND deleted_at IS NULL`, id)
}

func (r *PgCategoryRepository) GetByIDForUpdate(ctx context.Context, id string) (*domain.Category, error) {
	return r.get(ctx, `WHERE id = $1 AND deleted_at IS NULL FOR UPDATE`, id)
}

func (r *PgCategoryRepository) get(ctx context.Context, clause string, args ...any) (*domain.Category, error) {
	item := &domain.Category{}
	var parentID string
	err := r.db(ctx).QueryRow(ctx, categorySelectSQL+" "+clause, args...).Scan(
		&item.ID, &item.Name, &item.Slug, &item.Description, &item.Icon, &item.Color,
		&item.DefaultTags, &parentID, &item.NodeKind, &item.LifecycleStatus, &item.Version,
		&item.SortOrder, &item.ThreadCount, &item.PostCount, &item.IsClosed, &item.CreatedAt, &item.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrCategoryNotFound
	}
	if err != nil {
		return nil, err
	}
	if parentID != "" {
		item.ParentID = &parentID
	}
	item.NormalizeHierarchy()
	return item, nil
}

func (r *PgCategoryRepository) List(ctx context.Context) ([]*domain.Category, error) {
	return r.list(ctx, `WHERE deleted_at IS NULL`, nil)
}

func (r *PgCategoryRepository) ListChildren(ctx context.Context, parentID string) ([]*domain.Category, error) {
	return r.list(ctx, `WHERE deleted_at IS NULL AND parent_id = $1`, []any{parentID})
}

func (r *PgCategoryRepository) list(ctx context.Context, clause string, args []any) ([]*domain.Category, error) {
	rows, err := r.db(ctx).Query(ctx, categorySelectSQL+" "+clause+" ORDER BY sort_order ASC, created_at ASC", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]*domain.Category, 0)
	for rows.Next() {
		item := &domain.Category{}
		var parentID string
		if err := rows.Scan(
			&item.ID, &item.Name, &item.Slug, &item.Description, &item.Icon, &item.Color,
			&item.DefaultTags, &parentID, &item.NodeKind, &item.LifecycleStatus, &item.Version,
			&item.SortOrder, &item.ThreadCount, &item.PostCount, &item.IsClosed, &item.CreatedAt, &item.UpdatedAt,
		); err != nil {
			return nil, err
		}
		if parentID != "" {
			item.ParentID = &parentID
		}
		item.NormalizeHierarchy()
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PgCategoryRepository) Update(ctx context.Context, cat *domain.Category) error {
	cat.NormalizeHierarchy()
	tag, err := r.db(ctx).Exec(ctx, `UPDATE categories
		SET name=$1, slug=$2, description=$3, icon=$4, color=$5, default_tags=$6, parent_id=NULLIF($7, '')::bigint,
			node_kind=$8, lifecycle_status=$9, version=$10, sort_order=$11, is_closed=$12, updated_at=$13
		WHERE id=$14 AND deleted_at IS NULL`,
		cat.Name, cat.Slug, cat.Description, cat.Icon, cat.Color, cat.DefaultTags, parentIDValue(cat.ParentID),
		cat.NodeKind, cat.LifecycleStatus, cat.Version, cat.SortOrder, cat.IsClosed, cat.UpdatedAt, cat.ID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrCategoryNotFound
	}
	return nil
}

func (r *PgCategoryRepository) UpdateIfVersion(ctx context.Context, cat *domain.Category, expected int64) error {
	cat.NormalizeHierarchy()
	tag, err := r.db(ctx).Exec(ctx, `UPDATE categories
		SET name=$1, slug=$2, description=$3, icon=$4, color=$5, default_tags=$6, parent_id=NULLIF($7, '')::bigint,
			node_kind=$8, lifecycle_status=$9, version=$10, sort_order=$11, is_closed=$12, updated_at=$13
		WHERE id=$14 AND deleted_at IS NULL AND version=$15`,
		cat.Name, cat.Slug, cat.Description, cat.Icon, cat.Color, cat.DefaultTags, parentIDValue(cat.ParentID),
		cat.NodeKind, cat.LifecycleStatus, cat.Version, cat.SortOrder, cat.IsClosed, cat.UpdatedAt, cat.ID, expected)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		if _, lookupErr := r.GetByID(ctx, cat.ID); lookupErr != nil {
			return lookupErr
		}
		return ErrCategoryVersionConflict
	}
	return nil
}

func (r *PgCategoryRepository) Delete(ctx context.Context, id string) error {
	tag, err := r.db(ctx).Exec(ctx, `UPDATE categories SET deleted_at = $1 WHERE id = $2 AND deleted_at IS NULL`, time.Now().UTC(), id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrCategoryNotFound
	}
	return nil
}

const categorySelectSQL = `SELECT id::text, name, slug, description, icon, color, default_tags,
	COALESCE(parent_id::text, ''), node_kind, lifecycle_status, version, sort_order, thread_count, post_count, is_closed, created_at, updated_at
	FROM categories`

func parentIDValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

var _ CategoryRepository = (*PgCategoryRepository)(nil)
