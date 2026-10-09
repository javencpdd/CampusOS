package plugin

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/security"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPostgresSecretRotationWorkerRestore(t *testing.T) {
	if os.Getenv("CAMPUSOS_SECRET_ROTATION_TEST_ISOLATED") != "1" {
		t.Skip("requires owned rotation database")
	}
	ctx := t.Context()
	pool, err := pgxpool.New(ctx, os.Getenv("CAMPUSOS_PG_INTEGRATION_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var database string
	if err := pool.QueryRow(ctx, "SELECT current_database()").Scan(&database); err != nil || !strings.HasPrefix(database, "campusos_v12_01b_rotation") {
		t.Fatal("refusing non-isolated database")
	}
	phase := os.Getenv("CAMPUSOS_SECRET_ROTATION_TEST_PHASE")
	if phase != "seed" && phase != "restore" {
		t.Fatal("phase must be seed|restore")
	}
	store := NewPgAuthorizationStore(pool)
	oldKey, newKey := []byte("0123456789abcdef0123456789abcdef"), []byte("abcdef0123456789abcdef0123456789")
	old, err := NewSecretService(store, oldKey, "old-v1")
	if err != nil {
		t.Fatal(err)
	}
	current, err := NewSecretService(store, newKey, "new-v2")
	if err != nil {
		t.Fatal(err)
	}
	ring, err := security.NewKeyring("new-v2", map[string][]byte{"old-v1": oldKey, "new-v2": newKey})
	if err != nil {
		t.Fatal(err)
	}
	rotating, err := NewSecretServiceWithKeyring(store, ring)
	if err != nil {
		t.Fatal(err)
	}
	const badID, goodID, laterID int64 = 99261001, 99261002, 99261003
	owner := int64(99261004)
	if phase == "seed" {
		if _, err := pool.Exec(ctx, `INSERT INTO users(id,username,nickname,email) VALUES($1,'rotation-owner','Rotation owner','rotation-owner@example.invalid')`, owner); err != nil {
			t.Fatal(err)
		}
		for _, id := range []int64{badID, goodID, laterID} {
			if _, err := pool.Exec(ctx, `INSERT INTO plugins(id,name,display_name,version,runtime) VALUES($1,$2,'Rotation fixture','1.0.0','grpc')`, id, "rotation-"+strings.TrimPrefix(time.Unix(id, 0).UTC().Format("150405"), " ")); err != nil {
				t.Fatal(err)
			}
		}
		for _, fixture := range []struct {
			id          int64
			owner       *int64
			name, value string
		}{
			{badID, nil, "bad.token", "hidden-bad-value"}, {goodID, nil, "system.token", "historical-value"},
			{goodID, nil, "system.token", "system-value"}, {goodID, &owner, "user.token", "user-value"},
			{goodID, nil, "revoked.token", "revoked-value"}, {laterID, nil, "later.token", "later-value"},
		} {
			if _, err := old.Put(ctx, fixture.id, fixture.owner, fixture.name, fixture.value, nil); err != nil {
				t.Fatal(err)
			}
		}
		if err := old.Revoke(ctx, goodID, nil, "revoked.token"); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `UPDATE plugin_secret_values SET ciphertext='broken-envelope' WHERE plugin_id=$1`, badID); err != nil {
			t.Fatal(err)
		}
		before := rotationHistoryFingerprint(t, ctx, pool)
		locker := NewPgSecretRotationLocker(pool)
		held, ok, err := locker.TryAcquire(ctx)
		if err != nil || !ok {
			t.Fatal("could not acquire owned lease")
		}
		if err := held.Check(ctx); err != nil {
			t.Fatal(err)
		}
		worker, err := NewSecretRotationWorker(rotating, store, reliability.NewPostgreSQLStore(pool), locker, rotationTestConfig())
		if err != nil {
			t.Fatal(err)
		}
		busy, err := worker.RunOnce(ctx)
		if err != nil || !busy.LeaseBusy || busy.Rewrapped != 0 {
			t.Fatalf("busy lease: %+v %v", busy, err)
		}
		if err := held.Release(ctx); err != nil {
			t.Fatal(err)
		}
		failed := 0
		changed := 0
		for pass := 0; pass < 9; pass++ {
			result, err := worker.RunOnce(ctx)
			if err != nil || result.ScannedPlugins > 1 || result.Rewrapped > 1 {
				t.Fatalf("bounded pass: %+v %v", result, err)
			}
			failed += result.FailedPlugins
			changed += result.Rewrapped
		}
		if failed < 1 || changed != 3 {
			t.Fatalf("cross-plugin progress: failures=%d changed=%d", failed, changed)
		}
		if before != rotationHistoryFingerprint(t, ctx, pool) {
			t.Fatal("historical or revoked envelope changed")
		}
		guard, ok, err := locker.TryAcquire(ctx)
		if err != nil || !ok {
			t.Fatal("lease not released after worker")
		}
		pgGuard := guard.(*pgSecretRotationGuard)
		if _, err := pgGuard.conn.Exec(ctx, `SELECT pg_advisory_unlock($1::integer,$2::integer)`, secretRotationLockNamespace, secretRotationLockID); err != nil {
			t.Fatal(err)
		}
		if !errors.Is(guard.Check(ctx), ErrSecretRotationLease) || !errors.Is(guard.Release(ctx), ErrSecretRotationLease) {
			t.Fatal("lost session lease was accepted")
		}
		guard, ok, err = locker.TryAcquire(ctx)
		if err != nil || !ok {
			t.Fatal("uncertain connection retained lock")
		}
		if err := guard.Release(ctx); err != nil {
			t.Fatal(err)
		}
	}
	for _, fixture := range []struct {
		id          int64
		owner       *int64
		name, value string
	}{{goodID, nil, "system.token", "system-value"}, {goodID, &owner, "user.token", "user-value"}, {laterID, nil, "later.token", "later-value"}} {
		value, err := current.Resolve(ctx, fixture.id, fixture.owner, fixture.name)
		if err != nil || value != fixture.value {
			t.Fatal("current key could not read accepted fixture")
		}
	}
	inventory, err := store.InspectPluginSecretKeyReferences(ctx, "old-v1")
	if err != nil || inventory.ActiveReferences != 1 || inventory.RotatedReferences != 1 || inventory.RevokedReferences != 1 || inventory.TotalReferences != 3 {
		t.Fatalf("whole-table inventory: %+v %v", inventory, err)
	}
	var audits, failures, unsafe int
	if err := pool.QueryRow(ctx, `SELECT count(*),count(*) FILTER(WHERE status='failed'),count(*) FILTER(WHERE details::text LIKE '%token%' OR details::text LIKE '%value%' OR error_message LIKE '%hidden%') FROM platform_operation_runs WHERE kind=$1`, secretRotationOperationKind).Scan(&audits, &failures, &unsafe); err != nil || audits < 4 || failures < 1 || unsafe != 0 {
		t.Fatalf("audit: total=%d failure=%d unsafe=%d err=%v", audits, failures, unsafe, err)
	}
}
func rotationHistoryFingerprint(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()
	var result string
	if err := pool.QueryRow(ctx, `SELECT md5(string_agg(id::text||key_version||algorithm||nonce||ciphertext,'|' ORDER BY id)) FROM plugin_secret_values WHERE status<>'active'`).Scan(&result); err != nil {
		t.Fatal(err)
	}
	return result
}
