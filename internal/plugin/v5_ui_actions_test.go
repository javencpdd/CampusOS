package plugin

import (
	"context"
	"encoding/json"
	identity "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/gin-gonic/gin"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenericUIActionsUseCapabilitiesAndRecheckAuthority(t *testing.T) {
	v, user, h := newUIActionFixture(t)
	ctx := context.Background()
	facts, err := v.s.UI(ctx, v.c, false, user)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	h.publicRead = func(_ context.Context, target string, max int64) ([]byte, string, error) {
		calls++
		if target != "https://public.example/document.xml" || max != 1<<20 {
			t.Fatal("unbounded or replaced target")
		}
		return []byte("bounded public bytes"), "text/plain", nil
	}
	q := V5UIActionRequest{Proof: facts.Proof, Action: "web.read", URL: "https://public.example/document.xml", Kind: "document"}
	out, err := h.uiAction(ctx, v.c, q, user)
	if err != nil || string(out.Bytes) != "bounded public bytes" || calls != 1 {
		t.Fatal("generic public read", out, err)
	}
	if _, err = h.uiAction(ctx, v.c, q, v5TestProof("user", "1002")); err == nil {
		t.Fatal("other owner read")
	}
	bad := q
	bad.Proof = "sha256:" + strings.Repeat("f", 64)
	if _, err = h.uiAction(ctx, v.c, bad, user); err == nil {
		t.Fatal("forged proof")
	}
	bad = q
	bad.Action = "shell.exec"
	if _, err = h.uiAction(ctx, v.c, bad, user); err == nil {
		t.Fatal("arbitrary host call")
	}
	out, err = h.uiAction(ctx, v.c, V5UIActionRequest{Proof: facts.Proof, Action: "editor.article.import"}, user)
	if err != nil || !out.Authorized {
		t.Fatal("generic editor permission", err)
	}
	h.publicRead = func(context.Context, string, int64) ([]byte, string, error) {
		own, _ := v.s.installation(ctx, v.c, "1001")
		v.mutate(t, V5Mutation{Kind: "disable", ExpectedGeneration: own.Generation, IdempotencyKey: "read-revocation"}, false, user)
		return []byte("must never escape"), "text/plain", nil
	}
	out, err = h.uiAction(ctx, v.c, q, user)
	if err == nil || len(out.Bytes) != 0 {
		t.Fatal("late content after revocation", err)
	}
}

func TestPublicResourceBrokerRejectsPrivateAndCredentialTargets(t *testing.T) {
	for _, target := range []string{"http://example.com/feed", "https://127.0.0.1/feed", "https://localhost/feed", "https://name:secret@example.com/feed", "https://example.com/feed#fragment", "file:///etc/passwd"} {
		if _, _, err := readPublicResource(context.Background(), target, 1024); err == nil {
			t.Fatal("unsafe target", target)
		}
	}
}

func TestUserUIActionsCannotInspectOrOverrideServerProxyPolicy(t *testing.T) {
	for _, fields := range []string{`,"profile":"clash"`, `,"proxy":"http://127.0.0.1:7890"`, `,"media_profile":"clash"`} {
		response := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(response)
		c.Request = httptest.NewRequest("POST", "/ui-actions", strings.NewReader(`{"proof":"sha256:`+strings.Repeat("a", 64)+`","action":"web.read","url":"https://example.com","kind":"document"`+fields+`}`))
		(&V5HTTPHandler{Platform: &V5Platform{}}).UIAction(c)
		if response.Code != http.StatusBadRequest {
			t.Fatal("user could submit server proxy settings", response.Code)
		}
	}
	v, user, h := newUIActionFixture(t)
	facts, err := v.s.UI(context.Background(), v.c, false, user)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = h.uiAction(context.Background(), v.c, V5UIActionRequest{Proof: facts.Proof, Action: "web.profiles"}, user); err == nil {
		t.Fatal("server proxy registry exposed to user")
	}
}

func TestDelegatedGenericConfigurationWritePinsCurrentUIProof(t *testing.T) {
	v, user, _ := newUIActionFixture(t)
	ctx := context.Background()
	facts, err := v.s.UI(ctx, v.c, false, user)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := v.s.configuration(ctx, v.c, "1001", v.manifest)
	if err != nil {
		t.Fatal(err)
	}
	intent := V5Mutation{Version: v.c.Version, Kind: "configure", ConfigurationPart: "values", ExpectedRevision: cfg.Revision, Values: json.RawMessage(`{"endpoint":"https://public.example/updated"}`), IdempotencyKey: "generic-config", UIProof: facts.Proof}
	forged := intent
	forged.UIProof = "sha256:" + strings.Repeat("0", 64)
	forged.IdempotencyKey = "forged-config"
	if _, err = v.s.Mutate(ctx, v.c.Publisher, v.c.Key, forged, false, user); err == nil {
		t.Fatal("forged UI proof wrote configuration")
	}
	if _, err = v.s.Mutate(ctx, v.c.Publisher, v.c.Key, intent, false, user); err != nil {
		t.Fatal(err)
	}
	cfg, err = v.s.configuration(ctx, v.c, "1001", v.manifest)
	if err != nil {
		t.Fatal(err)
	}
	intent.ExpectedRevision = cfg.Revision
	intent.IdempotencyKey = "old-proof-config"
	if _, err = v.s.Mutate(ctx, v.c.Publisher, v.c.Key, intent, false, user); err == nil {
		t.Fatal("old context proof wrote newer configuration")
	}
}

func TestOpaqueUIAssetsNeedCurrentLeaseAndEnforceSandbox(t *testing.T) {
	v, user, _ := newUIActionFixture(t)
	host, err := NewV5UIHost(v.s, "https://{isolation}.plugins.test", []string{"http://127.0.0.1:3000"})
	if err != nil {
		t.Fatal(err)
	}
	facts, source, err := host.issue(context.Background(), v.c, false, "http://127.0.0.1:3000", user)
	if err != nil || !strings.HasPrefix(source, "http://127.0.0.1:3000/api/v1/plugin-v5/ui-assets/") {
		t.Fatal("DNS-free source", source, err)
	}
	id := strings.Split(strings.TrimPrefix(source, "http://127.0.0.1:3000/api/v1/plugin-v5/ui-assets/"), "/")[0]
	read := func(destination, lease string) *httptest.ResponseRecorder {
		request := httptest.NewRequest("GET", source, nil)
		request.Header.Set("Sec-Fetch-Dest", destination)
		response := httptest.NewRecorder()
		host.serveOpaque(response, request, lease, facts.Artifact.Entrypoint)
		return response
	}
	response := read("iframe", id)
	if response.Code != 200 || !strings.Contains(response.Header().Get("Content-Security-Policy"), "sandbox allow-scripts;") || strings.Contains(response.Header().Get("Content-Security-Policy"), "allow-same-origin") || response.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Fatal("unsafe opaque asset", response.Code, response.Header())
	}
	if read("document", id).Code != 404 || read("serviceworker", id).Code != 404 || read("iframe", strings.Repeat("0", 32)).Code != 404 {
		t.Fatal("lease or top-level bypass")
	}
	own, _ := v.s.installation(context.Background(), v.c, "1001")
	v.mutate(t, V5Mutation{Kind: "disable", ExpectedGeneration: own.Generation, IdempotencyKey: "opaque-off"}, false, user)
	if read("iframe", id).Code != 404 {
		t.Fatal("revoked lease still served")
	}
}
func newUIActionFixture(t *testing.T) (v5TestPlatform, identity.SessionPrincipalResolver, *V5HTTPHandler) {
	return newUIActionClientFixture(t, true)
}
func newUIActionClientFixture(t *testing.T, allowLocal bool) (v5TestPlatform, identity.SessionPrincipalResolver, *V5HTTPHandler) {
	t.Helper()
	v := newV5TestPlatform(t)
	v.manifest.Data.ConfigScope = "user"
	v.manifest.UI[0].Isolation = "opaque"
	v.manifest.Consumes = []PluginV5Consume{}
	for _, code := range []string{"ui.surface.open", V5WebPublicRead, V5ArticleCompose, V5WebLocalRead} {
		v.manifest.Consumes = append(v.manifest.Consumes, PluginV5Consume{Capability: code, Scope: PluginV5ConsumeScope{Kind: "self"}, Purpose: "Read public documents or prepare user-confirmed content", Required: code != V5WebLocalRead})
	}
	v.manifest.Configuration = json.RawMessage(`{"contract":"campusos.plugin-config/v1","version":"v1","schema":{"type":"object","additionalProperties":false,"properties":{"endpoint":{"type":"string","title":"Endpoint","maxLength":512}},"required":["endpoint"]},"defaults":{"endpoint":"https://public.example/document.xml"},"selectors":[],"secret_names":[]}`)
	wire, _ := json.Marshal(v.manifest)
	if e := os.WriteFile(filepath.Join(v.dir, "plugin.json"), wire, 0600); e != nil {
		t.Fatal(e)
	}
	raw, p, e := BuildPluginV5Package(v.dir)
	if e != nil {
		t.Fatal(e)
	}
	private, trust := testV5Trust(t)
	envelope, e := SignPluginV5Package(raw, "test-key", private)
	if e != nil {
		t.Fatal(e)
	}
	catalog, e := NewPluginV5PackageCatalog(trust)
	if e != nil {
		t.Fatal(e)
	}
	v.s.catalog = catalog
	v.raw = raw
	v.envelope = envelope
	v.c = V5Coordinate{p.Manifest.Publisher, p.Manifest.Key, p.Manifest.Version}
	v.installed(t)
	consumes := []PluginV5Consume{}
	for _, c := range v.manifest.Consumes {
		if allowLocal || c.Capability != V5WebLocalRead {
			consumes = append(consumes, c)
		}
	}
	v.mutate(t, V5Mutation{Kind: "grant", Approved: true, Consumes: consumes, IdempotencyKey: "public-grant"}, true, v.admin)
	global, _ := v.s.installation(context.Background(), v.c, "")
	v.mutate(t, V5Mutation{Kind: "enable", ExpectedGeneration: global.Generation, IdempotencyKey: "rss-global-on"}, true, v.admin)
	user := v5TestProof("user", "1001")
	v.mutate(t, V5Mutation{Kind: "install", IdempotencyKey: "rss-personal"}, false, user)
	v.mutate(t, V5Mutation{Kind: "consent", Accepted: true, PurposeHash: V5PurposeHash(v.manifest), IdempotencyKey: "rss-consent"}, false, user)
	own, _ := v.s.installation(context.Background(), v.c, "1001")
	v.mutate(t, V5Mutation{Kind: "enable", ExpectedGeneration: own.Generation, IdempotencyKey: "rss-personal-on"}, false, user)
	h := &V5HTTPHandler{Platform: v.s, ValidatePublicImage: func(raw []byte) ([]byte, string, error) { return raw, "image/png", nil }}
	return v, user, h
}
