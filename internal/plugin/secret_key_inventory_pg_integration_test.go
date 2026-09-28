package plugin

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/campusos/CampusOS/internal/platform/security"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPluginSecretKeyInventoryRejectsInvalidInput(t *testing.T) {
	store := NewPgAuthorizationStore(nil)
	for _, keyID := range []string{"", " old-v1", "old-v1 ", "old-v1\n", "old-v1\x00", strings.Repeat("x", 65)} {
		if _, err := store.InspectPluginSecretKeyReferences(context.Background(), keyID); err == nil {
			t.Fatalf("accepted invalid key ID %q", keyID)
		}
	}
	if _, err := store.InspectPluginSecretKeyReferences(context.Background(), "old-v1"); err == nil {
		t.Fatal("accepted unavailable PostgreSQL store")
	}
}

// This test runs only against the disposable PostgreSQL database created by
// the V12-01b key-inspection drill. It exercises a real read-only snapshot.
func TestPostgresPluginSecretKeyInventory(t *testing.T) {
	if os.Getenv("CAMPUSOS_SECRET_KEY_INSPECT_TEST_ISOLATED") != "1" {
		t.Skip("requires owned V12-01b key-inspection PostgreSQL database")
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
	t.Cleanup(pool.Close)
	var database string
	if err := pool.QueryRow(ctx, `SELECT current_database()`).Scan(&database); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(database, "campusos_v12_01b_key_inspect") {
		t.Fatalf("refusing non-isolated database %q", database)
	}

	const firstPluginID = int64(99126001)
	const secondPluginID = int64(99126002)
	userID := int64(99126003)
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := pool.Exec(cleanupCtx, `DELETE FROM plugins WHERE id IN ($1,$2)`, firstPluginID, secondPluginID); err != nil {
			t.Errorf("remove key-inspection plugin fixtures: %v", err)
		}
		if _, err := pool.Exec(cleanupCtx, `DELETE FROM users WHERE id=$1`, userID); err != nil {
			t.Errorf("remove key-inspection user fixture: %v", err)
		}
	})
	if _, err := pool.Exec(ctx, `INSERT INTO users(id,username,nickname,email) VALUES($1,'v12_key_inspect_owner','Key Inspect Owner','v12-key-inspect@example.invalid')`, userID); err != nil {
		t.Fatal(err)
	}
	repo := NewPgPluginRepository(pool)
	for _, fixture := range []struct {
		id   int64
		name string
	}{
		{firstPluginID, "v12-01b-key-inspect-first"},
		{secondPluginID, "v12-01b-key-inspect-second"},
	} {
		record := &PluginRecord{
			ID: fixture.id, Name: fixture.name, DisplayName: "V12 Key Inspect Fixture", Version: "1.0.0",
			Runtime: "process", Status: string(StatusRunning), BackendState: string(BackendRunning),
			FrontendState: string(FrontendUnloaded), HealthState: string(HealthHealthy), Config: `{}`,
			Checksum: strings.Repeat("a", 64), InstalledBy: "test", InstalledAt: time.Now(), UpdatedAt: time.Now(),
		}
		if err := repo.Save(ctx, record); err != nil {
			t.Fatal(err)
		}
	}

	store := NewPgAuthorizationStore(pool)
	oldKey := []byte("0123456789abcdef0123456789abcdef")
	newKey := []byte("abcdef0123456789abcdef0123456789")
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

	if _, err := oldOnly.Put(ctx, firstPluginID, nil, "system.active", "synthetic-old-active", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := oldOnly.Put(ctx, secondPluginID, &userID, "user.active", "synthetic-user-active", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := oldOnly.Put(ctx, firstPluginID, nil, "system.history", "synthetic-old-history", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := newOnly.Put(ctx, firstPluginID, nil, "system.history", "synthetic-new-active", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := oldOnly.Put(ctx, secondPluginID, nil, "system.revoked", "synthetic-old-revoked", nil); err != nil {
		t.Fatal(err)
	}
	if err := store.RevokeSecret(ctx, secondPluginID, nil, "system.revoked"); err != nil {
		t.Fatal(err)
	}

	oldBefore, err := store.InspectPluginSecretKeyReferences(ctx, "old-v1")
	if err != nil {
		t.Fatal(err)
	}
	assertPluginSecretKeyInventoryCounts(t, oldBefore, 4, 2, 1, 1, 0, 2)
	newBefore, err := store.InspectPluginSecretKeyReferences(ctx, "new-v2")
	if err != nil {
		t.Fatal(err)
	}
	assertPluginSecretKeyInventoryCounts(t, newBefore, 1, 1, 0, 0, 0, 1)

	for _, fixture := range []struct {
		pluginID int64
		owner    *int64
		name     string
	}{
		{firstPluginID, nil, "system.active"},
		{secondPluginID, &userID, "user.active"},
	} {
		if _, changed, err := rotating.RewrapActive(ctx, fixture.pluginID, fixture.owner, fixture.name); err != nil || !changed {
			t.Fatalf("rewrap active fixture: changed=%v err=%v", changed, err)
		}
	}
	oldAfter, err := store.InspectPluginSecretKeyReferences(ctx, "old-v1")
	if err != nil {
		t.Fatal(err)
	}
	assertPluginSecretKeyInventoryCounts(t, oldAfter, 2, 0, 1, 1, 0, 2)
	newAfter, err := store.InspectPluginSecretKeyReferences(ctx, "new-v2")
	if err != nil {
		t.Fatal(err)
	}
	assertPluginSecretKeyInventoryCounts(t, newAfter, 3, 3, 0, 0, 0, 2)
	if zero, err := store.InspectPluginSecretKeyReferences(ctx, "never-used"); err != nil {
		t.Fatal(err)
	} else {
		assertPluginSecretKeyInventoryCounts(t, zero, 0, 0, 0, 0, 0, 0)
	}
	// A key ID with SQL metacharacters is still treated as a bound value.
	if injected, err := store.InspectPluginSecretKeyReferences(ctx, "old-v1' OR true --"); err != nil {
		t.Fatal(err)
	} else {
		assertPluginSecretKeyInventoryCounts(t, injected, 0, 0, 0, 0, 0, 0)
	}
}

func assertPluginSecretKeyInventoryCounts(t *testing.T, got PluginSecretKeyInventory, total, active, rotated, revoked, other, plugins int64) {
	t.Helper()
	if got.SnapshotAt.IsZero() || got.TotalReferences != total || got.ActiveReferences != active ||
		got.RotatedReferences != rotated || got.RevokedReferences != revoked ||
		got.OtherReferences != other || got.PluginCount != plugins {
		t.Fatalf("unexpected key inventory counts: %+v", got)
	}
}
