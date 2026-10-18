package server

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	identitydomain "github.com/campusos/CampusOS/internal/modules/core/identity/domain"
	identityrepo "github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	"github.com/campusos/CampusOS/internal/plugin"
	"github.com/campusos/CampusOS/internal/projectaudit"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/jackc/pgx/v5/pgxpool"
)

// One owned PG16, real cmd/server, current production builds and a separate UI
// listener. Browser requests use actual identities and resource services.
func TestPluginV5UIActualServerBrowser(t *testing.T) {
	dsn, binary, work := os.Getenv("CAMPUSOS_V5_UI_DATABASE_URL"), os.Getenv("CAMPUSOS_V5_UI_SERVER_BINARY"), os.Getenv("CAMPUSOS_V5_UI_OUTPUT")
	if dsn == "" || binary == "" || work == "" {
		t.Skip("requires owned V12-04 full UI target")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil || cfg.ConnConfig.Database != "campusos_v12_04_ui" {
		t.Fatal("requires exact owned V12-04 UI database")
	}
	root, err := projectaudit.FindRepositoryRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	password, jwtKey, mfaKey, controlKey := adminSPABrowserSecret(t), adminSPABrowserSecret(t), adminSPABrowserSecret(t), adminSPABrowserSecret(t)
	if err := SeedAdmin(pool, adminSeedOptions{Environment: "test", PasswordHashEnabled: true, BootstrapAdminSecret: password}); err != nil {
		t.Fatal(err)
	}
	admins := identityrepo.NewPgAdminDomainRepository(pool)
	account, role := "adm.v5.ui.browser", "v5_ui_browser"
	if err := admins.CreateAccount(ctx, identityrepo.AdminDomainAccount{ID: account, Username: "v5-ui-browser", Email: "v5-ui-browser@example.test", DisplayName: "插件界面验收管理员"}); err != nil {
		t.Fatal(err)
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := admins.SetCredentialCAS(ctx, account, hash, 0, time.Now()); err != nil || !ok {
		t.Fatal("credential", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO admin_roles(id,name) VALUES($1,'插件界面验收')`, role); err != nil {
		t.Fatal(err)
	}
	if err := admins.BindRole(ctx, identityrepo.AdminDomainRoleBinding{ID: account + ".role", AccountID: account, RoleID: role, GrantedBy: account}); err != nil {
		t.Fatal(err)
	}
	for _, permission := range []string{"plugin.plugin.read", "plugin.plugin.install", "plugin.plugin.configure", "plugin.plugin.lifecycle", "plugin.plugin.uninstall"} {
		if _, err := pool.Exec(ctx, `INSERT INTO admin_role_permissions(role_id,permission_code,created_by) VALUES($1,$2,'test')`, role, permission); err != nil {
			t.Fatal(err)
		}
	}
	users := identityrepo.NewPgUserRepository(pool)
	now := time.Now().UTC()
	for n, name := range []string{"v5_ui_alice", "v5_ui_bob"} {
		id := fmt.Sprint(8601001 + n)
		if err := users.Create(ctx, &identitydomain.User{ID: id, Username: name, Nickname: name, Email: name + "@example.test", Status: identitydomain.UserStatusActive, CreatedAt: now, UpdatedAt: now}); err != nil {
			t.Fatal(err)
		}
		if err := users.CreateVerifiedAccount(ctx, id, name+"@example.test", hash); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, `INSERT INTO categories(id,name,slug,node_kind,lifecycle_status) VALUES(8601100,'UI test group','v5-ui-group','group','active');INSERT INTO categories(id,name,slug,parent_id,node_kind,lifecycle_status) VALUES(8601101,'UI test board','v5-ui-board',8601100,'board','active');INSERT INTO category_thread_type_policies(category_id,thread_type,enabled) VALUES(8601101,'article',TRUE);`); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(work, 0700); err != nil {
		t.Fatal(err)
	}
	processDir := t.TempDir()
	pub, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	trust, _ := json.Marshal(plugin.PluginV5TrustStore{Contract: "campusos.plugin-trust/v1", Keys: []plugin.PluginV5TrustKey{{Publisher: "campusos", KeyID: "ui-test", PublicKey: base64.StdEncoding.EncodeToString(pub), Status: "active"}}})
	trustPath := filepath.Join(work, "trust.json")
	if err := os.WriteFile(trustPath, trust, 0600); err != nil {
		t.Fatal(err)
	}
	packages := []map[string]string{}
	writePackage := func(source, name string) {
		t.Helper()
		archive, p, err := plugin.BuildPluginV5Package(source)
		if err != nil {
			t.Fatal("build", name, err)
		}
		envelope, err := plugin.SignPluginV5Package(archive, "ui-test", private)
		if err != nil {
			t.Fatal(err)
		}
		ap, rp := filepath.Join(work, name+".tar.gz"), filepath.Join(work, name+".release.json")
		if os.WriteFile(ap, archive, 0600) != nil || os.WriteFile(rp, envelope, 0600) != nil {
			t.Fatal("write signed package")
		}
		packages = append(packages, map[string]string{"key": p.Manifest.Key, "version": p.Manifest.Version, "archive": ap, "release": rp})
	}
	writePackage(filepath.Join(root, "plugins/campusos.pdf-viewer"), "pdf-viewer")
	template, err := os.ReadFile(filepath.Join(root, "plugins/campusos.pdf-viewer/plugin.json"))
	if err != nil {
		t.Fatal(err)
	}
	m, err := plugin.ParsePluginV5Manifest(template)
	if err != nil {
		t.Fatal(err)
	}
	m.Key = "full-config"
	m.Version = "1.0.0"
	m.Consumes = []plugin.PluginV5Consume{}
	m.Data.ConfigScope = "both"
	m.Configuration = json.RawMessage(`{"contract":"campusos.plugin-config/v1","version":"v1","schema":{"type":"object","additionalProperties":false,"properties":{"label":{"type":"string","title":"名称","minLength":1,"maxLength":32},"enabled":{"type":"boolean","title":"开关"},"count":{"type":"integer","title":"精确整数","minimum":9007199254740992,"maximum":9007199254740995},"ratio":{"type":"number","title":"精确小数","minimum":0.10000000000000000001,"maximum":0.10000000000000000003},"mode":{"type":"number","title":"数字枚举","enum":[0.10000000000000000001,0.10000000000000000002]},"profile":{"type":"string","title":"模型配置"},"collection":{"type":"string","title":"公开集合"}},"required":["label","enabled","count","ratio"]},"defaults":{"label":"初始配置","enabled":false,"count":9007199254740993,"ratio":0.10000000000000000001},"selectors":[{"field":"profile","kind":"profile"},{"field":"collection","kind":"public_collection"}],"secret_names":["api_key"]}`)
	source := filepath.Join(processDir, "full-config")
	for n := range m.UI {
		raw := []byte(`<!doctype html><title>Configuration contract fixture</title><p id="app">connecting</p><script src="./main.js"></script>`)
		bridge := []byte(fmt.Sprintf(`const audience=%q;const host=new URLSearchParams(location.search).get('host_origin');addEventListener('message',e=>{const d=e.data,p=e.ports[0];if(e.source!==parent||e.origin!==host||d?.type!=='campusos.bridge.connect'||d.plugin_key!=='campusos.full-config'||d.plugin_version!=='1.0.0'||d.surface_id!=='ui'||d.audience!==audience||!p)return;p.onmessage=e=>{if(e.data.request_id==='ordinary'&&e.data.result)document.getElementById('app').textContent=e.data.result.values_json};p.start();p.postMessage({type:'campusos.bridge.handshake',version:'campusos.bridge/v1',challenge:d.challenge});p.postMessage({version:'campusos.bridge/v1',request_id:'ordinary',method:'config.read',params:{}})});parent.postMessage({type:'campusos.bridge.ready',version:'campusos.bridge/v1',plugin_key:'campusos.full-config',plugin_version:'1.0.0',surface_id:'ui',audience},host);`, m.UI[n].Audience))
		m.UI[n].Digest = fmt.Sprintf("sha256:%x", sha256.Sum256(raw))
		p := filepath.Join(source, m.UI[n].Entrypoint)
		if os.MkdirAll(filepath.Dir(p), 0700) != nil || os.WriteFile(p, raw, 0600) != nil {
			t.Fatal("write UI fixture")
		}
		if err := os.WriteFile(filepath.Join(filepath.Dir(p), "main.js"), bridge, 0600); err != nil {
			t.Fatal(err)
		}
	}
	wire, _ := json.Marshal(m)
	if os.WriteFile(filepath.Join(source, "plugin.json"), wire, 0600) != nil {
		t.Fatal("manifest")
	}
	writePackage(source, "full-config")
	control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.Header.Get("Authorization") != "Bearer "+controlKey {
			w.WriteHeader(403)
			return
		}
		var err error
		switch r.URL.Path {
		case "/expire-mfa":
			_, err = pool.Exec(r.Context(), `UPDATE admin_sessions SET mfa_authenticated_at=now()-interval '1 day' WHERE account_id=$1`, account)
		case "/restore-mfa":
			_, err = pool.Exec(r.Context(), `UPDATE admin_sessions SET mfa_authenticated_at=now() WHERE account_id=$1`, account)
		case "/revoke-configure":
			_, err = pool.Exec(r.Context(), `DELETE FROM admin_role_permissions WHERE role_id=$1 AND permission_code='plugin.plugin.configure'`, role)
		case "/restore-configure":
			_, err = pool.Exec(r.Context(), `INSERT INTO admin_role_permissions(role_id,permission_code,created_by) VALUES($1,'plugin.plugin.configure','test') ON CONFLICT DO NOTHING`, role)
		case "/suspend-user":
			_, err = pool.Exec(r.Context(), `UPDATE users SET status='suspended' WHERE id=8601001`)
		default:
			w.WriteHeader(404)
			return
		}
		if err != nil {
			w.WriteHeader(500)
			return
		}
		w.WriteHeader(204)
	}))
	defer control.Close()
	allocate := func() int {
		t.Helper()
		l, e := net.Listen("tcp", "127.0.0.1:0")
		if e != nil {
			t.Fatal(e)
		}
		defer l.Close()
		return l.Addr().(*net.TCPAddr).Port
	}
	apiPort, uiPort, uiListenPort, adminPort, webPort := allocate(), allocate(), allocate(), allocate(), allocate()
	base := fmt.Sprintf("http://127.0.0.1:%d", apiPort)
	uiOrigin := fmt.Sprintf("http://{isolation}.localhost:%d", uiPort)
	logfile, err := os.OpenFile(filepath.Join(work, "actual-server.log"), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer logfile.Close()
	gateway := exec.CommandContext(ctx, "node", filepath.Join(root, "deploy/docker/plugin-ui-server.mjs"))
	gateway.Env = []string{"PATH=" + os.Getenv("PATH"), "CAMPUSOS_PLUGIN_UI_HOST=127.0.0.1", fmt.Sprintf("CAMPUSOS_PLUGIN_UI_PORT=%d", uiPort), fmt.Sprintf("CAMPUSOS_PLUGIN_UI_UPSTREAM=http://127.0.0.1:%d", uiListenPort)}
	gatewayLog, err := os.OpenFile(filepath.Join(work, "ui-gateway.log"), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer gatewayLog.Close()
	gateway.Stdout = gatewayLog
	gateway.Stderr = gatewayLog
	if err := gateway.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = gateway.Process.Kill(); _ = gateway.Wait() }()
	command := exec.CommandContext(ctx, binary)
	command.Dir = processDir
	for k, v := range map[string]string{"PATH": os.Getenv("PATH"), "CAMPUSOS_SKIP_DOTENV": "true", "DATABASE_DSN": dsn, "SERVER_HOST": "127.0.0.1", "SERVER_PORT": fmt.Sprint(apiPort), "CAMPUSOS_ENV": "test", "REDIS_ENABLED": "false", "NATS_URL": "nats://127.0.0.1:1", "HOST_API_ENABLED": "false", "AUTH_PASSWORD_HASH_ENABLED": "true", "AUTH_ALLOW_DEVELOPMENT_DEFAULT_ADMIN": "false", "AUTH_REFRESH_BODY_COMPAT": "false", "JWT_SECRET": jwtKey, "AUTH_MFA_ACTIVE_KEY_ID": "test", "AUTH_MFA_ENCRYPTION_KEYS": "test:" + mfaKey, "PLUGIN_DATA_DIR": filepath.Join(processDir, "managed"), "CAMPUSOS_PLUGIN_V5_TRUST_STORE": trustPath, "CAMPUSOS_PLUGIN_UI_LISTEN": fmt.Sprintf("127.0.0.1:%d", uiListenPort), "CAMPUSOS_PLUGIN_UI_ORIGIN": uiOrigin, "CAMPUSOS_PLUGIN_UI_PARENTS": fmt.Sprintf("http://127.0.0.1:%d http://127.0.0.1:%d", adminPort, webPort), "EMAIL_PROVIDER": "fake", "AI_ENABLED": "false", "OBSERVABILITY_PROMETHEUS_ENABLED": "false", "GIN_MODE": "release"} {
		command.Env = append(command.Env, k+"="+v)
	}
	command.Stdout = logfile
	command.Stderr = logfile
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait(); close(done) }()
	defer func() {
		_ = command.Process.Signal(syscall.SIGTERM)
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			_ = command.Process.Kill()
			<-done
		}
	}()
	healthy := false
	client := &http.Client{Timeout: time.Second}
	for n := 0; n < 150; n++ {
		r, e := client.Get(base + "/api/v1/health")
		if e == nil {
			r.Body.Close()
			if r.StatusCode == 200 {
				healthy = true
				break
			}
		}
		select {
		case <-done:
			t.Fatal("actual server stopped; inspect private server log")
		default:
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !healthy {
		t.Fatal("actual server health deadline")
	}
	browserConfig := map[string]any{"backend": base, "uiOrigin": uiOrigin, "adminPort": adminPort, "webPort": webPort, "control": control.URL, "controlKey": controlKey, "password": password, "adminUsername": "v5-ui-browser@example.test", "alice": "v5_ui_alice@example.test", "bob": "v5_ui_bob@example.test", "packages": packages, "result": filepath.Join(work, "browser-result.json"), "output": work, "tailOnly": os.Getenv("CAMPUSOS_V5_UI_TAIL_ONLY") == "1"}
	configPath := filepath.Join(work, "browser-input.json")
	wire, _ = json.Marshal(browserConfig)
	if err := os.WriteFile(configPath, wire, 0600); err != nil {
		t.Fatal(err)
	}
	node := exec.CommandContext(ctx, "node", filepath.Join(root, "scripts/v12-04-ui-browser.mjs"), configPath)
	browserTmp, err := os.MkdirTemp("/tmp", "v12-04-ui-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(browserTmp)
	for _, dir := range []string{"data", "config", "cache"} {
		if err := os.MkdirAll(filepath.Join(browserTmp, dir), 0700); err != nil {
			t.Fatal(err)
		}
	}
	node.Env = append(os.Environ(), "TMPDIR="+browserTmp, "XDG_DATA_HOME="+filepath.Join(browserTmp, "data"), "XDG_CONFIG_HOME="+filepath.Join(browserTmp, "config"), "XDG_CACHE_HOME="+filepath.Join(browserTmp, "cache"))
	node.Dir = root
	browserLog, err := os.OpenFile(filepath.Join(work, "browser.log"), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	node.Stdout = browserLog
	node.Stderr = browserLog
	err = node.Run()
	_ = browserLog.Close()
	if err != nil {
		t.Fatalf("actual UI browser failed; inspect private browser log: %v", err)
	}
	var report struct {
		Status string   `json:"status"`
		Checks []string `json:"checks"`
	}
	wire, err = os.ReadFile(filepath.Join(work, "browser-result.json"))
	requiredChecks := 25
	if os.Getenv("CAMPUSOS_V5_UI_TAIL_ONLY") == "1" {
		requiredChecks = 20
	}
	if err != nil || json.Unmarshal(wire, &report) != nil || report.Status != "passed" || len(report.Checks) < requiredChecks {
		t.Fatal("missing required full UI browser evidence", err)
	}
	var configRaw []byte
	var audits int
	if err := pool.QueryRow(ctx, `SELECT state->'values' FROM plugin_v5_configurations WHERE publisher='campusos' AND plugin_key='full-config' AND version='1.0.0' AND owner_user_id IS NULL`).Scan(&configRaw); err != nil {
		t.Fatal("current PG config", err)
	}
	if !strings.Contains(string(configRaw), "9007199254740994") || !strings.Contains(string(configRaw), "0.10000000000000000002") {
		t.Fatal("decimal precision lost")
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM platform_command_audits WHERE command_code='plugin.v5.grant' AND actor_type='admin' AND actor_id=$1`, account).Scan(&audits); err != nil || audits < 2 {
		t.Fatal("missing reliable grants", audits, err)
	}
	logData, _ := os.ReadFile(filepath.Join(work, "actual-server.log"))
	for _, secret := range []string{password, jwtKey, mfaKey, controlKey} {
		if strings.Contains(string(logData), secret) {
			t.Fatal("credential in server log")
		}
	}
	b, err := os.Open(binary)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	digest := sha256.New()
	if _, err := io.Copy(digest, b); err != nil {
		t.Fatal(err)
	}
	proof, _ := json.MarshalIndent(map[string]any{"status": "passed", "actual_cmd_server": true, "server_binary_sha256": fmt.Sprintf("%x", digest.Sum(nil)), "actual_pg_configuration": true, "typed_grant_audits": audits, "browser_checks": len(report.Checks), "migration": 28}, "", "  ")
	if err := os.WriteFile(filepath.Join(work, "server-result.json"), append(proof, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
}
