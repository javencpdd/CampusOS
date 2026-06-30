package hostapi

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	_ "modernc.org/sqlite"
)

const defaultPluginDataDir = ".campusos/plugin-data"

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
	if err := os.MkdirAll(cleanRoot, 0o755); err != nil {
		return nil, err
	}
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
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
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
	if err := validatePluginStorageName(pluginName); err != nil {
		return "", err
	}
	rootDir := s.rootDir
	if rootDir == "" {
		rootDir = defaultPluginDataDir
	}
	rootAbs, err := filepath.Abs(filepath.Clean(rootDir))
	if err != nil {
		return "", err
	}
	target := filepath.Join(rootAbs, pluginName, "plugin.db")
	targetAbs, err := filepath.Abs(filepath.Clean(target))
	if err != nil {
		return "", err
	}
	if targetAbs != rootAbs && !strings.HasPrefix(targetAbs, rootAbs+string(os.PathSeparator)) {
		return "", fmt.Errorf("plugin storage path escapes root: %s", pluginName)
	}
	return targetAbs, nil
}

func validatePluginStorageName(name string) error {
	if name == "" {
		return errors.New("plugin name is required")
	}
	for _, r := range name {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' {
			continue
		}
		return fmt.Errorf("invalid plugin name for storage: %q", name)
	}
	return nil
}
