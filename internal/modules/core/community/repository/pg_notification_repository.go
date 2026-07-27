package repository

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PgNotificationRepository struct {
	pool *pgxpool.Pool
}

func NewPgNotificationRepository(pool *pgxpool.Pool) *PgNotificationRepository {
	return &PgNotificationRepository{pool: pool}
}

func (r *PgNotificationRepository) db(ctx context.Context) transaction.Executor {
	return transaction.ExecutorFor(ctx, r.pool)
}

func (r *PgNotificationRepository) Create(ctx context.Context, value *domain.Notification) error {
	metadata, err := json.Marshal(value.Metadata)
	if err != nil {
		return err
	}
	_, err = r.db(ctx).Exec(ctx, `INSERT INTO notifications
		(id, user_id, type, title, content, action_url, is_read, read_at, metadata, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9::jsonb, $10, $11)`,
		value.ID, value.UserID, value.Type, value.Title, value.Content, value.ActionURL,
		value.IsRead, value.ReadAt, metadata, value.CreatedAt, value.UpdatedAt)
	return err
}

func (r *PgNotificationRepository) ListByUser(ctx context.Context, userID string, page, pageSize int) ([]*domain.Notification, int64, error) {
	page, pageSize = normalizeNotificationPage(page, pageSize)
	var total int64
	if err := r.db(ctx).QueryRow(ctx, `SELECT COUNT(*) FROM notifications
		WHERE user_id=$1 AND deleted_at IS NULL`, userID).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.db(ctx).Query(ctx, `SELECT id::text, user_id::text, type, title, content, action_url,
		is_read, read_at, metadata, created_at, updated_at
		FROM notifications
		WHERE user_id=$1 AND deleted_at IS NULL
		ORDER BY created_at DESC, id DESC
		LIMIT $2 OFFSET $3`, userID, pageSize, (page-1)*pageSize)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]*domain.Notification, 0)
	for rows.Next() {
		value, scanErr := scanNotification(rows)
		if scanErr != nil {
			return nil, 0, scanErr
		}
		items = append(items, value)
	}
	return items, total, rows.Err()
}

func (r *PgNotificationRepository) CountUnread(ctx context.Context, userID string) (int64, error) {
	var count int64
	err := r.db(ctx).QueryRow(ctx, `SELECT COUNT(*) FROM notifications
		WHERE user_id=$1 AND is_read=FALSE AND deleted_at IS NULL`, userID).Scan(&count)
	return count, err
}

func (r *PgNotificationRepository) MarkRead(ctx context.Context, userID, id string, at time.Time) error {
	tag, err := r.db(ctx).Exec(ctx, `UPDATE notifications
		SET is_read=TRUE, read_at=COALESCE(read_at, $1), updated_at=$1
		WHERE id=$2 AND user_id=$3 AND deleted_at IS NULL`, at, id, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotificationNotFound
	}
	return nil
}

func (r *PgNotificationRepository) MarkAllRead(ctx context.Context, userID string, at time.Time) (int64, error) {
	tag, err := r.db(ctx).Exec(ctx, `UPDATE notifications
		SET is_read=TRUE, read_at=$1, updated_at=$1
		WHERE user_id=$2 AND is_read=FALSE AND deleted_at IS NULL`, at, userID)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

type notificationScanner interface {
	Scan(...interface{}) error
}

func scanNotification(row notificationScanner) (*domain.Notification, error) {
	value := &domain.Notification{}
	var metadata []byte
	if err := row.Scan(
		&value.ID, &value.UserID, &value.Type, &value.Title, &value.Content, &value.ActionURL,
		&value.IsRead, &value.ReadAt, &metadata, &value.CreatedAt, &value.UpdatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotificationNotFound
		}
		return nil, err
	}
	if len(metadata) > 0 {
		if err := json.Unmarshal(metadata, &value.Metadata); err != nil {
			return nil, err
		}
	}
	if value.Metadata == nil {
		value.Metadata = map[string]interface{}{}
	}
	return value, nil
}
