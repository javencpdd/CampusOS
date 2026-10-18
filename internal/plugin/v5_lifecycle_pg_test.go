package plugin

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	identity "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	identityservice "github.com/campusos/CampusOS/internal/modules/core/identity/service"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/jackc/pgx/v5/pgxpool"
)

func v5OwnedPool(t *testing.T) (context.Context, *pgxpool.Pool) {
	t.Helper()
	dsn := os.Getenv("CAMPUSOS_V5_DATABASE_URL")
	if dsn == "" {
		t.Skip("requires owned V12-03b exit database")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil || cfg.ConnConfig.Database != "campusos_v12_03b_exit" {
		t.Fatal("requires exact owned exit database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal("owned database unavailable")
	}
	t.Cleanup(pool.Close)
	return ctx, pool
}
func v5PGAdmin(t *testing.T, ctx context.Context, pool *pgxpool.Pool) (identity.AdminExecutionAuthorityReader, identity.SessionPrincipalResolver, string) {
	t.Helper()
	id := "adm.v5." + v5RandomID()[:12]
	admins := repository.NewPgAdminDomainRepository(pool)
	sessions := repository.NewPgAdminSessionRepository(pool)
	if err := admins.CreateAccount(ctx, repository.AdminDomainAccount{ID: id, Username: id, Email: id + "@example.test", DisplayName: "V5 owned target"}); err != nil {
		t.Fatal(err)
	}
	if err := admins.BindRole(ctx, repository.AdminDomainRoleBinding{ID: id + ".role", AccountID: id, RoleID: "security_admin", GrantedBy: id}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	mfa := now.Add(-time.Minute)
	sid := id + ".session"
	if err := sessions.CreateSession(ctx, repository.AdminSession{ID: sid, AccountID: id, RefreshTokenDigest: fmt.Sprintf("%x", sha256.Sum256([]byte(sid))), TokenFamilyID: id, AuthenticationStrength: "mfa", MFAAuthenticatedAt: &mfa, CreatedAt: now, UpdatedAt: now, LastActiveAt: now, ExpiresAt: now.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	jwt := auth.NewJWTManager(auth.JWTConfig{Secret: "owned-v5-pg-signing-fixture", Issuer: "v5-pg", Audience: "admin", AccessTTL: time.Hour})
	signed, err := jwt.GenerateAccessToken(id, id, auth.AccessTokenContext{SessionID: sid, AuthVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	claims, err := jwt.VerifyAccessToken(signed)
	if err != nil {
		t.Fatal(err)
	}
	current := identityservice.NewAdminSessionService(admins, sessions, jwt)
	return identityservice.NewBoardGovernanceAuthorityService(nil, admins, current), func(ctx context.Context) (identity.PrincipalContext, error) {
		return identity.ResolveAdminSessionPrincipal(ctx, current, claims)
	}, sid
}

func TestPostgresV5LifecycleAtomicityAndRecovery(t *testing.T) {
	ctx, pool := v5OwnedPool(t)
	fixture := newV5TestPlatform(t)
	admins, proof, sid := v5PGAdmin(t, ctx, pool)
	// Every target invocation gets a new family, while files/DB remain durable.
	manifest := fixture.manifest
	manifest.Key = "target-" + v5RandomID()[:12]
	wire, _ := json.Marshal(manifest)
	if err := os.WriteFile(filepath.Join(fixture.dir, "plugin.json"), wire, 0600); err != nil {
		t.Fatal(err)
	}
	raw, p, err := BuildPluginV5Package(fixture.dir)
	if err != nil {
		t.Fatal(err)
	}
	priv, trust := testV5Trust(t)
	envelope, err := SignPluginV5Package(raw, "test-key", priv)
	if err != nil {
		t.Fatal(err)
	}
	c := V5Coordinate{p.Manifest.Publisher, p.Manifest.Key, p.Manifest.Version}
	work := os.Getenv("CAMPUSOS_V5_OUTPUT")
	if work == "" {
		t.Fatal("owned output required")
	}
	if err := os.MkdirAll(work, 0700); err != nil {
		t.Fatal(err)
	}
	files, err := NewV5ManagedFiles(filepath.Join(work, "managed"))
	if err != nil {
		t.Fatal(err)
	}
	defer files.Close()
	catalog, err := NewPluginV5PackageCatalogWithIdentityStore(trust, NewPgPluginV5IdentityStore(pool))
	if err != nil {
		t.Fatal(err)
	}
	fault := &v5ReliableFault{Store: reliability.NewPostgreSQLStore(pool)}
	reliable := reliability.NewService(transaction.NewPostgreSQL(pool), fault)
	store := NewPgV5StateStore(pool)
	s := NewV5Platform(store, files, catalog, reliable, admins)
	importOp, err := s.Import(ctx, raw, envelope, "import-"+c.Key, proof)
	if err != nil {
		t.Fatal("pg import", err)
	}
	command := func(r V5Mutation) V5Operation {
		t.Helper()
		r.Version = c.Version
		if r.IdempotencyKey == "" {
			r.IdempotencyKey = v5RandomID()
		}
		op, err := s.Mutate(ctx, c.Publisher, c.Key, r, true, proof)
		if err != nil {
			t.Fatal(r.Kind, err)
		}
		return op
	}
	install := command(V5Mutation{Kind: "install"})
	command(V5Mutation{Kind: "grant", Approved: true, Consumes: []PluginV5Consume{}})
	i, _ := s.installation(ctx, c, "")
	t.Run("sql-atomic-receipts", func(t *testing.T) {
		for _, failure := range []string{"audit", "outbox"} {
			fault.fail = failure
			r := V5Mutation{Kind: "enable", Version: c.Version, ExpectedGeneration: i.Generation, IdempotencyKey: "fault-" + failure + c.Key}
			if _, err := s.Mutate(ctx, c.Publisher, c.Key, r, true, proof); err == nil {
				t.Fatal("receipt failure ignored")
			}
			after, _ := s.installation(ctx, c, "")
			if after != i {
				t.Fatal("partial SQL mutation")
			}
			if _, err := store.operation(ctx, "admin", importOp.ActorID, r.IdempotencyKey); err != ErrV5NotFound {
				t.Fatal("partial operation")
			}
		}
		fault.fail = ""
		// A deferred constraint rejects the real transaction COMMIT, after action,
		// audit and Outbox were written. No callback-only simulation is used.
		_, err := pool.Exec(ctx, `CREATE FUNCTION v5_drill_commit_failure() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.state->>'idempotency_key' LIKE 'commit-fail-%' THEN RAISE EXCEPTION 'owned commit fault'; END IF; RETURN NEW; END $$; CREATE CONSTRAINT TRIGGER v5_drill_commit_failure AFTER INSERT ON plugin_operations DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION v5_drill_commit_failure()`)
		if err != nil {
			t.Fatal(err)
		}
		_, err = s.Mutate(ctx, c.Publisher, c.Key, V5Mutation{Kind: "enable", Version: c.Version, ExpectedGeneration: i.Generation, IdempotencyKey: "commit-fail-" + c.Key}, true, proof)
		if _, e := pool.Exec(ctx, `DROP TRIGGER v5_drill_commit_failure ON plugin_operations; DROP FUNCTION v5_drill_commit_failure()`); e != nil {
			t.Fatal(e)
		}
		if err == nil {
			t.Fatal("commit fault ignored")
		}
		after, _ := s.installation(ctx, c, "")
		if after != i {
			t.Fatal("commit fault changed state")
		}
	})
	t.Run("independent-hosts-cas", func(t *testing.T) {
		var wins atomic.Int32
		var wg sync.WaitGroup
		for n := 0; n < 12; n++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				cat, e := NewPluginV5PackageCatalogWithIdentityStore(trust, NewPgPluginV5IdentityStore(pool))
				if e != nil {
					t.Error(e)
					return
				}
				other := NewV5Platform(NewPgV5StateStore(pool), files, cat, reliable, admins)
				_, e = other.Mutate(ctx, c.Publisher, c.Key, V5Mutation{Kind: "enable", Version: c.Version, ExpectedGeneration: i.Generation, IdempotencyKey: v5RandomID()}, true, proof)
				if e == nil {
					wins.Add(1)
				} else if e != ErrV5Conflict {
					t.Error(e)
				}
			}()
		}
		wg.Wait()
		if wins.Load() != 1 {
			t.Fatal("CAS winners", wins.Load())
		}
	})
	var op V5Operation
	t.Run("durable-lease-and-prepare", func(t *testing.T) {
		s.now = func() time.Time { return time.Now().Add(time.Minute) }
		if _, err := s.ProcessOnce(ctx, 50); err != nil {
			t.Fatal(err)
		}
		rows, err := store.list(ctx, v5Operation, v5Query{Publisher: c.Publisher, Key: c.Key, Limit: 50})
		if err != nil {
			t.Fatal(err)
		}
		for _, raw := range rows {
			var candidate V5Operation
			json.Unmarshal(raw, &candidate)
			if candidate.Kind == "enable" && candidate.Status == "waiting" {
				op = candidate
			}
		}
		if op.ID == "" || op.Preparation == nil {
			t.Fatal("durable preparation missing")
		}
		if err := s.Observe(ctx, V5Observation{op.ID, op.Lease, op.Generation, op.Preparation.ConfigDigest, true}); err != nil {
			t.Fatal("host-only contract observation", err)
		}
		facts, err := s.ExecutionFacts(ctx, c, "")
		if err != nil || !facts.ReadyForDispatch {
			t.Fatal(facts, err)
		}
		if err := s.ReconcileStartup(ctx); err != nil {
			t.Fatal(err)
		}
		facts, err = s.ExecutionFacts(ctx, c, "")
		if err != nil || facts.ReadyForDispatch || facts.Installation.Generation <= op.Generation {
			t.Fatal("restart resurrected old generation", facts, err)
		}
	})
	t.Run("sql-invariants", func(t *testing.T) {
		for _, query := range []string{`DELETE FROM plugin_operations`, `TRUNCATE plugin_v5_admin_grants`, `UPDATE plugin_operations SET state=jsonb_set(state,'{request_hash}',to_jsonb('sha256:'||repeat('f',64)))`, `UPDATE plugin_installations SET state=jsonb_set(state,'{generation}','"0"')`, `UPDATE plugin_package_releases SET state=jsonb_set(state,'{package_digest}',to_jsonb('sha256:'||repeat('f',64)))`} {
			if _, err := pool.Exec(ctx, query); err == nil {
				t.Fatal("invariant bypass", query)
			}
		}
	})
	t.Run("current-mfa-and-revocation", func(t *testing.T) {
		if _, err := pool.Exec(ctx, `UPDATE admin_sessions SET mfa_authenticated_at=now()-interval '1 day' WHERE id=$1`, sid); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Mutate(ctx, c.Publisher, c.Key, V5Mutation{Kind: "disable", Version: c.Version, ExpectedGeneration: install.Generation, IdempotencyKey: "expired-mfa" + c.Key}, true, proof); err == nil {
			t.Fatal("old MFA accepted")
		}
		if _, err := s.Inventory(ctx, true, "", 25, proof); err != nil {
			t.Fatal("read incorrectly requires MFA", err)
		}
		if _, err := pool.Exec(ctx, `UPDATE admin_sessions SET revoked_at=now(),revoke_reason='owned target' WHERE id=$1`, sid); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Inventory(ctx, true, "", 25, proof); err == nil {
			t.Fatal("revoked session admitted")
		}
	})
	for name, raw := range map[string][]byte{"coordinate.json": mustV5JSON(c), "trust.json": trust, "package.tar.gz": raw, "release.json": envelope} {
		if err := os.WriteFile(filepath.Join(work, name), raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
}
func mustV5JSON(v any) []byte { b, _ := json.Marshal(v); return b }
func TestPostgresV5Restore(t *testing.T) {
	ctx, pool := v5OwnedPool(t)
	work := os.Getenv("CAMPUSOS_V5_OUTPUT")
	raw, err := os.ReadFile(filepath.Join(work, "coordinate.json"))
	if err != nil {
		t.Fatal(err)
	}
	var c V5Coordinate
	if json.Unmarshal(raw, &c) != nil {
		t.Fatal("coordinate")
	}
	trust, err := os.ReadFile(filepath.Join(work, "trust.json"))
	if err != nil {
		t.Fatal(err)
	}
	cat, err := NewPluginV5PackageCatalogWithIdentityStore(trust, NewPgPluginV5IdentityStore(pool))
	if err != nil {
		t.Fatal(err)
	}
	files, err := NewV5ManagedFiles(filepath.Join(work, "managed"))
	if err != nil {
		t.Fatal(err)
	}
	defer files.Close()
	s := NewV5Platform(NewPgV5StateStore(pool), files, cat, reliability.NewService(transaction.NewPostgreSQL(pool), reliability.NewPostgreSQLStore(pool)), nil)
	i, err := s.installation(ctx, c, "")
	if err != nil || i.Desired != "enabled" || i.Observed != "pending" {
		t.Fatal("restart facts", i, err)
	}
	if _, err := s.ProcessOnce(ctx, 50); err != nil {
		t.Fatal(err)
	}
	facts, err := s.ExecutionFacts(ctx, c, "")
	if err != nil || facts.ReadyForDispatch {
		t.Fatal("no production runtime observer", facts, err)
	}
}
