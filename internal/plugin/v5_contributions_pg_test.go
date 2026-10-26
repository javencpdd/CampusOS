package plugin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/jackc/pgx/v5/pgxpool"
)

func v5ContributionOwnedPool(t *testing.T) (context.Context, *pgxpool.Pool, string) {
	t.Helper()
	dsn, work := os.Getenv("CAMPUSOS_V5_CONTRIBUTION_DATABASE_URL"), os.Getenv("CAMPUSOS_V5_CONTRIBUTION_OUTPUT")
	if dsn == "" {
		t.Skip("requires owned03c contribution database")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil || cfg.ConnConfig.Database != "campusos_v12_03c_directory" || work == "" {
		t.Fatal("requires exact owned03c database/output")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal("owned database unavailable")
	}
	t.Cleanup(pool.Close)
	if err := os.MkdirAll(work, 0700); err != nil {
		t.Fatal(err)
	}
	return ctx, pool, work
}

func v5ContributionPGReady(t *testing.T, ctx context.Context, s *V5Platform, op V5Operation) {
	t.Helper()
	if _, err := s.ProcessOnce(ctx, 50); err != nil {
		t.Fatal(err)
	}
	var current V5Operation
	if err := s.store.load(ctx, v5Operation, op.ID, &current); err != nil || current.Status != "waiting" || current.Preparation == nil {
		t.Fatal("PG bounded host preparation", err, current.Status)
	}
	if err := s.Observe(ctx, V5Observation{current.ID, current.Lease, current.Generation, current.Preparation.ConfigDigest, true}); err != nil {
		t.Fatal("PG bounded host observation", err)
	}
}

func TestPostgresV5ContributionDirectory(t *testing.T) {
	ctx, pool, work := v5ContributionOwnedPool(t)
	v, priv := newV5ContributionTestPlatform(t)
	trust := append([]byte(nil), v.s.catalog.trust...)
	admins, proof, _ := v5PGAdmin(t, ctx, pool)
	files, err := NewV5ManagedFiles(filepath.Join(work, "managed"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = files.Close() })
	catalog, err := NewPluginV5PackageCatalogWithIdentityStore(trust, NewPgPluginV5IdentityStore(pool))
	if err != nil {
		t.Fatal(err)
	}
	v.s = NewV5Platform(NewPgV5StateStore(pool), files, catalog, reliability.NewService(transaction.NewPostgreSQL(pool), reliability.NewPostgreSQLStore(pool)), admins)
	v.admin = proof
	i := v5ContributionApproved(t, v)
	v5ContributionPGReady(t, ctx, v.s, v.mutate(t, V5Mutation{Kind: "enable", ExpectedGeneration: i.Generation, IdempotencyKey: "enable"}, true, proof))
	d := NewV5ContributionDirectory(v.s)
	q := v5ContributionQueries()[1]
	b, err := d.Resolve(ctx, "", q, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Run("typed-current-metadata", func(t *testing.T) {
		for _, query := range v5ContributionQueries() {
			if _, err := d.Resolve(ctx, "", query, nil); err != nil {
				t.Fatal(query, err)
			}
		}
	})
	second := v5ContributionRelease(t, v, priv, "second-provider")
	// The real actor's keys must be distinct across families.
	second.admin = proof
	for _, r := range []V5Mutation{{Kind: "install", IdempotencyKey: "second-install"}, {Kind: "grant", IdempotencyKey: "second-grant", Approved: true, Consumes: []PluginV5Consume{}, Resources: second.manifest.HostResources}} {
		if r.Kind == "install" {
			if _, err := second.s.Import(ctx, second.raw, second.envelope, "second-import", proof); err != nil {
				t.Fatal(err)
			}
		}
		second.mutate(t, r, true, proof)
	}
	i, _ = v.s.installation(ctx, second.c, "")
	v5ContributionPGReady(t, ctx, v.s, second.mutate(t, V5Mutation{Kind: "enable", ExpectedGeneration: i.Generation, IdempotencyKey: "second-enable"}, true, proof))
	t.Run("conflict-no-takeover", func(t *testing.T) {
		if _, err := d.Resolve(ctx, "", q, nil); err != ErrV5ContributionConflict || d.Recheck(ctx, b) != ErrV5ContributionConflict {
			t.Fatal("PG conflict ignored", err)
		}
		ref := b.Descriptor().V5ContributionRef
		if _, err := d.Resolve(ctx, "", q, &ref); err != nil {
			t.Fatal("explicit PG selection lost", err)
		}
	})
	i, _ = v.s.installation(ctx, second.c, "")
	second.mutate(t, V5Mutation{Kind: "disable", ExpectedGeneration: i.Generation, IdempotencyKey: "second-disable"}, true, proof)
	g, _ := v.s.grant(ctx, v.c)
	v.mutate(t, V5Mutation{Kind: "revoke", ExpectedRevision: g.Revision, IdempotencyKey: "revoke"}, true, proof)
	t.Run("current-revocation", func(t *testing.T) {
		if _, err := d.Resolve(ctx, "", q, nil); err != ErrV5ContributionUnpublished {
			t.Fatal("PG revoked contribution exposed", err)
		}
	})
	g, _ = v.s.grant(ctx, v.c)
	v.mutate(t, V5Mutation{Kind: "grant", Approved: true, Consumes: []PluginV5Consume{}, Resources: v.manifest.HostResources, ExpectedRevision: g.Revision, IdempotencyKey: "regrant"}, true, proof)
	i, _ = v.s.installation(ctx, v.c, "")
	v5ContributionPGReady(t, ctx, v.s, v.mutate(t, V5Mutation{Kind: "enable", ExpectedGeneration: i.Generation, IdempotencyKey: "reenable"}, true, proof))
	current, err := d.Resolve(ctx, "", q, nil)
	if err != nil || current.Descriptor().InstanceGeneration <= b.Descriptor().InstanceGeneration {
		t.Fatal("new PG generation", err)
	}
	for name, raw := range map[string][]byte{"trust.json": trust, "descriptor.json": mustV5JSON(current.Descriptor())} {
		if err := os.WriteFile(filepath.Join(work, name), raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestPostgresV5ContributionReload(t *testing.T) {
	ctx, pool, work := v5ContributionOwnedPool(t)
	trust, err := os.ReadFile(filepath.Join(work, "trust.json"))
	if err != nil {
		t.Fatal(err)
	}
	wire, err := os.ReadFile(filepath.Join(work, "descriptor.json"))
	var saved V5ContributionDescriptor
	if err != nil || json.Unmarshal(wire, &saved) != nil {
		t.Fatal("missing saved descriptor", err)
	}
	files, err := NewV5ManagedFiles(filepath.Join(work, "managed"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = files.Close() })
	catalog, err := NewPluginV5PackageCatalogWithIdentityStore(trust, NewPgPluginV5IdentityStore(pool))
	if err != nil {
		t.Fatal(err)
	}
	s := NewV5Platform(NewPgV5StateStore(pool), files, catalog, reliability.NewService(transaction.NewPostgreSQL(pool), reliability.NewPostgreSQLStore(pool)), nil)
	d := NewV5ContributionDirectory(s)
	q := v5ContributionQueries()[1]
	b, err := d.Resolve(ctx, "", q, &saved.V5ContributionRef)
	if err != nil || b.Descriptor().InstanceGeneration != saved.InstanceGeneration {
		t.Fatal("directory required volatile provider registration", err)
	}
	if err := s.ReconcileStartup(ctx); err != nil {
		t.Fatal(err)
	}
	if err := d.Recheck(ctx, b); err == nil {
		t.Fatal("host restart reused old ready generation")
	}
	if _, err := s.ProcessOnce(ctx, 50); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Resolve(ctx, "", q, &saved.V5ContributionRef); err != ErrV5ContributionUnpublished {
		t.Fatal("pending worker fabricated ready contribution", err)
	}
}
