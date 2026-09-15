package hostapi

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"

	"github.com/campusos/CampusOS/internal/plugin"
	_ "modernc.org/sqlite"
)

const defaultPluginDataDir = "data/plugin_data"

type SQLiteKVStore struct {
	rootDir string
}

func NewSQLiteKVStore(rootDir string) (*SQLiteKVStore, error) {
	if rootDir == "" {
		rootDir = defaultPluginDataDir
	}
	cleanRoot, err := filepath.Abs(filepath.Clean(rootDir))
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(cleanRoot, 0o700); err != nil {
		return nil, err
	}
	_ = os.Chmod(cleanRoot, 0o700)
	return &SQLiteKVStore{rootDir: cleanRoot}, nil
}

func (s *SQLiteKVStore) Get(ctx context.Context, pluginName, key string) (string, bool, error) {
	db, err := s.open(ctx, pluginName)
	if err != nil {
		return "", false, err
	}
	defer db.Close()

	var value string
	err = db.QueryRowContext(ctx, `SELECT value FROM plugin_kv WHERE key = ?`, key).Scan(&value)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", false, nil
		}
		return "", false, err
	}
	return value, true, nil
}

func (s *SQLiteKVStore) Set(ctx context.Context, pluginName, key, value string) error {
	if key == "" {
		return errors.New("storage key is required")
	}
	db, err := s.open(ctx, pluginName)
	if err != nil {
		return err
	}
	defer db.Close()

	_, err = db.ExecContext(ctx, `
		INSERT INTO plugin_kv (key, value, updated_at)
		VALUES (?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(key) DO UPDATE SET
			value = excluded.value,
			updated_at = CURRENT_TIMESTAMP
	`, key, value)
	return err
}

func (s *SQLiteKVStore) Delete(ctx context.Context, pluginName, key string) error {
	db, err := s.open(ctx, pluginName)
	if err != nil {
		return err
	}
	defer db.Close()

	_, err = db.ExecContext(ctx, `DELETE FROM plugin_kv WHERE key = ?`, key)
	return err
}

func (s *SQLiteKVStore) open(ctx context.Context, pluginName string) (*sql.DB, error) {
	dbPath, err := s.dbPath(pluginName)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o700); err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	_ = os.Chmod(dbPath, 0o600)
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS plugin_kv (
			key        TEXT PRIMARY KEY,
			value      TEXT NOT NULL,
			updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		)
	`); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func (s *SQLiteKVStore) dbPath(pluginName string) (string, error) {
	rootDir := s.rootDir
	if rootDir == "" {
		rootDir = defaultPluginDataDir
	}
	layout, err := plugin.PreparePluginStorage(rootDir, pluginName)
	if err != nil {
		return "", err
	}
	return layout.SQLitePath, nil
}
