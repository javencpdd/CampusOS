package plugin

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	identity "github.com/campusos/CampusOS/internal/modules/core/identity/port"
)

func newV5UIFixture(t *testing.T) (v5TestPlatform, identity.SessionPrincipalResolver) {
	t.Helper()
	v := newV5TestPlatform(t)
	v.manifest.Publisher = "campusos"
	v.manifest.Key = "pdf-viewer"
	v.manifest.Consumes = []PluginV5Consume{}
	for _, code := range []string{"ui.resource.selected.read", "ui.surface.open"} {
		v.manifest.Consumes = append(v.manifest.Consumes, PluginV5Consume{Capability: code, Scope: PluginV5ConsumeScope{Kind: "self"}, Purpose: "Read only the PDF selected through the current host resource context", Required: true})
	}
	wire, _ := json.Marshal(v.manifest)
	if err := os.WriteFile(filepath.Join(v.dir, "plugin.json"), wire, 0600); err != nil {
		t.Fatal(err)
	}
	raw, p, err := BuildPluginV5Package(v.dir)
	if err != nil {
		t.Fatal(err)
	}
	priv, trust := testV5Trust(t)
	envelope, err := SignPluginV5Package(raw, "test-key", priv)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := NewPluginV5PackageCatalog(trust)
	if err != nil {
		t.Fatal(err)
	}
	v.s.catalog = catalog
	v.raw = raw
	v.envelope = envelope
	v.c = V5Coordinate{p.Manifest.Publisher, p.Manifest.Key, p.Manifest.Version}
	i := v.installed(t)
	v.mutate(t, V5Mutation{Kind: "grant", Approved: true, Consumes: v.manifest.Consumes, IdempotencyKey: "ui-approve"}, true, v.admin)
	i, _ = v.s.installation(context.Background(), v.c, "")
	v.mutate(t, V5Mutation{Kind: "enable", ExpectedGeneration: i.Generation, IdempotencyKey: "ui-global-enable"}, true, v.admin)
	user := v5TestProof("user", "1001")
	v.mutate(t, V5Mutation{Kind: "install", IdempotencyKey: "ui-user-install"}, false, user)
	v.mutate(t, V5Mutation{Kind: "consent", Accepted: true, PurposeHash: V5PurposeHash(v.manifest), IdempotencyKey: "ui-consent"}, false, user)
	personal, _ := v.s.installation(context.Background(), v.c, "1001")
	v.mutate(t, V5Mutation{Kind: "enable", ExpectedGeneration: personal.Generation, IdempotencyKey: "ui-user-enable"}, false, user)
	return v, user
}

func TestV5UIOnlyCurrentProofAndResourceFences(t *testing.T) {
	for _, change := range []string{"disable", "uninstall", "consent", "grant", "config", "session", "other-owner", "trust"} {
		t.Run(change, func(t *testing.T) {
			v, user := newV5UIFixture(t)
			ctx := context.Background()
			active := user
			resolve := func(ctx context.Context) (identity.PrincipalContext, error) { return active(ctx) }
			f, err := v.s.PDFUI(ctx, "1001", resolve)
			if err != nil {
				t.Fatal(err)
			}
			before, _ := v.s.installation(ctx, v.c, "")
			if before.Observed == "ready" || before.ActiveGeneration != 0 {
				t.Fatal("UI fabricated backend readiness", before)
			}
			raw, mime, err := v.s.UIAsset(ctx, f, f.Artifact.Entrypoint, false, resolve)
			if err != nil || !strings.Contains(string(raw), "Isolated UI") || !strings.HasPrefix(mime, "text/html") {
				t.Fatal(mime, err)
			}
			for _, name := range []string{"plugin.json", "../plugin.json", "dist/user/../../plugin.json", "dist/admin/index.html"} {
				if _, _, err = v.s.UIAsset(ctx, f, name, false, resolve); err == nil {
					t.Fatal("cross-audience/path accepted", name)
				}
			}
			personal, _ := v.s.installation(ctx, v.c, "1001")
			switch change {
			case "disable", "uninstall":
				v.mutate(t, V5Mutation{Kind: change, ExpectedGeneration: personal.Generation, IdempotencyKey: "ui-change"}, false, user)
			case "consent":
				var consent V5Consent
				v.store.load(ctx, v5Consent, v5VersionOwnerID(v.c, "1001"), &consent)
				v.mutate(t, V5Mutation{Kind: "consent", ExpectedRevision: consent.Revision, PurposeHash: V5PurposeHash(v.manifest), IdempotencyKey: "ui-revoke-consent"}, false, user)
			case "grant":
				g, _ := v.s.grant(ctx, v.c)
				v.mutate(t, V5Mutation{Kind: "revoke", ExpectedRevision: g.Revision, IdempotencyKey: "ui-revoke"}, true, v.admin)
			case "config":
				v.mutate(t, V5Mutation{Kind: "configure", Values: json.RawMessage(`{"locale":"en-US"}`), ConfigurationPart: "values", IdempotencyKey: "ui-config"}, false, user)
			case "session":
				active = func(context.Context) (identity.PrincipalContext, error) {
					return identity.PrincipalContext{}, ErrV5Denied
				}
			case "other-owner":
				active = v5TestProof("user", "1002")
			case "trust":
				if err := v.s.catalog.SetTrust([]byte(`{"contract":"campusos.plugin-trust/v1","keys":[]}`)); err != nil {
					t.Fatal(err)
				}
			}
			if _, _, err = v.s.UIAsset(ctx, f, f.Artifact.Entrypoint, false, resolve); err == nil {
				t.Fatal("old UI asset proof survived", change)
			}
		})
	}
}

func TestV5UIHostOriginLeaseHeadersAndRevocation(t *testing.T) {
	v, user := newV5UIFixture(t)
	host, err := NewV5UIHost(v.s, "http://{isolation}.localhost:3003", []string{"http://127.0.0.1:3000"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	_, src, err := host.issue(ctx, v.c, false, "http://127.0.0.1:3000", user)
	if err != nil {
		t.Fatal(err)
	}
	_, same, err := host.issue(ctx, v.c, false, "http://127.0.0.1:3000", user)
	if err != nil || same != src {
		t.Fatal("identical proof not reused", err)
	}
	u, _ := url.Parse(src)
	if _, err := NewV5UIHost(v.s, "http://localhost:3003", []string{"http://127.0.0.1:3000"}); err == nil {
		t.Fatal("shared plugin origin accepted")
	}
	for _, origin := range []string{"http://{isolation}.bad..test:3003", "http://{isolation}.localhost:65536", "http://{isolation}.bad;test:3003"} {
		if _, err := NewV5UIHost(v.s, origin, []string{"http://127.0.0.1:3000"}); err == nil {
			t.Fatal("unsafe isolation origin", origin)
		}
	}
	if _, err := NewV5UIHost(v.s, "http://{isolation}.localhost:3003", []string{"http://parent.localhost:3003"}); err == nil {
		t.Fatal("parent inside untrusted isolation namespace")
	}
	other := v5TestProof("user", "1002")
	v.mutate(t, V5Mutation{Kind: "install", IdempotencyKey: "other-ui-install"}, false, other)
	v.mutate(t, V5Mutation{Kind: "consent", Accepted: true, PurposeHash: V5PurposeHash(v.manifest), IdempotencyKey: "other-ui-consent"}, false, other)
	otherInstall, _ := v.s.installation(ctx, v.c, "1002")
	v.mutate(t, V5Mutation{Kind: "enable", ExpectedGeneration: otherInstall.Generation, IdempotencyKey: "other-ui-enable"}, false, other)
	_, otherSrc, err := host.issue(ctx, v.c, false, "http://127.0.0.1:3000", other)
	if err != nil {
		t.Fatal(err)
	}
	otherOrigin, _ := url.Parse(otherSrc)
	if otherOrigin.Host == u.Host {
		t.Fatal("different owners share an isolation origin")
	}

	request := func(hostname, path, dest string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest("GET", path, nil)
		req.Host = hostname
		req.Header.Set("Sec-Fetch-Dest", dest)
		req.Header.Set("Authorization", "Bearer must-be-ignored")
		r := httptest.NewRecorder()
		host.ServeHTTP(r, req)
		return r
	}
	ok := request(u.Host, u.Path, "iframe")
	if ok.Code != 200 || !strings.Contains(ok.Header().Get("Content-Security-Policy"), "connect-src 'none'") || !strings.Contains(ok.Header().Get("Content-Security-Policy"), "frame-ancestors http://127.0.0.1:3000") || ok.Header().Get("Cache-Control") != "private, no-store" || ok.Header().Get("Set-Cookie") != "" || ok.Header().Get("Origin-Agent-Cluster") != "?1" {
		t.Fatal(ok.Code, ok.Header())
	}
	for _, negative := range []struct{ host, path, dest string }{{"127.0.0.1:8080", u.Path, "iframe"}, {otherOrigin.Host, u.Path, "iframe"}, {u.Host, u.Path, "document"}, {u.Host, "/api/v1/auth/me", ""}, {u.Host, strings.Replace(u.Path, "index.html", "../../plugin.json", 1), "iframe"}} {
		if r := request(negative.host, negative.path, negative.dest); r.Code != 404 {
			t.Fatal("public origin/path admitted", negative, r.Code)
		}
	}
	if _, _, err := host.issue(ctx, v.c, false, "https://attacker.test", user); err == nil {
		t.Fatal("unlisted parent admitted")
	}
	i, _ := v.s.installation(ctx, v.c, "")
	v.mutate(t, V5Mutation{Kind: "disable", ExpectedGeneration: i.Generation, IdempotencyKey: "disable-global"}, true, v.admin)
	if r := request(u.Host, u.Path, "iframe"); r.Code != 404 {
		t.Fatal("revoked asset remains available", r.Code)
	}
	host.now = func() time.Time { return time.Now().Add(time.Hour) }
	if r := request(u.Host, u.Path, "iframe"); r.Code != 404 {
		t.Fatal("expired lease admitted")
	}
}

func TestV5UIOwnerRotationAndExpiry(t *testing.T) {
	v, user := newV5UIFixture(t)
	ctx := context.Background()
	before, err := v.s.PDFUI(ctx, "1001", user)
	if err != nil {
		t.Fatal(err)
	}
	rotated := func(ctx context.Context) (identity.PrincipalContext, error) {
		p, err := user(ctx)
		p.CredentialID = "rotated.valid.session"
		return p, err
	}
	after, err := v.s.PDFUI(ctx, "1001", rotated)
	if err != nil || after.AuthorityProof != before.AuthorityProof || after.Proof == before.Proof {
		t.Fatal("owner continuity/credential lease fence", after, err)
	}
	if _, _, err := v.s.UIAsset(ctx, before, before.Artifact.Entrypoint, false, rotated); err == nil {
		t.Fatal("old credential-bound asset lease survived rotation")
	}
	grant, _ := v.s.grant(ctx, v.c)
	grant.ExpiresAtMS = time.Now().Add(-time.Second).UnixMilli()
	if err := v.store.save(ctx, v5Grant, v.c.id(), grant); err != nil {
		t.Fatal(err)
	}
	if _, err := v.s.PDFUI(ctx, "1001", user); err == nil {
		t.Fatal("expired grant admitted UI")
	}
	page, err := v.s.Inventory(ctx, false, "", 1, user)
	if err != nil || len(page.Items) != 1 || page.Items[0].Release.V5Coordinate != v.c || page.Items[0].Configuration != nil {
		t.Fatal("owner lost revoked installation cleanup or private config leaked in list", page, err)
	}
	other, err := v.s.Inventory(ctx, false, "", 1, v5TestProof("user", "1002"))
	if err != nil || len(other.Items) != 0 {
		t.Fatal("expired/other-owner install visible", other, err)
	}
	i, _ := v.s.installation(ctx, v.c, "1001")
	v.mutate(t, V5Mutation{Kind: "uninstall", ExpectedGeneration: i.Generation, IdempotencyKey: "cleanup-expired"}, false, user)
	page, err = v.s.Inventory(ctx, false, "", 1, user)
	if err != nil || len(page.Items) != 0 {
		t.Fatal("expired uninstalled package remained public", page, err)
	}
}
