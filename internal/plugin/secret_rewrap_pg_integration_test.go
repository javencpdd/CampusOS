package plugin

import (
	"context"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/campusos/CampusOS/internal/platform/security"
	"github.com/jackc/pgx/v5/pgxpool"
)

// The seed and restore phases run only in the owned, disposable PostgreSQL
// database created by scripts/v12-01b-secret-rewrap-drill.sh.
func TestPostgresSecretActiveRewrapRestore(t *testing.T) {
	if os.Getenv("CAMPUSOS_SECRET_REWRAP_TEST_ISOLATED") != "1" {
		t.Skip("requires owned V12-01b isolated PostgreSQL database")
	}
	phase := os.Getenv("CAMPUSOS_SECRET_REWRAP_TEST_PHASE")
	if phase != "seed" && phase != "restore" {
		t.Fatal("CAMPUSOS_SECRET_REWRAP_TEST_PHASE must be seed or restore")
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
	if !strings.HasPrefix(database, "campusos_v12_01b_rewrap") {
		t.Fatalf("refusing non-isolated database %q", database)
	}

	const pluginID = int64(99124001)
	const negativePluginID = int64(99124003)
	userID := int64(99124002)
	oldKey := []byte("0123456789abcdef0123456789abcdef")
	newKey := []byte("abcdef0123456789abcdef0123456789")
	store := NewPgAuthorizationStore(pool)
	oldOnly, err := NewSecretService(store, oldKey, "old-v1")
	if err != nil {
		t.Fatal(err)
	}
	newOnly, err := NewSecretService(store, newKey, "new-v2")
	if err != nil {
		t.Fatal(err)
	}
	keyring, err := security.NewKeyring("new-v2", map[string][]byte{"old-v1": oldKey, "new-v2": newKey})
	if err != nil {
		t.Fatal(err)
	}
	rotating, err := NewSecretServiceWithKeyring(store, keyring)
	if err != nil {
		t.Fatal(err)
	}

	if phase == "seed" {
		if _, err := pool.Exec(ctx, `INSERT INTO users(id,username,nickname,email) VALUES($1,'v12_rewrap_owner','Rewrap Owner','v12-rewrap@example.invalid')`, userID); err != nil {
			t.Fatal(err)
		}
		repo := NewPgPluginRepository(pool)
		for _, fixture := range []struct {
			id   int64
			name string
		}{
			{pluginID, "v12-01b-rewrap-fixture"},
			{negativePluginID, "v12-01b-rewrap-negative-fixture"},
		} {
			record := &PluginRecord{
				ID: fixture.id, Name: fixture.name, DisplayName: "V12 Rewrap Fixture", Version: "1.0.0",
				Runtime: "process", Status: string(StatusRunning), BackendState: string(BackendRunning),
				FrontendState: string(FrontendUnloaded), HealthState: string(HealthHealthy), Config: `{}`,
				Checksum: strings.Repeat("a", 64), InstalledBy: "test", InstalledAt: time.Now(), UpdatedAt: time.Now(),
			}
			if err := repo.Save(ctx, record); err != nil {
				t.Fatal(err)
			}
		}

		historical, err := oldOnly.Put(ctx, pluginID, nil, "system.legacy", "system-history-fixture", &userID)
		if err != nil {
			t.Fatal(err)
		}
		historicalRead, err := store.ActiveSecret(ctx, pluginID, nil, "system.legacy")
		if err != nil {
			t.Fatal(err)
		}
		system, err := oldOnly.Put(ctx, pluginID, nil, "system.legacy", "system-current-fixture", &userID)
		if err != nil {
			t.Fatal(err)
		}
		user, err := oldOnly.Put(ctx, pluginID, &userID, "mail.password", "user-current-fixture", &userID)
		if err != nil {
			t.Fatal(err)
		}
		for _, fixture := range []struct {
			id    int64
			scope string
		}{{system.ID, "system"}, {user.ID, "user"}} {
			if _, err := pool.Exec(ctx, `UPDATE plugin_secret_values SET metadata=jsonb_set(metadata,'{scope}',to_jsonb($2::text)),created_by=$3 WHERE id=$1`, fixture.id, fixture.scope, userID); err != nil {
				t.Fatal(err)
			}
		}
		beforeHistorical := readRewrapIdentity(t, ctx, pool, historical.ID)
		beforeSystem := readRewrapIdentity(t, ctx, pool, system.ID)
		beforeUser := readRewrapIdentity(t, ctx, pool, user.ID)
		for _, fixture := range []struct {
			owner *int64
			name  string
			id    int64
		}{
			{nil, "system.legacy", system.ID},
			{&userID, "mail.password", user.ID},
		} {
			updated, changed, err := rotating.RewrapActive(ctx, pluginID, fixture.owner, fixture.name)
			if err != nil || !changed || updated.ID != fixture.id || updated.KeyVersion != "new-v2" || len(updated.Ciphertext) != 0 || len(updated.Nonce) != 0 {
				t.Fatalf("active rewrap failed for %s: changed=%v err=%v", fixture.name, changed, err)
			}
			_, changed, err = rotating.RewrapActive(ctx, pluginID, fixture.owner, fixture.name)
			if err != nil || changed {
				t.Fatalf("active rewrap was not idempotent for %s: changed=%v err=%v", fixture.name, changed, err)
			}
		}
		if !reflect.DeepEqual(beforeHistorical, readRewrapIdentity(t, ctx, pool, historical.ID)) ||
			!reflect.DeepEqual(beforeSystem, readRewrapIdentity(t, ctx, pool, system.ID)) ||
			!reflect.DeepEqual(beforeUser, readRewrapIdentity(t, ctx, pool, user.ID)) {
			t.Fatal("rewrap changed historical or active secret identity, ownership, metadata, or timestamps")
		}
		assertRewrapFixtureRows(t, ctx, pool, pluginID)
		activeSystem, err := store.ActiveSecret(ctx, pluginID, nil, "system.legacy")
		if err != nil {
			t.Fatal(err)
		}
		historicalReplacement := historicalRead
		historicalReplacement.KeyVersion = "new-v2"
		if _, err := store.ReplaceSecretIfCurrent(ctx, historicalRead, historicalReplacement); !errors.Is(err, ErrSecretChanged) {
			t.Fatalf("historical row CAS returned %v, want ErrSecretChanged", err)
		}
		wrongOwnerReplacement := activeSystem
		wrongOwnerReplacement.OwnerUserID = &userID
		if _, err := store.ReplaceSecretIfCurrent(ctx, activeSystem, wrongOwnerReplacement); !errors.Is(err, ErrSecretChanged) {
			t.Fatalf("wrong-owner CAS returned %v, want ErrSecretChanged", err)
		}
		assertRewrapFixtureRows(t, ctx, pool, pluginID)

		if _, err := oldOnly.Put(ctx, negativePluginID, nil, "stale.case", "stale-first-fixture", &userID); err != nil {
			t.Fatal(err)
		}
		staleRead, err := store.ActiveSecret(ctx, negativePluginID, nil, "stale.case")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := oldOnly.Put(ctx, negativePluginID, nil, "stale.case", "stale-newer-fixture", &userID); err != nil {
			t.Fatal(err)
		}
		if _, err := store.ReplaceSecretIfCurrent(ctx, staleRead, staleRead); !errors.Is(err, ErrSecretChanged) {
			t.Fatalf("Put-after-read CAS returned %v, want ErrSecretChanged", err)
		}
		if value, err := oldOnly.Resolve(ctx, negativePluginID, nil, "stale.case"); err != nil || value != "stale-newer-fixture" {
			t.Fatalf("Put-after-read CAS changed the newer secret: err=%v", err)
		}

		if _, err := oldOnly.Put(ctx, negativePluginID, nil, "same.row", "same-row-fixture", &userID); err != nil {
			t.Fatal(err)
		}
		sameRowRead, err := store.ActiveSecret(ctx, negativePluginID, nil, "same.row")
		if err != nil {
			t.Fatal(err)
		}
		if _, changed, err := rotating.RewrapActiveIfCurrent(ctx, negativePluginID, nil, "same.row", sameRowRead.ID); err != nil || !changed {
			t.Fatalf("same-row first CAS failed: changed=%v err=%v", changed, err)
		}
		staleEnvelopeReplacement := sameRowRead
		staleEnvelopeReplacement.KeyVersion = "stale-other"
		if _, err := store.ReplaceSecretIfCurrent(ctx, sameRowRead, staleEnvelopeReplacement); !errors.Is(err, ErrSecretChanged) {
			t.Fatalf("same-row stale envelope CAS returned %v, want ErrSecretChanged", err)
		}
		if value, err := newOnly.Resolve(ctx, negativePluginID, nil, "same.row"); err != nil || value != "same-row-fixture" {
			t.Fatalf("same-row stale CAS changed the first envelope: err=%v", err)
		}

		if _, err := oldOnly.Put(ctx, negativePluginID, nil, "revoke.case", "revoke-fixture", &userID); err != nil {
			t.Fatal(err)
		}
		revokedRaw, err := store.ActiveSecret(ctx, negativePluginID, nil, "revoke.case")
		if err != nil {
			t.Fatal(err)
		}
		if err := store.RevokeSecret(ctx, negativePluginID, nil, "revoke.case"); err != nil {
			t.Fatal(err)
		}
		if _, err := store.ReplaceSecretIfCurrent(ctx, revokedRaw, revokedRaw); !errors.Is(err, ErrSecretChanged) {
			t.Fatalf("revoked row CAS returned %v, want ErrSecretChanged", err)
		}
		if _, err := store.ActiveSecret(ctx, negativePluginID, nil, "revoke.case"); !errors.Is(err, ErrMarketNotFound) {
			t.Fatalf("revoked row remained active: %v", err)
		}

		corrupt, err := oldOnly.Put(ctx, negativePluginID, nil, "corrupt.case", "corrupt-fixture", &userID)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `UPDATE plugin_secret_values SET ciphertext=set_byte(ciphertext,0,(get_byte(ciphertext,0)+1)%256) WHERE id=$1`, corrupt.ID); err != nil {
			t.Fatal(err)
		}
		if _, changed, err := rotating.RewrapActive(ctx, negativePluginID, nil, "corrupt.case"); err == nil || changed {
			t.Fatalf("corrupt envelope rewrap did not fail closed: changed=%v err=%v", changed, err)
		}
		missing, err := oldOnly.Put(ctx, negativePluginID, nil, "missing.key", "missing-key-fixture", &userID)
		if err != nil {
			t.Fatal(err)
		}
		if _, changed, err := newOnly.RewrapActive(ctx, negativePluginID, nil, "missing.key"); err == nil || changed {
			t.Fatalf("missing old key rewrap did not fail closed: changed=%v err=%v", changed, err)
		}
		for _, id := range []int64{corrupt.ID, missing.ID} {
			var keyVersion string
			if err := pool.QueryRow(ctx, `SELECT key_version FROM plugin_secret_values WHERE id=$1`, id).Scan(&keyVersion); err != nil || keyVersion != "old-v1" {
				t.Fatalf("failed rewrap modified old-key row: key_version=%s err=%v", keyVersion, err)
			}
		}
	}

	for _, fixture := range []struct {
		owner *int64
		name  string
		want  string
		scope string
	}{
		{nil, "system.legacy", "system-current-fixture", "system"},
		{&userID, "mail.password", "user-current-fixture", "user"},
	} {
		value, err := newOnly.Resolve(ctx, pluginID, fixture.owner, fixture.name)
		if err != nil || value != fixture.want {
			t.Fatalf("new-only read failed in %s phase for %s: err=%v", phase, fixture.name, err)
		}
		if _, err := oldOnly.Resolve(ctx, pluginID, fixture.owner, fixture.name); err == nil {
			t.Fatalf("old-only key read new envelope in %s phase for %s", phase, fixture.name)
		}
		var scope string
		var createdBy int64
		if err := pool.QueryRow(ctx, `SELECT metadata->>'scope',created_by FROM plugin_secret_values WHERE plugin_id=$1 AND owner_user_id IS NOT DISTINCT FROM $2 AND secret_name=$3 AND status='active'`, pluginID, fixture.owner, fixture.name).Scan(&scope, &createdBy); err != nil || scope != fixture.scope || createdBy != userID {
			t.Fatalf("metadata or creator changed in %s phase for %s: err=%v", phase, fixture.name, err)
		}
	}
	assertRewrapFixtureRows(t, ctx, pool, pluginID)
	if phase == "restore" {
		if _, err := newOnly.Resolve(ctx, negativePluginID, nil, "missing.key"); err == nil {
			t.Fatal("restored old-key negative fixture opened without old key")
		}
	}
}

type rewrapIdentity struct {
	ID        int64
	PluginID  int64
	Owner     *int64
	Name      string
	Status    string
	Metadata  string
	CreatedBy *int64
	CreatedAt time.Time
	RotatedAt *time.Time
	RevokedAt *time.Time
}

func readRewrapIdentity(t *testing.T, ctx context.Context, pool *pgxpool.Pool, id int64) rewrapIdentity {
	t.Helper()
	var value rewrapIdentity
	if err := pool.QueryRow(ctx, `SELECT id,plugin_id,owner_user_id,secret_name,status,metadata::text,created_by,created_at,rotated_at,revoked_at FROM plugin_secret_values WHERE id=$1`, id).Scan(
		&value.ID, &value.PluginID, &value.Owner, &value.Name, &value.Status, &value.Metadata,
		&value.CreatedBy, &value.CreatedAt, &value.RotatedAt, &value.RevokedAt); err != nil {
		t.Fatal(err)
	}
	return value
}

func assertRewrapFixtureRows(t *testing.T, ctx context.Context, pool *pgxpool.Pool, pluginID int64) {
	t.Helper()
	var active, rotated, revoked, oldActive, newActive, oldHistory int
	if err := pool.QueryRow(ctx, `SELECT
		count(*) FILTER (WHERE status='active'),
		count(*) FILTER (WHERE status='rotated'),
		count(*) FILTER (WHERE status='revoked'),
		count(*) FILTER (WHERE status='active' AND key_version='old-v1'),
		count(*) FILTER (WHERE status='active' AND key_version='new-v2'),
		count(*) FILTER (WHERE status='rotated' AND key_version='old-v1')
		FROM plugin_secret_values WHERE plugin_id=$1`, pluginID).Scan(
		&active, &rotated, &revoked, &oldActive, &newActive, &oldHistory); err != nil {
		t.Fatal(err)
	}
	if active != 2 || rotated != 1 || revoked != 0 || oldActive != 0 || newActive != 2 || oldHistory != 1 {
		t.Fatalf("unexpected rewrap row counts: active=%d rotated=%d revoked=%d old-active=%d new-active=%d old-history=%d",
			active, rotated, revoked, oldActive, newActive, oldHistory)
	}
	var leaked int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM plugin_secret_values WHERE plugin_id=$1 AND (encode(ciphertext,'escape') LIKE '%fixture%' OR metadata::text LIKE '%fixture%')`, pluginID).Scan(&leaked); err != nil {
		t.Fatal(err)
	}
	if leaked != 0 {
		t.Fatal("plaintext fixture leaked into ciphertext or metadata")
	}
}
