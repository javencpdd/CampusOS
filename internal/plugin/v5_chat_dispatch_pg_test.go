package plugin

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	identity "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	identityservice "github.com/campusos/CampusOS/internal/modules/core/identity/service"
	platformfeature "github.com/campusos/CampusOS/internal/platform/feature"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/campusos/CampusOS/pkg/auth"
	campusossdk "github.com/campusos/CampusOS/sdk/go"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Real repository projection without importing portadapt's legacy HTTP bridge
// (which depends back on plugin). Authentication still uses SessionService.
type v5ChatPGUsers struct{ repo *repository.PgUserRepository }

func (r v5ChatPGUsers) GetUser(ctx context.Context, id string) (identity.User, error) {
	u, err := r.repo.GetByID(ctx, id)
	if err != nil {
		return identity.User{}, err
	}
	return identity.User{ID: u.ID, Status: string(u.Status)}, nil
}
func (r v5ChatPGUsers) GetUserByUsername(ctx context.Context, name string) (identity.User, error) {
	u, err := r.repo.GetByUsername(ctx, name)
	if err != nil {
		return identity.User{}, err
	}
	return identity.User{ID: u.ID, Status: string(u.Status)}, nil
}
func (r v5ChatPGUsers) GetUserForUpdate(ctx context.Context, id string) (identity.User, error) {
	u, err := r.repo.GetByIDForUpdate(ctx, id)
	if err != nil {
		return identity.User{}, err
	}
	return identity.User{ID: u.ID, Status: string(u.Status)}, nil
}

func v5ChatOwnedPool(t *testing.T) (context.Context, *pgxpool.Pool, string) {
	t.Helper()
	dsn, work := os.Getenv("CAMPUSOS_V5_CHAT_DATABASE_URL"), os.Getenv("CAMPUSOS_V5_CHAT_OUTPUT")
	if dsn == "" {
		t.Skip("requires owned03c chat database")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil || cfg.ConnConfig.Database != "campusos_v12_03c_chat" || work == "" {
		t.Fatal("requires exact owned03c chat database/output")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err := os.MkdirAll(work, 0700); err != nil {
		t.Fatal(err)
	}
	return ctx, pool, work
}

func v5ChatPGSession(t *testing.T, ctx context.Context, pool *pgxpool.Pool, reliable *reliability.Service) (*identityservice.SessionService, identity.SessionPrincipalResolver, string) {
	return v5ChatPGSessionFor(t, ctx, pool, reliable, "2801001")
}
func v5ChatPGSessionFor(t *testing.T, ctx context.Context, pool *pgxpool.Pool, reliable *reliability.Service, uid string) (*identityservice.SessionService, identity.SessionPrincipalResolver, string) {
	t.Helper()
	username, email := "v5-chat-"+uid, "v5-chat-"+uid+"@example.test"
	if uid == "2801001" {
		username, email = "v5-chat-user", "v5-chat@example.test"
	}
	if _, err := pool.Exec(ctx, `INSERT INTO users(id,username,nickname,email,status) VALUES($1,$2,'V5 Chat',$3,'active') ON CONFLICT(id) DO NOTHING`, uid, username, email); err != nil {
		t.Fatal(err)
	}
	users := repository.NewPgUserRepository(pool)
	u, err := users.GetByID(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	jwt := auth.NewJWTManager(auth.JWTConfig{Secret: v5RandomID() + v5RandomID(), AccessTTL: time.Hour})
	sessions, err := identityservice.NewSessionService(repository.NewPgSessionRepository(pool), users, jwt, identityservice.SessionConfig{IPHashSecret: v5RandomID()})
	if err != nil {
		t.Fatal(err)
	}
	sessions.SetReliability(reliable)
	tokens, err := sessions.Issue(ctx, u, identityservice.SessionMetadata{DeviceID: "owned-chat", DeviceName: "Owned native chat target"})
	if err != nil {
		t.Fatal(err)
	}
	claims, err := jwt.VerifyAccessToken(tokens.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	return sessions, func(ctx context.Context) (identity.PrincipalContext, error) {
		return identity.ResolveUserSessionPrincipal(ctx, sessions, sessions, claims)
	}, tokens.Session.ID
}

func v5ChatPGFeatures(t *testing.T) *platformfeature.Registry {
	t.Helper()
	features := platformfeature.NewAuthoritativeRegistry(platformfeature.NewMemoryStore())
	if err := features.Register(platformfeature.Definition{ID: "contract-chat-consumer", Mode: platformfeature.HotGated, DefaultEnabled: true}); err != nil {
		t.Fatal(err)
	}
	return features
}

func TestPostgresV5ChatDispatch(t *testing.T) {
	ctx, pool, work := v5ChatOwnedPool(t)
	v, _ := newV5ContributionTestPlatform(t)
	trust := append([]byte(nil), v.s.catalog.trust...)
	admins, adminProof, _ := v5PGAdmin(t, ctx, pool)
	files, err := NewV5ManagedFiles(filepath.Join(work, "managed"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = files.Close() })
	catalog, err := NewPluginV5PackageCatalogWithIdentityStore(trust, NewPgPluginV5IdentityStore(pool))
	if err != nil {
		t.Fatal(err)
	}
	reliable := reliability.NewService(transaction.NewPostgreSQL(pool), reliability.NewPostgreSQLStore(pool))
	v.s = NewV5Platform(NewPgV5StateStore(pool), files, catalog, reliable, admins)
	v.s.Users = v5ChatPGUsers{repository.NewPgUserRepository(pool)}
	v.admin = adminProof
	i := v5ContributionApproved(t, v)
	v5ContributionPGReady(t, ctx, v.s, v.mutate(t, V5Mutation{Kind: "enable", ExpectedGeneration: i.Generation, IdempotencyKey: "global-enable"}, true, adminProof))
	sessions, userProof, sid := v5ChatPGSession(t, ctx, pool, reliable)
	op := v.mutate(t, V5Mutation{Kind: "install", IdempotencyKey: "user-install"}, false, userProof)
	v5ContributionPGReady(t, ctx, v.s, v.mutate(t, V5Mutation{Kind: "enable", ExpectedGeneration: op.Generation, IdempotencyKey: "user-enable"}, false, userProof))
	directory := NewV5ContributionDirectory(v.s)
	b, err := directory.Resolve(ctx, "2801001", v5ContributionQueries()[1], nil)
	if err != nil {
		t.Fatal(err)
	}
	features := v5ChatPGFeatures(t)
	d := NewV5ChatDispatcher(directory, features, "contract-chat-consumer", v5ChatTestTransport(v5ChatReply))
	t.Run("current-real-user-and-signed-package", func(t *testing.T) {
		out, err := d.Chat(ctx, b, v5ChatTestRequest(b), userProof)
		if err != nil || out.Text != "bounded reply" {
			t.Fatal("real User/PG dispatch", err)
		}
	})
	t.Run("feature-before-transport", func(t *testing.T) {
		if _, err := features.Request("contract-chat-consumer", false); err != nil {
			t.Fatal(err)
		}
		if out, err := d.Chat(ctx, b, v5ChatTestRequest(b), userProof); err != ErrV5FeatureDisabled || out.Text != "" {
			t.Fatal("disabled consumer admitted", err)
		}
		if _, err := features.Request("contract-chat-consumer", true); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("session-revoke-during-io", func(t *testing.T) {
		d.transport = v5ChatTestTransport(func(callCtx context.Context, target V5ChatTarget, req campusossdk.V5ChatRequest, out io.Writer) error {
			if err := sessions.RevokeCurrent(callCtx, "2801001", sid); err != nil {
				return err
			}
			return v5ChatReply(callCtx, target, req, out)
		})
		if out, err := d.Chat(ctx, b, v5ChatTestRequest(b), userProof); err != ErrV5Denied || out.Text != "" {
			t.Fatal("revoked current session released response", err)
		}
	})
	_, userProof, _ = v5ChatPGSession(t, ctx, pool, reliable)
	t.Run("lifecycle-commit-during-io", func(t *testing.T) {
		d.transport = v5ChatTestTransport(func(callCtx context.Context, target V5ChatTarget, req campusossdk.V5ChatRequest, out io.Writer) error {
			i, err := v.s.installation(callCtx, v.c, "")
			if err != nil {
				return err
			}
			_, err = v.s.Mutate(callCtx, v.c.Publisher, v.c.Key, V5Mutation{Kind: "disable", Version: v.c.Version, ExpectedGeneration: i.Generation, IdempotencyKey: "disable-during-chat"}, true, adminProof)
			if err != nil {
				return err
			}
			return v5ChatReply(callCtx, target, req, out)
		})
		if out, err := d.Chat(ctx, b, v5ChatTestRequest(b), userProof); err == nil || out.Text != "" {
			t.Fatal("stale instance released response", err)
		}
		i, _ := v.s.installation(ctx, v.c, "")
		if i.Desired != "disabled" {
			t.Fatal("dispatcher held a transaction across IO")
		}
	})
	i, _ = v.s.installation(ctx, v.c, "")
	v5ContributionPGReady(t, ctx, v.s, v.mutate(t, V5Mutation{Kind: "enable", ExpectedGeneration: i.Generation, IdempotencyKey: "global-again"}, true, adminProof))
	i, _ = v.s.installation(ctx, v.c, "2801001")
	v5ContributionPGReady(t, ctx, v.s, v.mutate(t, V5Mutation{Kind: "enable", ExpectedGeneration: i.Generation, IdempotencyKey: "user-again"}, false, userProof))
	b, err = directory.Resolve(ctx, "2801001", v5ContributionQueries()[1], nil)
	if err != nil {
		t.Fatal(err)
	}
	for name, raw := range map[string][]byte{"trust.json": trust, "descriptor.json": mustV5JSON(b.Descriptor()), "revoked-session.json": mustV5JSON(sid)} {
		if err := os.WriteFile(filepath.Join(work, name), raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestPostgresV5ChatDispatchRestart(t *testing.T) {
	ctx, pool, work := v5ChatOwnedPool(t)
	read := func(name string, out any) {
		t.Helper()
		raw, err := os.ReadFile(filepath.Join(work, name))
		if err != nil || json.Unmarshal(raw, out) != nil {
			t.Fatal("missing restart evidence", name)
		}
	}
	var descriptor V5ContributionDescriptor
	var sid string
	read("descriptor.json", &descriptor)
	read("revoked-session.json", &sid)
	saved, err := repository.NewPgSessionRepository(pool).GetByID(ctx, sid)
	if err != nil || saved.RevokedAt == nil {
		t.Fatal("restart resurrected revoked user session", err)
	}
	trust, err := os.ReadFile(filepath.Join(work, "trust.json"))
	if err != nil {
		t.Fatal(err)
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
	reliable := reliability.NewService(transaction.NewPostgreSQL(pool), reliability.NewPostgreSQLStore(pool))
	s := NewV5Platform(NewPgV5StateStore(pool), files, catalog, reliable, nil)
	s.Users = v5ChatPGUsers{repository.NewPgUserRepository(pool)}
	directory := NewV5ContributionDirectory(s)
	b, err := directory.Resolve(ctx, "2801001", v5ContributionQueries()[1], &descriptor.V5ContributionRef)
	if err != nil {
		t.Fatal(err)
	}
	_, proof, _ := v5ChatPGSession(t, ctx, pool, reliable)
	if err := s.ReconcileStartup(ctx); err != nil {
		t.Fatal(err)
	}
	called := false
	transport := v5ChatTestTransport(func(context.Context, V5ChatTarget, campusossdk.V5ChatRequest, io.Writer) error {
		called = true
		return nil
	})
	d := NewV5ChatDispatcher(directory, v5ChatPGFeatures(t), "contract-chat-consumer", transport)
	if out, err := d.Chat(ctx, b, v5ChatTestRequest(b), proof); err == nil || out.Text != "" || called {
		t.Fatal("restart admitted stale instance to transport", err)
	}
}
