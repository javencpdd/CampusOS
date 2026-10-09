package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"os"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/campusos/CampusOS/internal/plugin"
	"github.com/jackc/pgx/v5/pgxpool"
)

const secretInspectKeyUsage = "usage: campusosctl secret inspect-key --key-id ID"

type secretInspectKeyResult struct {
	Table                 string    `json:"table"`
	KeyID                 string    `json:"key_id"`
	SnapshotAt            time.Time `json:"snapshot_at"`
	TotalReferences       int64     `json:"total_references"`
	ActiveReferences      int64     `json:"active_references"`
	RotatedReferences     int64     `json:"rotated_references"`
	RevokedReferences     int64     `json:"revoked_references"`
	OtherReferences       int64     `json:"other_references"`
	PluginCount           int64     `json:"plugin_count"`
	SnapshotHasReferences bool      `json:"snapshot_has_references"`
}

func parseSecretInspectKeyOptions(args []string) (string, error) {
	flags := flag.NewFlagSet("secret inspect-key", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	keyID := flags.String("key-id", "", "encryption key ID to inspect")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 {
		return "", errors.New(secretInspectKeyUsage)
	}
	// Match the Keyring's byte length and whitespace boundary while refusing
	// terminal control characters in a value that will be echoed as JSON.
	if !utf8.ValidString(*keyID) || len(*keyID) == 0 || len(*keyID) > 64 ||
		*keyID != strings.TrimSpace(*keyID) {
		return "", errors.New("--key-id must be a printable ID of 1 to 64 bytes")
	}
	for _, ch := range *keyID {
		if unicode.IsControl(ch) || unicode.Is(unicode.Cf, ch) {
			return "", errors.New("--key-id must be a printable ID of 1 to 64 bytes")
		}
	}
	return *keyID, nil
}

func runSecretInspectKey(args []string, stdout io.Writer) error {
	keyID, err := parseSecretInspectKeyOptions(args)
	if err != nil {
		return err
	}
	dsn := strings.TrimSpace(os.Getenv("DATABASE_DSN"))
	if dsn == "" {
		return errors.New("DATABASE_DSN is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return errors.New("connect secret database")
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		return errors.New("connect secret database")
	}
	inventory, err := plugin.NewPgAuthorizationStore(pool).InspectPluginSecretKeyReferences(ctx, keyID)
	if err != nil {
		return errors.New("inspect secret key references")
	}
	result := secretInspectKeyResult{
		Table:                 "plugin_secret_values",
		KeyID:                 keyID,
		SnapshotAt:            inventory.SnapshotAt,
		TotalReferences:       inventory.TotalReferences,
		ActiveReferences:      inventory.ActiveReferences,
		RotatedReferences:     inventory.RotatedReferences,
		RevokedReferences:     inventory.RevokedReferences,
		OtherReferences:       inventory.OtherReferences,
		PluginCount:           inventory.PluginCount,
		SnapshotHasReferences: inventory.TotalReferences != 0,
	}
	if err := json.NewEncoder(stdout).Encode(result); err != nil {
		return errors.New("write secret key inspection result")
	}
	return nil
}
