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

	identityrepo "github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	"github.com/campusos/CampusOS/internal/plugin"
	"github.com/campusos/CampusOS/internal/projectaudit"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/jackc/pgx/v5/pgxpool"
)

// This target uses the production Admin build and actual cmd/server. Only the
// owned control port can expire/revoke current PG facts; no API is mocked.
func TestPluginV5ConfigActualServerBrowser(t *testing.T) {
	dsn, binary, work := os.Getenv("CAMPUSOS_V5_CONFIG_UI_DATABASE_URL"), os.Getenv("CAMPUSOS_V5_CONFIG_UI_SERVER_BINARY"), os.Getenv("CAMPUSOS_V5_CONFIG_UI_OUTPUT")
	if dsn == "" || binary == "" || work == "" {
		t.Skip("requires owned V12-04 config UI database, binary and evidence directory")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil || cfg.ConnConfig.Database != "campusos_v12_04_config_ui" {
		t.Fatal("requires exact owned config UI database")
	}
	root, err := projectaudit.FindRepositoryRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
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
	account, role := "adm.config.browser", "v5_config_browser"
	if err := admins.CreateAccount(ctx, identityrepo.AdminDomainAccount{ID: account, Username: "config-browser", Email: "config-browser@example.test", DisplayName: "配置验收管理员"}); err != nil {
		t.Fatal(err)
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := admins.SetCredentialCAS(ctx, account, hash, 0, time.Now()); err != nil || !ok {
		t.Fatal("credential", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO admin_roles(id,name) VALUES($1,'插件配置验收')`, role); err != nil {
		t.Fatal(err)
	}
	if err := admins.BindRole(ctx, identityrepo.AdminDomainRoleBinding{ID: account + ".role", AccountID: account, RoleID: role, GrantedBy: account}); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO admin_role_permissions(role_id,permission_code,created_by) VALUES($1,'plugin.plugin.read','test'),($1,'plugin.plugin.configure','test'),($1,'plugin.plugin.install','test')`, role); err != nil {
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
	trust, _ := json.Marshal(plugin.PluginV5TrustStore{Contract: "campusos.plugin-trust/v1", Keys: []plugin.PluginV5TrustKey{{Publisher: "campusos", KeyID: "config-ui-test", PublicKey: base64.StdEncoding.EncodeToString(pub), Status: "active"}}})
	trustPath := filepath.Join(work, "trust.json")
	if err := os.WriteFile(trustPath, trust, 0600); err != nil {
		t.Fatal(err)
	}
	template, err := os.ReadFile(filepath.Join(root, "plugins/campusos.pdf-viewer/plugin.json"))
	if err != nil {
		t.Fatal(err)
	}
	packages := []map[string]string{}
	for _, version := range []string{"1.0.0", "2.0.0", "3.0.0"} {
		var m plugin.PluginV5Manifest
		if json.Unmarshal(template, &m) != nil {
			t.Fatal("manifest template")
		}
		m.Key, m.Version, m.Data.ConfigScope = "config-form", version, "system"
		definition := `{"contract":"campusos.plugin-config/v1","version":"v1","schema":{"type":"object","additionalProperties":false,"properties":{"label":{"type":"string","title":"显示名称","minLength":1,"maxLength":40},"enabled":{"type":"boolean","title":"显示提示"},"mode":{"type":"string","title":"显示模式","enum":["简洁","详细"]}},"required":["label","enabled"]},"defaults":{"label":"初始配置","enabled":false},"selectors":[],"secret_names":[]}`
		if version == "2.0.0" {
			definition = `{"contract":"campusos.plugin-config/v1","version":"v1","schema":{"type":"object","additionalProperties":false,"properties":{"count":{"type":"integer","title":"数量"}},"required":["count"]},"defaults":{"count":9007199254740991},"selectors":[],"secret_names":[]}`
		}
		if version == "3.0.0" {
			m.Data.ConfigScope = "user"
		}
		m.Configuration = json.RawMessage(definition)
		source := filepath.Join(processDir, version)
		for n := range m.UI {
			raw := []byte("<!doctype html><title>UI contract fixture; never executed</title>")
			m.UI[n].Digest = fmt.Sprintf("sha256:%x", sha256.Sum256(raw))
			path := filepath.Join(source, m.UI[n].Entrypoint)
			if os.MkdirAll(filepath.Dir(path), 0700) != nil || os.WriteFile(path, raw, 0600) != nil {
				t.Fatal("write fixture UI")
			}
		}
		wire, _ := json.Marshal(m)
		if err := os.WriteFile(filepath.Join(source, "plugin.json"), wire, 0600); err != nil {
			t.Fatal(err)
		}
		archive, _, err := plugin.BuildPluginV5Package(source)
		if err != nil {
			t.Fatal("build package", err)
		}
		envelope, err := plugin.SignPluginV5Package(archive, "config-ui-test", private)
		if err != nil {
			t.Fatal(err)
		}
		archivePath, releasePath := filepath.Join(work, version+".tar.gz"), filepath.Join(work, version+".release.json")
		if os.WriteFile(archivePath, archive, 0600) != nil || os.WriteFile(releasePath, envelope, 0600) != nil {
			t.Fatal("write signed fixtures")
		}
		packages = append(packages, map[string]string{"version": version, "archive": archivePath, "release": releasePath})
	}
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
		case "/revoke-account":
			_, err = pool.Exec(r.Context(), `UPDATE admin_accounts SET status='suspended',version=version+1,status_reason='owned test' WHERE id=$1`, account)
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
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	logfile, err := os.OpenFile(filepath.Join(work, "actual-server.log"), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer logfile.Close()
	command := exec.Command(binary)
	command.Dir = processDir
	for k, v := range map[string]string{"PATH": os.Getenv("PATH"), "CAMPUSOS_SKIP_DOTENV": "true", "DATABASE_DSN": dsn, "SERVER_HOST": "127.0.0.1", "SERVER_PORT": fmt.Sprint(port), "CAMPUSOS_ENV": "test", "REDIS_ENABLED": "false", "NATS_URL": "nats://127.0.0.1:1", "HOST_API_ENABLED": "false", "AUTH_PASSWORD_HASH_ENABLED": "true", "AUTH_ALLOW_DEVELOPMENT_DEFAULT_ADMIN": "false", "AUTH_REFRESH_BODY_COMPAT": "false", "JWT_SECRET": jwtKey, "AUTH_MFA_ACTIVE_KEY_ID": "test", "AUTH_MFA_ENCRYPTION_KEYS": "test:" + mfaKey, "PLUGIN_DATA_DIR": filepath.Join(processDir, "managed"), "CAMPUSOS_PLUGIN_V5_TRUST_STORE": trustPath, "EMAIL_PROVIDER": "fake", "AI_ENABLED": "false", "OBSERVABILITY_PROMETHEUS_ENABLED": "false", "GIN_MODE": "release"} {
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
	client := &http.Client{Timeout: time.Second}
	ready := false
	for n := 0; n < 150; n++ {
		response, err := client.Get(base + "/api/v1/health")
		if err == nil {
			_ = response.Body.Close()
			if response.StatusCode == 200 {
				ready = true
				break
			}
		}
		select {
		case <-done:
			t.Fatal("actual server stopped; inspect retained log")
		default:
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !ready {
		t.Fatal("actual server health timeout")
	}
	browserConfig := map[string]any{"backend": base, "control": control.URL, "controlKey": controlKey, "password": password, "adminUsername": "config-browser@example.test", "packages": packages, "result": filepath.Join(work, "browser-result.json")}
	configPath := filepath.Join(processDir, "browser-private.json")
	wire, _ := json.Marshal(browserConfig)
	if err := os.WriteFile(configPath, wire, 0600); err != nil {
		t.Fatal(err)
	}
	node := exec.CommandContext(ctx, "node", filepath.Join(root, "scripts/v12-04-config-browser.mjs"), configPath)
	node.Dir = root
	// Keep Chrome's Unix socket path short; no credentials persist in evidence.
	node.Env = append(os.Environ(), "TMPDIR=/tmp")
	output, err := node.CombinedOutput()
	_ = os.WriteFile(filepath.Join(work, "browser.log"), output, 0600)
	if err != nil {
		t.Fatalf("actual config UI browser failed; inspect browser.log: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(work, "browser-result.json"))
	if err != nil {
		t.Fatal(err)
	}
	var report struct {
		Status string   `json:"status"`
		Mocked bool     `json:"mocked_api"`
		Checks []string `json:"checks"`
	}
	if json.Unmarshal(data, &report) != nil || report.Status != "passed" || report.Mocked || len(report.Checks) < 15 {
		t.Fatal("incomplete browser acceptance")
	}
	var value string
	var enabled bool
	if err := pool.QueryRow(ctx, `SELECT (state->'values'->>'label'),(state->'values'->>'enabled')::boolean FROM plugin_v5_configurations WHERE publisher='campusos' AND plugin_key='config-form' AND version='1.0.0' AND owner_user_id IS NULL`).Scan(&value, &enabled); err != nil || value != "并发更新" || enabled {
		t.Fatal("actual committed configuration differs", err)
	}
	var audits int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM platform_command_audits WHERE command_code='plugin.v5.configure' AND actor_type='admin' AND actor_id=$1`, account).Scan(&audits); err != nil || audits != 2 {
		t.Fatal("expected exactly two audited successful config writes", audits, err)
	}
	logData, _ := os.ReadFile(filepath.Join(work, "actual-server.log"))
	for _, secret := range []string{password, jwtKey, mfaKey, controlKey} {
		if strings.Contains(string(logData), secret) {
			t.Fatal("credential appeared in server logs")
		}
	}
	b, err := os.Open(binary)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	digest := sha256.New()
	if _, err = io.Copy(digest, b); err != nil {
		t.Fatal(err)
	}
	proof, _ := json.MarshalIndent(map[string]any{"status": "passed", "actual_cmd_server": true, "server_binary_sha256": fmt.Sprintf("%x", digest.Sum(nil)), "actual_pg_configuration": true, "exact_typed_audits": audits, "browser_checks": len(report.Checks), "migration": 28}, "", "  ")
	if err := os.WriteFile(filepath.Join(work, "server-result.json"), append(proof, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
}
