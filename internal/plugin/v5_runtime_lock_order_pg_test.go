package plugin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// A negative admission fixture, not a synthetic ready Runner: the global
// installation stays disabled and no factory or guest is started. A real PG
// account row is held while runtimeSpec is evaluated. It must not hold the
// family advisory lock while waiting for that row (HTTP verifies user first).
func TestV5RuntimeSpecLockOrderActualPostgres(t *testing.T) {
	dsn, input, output := os.Getenv("CAMPUSOS_V5_LOCK_DATABASE_URL"), os.Getenv("CAMPUSOS_V5_LOCK_INPUT"), os.Getenv("CAMPUSOS_V5_LOCK_OUTPUT")
	if dsn == "" {
		t.Skip("requires owned lock-order PostgreSQL target")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil || cfg.ConnConfig.Database != "campusos_v12_08a_lock" || !strings.Contains(filepath.ToSlash(input), "/.cache/v12-08a-ecosystem/acceptance.") || output == "" {
		t.Fatal("owned lock-order target required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	read := func(n string) []byte {
		b, e := os.ReadFile(filepath.Join(input, n))
		if e != nil {
			t.Fatal(e)
		}
		return b
	}
	h := NewV5AuthorTestHarness(t, ctx, pool, read("echo-1.1.0.tar.gz"), read("echo-1.1.0.release.json"), read("trust.json"))
	c := h.Coordinate
	if _, err := h.Platform.Mutate(ctx, c.Publisher, c.Key, V5Mutation{Kind: "install", Version: c.Version, IdempotencyKey: "lock-install"}, true, h.Admin); err != nil {
		t.Fatal(err)
	}
	p, err := VerifyPluginV5Release(read("echo-1.1.0.tar.gz"), read("echo-1.1.0.release.json"), read("trust.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.Platform.Mutate(ctx, c.Publisher, c.Key, V5Mutation{Kind: "grant", Version: c.Version, Approved: true, Consumes: p.Manifest.Consumes, Resources: p.Manifest.HostResources, IdempotencyKey: "lock-grant"}, true, h.Admin); err != nil {
		t.Fatal(err)
	}
	principal, err := h.User(ctx)
	if err != nil {
		t.Fatal(err)
	}
	owner := principal.Actor.ID
	i := V5Installation{Publisher: c.Publisher, Key: c.Key, Version: c.Version, DesiredVersion: c.Version, Owner: owner, Installed: true, Desired: "enabled", Observed: "stopped", Generation: 1}
	op := V5Operation{V5Coordinate: c, ID: v5RandomID(), Owner: owner, ActorKind: "user", ActorID: owner, IdempotencyKey: "runtime-lock-fixture", RequestHash: v5BytesDigest([]byte("runtime-lock-fixture")), Kind: "enable", Status: "waiting", Generation: 1, Lease: 1, LeaseUntil: time.Now().Add(30 * time.Second).UnixMilli()}
	if err := h.Platform.reliable.CheckedRead(ctx, func(tx context.Context) error {
		if err := h.Platform.store.lock(tx, c.family()); err != nil {
			return err
		}
		if err := h.Platform.saveInstall(tx, &i); err != nil {
			return err
		}
		return h.Platform.saveOp(tx, &op)
	}); err != nil {
		t.Fatal(err)
	}
	held, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Rollback(context.Background())
	if _, err = held.Exec(ctx, `SELECT id FROM users WHERE id=$1 FOR UPDATE`, owner); err != nil {
		t.Fatal(err)
	}
	var heldPID int
	if err = held.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&heldPID); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := h.Platform.runtimeSpec(ctx, op); done <- err }()
	defer func() {
		_ = held.Rollback(context.Background())
		cancel()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Error("runtimeSpec did not release after account barrier")
		}
	}()
	until := time.Now().Add(3 * time.Second)
	for {
		var waiting int
		err = pool.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND pid<>$1 AND wait_event_type='Lock' AND query LIKE '%FROM users%' AND query LIKE '%FOR UPDATE%'`, heldPID).Scan(&waiting)
		if err != nil {
			t.Fatal(err)
		}
		if waiting > 0 {
			break
		}
		select {
		case err := <-done:
			done <- err
			t.Fatal("runtimeSpec did not wait for current account row", err)
		default:
		}
		if time.Now().After(until) {
			t.Fatal("account lock wait not observed")
		}
		time.Sleep(10 * time.Millisecond)
	}
	probe, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var free bool
	err = probe.QueryRow(ctx, `SELECT pg_try_advisory_xact_lock(hashtextextended($1,0))`, "plugin-v5:"+c.family()).Scan(&free)
	_ = probe.Rollback(context.Background())
	if err != nil || !free {
		t.Fatal("runtimeSpec holds family lock while waiting for account row; inverses current HTTP authority order", err)
	}
	_ = held.Rollback(context.Background())
	if err := <-done; err != ErrV5Denied {
		done <- err
		t.Fatal("disabled global installation must still deny admission", err)
	} else {
		done <- err
	}
	wire, _ := json.MarshalIndent(map[string]any{"status": "passed", "real_pg_account_wait_observed": true, "family_lock_free_while_account_waits": true, "disabled_global_still_denied": true, "synthetic_ready": false, "guest_started": false}, "", "  ")
	if err := os.WriteFile(filepath.Join(output, "lock-order-proof.json"), append(wire, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
}
