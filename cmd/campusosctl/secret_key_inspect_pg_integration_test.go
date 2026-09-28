package main

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/campusos/CampusOS/internal/plugin"
	"github.com/jackc/pgx/v5/pgxpool"
)

type secretKeyInspectCLIResult struct {
	Table                 string `json:"table"`
	KeyID                 string `json:"key_id"`
	SnapshotAt            string `json:"snapshot_at"`
	TotalReferences       int64  `json:"total_references"`
	ActiveReferences      int64  `json:"active_references"`
	RotatedReferences     int64  `json:"rotated_references"`
	RevokedReferences     int64  `json:"revoked_references"`
	OtherReferences       int64  `json:"other_references"`
	PluginCount           int64  `json:"plugin_count"`
	SnapshotHasReferences bool   `json:"snapshot_has_references"`
}

func callSecretKeyInspectCLI(t *testing.T, dsn string, args ...string) (int, secretKeyInspectCLIResult) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := run(append([]string{"secret", "inspect-key"}, args...), &stdout, &stderr)
	combined := stdout.String() + stderr.String()
	for _, forbidden := range []string{
		"alpha.system", "alpha.personal", "beta.system", "beta.personal", "beta.revoked", "beta.new",
		"alpha-system-history-fixture", "alpha-system-current-fixture", "alpha-system-new-fixture", "alpha-user-fixture",
		"beta-system-fixture", "beta-user-fixture", "beta-revoked-fixture", "beta-new-fixture",
		"postgres://", dsn,
	} {
		if forbidden != "" && strings.Contains(combined, forbidden) {
			t.Fatal("key inspection output exposed a Secret name, value, or database address")
		}
	}
	var result secretKeyInspectCLIResult
	if code == 0 {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(stdout.Bytes(), &fields); err != nil {
			t.Fatalf("key inspection did not return JSON: %v", err)
		}
		for _, key := range []string{
			"table", "key_id", "snapshot_at", "total_references", "active_references",
			"rotated_references", "revoked_references", "other_references", "plugin_count", "snapshot_has_references",
		} {
			if _, ok := fields[key]; !ok {
				t.Fatalf("key inspection omitted safe field %s", key)
			}
		}
		if len(fields) != 10 {
			t.Fatal("key inspection returned an unexpected field")
		}
		if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
			t.Fatalf("decode key inspection JSON: %v", err)
		}
	}
	return code, result
}

func secretKeyInspectDatabaseState(t *testing.T, pool *pgxpool.Pool) (string, int64) {
	t.Helper()
	var secretFingerprint string
	if err := pool.QueryRow(t.Context(), `SELECT md5(coalesce(string_agg(id::text || ':' || xmin::text || ':' || to_jsonb(s)::text, ',' ORDER BY id), '')) FROM plugin_secret_values s`).Scan(&secretFingerprint); err != nil {
		t.Fatal(err)
	}
	var operationCount int64
	if err := pool.QueryRow(t.Context(), `SELECT count(*) FROM platform_operation_runs`).Scan(&operationCount); err != nil {
		t.Fatal(err)
	}
	return secretFingerprint, operationCount
}

func assertSecretKeyInspectionReadOnly(t *testing.T, pool *pgxpool.Pool, fingerprint string, operations int64) {
	t.Helper()
	gotFingerprint, gotOperations := secretKeyInspectDatabaseState(t, pool)
	if gotFingerprint != fingerprint || gotOperations != operations {
		t.Fatal("read-only key inspection changed Secret rows or operation audit count")
	}
}

// The companion script owns this database and runs seed and restore phases.
func TestPostgresSecretKeyInspectRestore(t *testing.T) {
	if os.Getenv("CAMPUSOS_SECRET_KEY_INSPECT_TEST_ISOLATED") != "1" {
		t.Skip("requires owned V12-01b isolated PostgreSQL database")
	}
	phase := os.Getenv("CAMPUSOS_SECRET_KEY_INSPECT_TEST_PHASE")
	if phase != "seed" && phase != "restore" {
		t.Fatal("CAMPUSOS_SECRET_KEY_INSPECT_TEST_PHASE must be seed or restore")
	}
	dsn := os.Getenv("CAMPUSOS_PG_INTEGRATION_DSN")
	if dsn == "" {
		t.Fatal("CAMPUSOS_PG_INTEGRATION_DSN is required")
	}
	ctx := t.Context()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var database string
	if err := pool.QueryRow(ctx, `SELECT current_database()`).Scan(&database); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(database, "campusos_v12_01b_key_inspect") {
		t.Fatalf("refusing non-isolated database %q", database)
	}
	t.Setenv("DATABASE_DSN", dsn)
	// Inspection must work without any Secret key material in its environment.
	t.Setenv("CAMPUSOS_SECRET_ACTIVE_KEY_ID", "")
	t.Setenv("CAMPUSOS_SECRET_ENCRYPTION_KEYS", "")
	t.Setenv("CAMPUSOS_PLUGIN_SECRET_KEY", "")
	t.Setenv("CAMPUSOS_PLUGIN_SECRET_KEY_VERSION", "")

	const pluginA = int64(99126021)
	const pluginB = int64(99126022)
	userID := int64(99126023)
	oldKey := []byte("0123456789abcdef0123456789abcdef")
	newKey := []byte("abcdef0123456789abcdef0123456789")
	store := plugin.NewPgAuthorizationStore(pool)
	oldOnly, err := plugin.NewSecretService(store, oldKey, "old-v1")
	if err != nil {
		t.Fatal(err)
	}
	newOnly, err := plugin.NewSecretService(store, newKey, "new-v2")
	if err != nil {
		t.Fatal(err)
	}
	if phase == "seed" {
		if _, err := pool.Exec(ctx, `INSERT INTO users(id,username,nickname,email) VALUES($1,'v12_key_inspect_cli_owner','Key Inspect CLI Owner','v12-key-inspect-cli@example.invalid')`, userID); err != nil {
			t.Fatal(err)
		}
		repo := plugin.NewPgPluginRepository(pool)
		for _, item := range []struct {
			id   int64
			name string
		}{
			{pluginA, "v12-01b-key-inspect-alpha"},
			{pluginB, "v12-01b-key-inspect-beta"},
		} {
			record := &plugin.PluginRecord{
				ID: item.id, Name: item.name, DisplayName: "V12 Key Inspect Fixture", Version: "1.0.0",
				Runtime: "process", Status: string(plugin.StatusRunning), BackendState: string(plugin.BackendRunning),
				FrontendState: string(plugin.FrontendUnloaded), HealthState: string(plugin.HealthHealthy), Config: `{}`,
				Checksum: strings.Repeat("b", 64), InstalledBy: "test", InstalledAt: time.Now(), UpdatedAt: time.Now(),
			}
			if err := repo.Save(ctx, record); err != nil {
				t.Fatal(err)
			}
		}
		// Plugin A has no active old-key rows, but its rotated and revoked
		// history still references the old key and blocks a retirement claim.
		for _, value := range []string{"alpha-system-history-fixture", "alpha-system-current-fixture"} {
			if _, err := oldOnly.Put(ctx, pluginA, nil, "alpha.system", value, nil); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := newOnly.Put(ctx, pluginA, nil, "alpha.system", "alpha-system-new-fixture", nil); err != nil {
			t.Fatal(err)
		}
		if _, err := oldOnly.Put(ctx, pluginA, &userID, "alpha.personal", "alpha-user-fixture", nil); err != nil {
			t.Fatal(err)
		}
		if err := oldOnly.Revoke(ctx, pluginA, &userID, "alpha.personal"); err != nil {
			t.Fatal(err)
		}
		for _, item := range []struct {
			owner *int64
			name  string
			value string
		}{
			{nil, "beta.system", "beta-system-fixture"},
			{&userID, "beta.personal", "beta-user-fixture"},
			{&userID, "beta.revoked", "beta-revoked-fixture"},
		} {
			if _, err := oldOnly.Put(ctx, pluginB, item.owner, item.name, item.value, nil); err != nil {
				t.Fatal(err)
			}
		}
		if err := oldOnly.Revoke(ctx, pluginB, &userID, "beta.revoked"); err != nil {
			t.Fatal(err)
		}
		if _, err := newOnly.Put(ctx, pluginB, nil, "beta.new", "beta-new-fixture", nil); err != nil {
			t.Fatal(err)
		}
	}
	// The fixture's SQL truth is independently checked before exercising the
	// operator command, including its cross-plugin and historical distribution.
	var active, rotated, revoked, pluginCount int64
	if err := pool.QueryRow(ctx, `SELECT count(*) FILTER (WHERE status='active'),count(*) FILTER (WHERE status='rotated'),count(*) FILTER (WHERE status='revoked'),count(DISTINCT plugin_id) FROM plugin_secret_values WHERE key_version='old-v1'`).Scan(&active, &rotated, &revoked, &pluginCount); err != nil {
		t.Fatal(err)
	}
	if active != 2 || rotated != 2 || revoked != 2 || pluginCount != 2 {
		t.Fatalf("old-key fixture SQL truth is incomplete: active=%d rotated=%d revoked=%d plugins=%d", active, rotated, revoked, pluginCount)
	}
	var alphaActive, alphaHistorical int64
	if err := pool.QueryRow(ctx, `SELECT count(*) FILTER (WHERE status='active'),count(*) FILTER (WHERE status IN ('rotated','revoked')) FROM plugin_secret_values WHERE plugin_id=$1 AND key_version='old-v1'`, pluginA).Scan(&alphaActive, &alphaHistorical); err != nil || alphaActive != 0 || alphaHistorical != 3 {
		t.Fatalf("history-only plugin fixture is incomplete: active=%d history=%d err=%v", alphaActive, alphaHistorical, err)
	}
	fingerprint, operations := secretKeyInspectDatabaseState(t, pool)
	for _, item := range []struct {
		keyID                    string
		active, rotated, revoked int64
		plugins                  int64
	}{
		{"old-v1", 2, 2, 2, 2},
		{"unused-v3", 0, 0, 0, 0},
	} {
		code, result := callSecretKeyInspectCLI(t, dsn, "--key-id", item.keyID)
		if code != 0 || result.Table != "plugin_secret_values" || result.KeyID != item.keyID ||
			result.ActiveReferences != item.active || result.RotatedReferences != item.rotated ||
			result.RevokedReferences != item.revoked || result.OtherReferences != 0 ||
			result.TotalReferences != item.active+item.rotated+item.revoked ||
			result.PluginCount != item.plugins || result.SnapshotHasReferences != (item.plugins > 0) {
			t.Fatalf("key inspection result differs from SQL truth for %s: code=%d result=%+v", item.keyID, code, result)
		}
		if parsed, err := time.Parse(time.RFC3339Nano, result.SnapshotAt); err != nil || parsed.IsZero() {
			t.Fatal("key inspection omitted a valid snapshot timestamp")
		}
		assertSecretKeyInspectionReadOnly(t, pool, fingerprint, operations)
	}
	if code, _ := callSecretKeyInspectCLI(t, dsn, "--key-id", " bad-key-id"); code == 0 {
		t.Fatal("invalid key identifier was accepted")
	}
	assertSecretKeyInspectionReadOnly(t, pool, fingerprint, operations)
}
