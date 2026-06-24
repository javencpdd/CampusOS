package repository

import (
	"context"
	"errors"
	"time"

	"github.com/campusos/CampusOS/internal/community/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PgCategoryRepository struct {
	pool *pgxpool.Pool
}

func NewPgCategoryRepository(pool *pgxpool.Pool) *PgCategoryRepository {
	return &PgCategoryRepository{pool: pool}
}

func (r *PgCategoryRepository) Create(ctx context.Context, cat *domain.Category) error {
	query := `INSERT INTO categories (id, name, slug, description, icon, parent_id, sort_order, is_closed, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`
	_, err := r.pool.Exec(ctx, query,
		cat.ID, cat.Name, cat.Slug, cat.Description, cat.Icon,
		cat.ParentID, cat.SortOrder, cat.IsClosed, cat.CreatedAt, cat.UpdatedAt)
	return err
}

func (r *PgCategoryRepository) GetByID(ctx context.Context, id string) (*domain.Category, error) {
	query := `SELECT id, name, slug, description, icon, parent_id, sort_order, thread_count, post_count, is_closed, created_at, updated_at
		FROM categories WHERE id = $1 AND deleted_at IS NULL`
	cat := &domain.Category{}
	err := r.pool.QueryRow(ctx, query, id).Scan(
		&cat.ID, &cat.Name, &cat.Slug, &cat.Description, &cat.Icon,
		&cat.ParentID, &cat.SortOrder, &cat.ThreadCount, &cat.PostCount,
		&cat.IsClosed, &cat.CreatedAt, &cat.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrCategoryNotFound
		}
		return nil, err
	}
	return cat, nil
}

func (r *PgCategoryRepository) List(ctx context.Context) ([]*domain.Category, error) {
	query := `SELECT id, name, slug, description, icon, parent_id, sort_order, thread_count, post_count, is_closed, created_at, updated_at
		FROM categories WHERE deleted_at IS NULL ORDER BY sort_order ASC, created_at ASC`
	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var categories []*domain.Category
	for rows.Next() {
		cat := &domain.Category{}
		if err := rows.Scan(
			&cat.ID, &cat.Name, &cat.Slug, &cat.Description, &cat.Icon,
			&cat.ParentID, &cat.SortOrder, &cat.ThreadCount, &cat.PostCount,
			&cat.IsClosed, &cat.CreatedAt, &cat.UpdatedAt); err != nil {
			return nil, err
		}
		categories = append(categories, cat)
	}
	return categories, nil
}

func (r *PgCategoryRepository) Update(ctx context.Context, cat *domain.Category) error {
	query := `UPDATE categories SET name=$1, description=$2, is_closed=$3, sort_order=$4, updated_at=$5
		WHERE id = $6 AND deleted_at IS NULL`
	tag, err := r.pool.Exec(ctx, query, cat.Name, cat.Description, cat.IsClosed, cat.SortOrder, time.Now().UTC(), cat.ID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrCategoryNotFound
	}
	return nil
}

func (r *PgCategoryRepository) Delete(ctx context.Context, id string) error {
	query := `UPDATE categories SET deleted_at = $1 WHERE id = $2 AND deleted_at IS NULL`
	tag, err := r.pool.Exec(ctx, query, time.Now().UTC(), id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrCategoryNotFound
	}
	return nil
}
