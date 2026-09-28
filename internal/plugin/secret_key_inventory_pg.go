package plugin

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// PluginSecretKeyInventory is a point-in-time count of references in the live
// plugin_secret_values table. It says nothing about backups or other stores.
// Counts include every plugin and every status, including any future status
// that is not active, rotated, or revoked.
type PluginSecretKeyInventory struct {
	SnapshotAt        time.Time `json:"snapshot_at"`
	TotalReferences   int64     `json:"total_references"`
	ActiveReferences  int64     `json:"active_references"`
	RotatedReferences int64     `json:"rotated_references"`
	RevokedReferences int64     `json:"revoked_references"`
	OtherReferences   int64     `json:"other_references"`
	PluginCount       int64     `json:"plugin_count"`
}

// InspectPluginSecretKeyReferences takes one read-only, repeatable-read
// snapshot. The only database operation is a bound aggregate SELECT; it never
// fetches secret names, encrypted payloads, or plaintext.
func (s *PgAuthorizationStore) InspectPluginSecretKeyReferences(ctx context.Context, keyID string) (PluginSecretKeyInventory, error) {
	if keyID == "" || len(keyID) > 64 || keyID != strings.TrimSpace(keyID) || strings.ContainsAny(keyID, "\x00\r\n") {
		return PluginSecretKeyInventory{}, errors.New("plugin secret key ID is invalid")
	}
	if s == nil || s.pool == nil {
		return PluginSecretKeyInventory{}, errors.New("plugin secret inventory is unavailable")
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return PluginSecretKeyInventory{}, err
	}
	defer tx.Rollback(ctx)

	var inventory PluginSecretKeyInventory
	err = tx.QueryRow(ctx, `SELECT statement_timestamp(), COUNT(*),
		COUNT(*) FILTER (WHERE status='active'),
		COUNT(*) FILTER (WHERE status='rotated'),
		COUNT(*) FILTER (WHERE status='revoked'),
		COUNT(DISTINCT plugin_id)
		FROM plugin_secret_values WHERE key_version=$1`, keyID).Scan(
		&inventory.SnapshotAt,
		&inventory.TotalReferences,
		&inventory.ActiveReferences,
		&inventory.RotatedReferences,
		&inventory.RevokedReferences,
		&inventory.PluginCount,
	)
	if err != nil {
		return PluginSecretKeyInventory{}, err
	}
	inventory.OtherReferences = inventory.TotalReferences - inventory.ActiveReferences - inventory.RotatedReferences - inventory.RevokedReferences
	if inventory.OtherReferences < 0 {
		return PluginSecretKeyInventory{}, errors.New("plugin secret inventory is inconsistent")
	}
	if err := tx.Commit(ctx); err != nil {
		return PluginSecretKeyInventory{}, err
	}
	return inventory, nil
}
