package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PgPluginRepository PostgreSQL 插件仓储
type PgPluginRepository struct {
	pool *pgxpool.Pool
}

func NewPgPluginRepository(pool *pgxpool.Pool) *PgPluginRepository {
	return &PgPluginRepository{pool: pool}
}

func (r *PgPluginRepository) Save(ctx context.Context, record *PluginRecord) error {
	query := `INSERT INTO plugins (id, name, display_name, version, description, author, runtime, status, api_key, config, error_message, installed_by, installed_at, updated_at)
		VALUES (nextval('plugins_id_seq'), $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		ON CONFLICT (name) WHERE deleted_at IS NULL
		DO UPDATE SET display_name = $2, version = $3, description = $4, author = $5, runtime = $6, status = $7, config = $9, updated_at = $13`

	_, err := r.pool.Exec(ctx, query,
		record.Name, record.DisplayName, record.Version, record.Description,
		record.Author, record.Runtime, record.Status, record.APIKey,
		record.Config, record.ErrorMsg, record.InstalledBy,
		record.InstalledAt, record.UpdatedAt,
	)
	return err
}

func (r *PgPluginRepository) GetByName(ctx context.Context, name string) (*PluginRecord, error) {
	query := `SELECT id, name, display_name, version, description, author, runtime, status, api_key, config, error_message, installed_by, installed_at, updated_at
		FROM plugins WHERE name = $1 AND deleted_at IS NULL`

	record := &PluginRecord{}
	err := r.pool.QueryRow(ctx, query, name).Scan(
		&record.ID, &record.Name, &record.DisplayName, &record.Version,
		&record.Description, &record.Author, &record.Runtime, &record.Status,
		&record.APIKey, &record.Config, &record.ErrorMsg, &record.InstalledBy,
		&record.InstalledAt, &record.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrAPIKeyNotFound
		}
		return nil, err
	}
	return record, nil
}

func (r *PgPluginRepository) List(ctx context.Context) ([]*PluginRecord, error) {
	query := `SELECT id, name, display_name, version, description, author, runtime, status, api_key, config, error_message, installed_by, installed_at, updated_at
		FROM plugins WHERE deleted_at IS NULL ORDER BY installed_at DESC`

	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []*PluginRecord
	for rows.Next() {
		record := &PluginRecord{}
		if err := rows.Scan(
			&record.ID, &record.Name, &record.DisplayName, &record.Version,
			&record.Description, &record.Author, &record.Runtime, &record.Status,
			&record.APIKey, &record.Config, &record.ErrorMsg, &record.InstalledBy,
			&record.InstalledAt, &record.UpdatedAt,
		); err != nil {
			return nil, err
		}
		list = append(list, record)
	}
	return list, nil
}

func (r *PgPluginRepository) UpdateStatus(ctx context.Context, name, status, errorMsg string) error {
	query := `UPDATE plugins SET status = $1, error_message = $2, updated_at = $3 WHERE name = $4 AND deleted_at IS NULL`
	_, err := r.pool.Exec(ctx, query, status, errorMsg, time.Now(), name)
	return err
}

func (r *PgPluginRepository) Delete(ctx context.Context, name string) error {
	query := `UPDATE plugins SET deleted_at = NOW() WHERE name = $1 AND deleted_at IS NULL`
	_, err := r.pool.Exec(ctx, query, name)
	return err
}

// PgAPIKeyRepository PostgreSQL API Key 仓储
type PgAPIKeyRepository struct {
	pool *pgxpool.Pool
}

func NewPgAPIKeyRepository(pool *pgxpool.Pool) *PgAPIKeyRepository {
	return &PgAPIKeyRepository{pool: pool}
}

func (r *PgAPIKeyRepository) Create(ctx context.Context, record *APIKeyRecord) error {
	query := `INSERT INTO api_keys (id, key, name, user_id, plugin_name, permissions, is_active, last_used_at, expires_at, created_at)
		VALUES (nextval('api_keys_id_seq'), $1, $2, $3, $4, $5, $6, $7, $8, $9)`

	permsJSON, _ := json.Marshal(record.Permissions)
	_, err := r.pool.Exec(ctx, query,
		record.Key, record.Name, record.UserID, record.PluginName,
		string(permsJSON), record.IsActive, record.LastUsedAt, record.ExpiresAt,
		record.CreatedAt,
	)
	return err
}

func (r *PgAPIKeyRepository) GetByKey(ctx context.Context, key string) (*APIKeyRecord, error) {
	query := `SELECT id, key, name, user_id, plugin_name, permissions, is_active, last_used_at, expires_at, created_at
		FROM api_keys WHERE key = $1 AND deleted_at IS NULL`

	record := &APIKeyRecord{}
	var permsJSON string
	err := r.pool.QueryRow(ctx, query, key).Scan(
		&record.ID, &record.Key, &record.Name, &record.UserID,
		&record.PluginName, &permsJSON, &record.IsActive,
		&record.LastUsedAt, &record.ExpiresAt, &record.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrAPIKeyNotFound
		}
		return nil, err
	}
	json.Unmarshal([]byte(permsJSON), &record.Permissions)
	return record, nil
}

func (r *PgAPIKeyRepository) ListByUser(ctx context.Context, userID int64) ([]*APIKeyRecord, error) {
	query := `SELECT id, key, name, user_id, plugin_name, permissions, is_active, last_used_at, expires_at, created_at
		FROM api_keys WHERE user_id = $1 AND deleted_at IS NULL`

	rows, err := r.pool.Query(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []*APIKeyRecord
	for rows.Next() {
		record := &APIKeyRecord{}
		var permsJSON string
		if err := rows.Scan(
			&record.ID, &record.Key, &record.Name, &record.UserID,
			&record.PluginName, &permsJSON, &record.IsActive,
			&record.LastUsedAt, &record.ExpiresAt, &record.CreatedAt,
		); err != nil {
			return nil, err
		}
		json.Unmarshal([]byte(permsJSON), &record.Permissions)
		list = append(list, record)
	}
	return list, nil
}

func (r *PgAPIKeyRepository) Deactivate(ctx context.Context, key string) error {
	query := `UPDATE api_keys SET is_active = false WHERE key = $1 AND deleted_at IS NULL`
	_, err := r.pool.Exec(ctx, query, key)
	return err
}

func (r *PgAPIKeyRepository) UpdateLastUsed(ctx context.Context, key string) error {
	query := `UPDATE api_keys SET last_used_at = NOW() WHERE key = $1 AND deleted_at IS NULL`
	_, err := r.pool.Exec(ctx, query, key)
	return err
}

func (r *PgAPIKeyRepository) Delete(ctx context.Context, key string) error {
	query := `UPDATE api_keys SET deleted_at = NOW() WHERE key = $1 AND deleted_at IS NULL`
	_, err := r.pool.Exec(ctx, query, key)
	return err
}
