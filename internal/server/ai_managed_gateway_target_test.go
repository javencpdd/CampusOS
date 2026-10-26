//go:build campusos_acceptance

package server

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/modules/core/identity/portadapt"
	identityrepo "github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	identityservice "github.com/campusos/CampusOS/internal/modules/core/identity/service"
	"github.com/campusos/CampusOS/internal/modules/features/ai"
	"github.com/campusos/CampusOS/internal/transport/httpapi"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/campusos/CampusOS/pkg/config"
	"github.com/jackc/pgx/v5/pgxpool"
)

type aiOwnedDNS struct{ unsafe atomic.Bool }

func (d *aiOwnedDNS) LookupIPAddr(context.Context, string) ([]net.IPAddr, error) {
	ips := []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}}
	if d.unsafe.Load() {
		ips = append(ips, net.IPAddr{IP: net.ParseIP("127.0.0.1")})
	}
	return ips, nil
}

type aiOwnedDial struct {
	target, port string
	calls        atomic.Int32
}

func (d *aiOwnedDial) DialContext(c context.Context, network, address string) (net.Conn, error) {
	if address != net.JoinHostPort("8.8.8.8", d.port) {
		return nil, errors.New("unowned dial")
	}
	d.calls.Add(1)
	return (&net.Dialer{}).DialContext(c, network, d.target)
}

// The production Server/router/Identity/PG and shared Egress are exercised.
// Only public DNS and physical dial are pinned to an owned TLS protocol peer;
// these acceptance hooks do not exist in a normal production build.
func TestAIManagedGatewayPostgres(t *testing.T) {
	dsn, root, repo := os.Getenv("CAMPUSOS_AI_MANAGED_TARGET_DSN"), os.Getenv("CAMPUSOS_AI_MANAGED_ROOT"), os.Getenv("CAMPUSOS_AI_REPO")
	if dsn == "" {
		t.Skip("requires owned14a managed PG/HTTP target")
	}
	pc, e := pgxpool.ParseConfig(dsn)
	if e != nil || pc.ConnConfig.Host != "127.0.0.1" || pc.ConnConfig.Database != "campusos_v12_14a_managed" || !filepath.IsAbs(root) || !strings.Contains(filepath.ToSlash(root), "/.cache/v12-14a-exit/") || !filepath.IsAbs(repo) {
		t.Fatal("refuse unowned target")
	}
	t.Chdir(root)
	for _, dir := range []string{"migrations", "modules"} {
		if e := os.CopyFS(filepath.Join(root, dir), os.DirFS(filepath.Join(repo, dir))); e != nil {
			t.Fatal(e)
		}
	}
	ctx := t.Context()
	pool, e := pgxpool.NewWithConfig(ctx, pc)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	sql := func(q string, args ...any) {
		t.Helper()
		if _, e := pool.Exec(ctx, q, args...); e != nil {
			t.Fatal(e)
		}
	}
	checks := []string{}
	check := func(s string) { checks = append(checks, s); t.Log(s) }
	newID := func() string {
		var b [16]byte
		if _, e := rand.Read(b[:]); e != nil {
			t.Fatal(e)
		}
		return "ar_" + hex.EncodeToString(b[:])
	}
	secret, localSecret := adminSPABrowserSecret(t), adminSPABrowserSecret(t)
	master := adminSPABrowserSecret(t)
	const prompt = "SYNTHETIC_PRIVATE_MANAGED_PROMPT"
	var mode atomic.Value
	mode.Store("success")
	var calls, escaped atomic.Int32
	started := make(chan struct{}, 16)
	handler := func(local bool) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/v1/chat/completions" {
				escaped.Add(1)
				http.Error(w, "unowned path", 500)
				return
			}
			calls.Add(1)
			key, model := secret, "remote-model"
			if local {
				key, model = localSecret, "local-model"
			}
			var req struct {
				Model     string       `json:"model"`
				Messages  []ai.Message `json:"messages"`
				MaxTokens int          `json:"max_tokens"`
			}
			decoder := json.NewDecoder(r.Body)
			decoder.DisallowUnknownFields()
			if r.Method != "POST" || r.Header.Get("Authorization") != "Bearer "+key || decoder.Decode(&req) != nil || req.Model != model || len(req.Messages) != 1 || req.Messages[0].Content != prompt || req.MaxTokens != 32 {
				t.Error("fixed Profile wire contract")
				w.WriteHeader(400)
				return
			}
			switch mode.Load() {
			case "wait":
				started <- struct{}{}
				select {
				case <-r.Context().Done():
				case <-time.After(5 * time.Second):
				}
				return
			case "error":
				http.Error(w, "UNTRUSTED "+prompt+" "+key, 503)
				return
			case "redirect":
				w.Header().Set("Location", "/escape")
				w.WriteHeader(307)
				return
			case "oversize":
				_, _ = w.Write([]byte(strings.Repeat("X", ai.MaxChatResponseBytes+1)))
				return
			case "tools":
				_, _ = w.Write([]byte(`{"model":"` + model + `","choices":[{"message":{"role":"assistant","content":"","tool_calls":[{"name":"forbidden"}]},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`))
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"model": model, "choices": []any{map[string]any{"message": map[string]string{"role": "assistant", "content": "SYNTHETIC_MANAGED_RESULT"}, "finish_reason": "stop"}}, "usage": map[string]int{"prompt_tokens": 3, "completion_tokens": 2, "total_tokens": 5}})
		}
	}
	local := httptest.NewServer(handler(true))
	defer local.Close()
	remote := httptest.NewTLSServer(handler(false))
	defer remote.Close()
	remoteURL, _ := url.Parse(remote.URL)
	_, port, _ := net.SplitHostPort(remoteURL.Host)
	roots := x509.NewCertPool()
	roots.AddCert(remote.Certificate())
	dns, dial := &aiOwnedDNS{}, &aiOwnedDial{target: remoteURL.Host, port: port}
	cfg := config.Load()
	cfg.Database.DSN = dsn
	cfg.Plugin.DataDir = filepath.Join(root, "data/plugin_data")
	cfg.Redis.Enabled = false
	cfg.AI = config.AIConfig{Enabled: true, BaseURL: "https://example.com:" + port + "/v1", Model: "remote-model", APIKey: secret, LocalBaseURL: local.URL + "/v1", LocalModel: "local-model", LocalAPIKey: localSecret, ActiveKeyID: "owned14", EncryptionKeys: "owned14:" + master, DefaultProfile: "remote-chat", BudgetRequests: 100, MaxConcurrent: 2, MaxOutputTokens: 64, Timeout: "2s"}
	var s *Server
	var infra *infrastructureBootstrap
	var listener *httptest.Server
	buildRouter := func(s *Server, in *infrastructureBootstrap) *httptest.Server {
		pluginHandler, e := s.pluginHTTPHandler()
		if e != nil {
			t.Fatal(e)
		}
		router := httpapi.Build(httpapi.Dependencies{
			JWT: s.identity.JWTManager(), SessionVerifier: s.identity.Sessions(), Permissions: s.identity.Permissions(), AdminAccess: s.identity.AdminAccess(), AdminMFA: s.identity.MFA(), AdminJWT: s.identity.AdminDomainJWT(), AdminSessions: portadapt.NewAdminDomainSessionVerifier(s.identity.AdminDomainSessions()), AdminCodes: portadapt.NewAdminCodeChecker(s.identity.AdminDomainLifecycle()),
			Features: s.features, Feature: s.featureHandler, ModuleCatalog: s.moduleCatalog, PluginManager: s.manager, Identity: s.identity.Handlers(), Community: s.community.Handlers(), AcademicTerm: s.academicTerm.Handler(), UserStorage: s.storage.Handler(), Space: s.space.Handler(), Plugin: pluginHandler, PluginV5: s.pluginsV5Handler(), AI: s.ai.Handler(), Integration: s.integration.Handler(), Webhook: s.webhook.Handler(), MCP: s.mcp.Handler(), Message: s.message.Handler(), Homepage: s.appearance.HomepageHandler(), WebTheme: s.appearance.WebThemeHandler(), RichText: s.richtext.Handler(), Schedule: s.schedule.Handler(), PersonalDocuments: s.personalDocuments.Handler(), Knowledge: s.knowledge.Handler(), MutualAid: s.mutualAid.Handler(), Secondhand: s.secondhand.Handler(), PlatformLog: s.platformLog.Handler(), Moderation: s.moderation.Handler(), Reliability: s.reliability.Handler(), EmailDelivery: s.emailDelivery.Handler(), Metrics: in.metrics,
		})
		return httptest.NewServer(router)
	}
	start := func(c *config.Config) {
		s = New(c)
		var err error
		infra, err = s.startInfrastructure()
		if err != nil {
			t.Fatal("normal Server", err)
		}
		s.ai.AcceptanceRemote(dns, dial, &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12})
		listener = buildRouter(s, infra)
	}
	stop := func() {
		if listener != nil {
			listener.Close()
			listener = nil
		}
		if infra != nil {
			infra.Stop()
			infra = nil
		}
	}
	defer stop()
	start(cfg)
	if !s.ai.Service().Status().Ready || calls.Load() != 0 {
		f, ok := s.features.Get(ai.FeatureID)
		t.Fatal("current managed definition/protection must be ready without auto-execution", s.ai.Service().Status(), f, ok)
	}
	sql(`INSERT INTO users(id,username,nickname,email) VALUES(714101,'owned14-first','Owned14','owned14-first@example.test'),(714102,'owned14-second','Owned14Other','owned14-other@example.test')`)
	issue := func(id string) (string, identityport.SessionPrincipalResolver, *auth.JWTClaims) {
		t.Helper()
		u, e := identityrepo.NewPgUserRepository(pool).GetByID(ctx, id)
		if e != nil {
			t.Fatal(e)
		}
		tokens, e := s.identity.Sessions().Issue(ctx, u, identityservice.SessionMetadata{DeviceName: "Owned managed target"})
		if e != nil {
			t.Fatal(e)
		}
		claims, e := s.identity.JWTManager().VerifyAccessToken(tokens.AccessToken)
		if e != nil {
			t.Fatal(e)
		}
		return tokens.AccessToken, func(c context.Context) (identityport.PrincipalContext, error) {
			return identityport.ResolveUserSessionPrincipal(c, s.identity.Sessions(), s.identity.Sessions(), claims)
		}, claims
	}
	userToken, userResolve, claims := issue("714101")
	_, otherResolve, _ := issue("714102")
	admins := identityrepo.NewPgAdminDomainRepository(pool)
	if e = admins.CreateAccount(ctx, identityrepo.AdminDomainAccount{ID: "714101", Username: "owned14-admin", Email: "owned14-admin@example.test", DisplayName: "Owned14Admin"}); e != nil {
		t.Fatal(e)
	}
	if e = admins.BindRole(ctx, identityrepo.AdminDomainRoleBinding{ID: "arb.ai.14", AccountID: "714101", RoleID: "security_admin", GrantedBy: "714101"}); e != nil {
		t.Fatal(e)
	}
	adminSession := func(sid, strength string) (string, identityport.SessionPrincipalResolver) {
		now := time.Now().UTC()
		var mfa *time.Time
		if strength == "mfa" {
			mfa = &now
		}
		if e = identityrepo.NewPgAdminSessionRepository(pool).CreateSession(ctx, identityrepo.AdminSession{ID: sid, AccountID: "714101", RefreshTokenDigest: adminSPABrowserSecret(t), TokenFamilyID: sid, AuthenticationStrength: strength, MFAAuthenticatedAt: mfa, CreatedAt: now, ExpiresAt: now.Add(time.Hour)}); e != nil {
			t.Fatal(e)
		}
		token, e := s.identity.AdminDomainJWT().GenerateAccessToken("714101", "Owned14", auth.AccessTokenContext{SessionID: sid, AuthVersion: 1})
		if e != nil {
			t.Fatal(e)
		}
		return token, func(c context.Context) (identityport.PrincipalContext, error) {
			return identityport.ResolveAdminSessionPrincipal(c, s.identity.AdminDomainSessions(), &auth.JWTClaims{UserID: "714101", SessionID: sid, AuthVersion: 1, TokenType: auth.AccessTokenType})
		}
	}
	adminToken, adminResolve := adminSession("ads_owned14_mfa", "mfa")
	passwordToken, _ := adminSession("ads_owned14_password", "password")
	requests := 0
	call := func(method, path, token string, body any, want int, browser bool) []byte {
		t.Helper()
		var r io.Reader
		if body != nil {
			b, _ := json.Marshal(body)
			r = bytes.NewReader(b)
		}
		req, e := http.NewRequestWithContext(ctx, method, listener.URL+path, r)
		if e != nil {
			t.Fatal(e)
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		if browser {
			req.Header.Set("Origin", listener.URL)
		}
		resp, e := http.DefaultClient.Do(req)
		if e != nil {
			t.Fatal(e)
		}
		defer resp.Body.Close()
		raw, e := io.ReadAll(resp.Body)
		requests++
		if e != nil || resp.StatusCode != want || resp.Header.Get("Cache-Control") != "private, no-store" {
			t.Fatalf("%s %s status=%d want=%d cache=%s body=%s", method, path, resp.StatusCode, want, resp.Header.Get("Cache-Control"), raw)
		}
		for _, bad := range []string{secret, localSecret, master, prompt, "SYNTHETIC_MANAGED_RESULT", "secret_ref", "fingerprint"} {
			if bytes.Contains(raw, []byte(bad)) {
				t.Fatal("Admin metadata leaked protected field")
			}
		}
		return raw
	}
	inventory := func() ai.ManagedInventory {
		t.Helper()
		raw := call("GET", "/api/v1/ai/profiles", adminToken, nil, 200, false)
		var envelope struct {
			Data ai.ManagedInventory `json:"data"`
		}
		if json.Unmarshal(raw, &envelope) != nil || len(envelope.Data.Profiles) != 2 || len(envelope.Data.Bindings) != 1 {
			t.Fatal("inventory shape")
		}
		return envelope.Data
	}
	profile := func(id string) ai.ManagedProfile {
		for _, p := range inventory().Profiles {
			if p.ID == id {
				return p
			}
		}
		t.Fatal("profile missing")
		return ai.ManagedProfile{}
	}
	binding := func() ai.ManagedBinding { return inventory().Bindings[0] }
	bind := func(id string) {
		b, p := binding(), profile(id)
		call("POST", "/api/v1/ai/bindings/chat", adminToken, map[string]string{"profile_id": id, "binding_version": strconv.FormatInt(b.Revision, 10), "profile_version": strconv.FormatInt(p.Revision, 10)}, 200, false)
	}
	state := func(id string, enabled bool) {
		p := profile(id)
		call("POST", "/api/v1/ai/profiles/"+id+"/state", adminToken, map[string]any{"version": strconv.FormatInt(p.Revision, 10), "enabled": enabled}, 200, false)
	}
	request := ai.ChatRequest{Messages: []ai.Message{{Role: "user", Content: prompt}}, MaxTokens: 32}
	invoke := func() (ai.ManagedResult, error) {
		return s.ai.Managed().Invoke(ctx, userResolve, newID(), binding().Revision, request)
	}
	call("GET", "/api/v1/ai/profiles", "", nil, 401, false)
	call("GET", "/api/v1/ai/profiles", userToken, nil, 401, false)
	b, p := binding(), profile("local-chat")
	body := map[string]string{"profile_id": "local-chat", "binding_version": fmt.Sprint(b.Revision), "profile_version": fmt.Sprint(p.Revision)}
	call("POST", "/api/v1/ai/bindings/chat", passwordToken, body, 403, false)
	call("POST", "/api/v1/ai/bindings/chat", adminToken, body, 403, true)
	call("POST", "/api/v1/ai/bindings/chat", adminToken, map[string]string{"profile_id": "local-chat", "binding_version": "1", "profile_version": fmt.Sprint(p.Revision)}, 409, false)
	body["endpoint"] = "http://untrusted.test"
	call("POST", "/api/v1/ai/bindings/chat", adminToken, body, 400, false)
	// Positive browser command uses the ordinary independent Admin token plus
	// a matching double-submit cookie/header; a bearer alone is insufficient.
	remoteProfile := profile("remote-chat")
	csrfBody, _ := json.Marshal(map[string]any{"version": fmt.Sprint(remoteProfile.Revision), "enabled": true})
	csrfRequest, _ := http.NewRequestWithContext(ctx, "POST", listener.URL+"/api/v1/ai/profiles/remote-chat/state", bytes.NewReader(csrfBody))
	csrfRequest.Header.Set("Authorization", "Bearer "+adminToken)
	csrfRequest.Header.Set("Origin", listener.URL)
	csrfRequest.Header.Set("Content-Type", "application/json")
	csrfRequest.Header.Set("Cookie", "campusos_admin_csrf=owned-matching-csrf")
	csrfRequest.Header.Set("X-Admin-CSRF-Token", "owned-matching-csrf")
	csrfResponse, e := http.DefaultClient.Do(csrfRequest)
	if e != nil {
		t.Fatal(e)
	}
	_, _ = io.Copy(io.Discard, csrfResponse.Body)
	csrfResponse.Body.Close()
	requests++
	if csrfResponse.StatusCode != 200 || csrfResponse.Header.Get("Cache-Control") != "private, no-store" {
		t.Fatal("valid browser CSRF command denied")
	}
	if _, e = s.ai.Managed().Invoke(ctx, adminResolve, newID(), b.Revision, request); !errors.Is(e, ai.ErrManagedDenied) {
		t.Fatal("same-ID Admin became User")
	}
	if _, e = s.ai.Service().ChatCompletion(ctx, request); e == nil || calls.Load() != 0 {
		t.Fatal("legacy capability bypass")
	}
	check("current_independent_Admin_read_manage_MFA_CSRF_CAS_closed_JSON_same_ID_and_legacy_bypass_denied")
	first, e := invoke()
	if e != nil || first.Response.Content != "SYNTHETIC_MANAGED_RESULT" || first.Run.Contract != ai.ManagedContractVersion || len(first.Run.Events) != 3 || first.Run.Charged == nil || *first.Run.Charged != 1 || !first.Run.UsageKnown {
		t.Fatal("managed remote", first.Run, e)
	}
	for i, event := range first.Run.Events {
		if event.Contract != ai.ManagedEventsVersion || event.Ordinal != i+1 || event.RunID != first.Run.ID {
			t.Fatal("ordered versioned event")
		}
	}
	if first.Run.Events[0].Kind != "started" || first.Run.Events[1].Kind != "usage" || first.Run.Events[2].Kind != "completed" {
		t.Fatal("event kinds")
	}
	if _, e = s.ai.Managed().Run(ctx, otherResolve, first.Run.ID); !errors.Is(e, ai.ErrManagedDenied) {
		t.Fatal("cross-owner metadata")
	}
	n := calls.Load()
	if _, e = s.ai.Managed().Invoke(ctx, userResolve, first.Run.ID, b.Revision, request); !errors.Is(e, ai.ErrManagedReplay) || calls.Load() != n {
		t.Fatal("replay")
	}
	check("actual_shared_Egress_public_DNS_pin_TLS_Secret_fixed_Profile_text_and_ordered_events")
	dialBefore := dial.calls.Load()
	dns.unsafe.Store(true)
	if out, e := invoke(); e == nil || out.Response.Content != "" || dial.calls.Load() != dialBefore {
		t.Fatal("mixed DNS private address escaped")
	}
	dns.unsafe.Store(false)
	for _, m := range []string{"error", "redirect", "oversize", "tools"} {
		mode.Store(m)
		n := calls.Load()
		out, e := invoke()
		if e == nil || out.Response.Content != "" || calls.Load() != n+1 || strings.Contains(e.Error(), secret) || strings.Contains(e.Error(), prompt) {
			t.Fatal("unsafe provider response/retry", m, e)
		}
	}
	if escaped.Load() != 0 {
		t.Fatal("redirect followed")
	}
	mode.Store("success")
	check("mixed_public_private_DNS_rejects_before_dial_error_body_tool_oversize_redirect_no_retry_or_fallback")
	bind("local-chat")
	localOut, e := invoke()
	if e != nil || localOut.Response.Model != "local-model" || localOut.Run.ProfileID != "local-chat" || localOut.Run.InstanceID == first.Run.InstanceID {
		t.Fatal("replaceable local profile", e)
	}
	check("audited_CAS_rebinding_replaces_Instance_and_uses_actual_fixed_loopback_local_model")
	// Current identity and lifecycle facts cancel real in-flight I/O. No buffered
	// answer escapes; the unknown dispatched cost remains charged exactly once.
	pending := func(change func()) ai.ManagedResult {
		t.Helper()
		mode.Store("wait")
		rev := binding().Revision
		id := newID()
		type result struct {
			o ai.ManagedResult
			e error
		}
		done := make(chan result, 1)
		go func() { o, e := s.ai.Managed().Invoke(ctx, userResolve, id, rev, request); done <- result{o, e} }()
		select {
		case <-started:
		case <-time.After(3 * time.Second):
			t.Fatal("actual pending I/O required")
		}
		change()
		select {
		case r := <-done:
			if r.e == nil || r.o.Response.Content != "" || r.o.Run.Charged == nil || *r.o.Run.Charged != 1 || r.o.Run.UsageKnown {
				t.Fatal("changed authority released answer or freed unknown budget", r.e)
			}
			mode.Store("success")
			return r.o
		case <-time.After(4 * time.Second):
			t.Fatal("in-flight cancellation exceeded deadline")
		}
		return ai.ManagedResult{}
	}
	pending(func() {
		if e := s.identity.Sessions().RevokeCurrent(ctx, "714101", claims.SessionID); e != nil {
			t.Fatal(e)
		}
	})
	userToken, userResolve, claims = issue("714101")
	pending(func() { bind("remote-chat") })
	pending(func() { state("remote-chat", false) })
	if out, e := invoke(); e == nil || out.Response.Content != "" {
		t.Fatal("disabled profile executed")
	}
	state("remote-chat", true)
	pending(func() {
		if _, e := s.features.Request(ai.FeatureID, false); e != nil {
			t.Fatal(e)
		}
	})
	if _, e := s.features.Request(ai.FeatureID, true); e != nil {
		t.Fatal(e)
	}
	check("current_User_revocation_binding_change_Profile_disable_and_Feature_disable_cancel_inflight_without_answer_or_budget_reset")
	// Persistent concurrency cannot be bypassed by spawning another instance.
	mode.Store("wait")
	rev := binding().Revision
	cancelCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() { _, e := s.ai.Managed().Invoke(cancelCtx, userResolve, newID(), rev, request); done <- e }()
	}
	for i := 0; i < 2; i++ {
		select {
		case <-started:
		case <-time.After(3 * time.Second):
			t.Fatal("missing concurrent call")
		}
	}
	if _, e = s.ai.Managed().Invoke(ctx, userResolve, newID(), rev, request); !errors.Is(e, ai.ErrManagedBudget) {
		t.Fatal("concurrency limit", e)
	}
	cancel()
	for i := 0; i < 2; i++ {
		if <-done == nil {
			t.Fatal("cancel returned answer")
		}
	}
	mode.Store("success")
	check("persistent_concurrent_reservations_enforce_two_requests_and_cancel_conservatively")
	// Required audited command failures are observed at the actual PG boundary.
	for _, fault := range []string{"reserve", "dispatch", "settle"} {
		sql(`CREATE FUNCTION owned_ai_fault() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.command_code='ai.call.` + fault + `' THEN RAISE EXCEPTION 'owned fault'; END IF; RETURN NEW; END $$;CREATE TRIGGER owned_ai_fault BEFORE INSERT ON platform_command_audits FOR EACH ROW EXECUTE FUNCTION owned_ai_fault()`)
		id := newID()
		n := calls.Load()
		out, e := s.ai.Managed().Invoke(ctx, userResolve, id, binding().Revision, request)
		if e == nil || out.Response.Content != "" {
			t.Fatal("required audit failure released answer", fault)
		}
		if fault == "settle" && (out.Run.State != "dispatched" || out.Run.Contract != ai.ManagedContractVersion) {
			t.Fatal("uncertain dispatch reported as a reusable reservation")
		}
		expected := n
		if fault == "settle" {
			expected++
		}
		if calls.Load() != expected {
			t.Fatal("audit rollback dispatched/replayed", fault)
		}
		sql(`DROP TRIGGER owned_ai_fault ON platform_command_audits;DROP FUNCTION owned_ai_fault()`)
		if fault == "settle" {
			sql(`UPDATE ai_model_runs SET lease_until_ms=1 WHERE id=$1`, id)
			deadline := time.Now().Add(3 * time.Second)
			for {
				var st string
				if e = pool.QueryRow(ctx, `SELECT state FROM ai_model_runs WHERE id=$1`, id).Scan(&st); e != nil {
					t.Fatal(e)
				}
				if st == "unknown" {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("normal resident settlement")
				}
				time.Sleep(20 * time.Millisecond)
			}
			if calls.Load() != expected {
				t.Fatal("lease replay")
			}
		}
	}
	check("real_required_reserve_dispatch_settle_audit_faults_rollback_no_answer_and_normal_lease_settlement_never_replays")
	beforeRestart := inventory()
	idBeforeRestart := localOut.Run.InstanceID
	stop()
	start(cfg)
	afterRestart := inventory()
	if len(afterRestart.Profiles) != 2 || afterRestart.Bindings[0] != beforeRestart.Bindings[0] {
		t.Fatal("restart lost binding")
	}
	for i, b := range beforeRestart.Budgets {
		if b != afterRestart.Budgets[i] {
			t.Fatal("restart reset budget")
		}
	}
	out, e := invoke()
	if e != nil || out.Run.InstanceID == idBeforeRestart {
		t.Fatal("restart reused instance", e)
	}
	// Wrong material under the same key ID cannot silently re-encrypt a Secret.
	stop()
	wrong := *cfg
	wrong.AI.EncryptionKeys = "owned14:" + adminSPABrowserSecret(t)
	start(&wrong)
	if s.ai.Service().Status().Ready {
		t.Fatal("wrong key accepted")
	}
	n = calls.Load()
	out, e = invoke()
	if e == nil || out.Response.Content != "" || calls.Load() != n || out.Run.Charged == nil || *out.Run.Charged != 0 {
		t.Fatal("known protection failure charged or dispatched", e)
	}
	stop()
	start(cfg)
	if !s.ai.Service().Status().Ready {
		t.Fatal("original key did not restore protection")
	}
	check("normal_process_restart_preserves_selection_budget_and_retires_instances_wrong_key_rejects_before_dispatch_zero_charge")
	// No request content or Secret may appear in persistent metadata/audit/Outbox.
	for _, table := range []string{"ai_model_profiles", "ai_model_secrets", "ai_model_bindings", "ai_model_budgets", "ai_model_runs", "ai_model_run_events", "platform_command_audits", "platform_outbox"} {
		var raw string
		if e = pool.QueryRow(ctx, `SELECT coalesce(string_agg(to_jsonb(t)::text,''),'') FROM `+table+` t`).Scan(&raw); e != nil {
			t.Fatal(e)
		}
		for _, bad := range []string{secret, localSecret, master, prompt, "SYNTHETIC_MANAGED_RESULT", "UNTRUSTED"} {
			if strings.Contains(raw, bad) {
				t.Fatal("protected text persisted", table)
			}
		}
	}
	var missingAudit, adminChanges int
	if e = pool.QueryRow(ctx, `SELECT count(*) FILTER(WHERE o.id IS NULL OR (a.actor_type='admin' AND a.permission_code<>'ai.ai.manage')),count(*) FILTER(WHERE a.actor_type='admin') FROM platform_command_audits a LEFT JOIN platform_outbox o ON o.id=a.event_id WHERE a.command_code LIKE 'ai.%'`).Scan(&missingAudit, &adminChanges); e != nil || missingAudit != 0 || adminChanges < 4 {
		t.Fatal("required typed audit/Outbox missing", missingAudit, adminChanges, e)
	}
	call("GET", "/api/v1/ai/status", adminToken, nil, 200, false)
	call("GET", "/api/v1/ai/logs?limit=100", adminToken, nil, 200, false)
	var plugins int
	if e = pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM plugin_installations)+(SELECT count(*) FROM plugin_user_installations)`).Scan(&plugins); e != nil || plugins != 0 {
		t.Fatal("external plugin installed")
	}
	check("no_Prompt_answer_raw_error_or_plaintext_secret_in_DB_required_audits_Outbox_or_Admin_HTTP_zero_external_plugins")
	run := func(label string, args []string, extra ...string) {
		t.Helper()
		f, e := os.Create(filepath.Join(root, label+".log"))
		if e != nil {
			t.Fatal(e)
		}
		cmd := exec.CommandContext(ctx, args[0], args[1:]...)
		cmd.Env = append(os.Environ(), extra...)
		cmd.Stdout, cmd.Stderr = f, f
		e = cmd.Run()
		f.Close()
		if e != nil {
			t.Fatal(label, e)
		}
	}
	down := exec.CommandContext(ctx, "bash", filepath.Join(repo, "scripts/migrate.sh"), "down")
	f, e := os.Create(filepath.Join(root, "managed-down-refused.log"))
	if e != nil {
		t.Fatal(e)
	}
	down.Stdout, down.Stderr = f, f
	e = down.Run()
	f.Close()
	if e == nil {
		t.Fatal("populated managed migration down succeeded")
	}
	check("populated_migration_35_rollback_refuses_without_secret_budget_or_history_loss")
	// Full v3 backup plus official offline apply/review on a different owned DB.
	backupFacts := inventory()
	run("managed-backup", []string{"bash", filepath.Join(repo, "scripts/backup.sh"), filepath.Join(root, "backups")})
	archives, e := filepath.Glob(filepath.Join(root, "backups/campusos-backup-*.tar.gz"))
	if e != nil || len(archives) != 1 {
		t.Fatal("v3 backup")
	}
	af, e := os.Open(archives[0])
	if e != nil {
		t.Fatal(e)
	}
	gz, e := gzip.NewReader(af)
	if e != nil {
		t.Fatal(e)
	}
	tr := tar.NewReader(gz)
	for {
		h, e := tr.Next()
		if e == io.EOF {
			break
		}
		if e != nil {
			t.Fatal(e)
		}
		data, e := io.ReadAll(tr)
		if e != nil {
			t.Fatal(e)
		}
		if h.Name == "files.tar.gz" {
			inner, e := gzip.NewReader(bytes.NewReader(data))
			if e != nil {
				t.Fatal(e)
			}
			data, e = io.ReadAll(inner)
			inner.Close()
			if e != nil {
				t.Fatal(e)
			}
		}
		for _, bad := range []string{master, secret, localSecret} {
			if bytes.Contains(data, []byte(bad)) {
				t.Fatal("deployment key or plaintext in v3 backup")
			}
		}
	}
	gz.Close()
	af.Close()
	stop()
	restoredDSN := os.Getenv("CAMPUSOS_AI_RESTORE_DSN")
	rpc, e := pgxpool.ParseConfig(restoredDSN)
	if e != nil || rpc.ConnConfig.Host != "127.0.0.1" || rpc.ConnConfig.Database != "campusos_v12_14a_restore" {
		t.Fatal("owned restore database required")
	}
	restoredRoot := filepath.Join(filepath.Dir(root), "restored-runtime")
	if e = os.MkdirAll(restoredRoot, 0700); e != nil {
		t.Fatal(e)
	}
	run("managed-restore", []string{os.Getenv("CAMPUSOSCTL"), "recovery", "apply", "--archive", archives[0], "--data-root", filepath.Join(restoredRoot, "data"), "--migrations", filepath.Join(repo, "migrations"), "--database", "campusos_v12_14a_restore", "--confirm-offline"}, "DATABASE_DSN="+restoredDSN)
	run("managed-inventory", []string{os.Getenv("CAMPUSOSCTL"), "recovery", "status"}, "DATABASE_DSN="+restoredDSN)
	raw, e := os.ReadFile(filepath.Join(root, "managed-inventory.log"))
	if e != nil {
		t.Fatal(e)
	}
	var inv struct {
		State struct {
			Epoch string `json:"epoch"`
		} `json:"state"`
		SHA string `json:"inventory_sha256"`
	}
	if json.Unmarshal(raw, &inv) != nil || inv.SHA == "" || !bytes.Contains(raw, []byte("managed_ai")) {
		t.Fatal("AI authority missing from recovery inventory")
	}
	run("managed-review", []string{os.Getenv("CAMPUSOSCTL"), "recovery", "review", "--data-root", filepath.Join(restoredRoot, "data"), "--epoch", inv.State.Epoch, "--inventory-sha256", inv.SHA, "--operator", "owned14-local", "--reason", "reviewed retained budgets; AI remains paused", "--confirm-offline"}, "DATABASE_DSN="+restoredDSN)
	if e = os.Chdir(restoredRoot); e != nil {
		t.Fatal(e)
	}
	restoredCfg := *cfg
	restoredCfg.Database.DSN = restoredDSN
	restoredCfg.Plugin.DataDir = filepath.Join(restoredRoot, "data/plugin_data")
	n = calls.Load()
	start(&restoredCfg)
	if s.ai.Service().Status().Ready || s.cfg.AI.Enabled || calls.Load() != n {
		t.Fatal("restored AI automatically resumed")
	}
	rp, e := pgxpool.New(ctx, restoredDSN)
	if e != nil {
		t.Fatal(e)
	}
	defer rp.Close()
	var enabled int
	if e = rp.QueryRow(ctx, `SELECT count(*) FROM ai_model_profiles WHERE enabled`).Scan(&enabled); e != nil || enabled != 0 {
		t.Fatal("restore did not disable Profiles")
	}
	for _, b := range backupFacts.Budgets {
		var got ai.ManagedBudget
		got.ProfileID = b.ProfileID
		if e = rp.QueryRow(ctx, `SELECT quota,charged,reserved FROM ai_model_budgets WHERE profile_id=$1`, b.ProfileID).Scan(&got.Quota, &got.Charged, &got.Reserved); e != nil || got != b {
			t.Fatal("restore reset/lost budget", e)
		}
	}
	if _, e = userResolve(ctx); e == nil {
		t.Fatal("archived User session still valid")
	}
	call("GET", "/api/v1/ai/profiles", adminToken, nil, 401, false)
	check("official_v3_backup_restore_excludes_deployment_keys_preserves_cipher_budget_history_invalidates_sessions_and_keeps_AI_paused_after_review")
	stop()
	proof := map[string]any{"status": "passed", "checks": checks, "check_count": len(checks), "http_requests": requests, "provider_requests": calls.Load(), "redirect_target_calls": escaped.Load(), "actual_server_composition": true, "actual_production_router": true, "actual_postgresql": true, "actual_shared_egress": true, "actual_local_HTTP_and_remote_TLS_protocol_peers": true, "actual_full_backup_restore": true, "production_injection_hook": false, "real_LLM_quality_claim": false, "external_plugins": plugins, "windows": false}
	raw, e = json.MarshalIndent(proof, "", "  ")
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(root, "managed-proof.json"), append(raw, '\n'), 0600); e != nil {
		t.Fatal(e)
	}
}
