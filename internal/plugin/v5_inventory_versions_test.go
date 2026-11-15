package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestV5InventoryOneFamilyWithRetainedOldInstall(t *testing.T) {
	v, user := v5BackupFixture(t)
	ctx := context.Background()
	current := v5CheckpointCurrent(t, v)
	op := v.mutate(t, V5Mutation{Kind: "purge_release", IdempotencyKey: "archive-before-catalog"}, true, v.admin)
	v.process(t)
	if v.operation(t, op.ID).Status != "done" {
		t.Fatal("archive not complete")
	}
	page, err := v.s.Inventory(ctx, false, "", 1, v5TestProof("user", user.Owner))
	if err != nil || len(page.Items) != 1 || page.Next != "" {
		t.Fatal("old and current became separate plugin entries", page, err)
	}
	item := page.Items[0]
	if item.Release.Version != current.Version || item.Installation == nil || item.Installation.Version != user.Version {
		t.Fatal("current target or retained installation lost", item)
	}
	if item.Configuration != nil {
		t.Fatal("list exposed private configuration")
	}
	after, _ := v.s.installation(ctx, v.c, user.Owner)
	if after != user {
		t.Fatal("read changed the user's installation")
	}
	other, err := v.s.Inventory(ctx, false, "", 1, v5TestProof("user", "1002"))
	if err != nil || len(other.Items) != 1 || other.Items[0].Installation != nil {
		t.Fatal("another user's old installation leaked", err)
	}
	if _, err := v.s.Detail(ctx, v.c, false, v5TestProof("user", "1002")); err != ErrV5NotFound {
		t.Fatal("another user read old version details", err)
	}
	g, _ := v.s.grant(ctx, item.Release.V5Coordinate)
	_, err = v.s.Mutate(ctx, v.c.Publisher, v.c.Key, V5Mutation{Kind: "revoke", Version: current.Version, ExpectedRevision: g.Revision, IdempotencyKey: "withdraw-current"}, true, v.admin)
	if err != nil {
		t.Fatal(err)
	}
	page, err = v.s.Inventory(ctx, false, "", 1, v5TestProof("user", user.Owner))
	if err != nil || len(page.Items) != 1 || page.Items[0].Release.Version != user.Version {
		t.Fatal("owner lost cleanup fallback after site withdrawal", err)
	}
}

func v5InventoryFamilyStoreContract(t *testing.T, ctx context.Context, store V5StateStore) {
	t.Helper()
	putInstall := func(kind v5RecordKind, key, version, owner string) {
		i := V5Installation{Publisher: "campusos", Key: key, Version: version, DesiredVersion: version, Owner: owner, Installed: true, Desired: "enabled", Observed: "stopped"}
		if err := store.save(ctx, kind, v5StateID(i.coordinate(), owner), i); err != nil {
			t.Fatal(err)
		}
	}
	putGrant := func(key, version string, approved bool) {
		g := V5Grant{V5Coordinate: V5Coordinate{"campusos", key, version}, Approved: approved}
		if err := store.save(ctx, v5Grant, g.id(), g); err != nil {
			t.Fatal(err)
		}
	}
	for _, key := range []string{"aaa", "aaa-extra"} {
		putInstall(v5Global, key, "10.0.0", "")
		putGrant(key, "10.0.0", true)
		putInstall(v5User, key, "2.0.0", "1001")
	}
	putInstall(v5Global, "zzz-retained", "3.0.0", "")
	putGrant("zzz-retained", "3.0.0", false)
	putInstall(v5User, "zzz-retained", "1.0.0", "1001")
	putInstall(v5User, "zzz-retained", "9.0.0", "1002")
	putInstall(v5User, "aaa-hidden", "1.0.0", "1002")
	after := ""
	for n, want := range []struct{ key, version string }{{"aaa", "10.0.0"}, {"aaa-extra", "10.0.0"}, {"zzz-retained", "1.0.0"}} {
		rows, err := store.list(ctx, v5Global, v5Query{VisibleOwner: "1001", Now: 1000, Limit: 1, After: after})
		if err != nil || len(rows) != 1 {
			t.Fatal("unexpected canonical page", n, len(rows), err)
		}
		var i V5Installation
		if json.Unmarshal(rows[0], &i) != nil || i.Key != want.key || i.Version != want.version || i.Owner == "1002" {
			t.Fatal("duplicate, wrong version or other owner in catalog", i)
		}
		after = i.coordinate().family()
		if n == 0 {
			putInstall(v5Global, "aaa", "11.0.0", "")
			putGrant("aaa", "11.0.0", true)
		}
	}
	rows, err := store.list(ctx, v5Global, v5Query{VisibleOwner: "1001", Now: 1000, Limit: 1, After: after})
	if err != nil || len(rows) != 0 {
		t.Fatal("catalog page repeated a version", err)
	}
}

func TestV5InventoryFamilyPaginationMemory(t *testing.T) {
	v5InventoryFamilyStoreContract(t, context.Background(), NewMemoryV5StateStore())
}

func TestPostgresV5InventoryFamilyPagination(t *testing.T) {
	dsn := os.Getenv("CAMPUSOS_V5_INVENTORY_DATABASE_URL")
	if dsn == "" {
		t.Skip("requires owned inventory query database")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil || !strings.HasPrefix(cfg.ConnConfig.Database, "campusos_v5_inventory_exit_") {
		t.Fatal("dedicated inventory database required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal("owned database unavailable")
	}
	defer pool.Close()
	err = transaction.NewPostgreSQL(pool).Within(ctx, func(tx context.Context) error {
		// Transaction-local query fixtures exercise production SQL without
		// rewriting package identity, account or lifecycle facts.
		for _, kind := range []v5RecordKind{v5Global, v5User, v5Grant} {
			_, err := transaction.ExecutorFor(tx, pool).Exec(tx, fmt.Sprintf(`CREATE TEMP TABLE %s (
			 record_id text PRIMARY KEY,state jsonb NOT NULL,
			 publisher text COLLATE "C" GENERATED ALWAYS AS(state->>'publisher') STORED,
			 plugin_key text COLLATE "C" GENERATED ALWAYS AS(state->>'plugin_key') STORED,
			 version text COLLATE "C" GENERATED ALWAYS AS(state->>'version') STORED) ON COMMIT DROP`, kind))
			if err != nil {
				return err
			}
		}
		v5InventoryFamilyStoreContract(t, tx, NewPgV5StateStore(pool))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
