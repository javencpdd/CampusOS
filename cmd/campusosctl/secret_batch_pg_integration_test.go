package main

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/campusos/CampusOS/internal/platform/security"
	"github.com/campusos/CampusOS/internal/plugin"
	"github.com/jackc/pgx/v5/pgxpool"
)

type secretBatchCLIResult struct {
	PluginID    int64  `json:"plugin_id"`
	ActiveKeyID string `json:"active_key_id"`
	DryRun      bool   `json:"dry_run"`
	Eligible    int64  `json:"eligible"`
	Selected    int    `json:"selected"`
	Rewrapped   int    `json:"rewrapped"`
	Remaining   int64  `json:"remaining"`
	Complete    bool   `json:"complete"`
	AuditID     string `json:"audit_id"`
}

func callSecretBatchCLI(t *testing.T, args ...string) (int, secretBatchCLIResult, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := run(append([]string{"secret", "rewrap"}, args...), &stdout, &stderr)
	output := stdout.String() + stderr.String()
	for _, forbidden := range []string{
		"system-current-fixture", "user-current-fixture", "token-current-fixture",
		"system.current", "mail.password", "api.token", "postgres://",
	} {
		if strings.Contains(output, forbidden) {
			t.Fatalf("operator command exposed a secret value, name, or DSN")
		}
	}
	var result secretBatchCLIResult
	if code == 0 {
		if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
			t.Fatalf("operator command did not return JSON: %v", err)
		}
	}
	return code, result, stderr.String()
}

// The script owns this database and runs seed and restore phases separately.
func TestPostgresSecretBatchRewrapRestore(t *testing.T) {
	if os.Getenv("CAMPUSOS_SECRET_BATCH_TEST_ISOLATED") != "1" {
		t.Skip("requires owned V12-01b isolated PostgreSQL database")
	}
	phase := os.Getenv("CAMPUSOS_SECRET_BATCH_TEST_PHASE")
	if phase != "seed" && phase != "restore" {
		t.Fatal("CAMPUSOS_SECRET_BATCH_TEST_PHASE must be seed or restore")
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
	if !strings.HasPrefix(database, "campusos_v12_01b_batch") {
		t.Fatalf("refusing non-isolated database %q", database)
	}
	t.Setenv("DATABASE_DSN", dsn)
	const pluginID = int64(99125001)
	const missingPluginID = int64(99125003)
	const corruptPluginID = int64(99125004)
	const racePluginID = int64(99125005)
	userID := int64(99125002)
	oldKey := []byte("0123456789abcdef0123456789abcdef")
	newKey := []byte("abcdef0123456789abcdef0123456789")
	setKeys := func(withOld bool) {
		t.Setenv("CAMPUSOS_SECRET_ACTIVE_KEY_ID", "new-v2")
		encoded := "new-v2:" + hex.EncodeToString(newKey)
		if withOld {
			encoded = "old-v1:" + hex.EncodeToString(oldKey) + "," + encoded
		}
		t.Setenv("CAMPUSOS_SECRET_ENCRYPTION_KEYS", encoded)
	}
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
		if _, err := pool.Exec(ctx, `INSERT INTO users(id,username,nickname,email) VALUES($1,'v12_batch_owner','Batch Owner','v12-batch@example.invalid')`, userID); err != nil {
			t.Fatal(err)
		}
		repo := plugin.NewPgPluginRepository(pool)
		for _, item := range []struct {
			id   int64
			name string
		}{
			{pluginID, "v12-01b-batch-positive"},
			{missingPluginID, "v12-01b-batch-missing"},
			{corruptPluginID, "v12-01b-batch-corrupt"},
			{racePluginID, "v12-01b-batch-race"},
		} {
			record := &plugin.PluginRecord{
				ID: item.id, Name: item.name, DisplayName: "V12 Batch Fixture", Version: "1.0.0",
				Runtime: "process", Status: string(plugin.StatusRunning), BackendState: string(plugin.BackendRunning),
				FrontendState: string(plugin.FrontendUnloaded), HealthState: string(plugin.HealthHealthy), Config: `{}`,
				Checksum: strings.Repeat("a", 64), InstalledBy: "test", InstalledAt: time.Now(), UpdatedAt: time.Now(),
			}
			if err := repo.Save(ctx, record); err != nil {
				t.Fatal(err)
			}
		}
		for _, item := range []struct {
			owner *int64
			name  string
			value string
		}{
			{nil, "system.current", "system-history-fixture"},
			{nil, "system.current", "system-current-fixture"},
			{&userID, "mail.password", "user-current-fixture"},
			{nil, "api.token", "token-current-fixture"},
		} {
			if _, err := oldOnly.Put(ctx, pluginID, item.owner, item.name, item.value, &userID); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := newOnly.Put(ctx, pluginID, nil, "already.current", "already-new-fixture", &userID); err != nil {
			t.Fatal(err)
		}
		setKeys(true)
		pluginArg := fmt.Sprintf("%d", pluginID)
		if code, preview, detail := callSecretBatchCLI(t, "--plugin-id", pluginArg, "--batch-size", "1"); code != 0 || !preview.DryRun || preview.Eligible != 3 || preview.Rewrapped != 0 {
			t.Fatalf("read-only preview failed: code=%d eligible=%d detail=%s", code, preview.Eligible, detail)
		}
		if code, _, _ := callSecretBatchCLI(t, "--plugin-id", pluginArg, "--batch-size", "1", "--apply", "--expected-count", "2", "--actor", "v12-operator", "--reason", "isolated batch test"); code == 0 {
			t.Fatal("stale dry-run count was accepted")
		}
		for remaining := int64(3); remaining > 0; remaining-- {
			code, result, detail := callSecretBatchCLI(t, "--plugin-id", pluginArg, "--batch-size", "1", "--apply", "--expected-count", fmt.Sprintf("%d", remaining), "--actor", "v12-operator", "--reason", "isolated batch test")
			if code != 0 || result.Selected != 1 || result.Rewrapped != 1 || result.Remaining != remaining-1 || result.AuditID == "" {
				t.Fatalf("bounded apply failed: code=%d selected=%d rewrapped=%d remaining=%d detail=%s", code, result.Selected, result.Rewrapped, result.Remaining, detail)
			}
		}
		if code, preview, detail := callSecretBatchCLI(t, "--plugin-id", pluginArg); code != 0 || preview.Eligible != 0 || !preview.Complete {
			t.Fatalf("completed preview failed: code=%d eligible=%d detail=%s", code, preview.Eligible, detail)
		}
		var activeOld, historyOld int
		if err := pool.QueryRow(ctx, `SELECT count(*) FILTER (WHERE status='active' AND key_version='old-v1'),count(*) FILTER (WHERE status='rotated' AND key_version='old-v1') FROM plugin_secret_values WHERE plugin_id=$1`, pluginID).Scan(&activeOld, &historyOld); err != nil || activeOld != 0 || historyOld != 1 {
			t.Fatalf("positive fixture key counts invalid: active-old=%d history-old=%d err=%v", activeOld, historyOld, err)
		}
		var succeeded, failed int
		if err := pool.QueryRow(ctx, `SELECT count(*) FILTER (WHERE status='succeeded'),count(*) FILTER (WHERE status='failed') FROM platform_operation_runs WHERE kind='plugin.secret_rewrap' AND subject_id=$1`, pluginArg).Scan(&succeeded, &failed); err != nil || succeeded != 3 || failed != 0 {
			t.Fatalf("operator audit count invalid: succeeded=%d failed=%d err=%v", succeeded, failed, err)
		}
		var leaked int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM platform_operation_runs WHERE kind='plugin.secret_rewrap' AND subject_id=$1 AND (details::text LIKE '%system.current%' OR details::text LIKE '%mail.password%' OR details::text LIKE '%api.token%' OR details::text LIKE '%fixture%')`, pluginArg).Scan(&leaked); err != nil || leaked != 0 {
			t.Fatalf("audit stored secret names or values: count=%d err=%v", leaked, err)
		}
		for _, negative := range []struct {
			pluginID int64
			name     string
			value    string
			corrupt  bool
		}{
			{missingPluginID, "missing.case", "missing-key-fixture", false},
			{corruptPluginID, "corrupt.case", "corrupt-key-fixture", true},
		} {
			row, err := oldOnly.Put(ctx, negative.pluginID, nil, negative.name, negative.value, &userID)
			if err != nil {
				t.Fatal(err)
			}
			if negative.corrupt {
				if _, err := pool.Exec(ctx, `UPDATE plugin_secret_values SET ciphertext=set_byte(ciphertext,0,(get_byte(ciphertext,0)+1)%256) WHERE id=$1`, row.ID); err != nil {
					t.Fatal(err)
				}
				setKeys(true)
			} else {
				setKeys(false)
			}
			code, _, _ := callSecretBatchCLI(t, "--plugin-id", fmt.Sprintf("%d", negative.pluginID), "--apply", "--expected-count", "1", "--actor", "v12-operator", "--reason", "isolated negative test")
			if code == 0 {
				t.Fatal("unreadable old-key row passed batch apply")
			}
			var version string
			if err := pool.QueryRow(ctx, `SELECT key_version FROM plugin_secret_values WHERE id=$1`, row.ID).Scan(&version); err != nil || version != "old-v1" {
				t.Fatalf("failed batch changed an unreadable row: version=%s err=%v", version, err)
			}
			var auditStatus string
			if err := pool.QueryRow(ctx, `SELECT status FROM platform_operation_runs WHERE kind='plugin.secret_rewrap' AND subject_id=$1 ORDER BY created_at DESC LIMIT 1`, fmt.Sprintf("%d", negative.pluginID)).Scan(&auditStatus); err != nil || auditStatus != "failed" {
				t.Fatalf("failed batch missing audit: status=%s err=%v", auditStatus, err)
			}
		}
		// A second operator observed the old envelope, then the first changed the
		// same row in place. The stale envelope must not win the conditional write.
		if _, err := oldOnly.Put(ctx, racePluginID, nil, "race.case", "race-old-fixture", &userID); err != nil {
			t.Fatal(err)
		}
		observed, err := store.ActiveSecret(ctx, racePluginID, nil, "race.case")
		if err != nil {
			t.Fatal(err)
		}
		ring, err := security.NewKeyring("new-v2", map[string][]byte{"old-v1": oldKey, "new-v2": newKey})
		if err != nil {
			t.Fatal(err)
		}
		first, err := plugin.NewSecretServiceWithKeyring(store, ring)
		if err != nil {
			t.Fatal(err)
		}
		if _, changed, err := first.RewrapActive(ctx, racePluginID, nil, "race.case"); err != nil || !changed {
			t.Fatalf("first rewrap failed: changed=%v err=%v", changed, err)
		}
		if _, err := store.ReplaceSecretIfCurrent(ctx, observed, observed); !errors.Is(err, plugin.ErrSecretChanged) {
			t.Fatalf("stale envelope CAS returned %v, want ErrSecretChanged", err)
		}
	}
	setKeys(false)
	for _, item := range []struct {
		owner *int64
		name  string
		want  string
	}{
		{nil, "system.current", "system-current-fixture"},
		{&userID, "mail.password", "user-current-fixture"},
		{nil, "api.token", "token-current-fixture"},
		{nil, "already.current", "already-new-fixture"},
	} {
		value, err := newOnly.Resolve(ctx, pluginID, item.owner, item.name)
		if err != nil || value != item.want {
			t.Fatalf("new-only read failed in %s phase: err=%v", phase, err)
		}
	}
	if phase == "restore" {
		if code, result, detail := callSecretBatchCLI(t, "--plugin-id", fmt.Sprintf("%d", pluginID)); code != 0 || result.Eligible != 0 || !result.Complete {
			t.Fatalf("restored operator preview failed: code=%d eligible=%d detail=%s", code, result.Eligible, detail)
		}
	}
}
