package plugin

// Test-only exports let external tests combine the actual wasm package (which
// imports plugin) with current PG identities without introducing an import
// cycle or adding synthetic authorization to production.
import (
	"context"
	"encoding/json"
	communityrepo "github.com/campusos/CampusOS/internal/modules/core/community/repository"
	communityservice "github.com/campusos/CampusOS/internal/modules/core/community/service"
	identity "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type V5WasmTestHarness struct {
	Platform               *V5Platform
	Hub                    *V5ManagedRuntime
	BindHosts              func(*V5ManagedRuntime)
	Coordinate             V5Coordinate
	ManagedRoot, TrustPath string
	Admin, User            identity.SessionPrincipalResolver
	Consumer               V5Consumer
	Events                 *V5EventService
	Mutate                 func(V5Mutation, bool) V5Operation
	Current                func(string) V5Installation
	Operation              func(string) V5Operation
	Configuration          func(string) V5Configuration
	SetPublic              func(bool)
	RevokeUser             func()
	RefreshUser            func() identity.SessionPrincipalResolver
	SetTrust               func(bool)
	GrantUntil             func(int64)
	SetAuditFault          func(bool)
	PublishEvent           func() string
	ImportVersion          func(string) V5Coordinate
	ImportVariant          func(string, func(*PluginV5Manifest)) V5Coordinate
	ImportUIVersion        func(string, string) V5Coordinate
	UserSession            func(*testing.T, string) identity.SessionPrincipalResolver
	Installation           func(*testing.T, V5Coordinate, string) V5Installation
	PrivateFiles           func(*testing.T, V5Coordinate, string) *os.Root
	ConfigurationAt        func(*testing.T, V5Coordinate, string) V5Configuration
}

func NewV5WasmTestHarness(t *testing.T, ctx context.Context, pool *pgxpool.Pool, code []byte) *V5WasmTestHarness {
	return newV5RuntimeTestHarness(t, ctx, pool, code, "", "")
}
func NewV5ContainerTestHarness(t *testing.T, ctx context.Context, pool *pgxpool.Pool, ref, transport string) *V5WasmTestHarness {
	return newV5RuntimeTestHarness(t, ctx, pool, nil, ref, transport)
}
func NewV5LifecycleTestHarness(t *testing.T, ctx context.Context, pool *pgxpool.Pool, code []byte, ref, transport string) *V5WasmTestHarness {
	return newV5RuntimeTestHarness(t, ctx, pool, code, ref, transport, func(m *PluginV5Manifest) {
		m.Data.PrivateKVBytes = 16384
		m.Consumes = append(m.Consumes, PluginV5Consume{Capability: "ui.resource.selected.read", Scope: PluginV5ConsumeScope{Kind: "self"}, Purpose: "Explicit lifecycle consent fixture; no resource is read", Required: true})
	})
}
func NewV5UILifecycleTestHarness(t *testing.T, ctx context.Context, pool *pgxpool.Pool) *V5WasmTestHarness {
	return newV5RuntimeTestHarness(t, ctx, pool, nil, "", "ui", func(m *PluginV5Manifest) {
		m.Data.PrivateKVBytes = 16384
		m.Consumes = []PluginV5Consume{{Capability: "ui.resource.selected.read", Scope: PluginV5ConsumeScope{Kind: "self"}, Purpose: "Read only a currently selected resource", Required: true}, {Capability: "ui.surface.open", Scope: PluginV5ConsumeScope{Kind: "self"}, Purpose: "Open this isolated UI", Required: true}}
	})
}
func newV5RuntimeTestHarness(t *testing.T, ctx context.Context, pool *pgxpool.Pool, code []byte, ref, transport string, options ...func(*PluginV5Manifest)) *V5WasmTestHarness {
	t.Helper()
	v, priv := newV5ContributionTestPlatform(t)
	m := v.manifest
	m.Key = "wasm-" + v5RandomID()[:12]
	if transport != "ui" {
		m.UI = nil
	}
	m.HostResources = &PluginV5ManifestResources{NetworkTargets: []string{}, StorageBytes: 1048576, CPUMillis: 500, MemoryMB: 128, MaxConcurrency: 1, TimeoutMS: 5000}
	if transport == "ui" {
		m.Provides = []PluginV5Provide{}
		m.HostResources = nil
		// The lifecycle target exercises both audiences; the base author
		// fixture intentionally ships only a user UI.
		adminUI := m.UI[0]
		adminUI.Audience = "admin"
		adminUI.Entrypoint = "dist/admin/index.html"
		raw, err := os.ReadFile(filepath.Join(v.dir, m.UI[0].Entrypoint))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(v.dir, "dist/admin"), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(v.dir, adminUI.Entrypoint), raw, 0600); err != nil {
			t.Fatal(err)
		}
		m.UI = append(m.UI, adminUI)
	} else {
		m.Provides = []PluginV5Provide{m.Provides[1]}
	}
	m.Consumes = []PluginV5Consume{
		{Capability: "community.thread.public.read", Scope: PluginV5ConsumeScope{Kind: "public"}, Purpose: "Read current public thread metadata", Required: true},
		{Capability: "community.thread.public.subscribe", Scope: PluginV5ConsumeScope{Kind: "public"}, Purpose: "Receive current public metadata events", Required: false},
	}
	m.Configuration = json.RawMessage(`{"contract":"campusos.plugin-config/v1","version":"v1","schema":{"type":"object","additionalProperties":false,"properties":{"message":{"type":"string","title":"Message"}},"required":[]},"defaults":{"message":"ordinary-v1"},"selectors":[],"secret_names":[]}`)
	for _, edit := range options {
		edit(m)
	}
	if transport == "ui" {
		m.Runtime = "none"
		m.Backend = nil
		if err := os.Remove(filepath.Join(v.dir, "dist/module.wasm")); err != nil {
			t.Fatal(err)
		}
	} else if ref != "" {
		m.Runtime = "container"
		m.Backend.Artifact.Kind = "container"
		m.Backend.Artifact.Path = ""
		m.Backend.Artifact.Digest = ""
		m.Backend.Artifact.Image = ref
		m.Backend.Transport = transport
		if err := os.Remove(filepath.Join(v.dir, "dist/module.wasm")); err != nil {
			t.Fatal(err)
		}
	} else {
		m.Backend.Artifact.Digest = v5BytesDigest(code)
		if err := os.WriteFile(filepath.Join(v.dir, "dist/module.wasm"), code, 0600); err != nil {
			t.Fatal(err)
		}
	}
	wire, _ := json.Marshal(m)
	if err := os.WriteFile(filepath.Join(v.dir, "plugin.json"), wire, 0600); err != nil {
		t.Fatal(err)
	}
	archive, p, err := BuildPluginV5Package(v.dir)
	if err != nil {
		t.Fatal(err)
	}
	release, err := SignPluginV5Package(archive, "test-key", priv)
	if err != nil {
		t.Fatal(err)
	}
	trust := append([]byte(nil), v.s.catalog.trust...)
	admins, adminProof, _ := v5PGAdmin(t, ctx, pool)
	managedParent := t.TempDir()
	managedRoot := filepath.Join(managedParent, "v5")
	files, err := NewV5ManagedFiles(managedRoot)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = files.Close() })
	catalog, err := NewPluginV5PackageCatalogWithIdentityStore(trust, NewPgPluginV5IdentityStore(pool))
	if err != nil {
		t.Fatal(err)
	}
	fault := &v5ReliableFault{Store: reliability.NewPostgreSQLStore(pool)}
	reliable := reliability.NewService(transaction.NewPostgreSQL(pool), fault)
	v.s = NewV5Platform(NewPgV5StateStore(pool), files, catalog, reliable, admins)
	v.s.Users = v5ChatPGUsers{repository.NewPgUserRepository(pool)}
	v.raw, v.envelope, v.manifest, v.c, v.admin = archive, release, p.Manifest, V5Coordinate{m.Publisher, m.Key, m.Version}, adminProof
	userSessions, userProof, userSID := v5ChatPGSession(t, ctx, pool, reliable)
	h := &V5WasmTestHarness{Platform: v.s, Hub: NewV5ManagedRuntime(NewV5ContributionDirectory(v.s)), Coordinate: v.c, Admin: adminProof, User: userProof}
	h.ManagedRoot, h.TrustPath = managedParent, filepath.Join(managedParent, "trust.json")
	h.ImportVariant = func(version string, edit func(*PluginV5Manifest)) V5Coordinate {
		t.Helper()
		var candidate PluginV5Manifest
		wire, _ := json.Marshal(m)
		if err := json.Unmarshal(wire, &candidate); err != nil {
			t.Fatal(err)
		}
		candidate.Version = version
		candidate.Data.Upgrade.Mode = "preserve"
		if edit != nil {
			edit(&candidate)
		}
		wire, err := json.Marshal(candidate)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(v.dir, "plugin.json"), wire, 0600); err != nil {
			t.Fatal(err)
		}
		archive, _, err := BuildPluginV5Package(v.dir)
		if err != nil {
			t.Fatal(err)
		}
		release, err := SignPluginV5Package(archive, "test-key", priv)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := v.s.Import(ctx, archive, release, v5RandomID(), h.Admin); err != nil {
			t.Fatal(err)
		}
		return V5Coordinate{candidate.Publisher, candidate.Key, candidate.Version}
	}
	h.ImportVersion = func(version string) V5Coordinate { return h.ImportVariant(version, nil) }
	h.ImportUIVersion = func(version, marker string) V5Coordinate {
		return h.ImportVariant(version, func(m *PluginV5Manifest) {
			for n := range m.UI {
				raw := []byte("<!doctype html><title>" + marker + "</title>")
				if err := os.WriteFile(filepath.Join(v.dir, m.UI[n].Entrypoint), raw, 0600); err != nil {
					t.Fatal(err)
				}
				m.UI[n].Digest = v5BytesDigest(raw)
			}
		})
	}
	h.UserSession = func(tt *testing.T, uid string) identity.SessionPrincipalResolver {
		_, proof, _ := v5ChatPGSessionFor(tt, ctx, pool, reliable, uid)
		return proof
	}
	h.Installation = func(tt *testing.T, c V5Coordinate, owner string) V5Installation {
		tt.Helper()
		i, err := v.s.installation(ctx, c, owner)
		if err != nil {
			tt.Fatal(err)
		}
		return i
	}
	h.PrivateFiles = func(tt *testing.T, c V5Coordinate, owner string) *os.Root {
		tt.Helper()
		i := h.Installation(tt, c, owner)
		root, err := v.s.files.dataView(i, i.DataGeneration)
		if err != nil {
			tt.Fatal(err)
		}
		return root
	}
	h.ConfigurationAt = func(tt *testing.T, c V5Coordinate, owner string) V5Configuration {
		tt.Helper()
		release, err := v.s.release(ctx, c)
		if err != nil {
			tt.Fatal(err)
		}
		m, err := ParsePluginV5Manifest(release.Manifest)
		if err != nil {
			tt.Fatal(err)
		}
		cfg, err := v.s.configuration(ctx, c, owner, m)
		if err != nil {
			tt.Fatal(err)
		}
		return cfg
	}
	if err := os.WriteFile(h.TrustPath, trust, 0600); err != nil {
		t.Fatal(err)
	}
	h.Mutate = func(r V5Mutation, admin bool) V5Operation {
		if r.IdempotencyKey == "" {
			r.IdempotencyKey = v5RandomID()
		}
		proof := h.User
		if admin {
			proof = h.Admin
		}
		return v.mutate(t, r, admin, proof)
	}
	h.Current = func(owner string) V5Installation {
		i, err := v.s.installation(ctx, v.c, owner)
		if err != nil {
			t.Fatal(err)
		}
		return i
	}
	h.Operation = func(id string) V5Operation {
		var op V5Operation
		if err := v.s.store.load(ctx, v5Operation, id, &op); err != nil {
			t.Fatal(err)
		}
		return op
	}
	h.Configuration = func(owner string) V5Configuration {
		cfg, err := v.s.configuration(ctx, v.c, owner, m)
		if err != nil {
			t.Fatal(err)
		}
		return cfg
	}
	h.RevokeUser = func() {
		if err := userSessions.RevokeCurrent(ctx, "2801001", userSID); err != nil {
			t.Fatal(err)
		}
	}
	h.RefreshUser = func() identity.SessionPrincipalResolver {
		userSessions, userProof, userSID = v5ChatPGSession(t, ctx, pool, reliable)
		h.User = userProof
		return userProof
	}
	h.SetTrust = func(active bool) {
		wire := trust
		if !active {
			wire = []byte(`{"contract":"campusos.plugin-trust/v1","keys":[]}`)
		}
		if err := catalog.SetTrust(wire); err != nil {
			t.Fatal(err)
		}
	}
	h.GrantUntil = func(expiry int64) {
		g, err := v.s.grant(ctx, v.c)
		if err != nil {
			t.Fatal(err)
		}
		h.Mutate(V5Mutation{Kind: "grant", Approved: true, Consumes: m.Consumes, Resources: m.HostResources, ExpiresAtMS: expiry, ExpectedRevision: g.Revision}, true)
	}
	h.SetAuditFault = func(failed bool) {
		fault.fail = ""
		if failed {
			fault.fail = "audit"
		}
	}
	features := v5ChatPGFeatures(t)
	h.Consumer = V5Consumer{Feature: features, FeatureID: "contract-chat-consumer", Authorize: func(context.Context, identity.PrincipalContext, V5ContributionDescriptor, json.RawMessage) (string, error) {
		return "rev42", nil
	}}
	if _, err := pool.Exec(ctx, `INSERT INTO categories(id,name,slug,node_kind,lifecycle_status) VALUES(505001,'05 group','owned05-group','group','active') ON CONFLICT(id) DO NOTHING; INSERT INTO categories(id,name,slug,parent_id,node_kind,lifecycle_status) VALUES(505002,'05 board','owned05-board',505001,'board','active') ON CONFLICT(id) DO NOTHING; INSERT INTO category_thread_type_policies(category_id,thread_type,enabled) VALUES(505002,'discussion',true) ON CONFLICT DO NOTHING; INSERT INTO threads(id,title,content,author_id,category_id,status,publication_status,moderation_status,deletion_status) VALUES(505003,'Actual public metadata','PRIVATE-BODY-SHOULD-NEVER-CROSS',2801001,505002,'published','published','clear','active') ON CONFLICT(id) DO NOTHING`); err != nil {
		t.Fatal(err)
	}
	reader := v5PGPublicQuery{communityservice.NewThreadService(communityrepo.NewPgThreadRepository(pool), nil)}
	h.BindHosts = func(hub *V5ManagedRuntime) {
		if err := hub.BindHost("community.thread.public.read", V5PublicThreadEndpoint(reader, features, "contract-chat-consumer")); err != nil {
			t.Fatal(err)
		}
	}
	h.BindHosts(h.Hub)
	h.SetPublic = func(public bool) {
		status := "private"
		if public {
			status = "published"
		}
		if _, err := pool.Exec(ctx, `UPDATE threads SET publication_status=$1 WHERE id=505003`, status); err != nil {
			t.Fatal(err)
		}
	}
	h.Events = NewV5EventService(h.Hub, NewPgV5EventStore(pool), func(ctx context.Context, kind, ref string) (bool, error) {
		r, err := reader.GetPublicThread(ctx, "505003")
		return r != nil && err == nil, err
	})
	h.PublishEvent = func() string {
		e, err := reliability.NewEvent("thread.updated", "thread", "505003", map[string]string{"body": "PRIVATE-BODY-SHOULD-NEVER-CROSS"})
		if err != nil {
			t.Fatal(err)
		}
		if err := reliable.Execute(ctx, reliability.Command{Code: "test.wasm.public.event", ActorType: "system", ActorID: "owned05", ResourceType: "thread", ResourceID: "505003", EventFactory: func() (reliability.Event, error) { return e, nil }}, func(context.Context) error { return nil }); err != nil {
			t.Fatal(err)
		}
		return e.ID
	}
	v.installed(t)
	h.Mutate(V5Mutation{Kind: "grant", Approved: true, Consumes: m.Consumes, Resources: m.HostResources, ExpiresAtMS: time.Now().Add(time.Hour).UnixMilli()}, true)
	return h
}
