package plugin

import (
	"context"
	"encoding/hex"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// The seed and restore phases run only in the owned, disposable database
// created by scripts/v12-01b-keyring-drill.sh.
func TestPostgresSecretKeyringRestore(t *testing.T) {
	if os.Getenv("CAMPUSOS_SECRET_TEST_ISOLATED") != "1" {
		t.Skip("requires owned V12-01b isolated PostgreSQL database")
	}
	phase := os.Getenv("CAMPUSOS_SECRET_TEST_PHASE")
	if phase != "seed" && phase != "restore" {
		t.Fatal("CAMPUSOS_SECRET_TEST_PHASE must be seed or restore")
	}
	dsn := os.Getenv("CAMPUSOS_PG_INTEGRATION_DSN")
	if dsn == "" {
		t.Fatal("CAMPUSOS_PG_INTEGRATION_DSN is required")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var database string
	if err := pool.QueryRow(ctx, `SELECT current_database()`).Scan(&database); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(database, "campusos_v12_01b_keyring") {
		t.Fatalf("refusing non-isolated database %q", database)
	}

	const pluginID = int64(99123001)
	userID := int64(99123002)
	const pluginName = "v12-01b-keyring-fixture"
	oldKey := []byte("0123456789abcdef0123456789abcdef")
	newKey := []byte("abcdef0123456789abcdef0123456789")
	store := NewPgAuthorizationStore(pool)
	if phase == "seed" {
		if _, err := pool.Exec(ctx, `INSERT INTO users(id,username,nickname,email) VALUES($1,'v12_keyring_owner','Keyring Owner','v12-keyring@example.invalid')`, userID); err != nil {
			t.Fatal(err)
		}
		repo := NewPgPluginRepository(pool)
		record := &PluginRecord{
			ID: pluginID, Name: pluginName, DisplayName: "V12 Keyring Fixture", Version: "1.0.0",
			Runtime: "process", Status: string(StatusRunning), BackendState: string(BackendRunning),
			FrontendState: string(FrontendUnloaded), HealthState: string(HealthHealthy), Config: `{}`,
			Checksum: strings.Repeat("a", 64), InstalledBy: "test", InstalledAt: time.Now(), UpdatedAt: time.Now(),
		}
		if err := repo.Save(ctx, record); err != nil {
			t.Fatal(err)
		}
		legacy, err := NewSecretService(store, oldKey, "old-v1")
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range []struct {
			owner *int64
			name  string
			value string
		}{
			{nil, "system.legacy", "system-old-fixture"},
			{&userID, "mail.password", "user-old-fixture"},
		} {
			if _, err := legacy.Put(ctx, pluginID, item.owner, item.name, item.value, &userID); err != nil {
				t.Fatal(err)
			}
		}
	}

	// The actual production environment constructor must choose the shared
	// keyring over the legacy plugin-only variable during the transition.
	t.Setenv("CAMPUSOS_PLUGIN_SECRET_KEY", hex.EncodeToString(oldKey))
	t.Setenv("CAMPUSOS_PLUGIN_SECRET_KEY_VERSION", "old-v1")
	t.Setenv("CAMPUSOS_SECRET_ACTIVE_KEY_ID", "new-v2")
	t.Setenv("CAMPUSOS_SECRET_ENCRYPTION_KEYS", "old-v1:"+hex.EncodeToString(oldKey)+",new-v2:"+hex.EncodeToString(newKey))
	rotated, err := NewSecretServiceFromEnv(store)
	if err != nil {
		t.Fatal(err)
	}
	if value, err := rotated.Resolve(ctx, pluginID, nil, "system.legacy"); err != nil || value != "system-old-fixture" {
		t.Fatalf("old system secret unavailable in %s phase: value=%q err=%v", phase, value, err)
	}
	if phase == "seed" {
		metadata, err := rotated.Put(ctx, pluginID, &userID, "mail.password", "user-new-fixture", &userID)
		if err != nil || metadata.KeyVersion != "new-v2" || len(metadata.Ciphertext) != 0 {
			t.Fatalf("new key write failed or leaked: metadata=%+v err=%v", metadata, err)
		}
	}
	if value, err := rotated.Resolve(ctx, pluginID, &userID, "mail.password"); err != nil || value != "user-new-fixture" {
		t.Fatalf("new user secret unavailable in %s phase: value=%q err=%v", phase, value, err)
	}
	if _, err := rotated.Resolve(ctx, pluginID, nil, "mail.password"); err == nil {
		t.Fatal("user secret resolved as system secret")
	}
	var oldRows, activeRows int
	if err := pool.QueryRow(ctx, `SELECT count(*) FILTER (WHERE status='rotated'),count(*) FILTER (WHERE status='active') FROM plugin_secret_values WHERE plugin_id=$1 AND owner_user_id=$2 AND secret_name='mail.password'`, pluginID, userID).Scan(&oldRows, &activeRows); err != nil {
		t.Fatal(err)
	}
	if oldRows != 1 || activeRows != 1 {
		t.Fatalf("expected one rotated and one active version, got old=%d active=%d", oldRows, activeRows)
	}
	var rawCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM plugin_secret_values WHERE plugin_id=$1 AND (encode(ciphertext,'escape') LIKE '%fixture%' OR metadata::text LIKE '%fixture%')`, pluginID).Scan(&rawCount); err != nil {
		t.Fatal(err)
	}
	if rawCount != 0 {
		t.Fatal("plaintext fixture leaked into database ciphertext or metadata")
	}

	t.Setenv("CAMPUSOS_SECRET_ENCRYPTION_KEYS", "new-v2:"+hex.EncodeToString(newKey))
	newOnly, err := NewSecretServiceFromEnv(store)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := newOnly.Resolve(ctx, pluginID, nil, "system.legacy"); err == nil {
		t.Fatal("old row opened without its retained read key")
	}
	if value, err := newOnly.Resolve(ctx, pluginID, &userID, "mail.password"); err != nil || value != "user-new-fixture" {
		t.Fatalf("active row unavailable without old key: value=%q err=%v", value, err)
	}
}
