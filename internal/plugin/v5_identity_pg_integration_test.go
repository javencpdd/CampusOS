package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func v5IdentityOwnedPool(t *testing.T) (context.Context, *pgxpool.Pool) {
	t.Helper()
	dsn := os.Getenv("CAMPUSOS_V5_IDENTITY_DATABASE_URL")
	if dsn == "" {
		t.Skip("requires owned V12-03b identity PostgreSQL database")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil || cfg.ConnConfig.Database != "campusos_v12_03b_identities" {
		t.Fatal("requires exact owned identity database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal("owned identity database unavailable")
	}
	t.Cleanup(pool.Close)
	return ctx, pool
}

func v5IdentityProcessChecks(t *testing.T, phase string) {
	t.Helper()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, decision := range []string{"record", "conflict", "revoked"} {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		cmd := exec.CommandContext(ctx, binary, "-test.run=^TestPostgresPluginV5IdentityChild$", "-test.count=1", "-test.v")
		cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "TMPDIR=" + os.Getenv("TMPDIR"), "CAMPUSOS_V5_IDENTITY_DATABASE_URL=" + os.Getenv("CAMPUSOS_V5_IDENTITY_DATABASE_URL"), "CAMPUSOS_V5_IDENTITY_OUTPUT=" + os.Getenv("CAMPUSOS_V5_IDENTITY_OUTPUT"), "CAMPUSOS_V5_IDENTITY_CHILD=" + decision}
		out, runErr := cmd.CombinedOutput()
		cancel()
		if runErr != nil {
			t.Fatalf("fresh %s process: %s", decision, out)
		}
		if err := os.WriteFile(filepath.Join(os.Getenv("CAMPUSOS_V5_IDENTITY_OUTPUT"), phase+"-"+decision+".log"), out, 0600); err != nil {
			t.Fatal(err)
		}
	}
}

// Invoked only as an actual, fresh native process by the parent target tests.
func TestPostgresPluginV5IdentityChild(t *testing.T) {
	decision := os.Getenv("CAMPUSOS_V5_IDENTITY_CHILD")
	if decision == "" {
		t.Skip("internal target child")
	}
	ctx, pool := v5IdentityOwnedPool(t)
	work := os.Getenv("CAMPUSOS_V5_IDENTITY_OUTPUT")
	read := func(name string) []byte {
		raw, err := os.ReadFile(filepath.Join(work, name))
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	trust := read("trust.json")
	name := "first"
	if decision == "conflict" {
		name = "second"
	} else if decision == "revoked" {
		trust = bytes.Replace(trust, []byte(`"active"`), []byte(`"revoked"`), 1)
	} else if decision != "record" {
		t.Fatal("invalid target decision")
	}
	c, err := NewPluginV5PackageCatalogWithIdentityStore(trust, NewPgPluginV5IdentityStore(pool))
	if err != nil {
		t.Fatal(err)
	}
	p, err := c.VerifyAndRecordContext(ctx, read(name+".tar.gz"), read(name+".release.json"))
	want := map[string]error{"record": nil, "conflict": ErrPluginV5PackageConflict, "revoked": ErrPluginV5PackageSignature}[decision]
	if !errors.Is(err, want) || (want == nil) != (p != nil) {
		t.Fatalf("fresh process decision %s incorrect: %v", decision, err)
	}
}

func TestPostgresPluginV5IdentityDurability(t *testing.T) {
	ctx, pool := v5IdentityOwnedPool(t)
	work := os.Getenv("CAMPUSOS_V5_IDENTITY_OUTPUT")
	if work == "" || os.MkdirAll(work, 0700) != nil {
		t.Fatal("owned output required")
	}
	dir := testV5PackageSource(t, 0)
	priv, trust := testV5Trust(t)
	write := func(name string, raw []byte) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(work, name), raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("trust.json", trust) // public key only; private key never leaves memory
	for _, name := range []string{"first", "second"} {
		if name == "second" {
			if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("different package bytes"), 0600); err != nil {
				t.Fatal(err)
			}
		}
		raw, _, err := BuildPluginV5Package(dir)
		if err != nil {
			t.Fatal(err)
		}
		envelope, err := SignPluginV5Package(raw, "test-key", priv)
		if err != nil {
			t.Fatal(err)
		}
		write(name+".tar.gz", raw)
		write(name+".release.json", envelope)
	}
	t.Run("fresh-processes", func(t *testing.T) { v5IdentityProcessChecks(t, "before-restart") })
	t.Run("database-immutability", func(t *testing.T) {
		for _, sql := range []string{
			`UPDATE plugin_package_identities SET package_digest='sha256:'||repeat('f',64)`,
			`UPDATE plugin_package_identities SET version='99.0.0'`,
			`DELETE FROM plugin_package_identities`,
			`TRUNCATE plugin_package_identities`,
			`INSERT INTO plugin_package_identities VALUES('X','demo','1.0.0','sha256:'||repeat('a',64),now())`,
			`INSERT INTO plugin_package_identities VALUES('campusos','demo','01.0.0','sha256:'||repeat('a',64),now())`,
			`INSERT INTO plugin_package_identities VALUES('campusos','demo','1.0.0',repeat('a',64),now())`,
		} {
			if _, err := pool.Exec(ctx, sql); err == nil {
				t.Fatal("database accepted identity mutation or invalid input")
			}
		}
		var count int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM plugin_package_identities`).Scan(&count); err != nil || count != 1 {
			t.Fatal("rejected SQL changed the ledger")
		}
	})
	// Prepare two signed candidates for a previously unseen identity.
	manifest, err := os.ReadFile(filepath.Join(dir, "plugin.json"))
	if err != nil {
		t.Fatal(err)
	}
	m, err := ParsePluginV5Manifest(manifest)
	if err != nil {
		t.Fatal(err)
	}
	m.Version = "9.1.0-race"
	manifest, _ = json.Marshal(m)
	if err := os.WriteFile(filepath.Join(dir, "plugin.json"), manifest, 0600); err != nil {
		t.Fatal(err)
	}
	first, _, err := BuildPluginV5Package(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("racing other bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	second, _, err := BuildPluginV5Package(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Run("independent-catalog-race", func(t *testing.T) {
		var wg sync.WaitGroup
		var mu sync.Mutex
		accepted := map[string]int{}
		conflicts := 0
		start := make(chan struct{})
		for i := 0; i < 16; i++ {
			raw := first
			if i%2 == 1 {
				raw = second
			}
			envelope, err := SignPluginV5Package(raw, "test-key", priv)
			if err != nil {
				t.Fatal(err)
			}
			c, err := NewPluginV5PackageCatalogWithIdentityStore(trust, NewPgPluginV5IdentityStore(pool))
			if err != nil {
				t.Fatal(err)
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				p, err := c.VerifyAndRecordContext(ctx, raw, envelope)
				mu.Lock()
				defer mu.Unlock()
				if err == ErrPluginV5PackageConflict {
					conflicts++
				} else if err != nil || p == nil {
					t.Error("concurrent publication unavailable", err)
				} else {
					accepted[p.Digest]++
				}
			}()
		}
		close(start)
		wg.Wait()
		if len(accepted) != 1 || conflicts != 8 {
			t.Fatal("database did not choose exactly one immutable digest")
		}
		for _, n := range accepted {
			if n != 8 {
				t.Fatal("identical publication was not idempotent")
			}
		}
	})
	t.Run("commit-failure", func(t *testing.T) {
		if _, err := pool.Exec(ctx, `CREATE FUNCTION v12_03b_reject_commit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected identity commit rejection'; END $$;
		CREATE CONSTRAINT TRIGGER v12_03b_commit_failure AFTER INSERT ON plugin_package_identities DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION v12_03b_reject_commit()`); err != nil {
			t.Fatal(err)
		}
		defer func() {
			if _, err := pool.Exec(ctx, `DROP TRIGGER v12_03b_commit_failure ON plugin_package_identities; DROP FUNCTION v12_03b_reject_commit()`); err != nil {
				t.Error(err)
			}
		}()
		id := PluginV5PackageIdentity{m.Publisher, m.Key, "9.1.0-commit", v5BytesDigest(first)}
		if err := NewPgPluginV5IdentityStore(pool).RecordPluginV5Identity(ctx, id); err != ErrPluginV5IdentityUnavailable {
			t.Fatal("failed commit reported success or leaked internal error", err)
		}
		var count int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM plugin_package_identities WHERE version=$1`, id.Version).Scan(&count); err != nil || count != 0 {
			t.Fatal("failed commit left an identity")
		}
	})
	t.Run("cancellation-and-closed-pool", func(t *testing.T) {
		canceled, cancel := context.WithCancel(ctx)
		cancel()
		store := NewPgPluginV5IdentityStore(pool)
		id := PluginV5PackageIdentity{m.Publisher, m.Key, "9.1.0-cancel", v5BytesDigest(first)}
		if err := store.RecordPluginV5Identity(canceled, id); err != ErrPluginV5IdentityUnavailable {
			t.Fatal("cancellation accepted")
		}
		pool.Close()
		c, err := NewPluginV5PackageCatalogWithIdentityStore(trust, store)
		if err != nil {
			t.Fatal(err)
		}
		envelope, err := SignPluginV5Package(first, "test-key", priv)
		if err != nil {
			t.Fatal(err)
		}
		if p, err := c.VerifyAndRecordContext(ctx, first, envelope); p != nil || err != ErrPluginV5IdentityUnavailable {
			t.Fatal("closed database fell back to cached or memory acceptance")
		}
	})
}

func TestPostgresPluginV5IdentityRestore(t *testing.T) {
	ctx, pool := v5IdentityOwnedPool(t)
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM plugin_package_identities`).Scan(&count); err != nil {
		t.Fatal("identity recovery database connection unavailable")
	}
	if count != 2 {
		t.Fatal("identity rows missing after PostgreSQL restart")
	}
	v5IdentityProcessChecks(t, "after-restart")
}
