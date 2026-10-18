package plugin

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func v5ContributionRelease(t *testing.T, v v5TestPlatform, priv ed25519.PrivateKey, key string) v5TestPlatform {
	t.Helper()
	m := *v.manifest
	m.Key = key
	m.Runtime = "wasm"
	wasm := []byte{0, 'a', 's', 'm', 1, 0, 0, 0}
	m.Backend = &PluginV5Backend{Artifact: PluginV5BackendArtifact{Kind: "wasm", Path: "dist/module.wasm", Digest: v5BytesDigest(wasm)}, Transport: "host-call"}
	m.HostResources = &PluginV5ManifestResources{NetworkTargets: []string{}, StorageBytes: 1048576, CPUMillis: 100, MemoryMB: 16, MaxConcurrency: 1, TimeoutMS: 1000}
	if err := os.WriteFile(filepath.Join(v.dir, "dist/module.wasm"), wasm, 0600); err != nil {
		t.Fatal(err)
	}
	_, wire := CurrentPluginV5Catalogs()
	var catalog v5ExtensionManifestCatalog
	if err := json.Unmarshal(wire, &catalog); err != nil {
		t.Fatal(err)
	}
	m.Provides = nil
	for n, p := range catalog.Points {
		id := []string{"renderer", "chat", "source"}[n]
		m.Provides = append(m.Provides, PluginV5Provide{ID: id, Point: p.Point, ContractVersion: p.ContractVersion,
			InputSchema: p.InputSchema, OutputSchema: p.OutputSchema, Capabilities: p.Capabilities, DataMode: p.DataModes[0]})
	}
	wire, err := json.Marshal(m)
	if err != nil || os.WriteFile(filepath.Join(v.dir, "plugin.json"), wire, 0600) != nil {
		t.Fatal("write manifest", err)
	}
	raw, p, err := BuildPluginV5Package(v.dir)
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := SignPluginV5Package(raw, "test-key", priv)
	if err != nil {
		t.Fatal(err)
	}
	v.raw, v.envelope, v.manifest = raw, envelope, p.Manifest
	v.c = V5Coordinate{m.Publisher, m.Key, m.Version}
	v.admin = v5TestProof("admin", "adm."+key)
	return v
}

func newV5ContributionTestPlatform(t *testing.T) (v5TestPlatform, ed25519.PrivateKey) {
	t.Helper()
	v := newV5TestPlatform(t)
	priv, trust := testV5Trust(t)
	if err := v.s.catalog.SetTrust(trust); err != nil {
		t.Fatal(err)
	}
	return v5ContributionRelease(t, v, priv, "first-provider"), priv
}

func v5ContributionQueries() []V5ContributionQuery {
	return []V5ContributionQuery{
		{V5PreviewRenderer, "v1", "pdf"}, {V5AIChat, "v1", "text"}, {V5KnowledgeSource, "v1", "public_text"},
	}
}

func v5ContributionApproved(t *testing.T, v v5TestPlatform) V5Installation {
	t.Helper()
	v.installed(t)
	v.mutate(t, V5Mutation{Kind: "grant", IdempotencyKey: "approve", Approved: true, Consumes: []PluginV5Consume{}, Resources: v.manifest.HostResources}, true, v.admin)
	i, err := v.s.installation(context.Background(), v.c, "")
	if err != nil {
		t.Fatal(err)
	}
	return i
}

func TestV5ContributionTypedLookup(t *testing.T) {
	ctx := context.Background()
	v, _ := newV5ContributionTestPlatform(t)
	d := NewV5ContributionDirectory(v.s)
	i := v5ContributionApproved(t, v)
	op := v.mutate(t, V5Mutation{Kind: "enable", ExpectedGeneration: i.Generation, IdempotencyKey: "enable"}, true, v.admin)
	v.process(t)
	if _, err := d.Resolve(ctx, "", v5ContributionQueries()[0], nil); err != ErrV5ContributionUnpublished {
		t.Fatal("pending instance appeared in directory", err)
	}
	// Bounded host lifecycle observation only, not a real runner.
	v.ready(t, op)
	for _, q := range v5ContributionQueries() {
		t.Run(string(q.Point), func(t *testing.T) {
			b, err := d.Resolve(ctx, "", q, nil)
			if err != nil || b.Descriptor().Point != q.Point || b.Descriptor().V5Coordinate != v.c || d.Recheck(ctx, b) != nil {
				t.Fatal("typed current contribution", b.Descriptor(), err)
			}
			info := b.Descriptor()
			info.Capabilities[0] = "mutated"
			if b.Descriptor().Capabilities[0] != q.Capability {
				t.Fatal("descriptor aliases host state")
			}
			wire, err := json.Marshal(b)
			if err != nil || string(wire) != "{}" {
				t.Fatal("binding exported internal facts", string(wire), err)
			}
		})
	}
	for _, q := range []V5ContributionQuery{{V5AIChat, "v2", "text"}, {"unknown", "v1", "text"}, {V5AIChat, "v1", "private"}} {
		if _, err := d.Resolve(ctx, "", q, nil); err != ErrV5ContributionUnsupported {
			t.Fatal("unsupported contract accepted", q, err)
		}
	}
	if _, err := NewV5ContributionDirectory(nil).Resolve(ctx, "", v5ContributionQueries()[0], nil); err != ErrV5PlatformUnavailable {
		t.Fatal("missing platform allowed", err)
	}
}

func TestV5ContributionConflictAndExplicitSelection(t *testing.T) {
	ctx := context.Background()
	v, priv := newV5ContributionTestPlatform(t)
	i := v5ContributionApproved(t, v)
	v.ready(t, v.mutate(t, V5Mutation{Kind: "enable", ExpectedGeneration: i.Generation, IdempotencyKey: "enable"}, true, v.admin))
	d := NewV5ContributionDirectory(v.s)
	q := v5ContributionQueries()[1]
	automatic, err := d.Resolve(ctx, "", q, nil)
	if err != nil {
		t.Fatal(err)
	}
	ref := automatic.Descriptor().V5ContributionRef
	explicit, err := d.Resolve(ctx, "", q, &ref)
	if err != nil {
		t.Fatal(err)
	}
	second := v5ContributionRelease(t, v, priv, "second-provider")
	i = v5ContributionApproved(t, second)
	second.ready(t, second.mutate(t, V5Mutation{Kind: "enable", ExpectedGeneration: i.Generation, IdempotencyKey: "enable"}, true, second.admin))
	if _, err := d.Resolve(ctx, "", q, nil); err != ErrV5ContributionConflict {
		t.Fatal("second install silently won", err)
	}
	if err := d.Recheck(ctx, automatic); err != ErrV5ContributionConflict {
		t.Fatal("automatic binding ignored newly ready competitor", err)
	}
	if err := d.Recheck(ctx, explicit); err != nil {
		t.Fatal("explicit selection changed", err)
	}
	ref.Version = "9.9.9"
	if _, err := d.Resolve(ctx, "", q, &ref); err != ErrV5ContributionUnpublished {
		t.Fatal("missing selected version fell back", err)
	}
	ref.Version = v.c.Version
	ref.ID = "not-declared"
	if _, err := d.Resolve(ctx, "", q, &ref); err != ErrV5ContributionUnpublished {
		t.Fatal("undeclared ID admitted", err)
	}
	ref.ID = "../chat"
	if _, err := d.Resolve(ctx, "", q, &ref); err != ErrV5Invalid {
		t.Fatal("invalid reference admitted", err)
	}
}

func TestV5ContributionLiveLifecycle(t *testing.T) {
	for _, action := range []string{"disable", "revoke", "configure", "user-uninstall", "user-disabled", "missing-package", "trust-revoked", "startup", "new-generation"} {
		t.Run(action, func(t *testing.T) {
			ctx := context.Background()
			v, _ := newV5ContributionTestPlatform(t)
			i := v5ContributionApproved(t, v)
			v.ready(t, v.mutate(t, V5Mutation{Kind: "enable", ExpectedGeneration: i.Generation, IdempotencyKey: "enable"}, true, v.admin))
			owner := ""
			users := &v5TestUsers{active: true}
			v.s.Users = users
			if action == "user-uninstall" || action == "user-disabled" {
				owner = "1001"
				user := v5TestProof("user", owner)
				op := v.mutate(t, V5Mutation{Kind: "install", IdempotencyKey: "user-install"}, false, user)
				v.ready(t, v.mutate(t, V5Mutation{Kind: "enable", ExpectedGeneration: op.Generation, IdempotencyKey: "user-enable"}, false, user))
			}
			d := NewV5ContributionDirectory(v.s)
			q := v5ContributionQueries()[0]
			b, err := d.Resolve(ctx, owner, q, nil)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := d.Resolve(ctx, "1002", q, nil); err != ErrV5ContributionUnpublished {
				t.Fatal("cross-owner provider exposed", err)
			}
			i, _ = v.s.installation(ctx, v.c, "")
			switch action {
			case "disable", "new-generation":
				op := v.mutate(t, V5Mutation{Kind: "disable", ExpectedGeneration: i.Generation, IdempotencyKey: "disable"}, true, v.admin)
				if action == "new-generation" {
					v.ready(t, v.mutate(t, V5Mutation{Kind: "enable", ExpectedGeneration: op.Generation, IdempotencyKey: "again"}, true, v.admin))
				}
			case "revoke":
				g, _ := v.s.grant(ctx, v.c)
				v.mutate(t, V5Mutation{Kind: "revoke", ExpectedRevision: g.Revision, IdempotencyKey: "revoke"}, true, v.admin)
			case "configure":
				v.mutate(t, V5Mutation{Kind: "configure", Values: json.RawMessage(`{"locale":"en-US"}`), IdempotencyKey: "configure"}, true, v.admin)
			case "user-uninstall":
				u, _ := v.s.installation(ctx, v.c, owner)
				v.mutate(t, V5Mutation{Kind: "uninstall", ExpectedGeneration: u.Generation, IdempotencyKey: "uninstall"}, false, v5TestProof("user", owner))
			case "user-disabled":
				users.active = false
			case "missing-package":
				r, _ := v.s.release(ctx, v.c)
				if err := v.s.files.RemovePackage(r.Digest); err != nil {
					t.Fatal(err)
				}
			case "trust-revoked":
				if err := v.s.catalog.SetTrust([]byte(`{"contract":"campusos.plugin-trust/v1","keys":[]}`)); err != nil {
					t.Fatal(err)
				}
			case "startup":
				if err := v.s.ReconcileStartup(ctx); err != nil {
					t.Fatal(err)
				}
			}
			if err := d.Recheck(ctx, b); err == nil {
				t.Fatal("stale authorization/instance admitted")
			} else if action == "new-generation" && err != ErrV5ContributionStale {
				t.Fatal("old generation was refreshed in place", err)
			}
		})
	}
}

func TestV5ContributionPaginationAndBudget(t *testing.T) {
	ctx := context.Background()
	v, _ := newV5ContributionTestPlatform(t)
	i := v5ContributionApproved(t, v)
	v.ready(t, v.mutate(t, V5Mutation{Kind: "enable", ExpectedGeneration: i.Generation, IdempotencyKey: "enable"}, true, v.admin))
	// Approved stopped families have no release bytes and cannot contribute;
	// they still fill discovery pages. Lookup must not stop at an empty page.
	for n := 0; n < 50; n++ {
		c := V5Coordinate{"campusos", fmt.Sprintf("aa-%04d", n), "1.0.0"}
		for _, row := range []struct {
			kind v5RecordKind
			id   string
			data any
		}{{v5Global, v5StateID(c, ""), V5Installation{Publisher: c.Publisher, Key: c.Key, Version: c.Version, Installed: true}},
			{v5Grant, c.id(), V5Grant{V5Coordinate: c, Approved: true}}} {
			if err := v.store.save(ctx, row.kind, row.id, row.data); err != nil {
				t.Fatal(err)
			}
		}
	}
	d := NewV5ContributionDirectory(v.s)
	if _, err := d.Resolve(ctx, "", v5ContributionQueries()[0], nil); err != nil {
		t.Fatal("empty filtered first page lost ready provider", err)
	}
	for n := 50; n < 1000; n++ {
		c := V5Coordinate{"campusos", fmt.Sprintf("aa-%04d", n), "1.0.0"}
		_ = v.store.save(ctx, v5Global, v5StateID(c, ""), V5Installation{Publisher: c.Publisher, Key: c.Key, Version: c.Version, Installed: true})
		_ = v.store.save(ctx, v5Grant, c.id(), V5Grant{V5Coordinate: c, Approved: true})
	}
	if _, err := d.Resolve(ctx, "", v5ContributionQueries()[0], nil); err != ErrV5ContributionBudget {
		t.Fatal("truncated discovery claimed unique/no provider", err)
	}
	ref := V5ContributionRef{v.c, "renderer"}
	if _, err := d.Resolve(ctx, "", v5ContributionQueries()[0], &ref); err != nil {
		t.Fatal("explicit selection unnecessarily scanned inventory", err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := d.Resolve(cancelled, "", v5ContributionQueries()[0], &ref); err != context.Canceled {
		t.Fatal("cancel ignored", err)
	}
}
