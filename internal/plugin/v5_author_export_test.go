package plugin

import (
	"context"
	"path/filepath"
	"testing"

	identity "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/jackc/pgx/v5/pgxpool"
)

// This target imports the exact independently built and CLI-signed author
// archive. It does not replace its manifest, program, signature or Grant.
func NewV5AuthorTestHarness(t *testing.T, ctx context.Context, pool *pgxpool.Pool, archive, envelope, trust []byte) *V5WasmTestHarness {
	t.Helper()
	p, err := VerifyPluginV5Release(archive, envelope, trust)
	if err != nil {
		t.Fatal(err)
	}
	managedRoot := t.TempDir()
	files, err := NewV5ManagedFiles(filepath.Join(managedRoot, "v5"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = files.Close() })
	catalog, err := NewPluginV5PackageCatalogWithIdentityStore(trust, NewPgPluginV5IdentityStore(pool))
	if err != nil {
		t.Fatal(err)
	}
	admins, proof, _ := v5PGAdmin(t, ctx, pool)
	reliable := reliability.NewService(transaction.NewPostgreSQL(pool), reliability.NewPostgreSQLStore(pool))
	s := NewV5Platform(NewPgV5StateStore(pool), files, catalog, reliable, admins)
	s.Users = v5ChatPGUsers{repository.NewPgUserRepository(pool)}
	if _, err := s.Import(ctx, archive, envelope, "author08a-import", proof); err != nil {
		t.Fatal(err)
	}
	c := V5Coordinate{p.Manifest.Publisher, p.Manifest.Key, p.Manifest.Version}
	_, user, _ := v5ChatPGSession(t, ctx, pool, reliable)
	h := &V5WasmTestHarness{Platform: s, Hub: NewV5ManagedRuntime(NewV5ContributionDirectory(s)), Coordinate: c, Admin: proof, User: user, ManagedRoot: managedRoot}
	h.UserSession = func(tt *testing.T, uid string) identity.SessionPrincipalResolver {
		_, user, _ := v5ChatPGSessionFor(tt, ctx, pool, reliable, uid)
		return user
	}
	h.Current = func(owner string) V5Installation {
		i, err := s.installation(ctx, c, owner)
		if err != nil {
			t.Fatal(err)
		}
		return i
	}
	features := v5ChatPGFeatures(t)
	// This compiled consumer admits explicit prompts with no host context only.
	// It supplies no resource-authority proof for public or private contexts.
	h.Consumer = V5Consumer{Feature: features, FeatureID: "contract-chat-consumer"}
	return h
}
