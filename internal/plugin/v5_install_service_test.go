package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	identity "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
)

type v5TestAdmins struct{ expires int64 }

func (a v5TestAdmins) LoadAdminExecutionAuthority(ctx context.Context, q identity.AdminExecutionQuery, resolve identity.SessionPrincipalResolver, lock bool) (identity.AdminExecutionAuthority, error) {
	p, e := resolve(ctx)
	if e != nil {
		return identity.AdminExecutionAuthority{}, e
	}
	return identity.AdminExecutionAuthority{Principal: p, Query: q, Allowed: q.Validate() == nil && p.Actor.Kind == "admin" && (!q.RequiresCurrentMFA() || p.AuthenticationStrength == "mfa"), ExpiresAtMS: a.expires, Binding: 1}, nil
}
func v5TestProof(kind, id string) identity.SessionPrincipalResolver {
	return func(context.Context) (identity.PrincipalContext, error) {
		strength := "password"
		if kind == "admin" {
			strength = "mfa"
		}
		return identity.PrincipalContext{Contract: identity.PrincipalContract, Actor: identity.PrincipalRef{Kind: kind, ID: id}, Audience: kind, AuthenticationStrength: strength, CredentialID: kind + ".session." + id}, nil
	}
}

type v5ReliableFault struct {
	reliability.Store
	fail string
}

func (f *v5ReliableFault) Enqueue(ctx context.Context, e *reliability.Event) (*reliability.Event, error) {
	if f.fail == "outbox" {
		return nil, errors.New("injected outbox rejection")
	}
	return f.Store.Enqueue(ctx, e)
}
func (f *v5ReliableFault) RecordCommandAudit(ctx context.Context, a reliability.CommandAudit) error {
	if f.fail == "audit" {
		return errors.New("injected audit rejection")
	}
	return f.Store.RecordCommandAudit(ctx, a)
}

type v5TestPlatform struct {
	s             *V5Platform
	store         *MemoryV5StateStore
	fault         *v5ReliableFault
	admin         identity.SessionPrincipalResolver
	raw, envelope []byte
	c             V5Coordinate
	manifest      *PluginV5Manifest
	dir           string
}

func newV5TestPlatform(t *testing.T) v5TestPlatform {
	t.Helper()
	dir := testV5PackageSource(t, 2)
	manifest, e := os.ReadFile(filepath.Join(dir, "plugin.json"))
	if e != nil {
		t.Fatal(e)
	}
	m, e := ParsePluginV5Manifest(manifest)
	if e != nil {
		t.Fatal(e)
	}
	m.Data.ConfigScope = "both"
	manifest, _ = json.Marshal(m)
	os.WriteFile(filepath.Join(dir, "plugin.json"), manifest, 0600)
	raw, p, e := BuildPluginV5Package(dir)
	if e != nil {
		t.Fatal(e)
	}
	priv, trust := testV5Trust(t)
	envelope, e := SignPluginV5Package(raw, "test-key", priv)
	if e != nil {
		t.Fatal(e)
	}
	catalog, e := NewPluginV5PackageCatalog(trust)
	if e != nil {
		t.Fatal(e)
	}
	files, e := NewV5ManagedFiles(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { files.Close() })
	store := NewMemoryV5StateStore()
	events := reliability.NewMemoryStore()
	fault := &v5ReliableFault{Store: events}
	reliable := reliability.NewService(transaction.NewMemory(store, events), fault)
	s := NewV5Platform(store, files, catalog, reliable, v5TestAdmins{time.Now().Add(time.Hour).UnixMilli()})
	return v5TestPlatform{s, store, fault, v5TestProof("admin", "adm.v5"), raw, envelope, V5Coordinate{p.Manifest.Publisher, p.Manifest.Key, p.Manifest.Version}, p.Manifest, dir}
}
func (v v5TestPlatform) imported(t *testing.T) {
	t.Helper()
	op, e := v.s.Import(context.Background(), v.raw, v.envelope, "initial-import", v.admin)
	if e != nil || op.Status != "done" {
		t.Fatal("import", op, e)
	}
}
func (v v5TestPlatform) mutate(t *testing.T, r V5Mutation, admin bool, proof identity.SessionPrincipalResolver) V5Operation {
	t.Helper()
	r.Version = v.c.Version
	op, e := v.s.Mutate(context.Background(), v.c.Publisher, v.c.Key, r, admin, proof)
	if e != nil {
		t.Fatal(r.Kind, e)
	}
	return op
}
func (v v5TestPlatform) installed(t *testing.T) V5Installation {
	t.Helper()
	v.imported(t)
	v.mutate(t, V5Mutation{Kind: "install", IdempotencyKey: "global-install"}, true, v.admin)
	i, e := v.s.installation(context.Background(), v.c, "")
	if e != nil {
		t.Fatal(e)
	}
	return i
}
func (v v5TestPlatform) approved(t *testing.T) V5Installation {
	t.Helper()
	v.installed(t)
	v.mutate(t, V5Mutation{Kind: "grant", IdempotencyKey: "approve", Approved: true, Consumes: []PluginV5Consume{}}, true, v.admin)
	i, e := v.s.installation(context.Background(), v.c, "")
	if e != nil {
		t.Fatal(e)
	}
	return i
}

func TestV5InstallOwnershipGrantAndIdempotency(t *testing.T) {
	v := newV5TestPlatform(t)
	ctx := context.Background()
	i := v.installed(t)
	user := v5TestProof("user", "1001")
	r := V5Mutation{Kind: "install", Version: v.c.Version, IdempotencyKey: "user-before-grant"}
	if _, e := v.s.Mutate(ctx, v.c.Publisher, v.c.Key, r, false, user); e != ErrV5Denied {
		t.Fatal("install itself created a grant", e)
	}
	if _, e := v.s.Mutate(ctx, v.c.Publisher, v.c.Key, V5Mutation{Kind: "enable", Version: v.c.Version, IdempotencyKey: "enable-before-grant", ExpectedGeneration: i.Generation}, true, v.admin); e != ErrV5Denied {
		t.Fatal("enabled unapproved package", e)
	}
	op, e := v.s.Import(ctx, v.raw, v.envelope, "initial-import", v.admin)
	if e != nil || op.ID == "" {
		t.Fatal("import replay", e)
	}
	v.mutate(t, V5Mutation{Kind: "grant", IdempotencyKey: "approve", Approved: true, Consumes: []PluginV5Consume{}}, true, v.admin)
	i, _ = v.s.installation(ctx, v.c, "")
	v.mutate(t, V5Mutation{Kind: "enable", IdempotencyKey: "global-enable", ExpectedGeneration: i.Generation}, true, v.admin)
	op = v.mutate(t, V5Mutation{Kind: "install", IdempotencyKey: "user-install"}, false, user)
	if op.Owner != "1001" || op.ActorKind != "user" {
		t.Fatal("wrong owner")
	}
	replay := v.mutate(t, V5Mutation{Kind: "install", IdempotencyKey: "user-install"}, false, user)
	if replay.ID != op.ID {
		t.Fatal("idempotency issued another operation")
	}
	r = V5Mutation{Kind: "enable", Version: v.c.Version, IdempotencyKey: "user-install", ExpectedGeneration: op.Generation}
	if _, e := v.s.Mutate(ctx, v.c.Publisher, v.c.Key, r, false, user); e != ErrV5Conflict {
		t.Fatal("idempotency payload rebound", e)
	}
	if _, e := v.s.installation(ctx, v.c, "1002"); e != ErrV5NotFound {
		t.Fatal("cross-owner install")
	}
	if _, e := v.s.Mutate(ctx, v.c.Publisher, v.c.Key, V5Mutation{Kind: "grant", Version: v.c.Version, IdempotencyKey: "user-admin-grant", Approved: true}, false, user); e != ErrV5Invalid {
		t.Fatal("user admitted admin command", e)
	}
	if _, e := v.s.Mutate(ctx, v.c.Publisher, v.c.Key, V5Mutation{Kind: "disable", Version: v.c.Version, IdempotencyKey: "wrong-domain"}, true, user); e != ErrV5Denied {
		t.Fatal("user principal entered management", e)
	}
}
func TestV5InstallAtomicFailuresAndConfig(t *testing.T) {
	v := newV5TestPlatform(t)
	ctx := context.Background()
	i := v.approved(t)
	for _, failure := range []string{"outbox", "audit"} {
		v.fault.fail = failure
		if _, e := v.s.Mutate(ctx, v.c.Publisher, v.c.Key, V5Mutation{Kind: "enable", Version: v.c.Version, IdempotencyKey: "failed-" + failure, ExpectedGeneration: i.Generation}, true, v.admin); e == nil {
			t.Fatal("mandatory receipt failure ignored")
		}
		after, _ := v.s.installation(ctx, v.c, "")
		if after != i {
			t.Fatal("failed command changed installation", failure)
		}
		if _, e := v.store.operation(ctx, "admin", "adm.v5", "failed-"+failure); e != ErrV5NotFound {
			t.Fatal("failed command left an operation")
		}
	}
	v.fault.fail = ""
	op := v.mutate(t, V5Mutation{Kind: "configure", IdempotencyKey: "config", Values: json.RawMessage(`{"locale":"en-US"}`)}, true, v.admin)
	cfg, _ := v.s.configuration(ctx, v.c, "", v.manifest)
	if cfg.Revision < 1 || string(cfg.Values) != `{"locale":"en-US"}` {
		t.Fatal("config not authoritative")
	}
	if _, e := v.s.Mutate(ctx, v.c.Publisher, v.c.Key, V5Mutation{Kind: "configure", Version: v.c.Version, IdempotencyKey: "stale-config", ExpectedRevision: 0, Values: json.RawMessage(`{"locale":"zh-CN"}`)}, true, v.admin); e != ErrV5Conflict {
		t.Fatal("config CAS bypass", e)
	}
	if _, e := v.s.Mutate(ctx, v.c.Publisher, v.c.Key, V5Mutation{Kind: "configure", Version: v.c.Version, IdempotencyKey: "secret", ExpectedRevision: cfg.Revision, Values: cfg.Values, SecretRefs: map[string]string{"token": "plaintext"}}, true, v.admin); e != ErrV5Denied {
		t.Fatal("secret declaration/ref accepted", e)
	}
	if op.Generation <= i.Generation {
		t.Fatal("configuration reused generation")
	}
}
func TestV5InstallCompetingCommandsAndRevocation(t *testing.T) {
	v := newV5TestPlatform(t)
	ctx := context.Background()
	i := v.approved(t)
	var wg sync.WaitGroup
	var mu sync.Mutex
	wins := 0
	for n := 0; n < 16; n++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			_, e := v.s.Mutate(ctx, v.c.Publisher, v.c.Key, V5Mutation{Kind: "enable", Version: v.c.Version, ExpectedGeneration: i.Generation, IdempotencyKey: "race-" + v5Counter(int64(n))}, true, v.admin)
			mu.Lock()
			defer mu.Unlock()
			if e == nil {
				wins++
			} else if e != ErrV5Conflict {
				t.Error(e)
			}
		}(n)
	}
	wg.Wait()
	if wins != 1 {
		t.Fatal("generation competition winners", wins)
	}
	g, _ := v.s.grant(ctx, v.c)
	v.mutate(t, V5Mutation{Kind: "revoke", ExpectedRevision: g.Revision, IdempotencyKey: "revoke"}, true, v.admin)
	after, _ := v.s.installation(ctx, v.c, "")
	if after.Desired != "disabled" || after.ActiveGeneration != 0 || after.Generation <= i.Generation {
		t.Fatal("revocation did not fence old generations")
	}
	g, _ = v.s.grant(ctx, v.c)
	v.mutate(t, V5Mutation{Kind: "grant", ExpectedRevision: g.Revision, Approved: true, IdempotencyKey: "regrant"}, true, v.admin)
	after, _ = v.s.installation(ctx, v.c, "")
	if after.Desired != "disabled" {
		t.Fatal("regrant resurrected an enabled instance")
	}
}
func TestV5TypedGrantAndConsentBinding(t *testing.T) {
	m, e := ParsePluginV5Manifest(v5ManifestFixtures(t)[0].Manifest)
	if e != nil {
		t.Fatal(e)
	}
	g := V5Grant{Approved: true, Consumes: append([]PluginV5Consume(nil), m.Consumes...), Resources: m.HostResources}
	if !v5GrantComplete(m, g) {
		t.Fatal("exact declared grant denied")
	}
	g.Consumes[0].Purpose = "different purpose"
	if v5GrantValid(m, g) {
		t.Fatal("purpose/Scope grant widened")
	}
	v := newV5TestPlatform(t)
	v.approved(t)
	user := v5TestProof("user", "1001")
	hash := V5PurposeHash(v.manifest)
	v.mutate(t, V5Mutation{Kind: "consent", IdempotencyKey: "consent", Accepted: true, PurposeHash: hash}, false, user)
	var consent V5Consent
	v.store.load(context.Background(), v5Consent, v5VersionOwnerID(v.c, "1001"), &consent)
	grant, _ := v.s.grant(context.Background(), v.c)
	if consent.GrantRevision != grant.Revision {
		t.Fatal("consent not bound to grant generation")
	}
	g2 := V5Grant{V5Coordinate: v.c, Approved: true, Revision: grant.Revision + 1}
	synthetic := *v.manifest
	synthetic.Consumes = []PluginV5Consume{{Scope: PluginV5ConsumeScope{Kind: "self"}}}
	if v.s.consentOK(context.Background(), v.c, "1001", &synthetic, g2) {
		t.Fatal("consent inherited across purpose/grant ABA")
	}
}

func TestV5InstanceConfigScopes(t *testing.T) {
	v := newV5TestPlatform(t)
	ctx := context.Background()
	v.approved(t)
	m := *v.manifest
	// Deliberately invalid records in an excluded domain cannot become effective
	// configuration; each included domain still uses the shared strict kernel.
	invalid := V5Configuration{V5Coordinate: v.c, Owner: "1001", Values: json.RawMessage(`{"not_declared":true}`), SecretRefs: map[string]string{}}
	if err := v.store.save(ctx, v5Config, v5VersionOwnerID(v.c, "1001"), invalid); err != nil {
		t.Fatal(err)
	}
	m.Data.ConfigScope = "system"
	if err := v.s.validateInstanceConfiguration(ctx, v.c, "1001", &m); err != nil {
		t.Fatal("system-only read user config", err)
	}
	m.Data.ConfigScope = "both"
	if err := v.s.validateInstanceConfiguration(ctx, v.c, "1001", &m); err == nil {
		t.Fatal("both ignored invalid included user config")
	}
	invalid.Owner = ""
	if err := v.store.save(ctx, v5Config, v5VersionOwnerID(v.c, ""), invalid); err != nil {
		t.Fatal(err)
	}
	m.Data.ConfigScope = "user"
	i, _ := v.s.installation(ctx, v.c, "")
	binding, capsule, err := v.s.preparation(ctx, i, &m)
	if err != nil || binding.SystemConfigRevision != 0 || strings.Contains(string(capsule), "not_declared") || strings.Contains(string(capsule), "system_configuration") {
		t.Fatal("excluded domain entered private preparation", binding, string(capsule), err)
	}
	if err := v.s.validateInstanceConfiguration(ctx, v.c, "", &m); err != nil {
		t.Fatal("global user-only installation read a system config", err)
	}
	if err := v.s.validateInstanceConfiguration(ctx, v.c, "1001", &m); err == nil {
		t.Fatal("user-only ignored current user's invalid config")
	}
}
