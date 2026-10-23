package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	identitydomain "github.com/campusos/CampusOS/internal/modules/core/identity/domain"
	identityrepo "github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	"github.com/campusos/CampusOS/internal/plugin"
	"github.com/campusos/CampusOS/pkg/auth"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPluginV5ActualServerInstallationBoundary(t *testing.T) {
	dsn, binary, work := os.Getenv("CAMPUSOS_V5_SERVER_DATABASE_URL"), os.Getenv("CAMPUSOS_V5_SERVER_BINARY"), os.Getenv("CAMPUSOS_V5_SERVER_OUTPUT")
	if dsn == "" || binary == "" || work == "" {
		t.Skip("owned v5 server target not configured")
	}
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil || config.ConnConfig.Database != "campusos_v12_03b_exit" {
		t.Fatal("requires owned v5 target database")
	}
	ctx := context.Background()
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err = SeedAdmin(pool, adminSeedOptions{Environment: "test", PasswordHashEnabled: true, BootstrapAdminSecret: adminSPABrowserSecret(t)}); err != nil {
		t.Fatal(err)
	}
	secret := adminSPABrowserSecret(t)
	admins := identityrepo.NewPgAdminDomainRepository(pool)
	adminSessions := identityrepo.NewPgAdminSessionRepository(pool)
	aid := "adm.http." + fmt.Sprint(time.Now().UnixNano())
	sid := aid + ".sid"
	now := time.Now().UTC()
	mfa := now.Add(-time.Minute)
	if err := admins.CreateAccount(ctx, identityrepo.AdminDomainAccount{ID: aid, Username: aid, Email: aid + "@example.test", DisplayName: "V5 HTTP target"}); err != nil {
		t.Fatal(err)
	}
	if err := admins.BindRole(ctx, identityrepo.AdminDomainRoleBinding{ID: aid + ".role", AccountID: aid, RoleID: "security_admin", GrantedBy: aid}); err != nil {
		t.Fatal(err)
	}
	if err := adminSessions.CreateSession(ctx, identityrepo.AdminSession{ID: sid, AccountID: aid, RefreshTokenDigest: fmt.Sprintf("%x", sha256.Sum256([]byte(sid))), TokenFamilyID: aid, AuthenticationStrength: "mfa", MFAAuthenticatedAt: &mfa, CreatedAt: now, UpdatedAt: now, LastActiveAt: now, ExpiresAt: now.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	adminJWT := auth.NewJWTManager(auth.JWTConfig{Secret: auth.DeriveDomainSecret(secret, "campusos-admin-domain-v1"), Issuer: "campusos-admin", Audience: "admin-api", KeyID: "admin-key", AccessTTL: time.Hour})
	adminToken, err := adminJWT.GenerateAccessToken(aid, aid, auth.AccessTokenContext{SessionID: sid, AuthVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	userJWT := auth.NewJWTManager(auth.JWTConfig{Secret: secret, AccessTTL: time.Hour})
	userTokens := map[string]string{}
	userSessions := map[string]string{}
	for _, uid := range []string{"2701001", "2701002"} {
		if _, err := pool.Exec(ctx, `INSERT INTO users(id,username,nickname,email,status) VALUES($1,$2,$2,$3,'active') ON CONFLICT(id) DO NOTHING`, uid, "v5-http-"+uid, uid+"@example.test"); err != nil {
			t.Fatal(err)
		}
		usid := fmt.Sprint(time.Now().UnixNano())
		if err := identityrepo.NewPgSessionRepository(pool).Create(ctx, &identitydomain.Session{ID: usid, UserID: uid, RefreshTokenDigest: fmt.Sprintf("%x", sha256.Sum256([]byte(usid))), TokenFamilyID: usid, AuthenticationStrength: identitydomain.MFAAuthenticationPassword, LastActiveAt: now, ExpiresAt: now.Add(time.Hour), CreatedAt: now, UpdatedAt: now}); err != nil {
			t.Fatal(err)
		}
		token, err := userJWT.GenerateAccessToken(uid, uid, auth.AccessTokenContext{SessionID: usid, AuthVersion: 1})
		if err != nil {
			t.Fatal(err)
		}
		userTokens[uid] = token
		userSessions[uid] = usid
	}
	processDir := t.TempDir()
	legacy := filepath.Join(processDir, "legacy")
	if os.MkdirAll(filepath.Join(legacy, ".installed", "obsolete", "broken"), 0700) != nil {
		t.Fatal("mkdir")
	}
	if os.WriteFile(filepath.Join(legacy, ".installed", "obsolete", "broken", "plugin.yaml"), []byte("MUST_NOT_BE_PARSED_OR_EXECUTED"), 0600) != nil {
		t.Fatal("write")
	}
	if os.MkdirAll(filepath.Join(legacy, "source"), 0700) != nil || os.WriteFile(filepath.Join(legacy, "source", "plugin.yaml"), []byte("MUST_NOT_BE_PARSED_OR_EXECUTED"), 0600) != nil {
		t.Fatal("write")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	if os.MkdirAll(work, 0700) != nil {
		t.Fatal("mkdir")
	}
	logfile, err := os.OpenFile(filepath.Join(work, "actual-server.log"), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer logfile.Close()
	command := exec.Command(binary)
	command.Dir = processDir
	env := map[string]string{"PATH": os.Getenv("PATH"), "CAMPUSOS_SKIP_DOTENV": "true", "DATABASE_DSN": dsn, "SERVER_HOST": "127.0.0.1", "SERVER_PORT": fmt.Sprint(port), "CAMPUSOS_ENV": "test", "REDIS_ENABLED": "false", "NATS_URL": "nats://127.0.0.1:1", "HOST_API_ENABLED": "false", "AUTH_PASSWORD_HASH_ENABLED": "true", "AUTH_ALLOW_DEVELOPMENT_DEFAULT_ADMIN": "false", "JWT_SECRET": secret, "AUTH_MFA_ACTIVE_KEY_ID": "test", "AUTH_MFA_ENCRYPTION_KEYS": "test:" + adminSPABrowserSecret(t), "PLUGIN_DATA_DIR": filepath.Join(work, "managed"), "CAMPUSOS_PLUGIN_V5_TRUST_STORE": filepath.Join(work, "..", "lifecycle", "trust.json"), "PLUGINS_DIR": legacy, "CAMPUSOS_PLUGIN_V4_DIR": legacy, "CAMPUSOS_PLUGIN_V4_DEV_SOURCE": "true", "EMAIL_PROVIDER": "fake", "AI_ENABLED": "false", "OBSERVABILITY_PROMETHEUS_ENABLED": "false", "GIN_MODE": "release"}
	for key, value := range env {
		command.Env = append(command.Env, key+"="+value)
	}
	command.Stdout = logfile
	command.Stderr = logfile
	if command.Start() != nil {
		t.Fatal("actual server start")
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
	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	client := &http.Client{Timeout: time.Second}
	ready := false
	for i := 0; i < 150; i++ {
		r, err := client.Get(base + "/api/v1/health")
		if err == nil {
			_ = r.Body.Close()
			if r.StatusCode == 200 {
				ready = true
				break
			}
		}
		select {
		case <-done:
			t.Fatal("server failed before health; inspect owned log")
		default:
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !ready {
		t.Fatal("server health timeout")
	}
	r, err := client.Get(base + "/api/v1/ui/runtime-manifest")
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	_ = r.Body.Close()
	if err != nil || r.StatusCode != 200 {
		t.Fatal("runtime manifest unavailable")
	}
	var response struct {
		Data struct {
			Plugins []json.RawMessage `json:"plugins"`
			Modules []json.RawMessage `json:"modules"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &response) != nil || response.Data.Plugins == nil || len(response.Data.Plugins) != 0 || len(response.Data.Modules) == 0 {
		t.Fatal("retired package published or compiled modules lost")
	}

	// Exercise the actual cmd/server routes with real signed JWTs and current PG
	// sessions/roles. Test credentials never leave this owned test process.
	request := func(method, path, token, content string, body []byte, browser, csrf bool) (int, []byte) {
		t.Helper()
		req, e := http.NewRequest(method, base+"/api/v1"+path, bytes.NewReader(body))
		if e != nil {
			t.Fatal(e)
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		if content != "" {
			req.Header.Set("Content-Type", content)
		}
		if browser {
			req.Header.Set("Origin", "http://localhost")
			req.AddCookie(&http.Cookie{Name: "campusos_admin_csrf", Value: "owned-csrf-token-123456"})
			if csrf {
				req.Header.Set("X-Admin-CSRF-Token", "owned-csrf-token-123456")
			}
		}
		r, e := client.Do(req)
		if e != nil {
			t.Fatal(e)
		}
		defer r.Body.Close()
		b, e := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if e != nil {
			t.Fatal(e)
		}
		if r.Header.Get("Cache-Control") != "private, no-store" && r.StatusCode == 200 {
			t.Fatal("private response cacheable")
		}
		return r.StatusCode, b
	}
	coordinateRaw, e := os.ReadFile(filepath.Join(work, "..", "lifecycle", "coordinate.json"))
	if e != nil {
		t.Fatal(e)
	}
	var coordinate plugin.V5Coordinate
	if json.Unmarshal(coordinateRaw, &coordinate) != nil {
		t.Fatal("coordinate")
	}
	path := "/admin/plugin-v5/" + coordinate.Publisher + "/" + coordinate.Key
	userPath := "/plugin-v5/" + coordinate.Publisher + "/" + coordinate.Key
	mutation := func(prefix, token string, r plugin.V5Mutation) plugin.V5OperationView {
		t.Helper()
		r.Version = coordinate.Version
		raw, _ := json.Marshal(r)
		status, b := request("POST", prefix+"/commands", token, "application/json", raw, false, false)
		if status != 200 {
			t.Fatalf("actual command %s: %d %s", r.Kind, status, b)
		}
		var out struct {
			Data plugin.V5OperationView `json:"data"`
		}
		if json.Unmarshal(b, &out) != nil || out.Data.ID == "" {
			t.Fatal("operation response")
		}
		return out.Data
	}
	if status, _ := request("GET", "/admin/plugin-v5", userTokens["2701001"], "", nil, false, false); status == 200 {
		t.Fatal("user JWT entered Admin")
	}
	if status, _ := request("POST", path+"/commands", adminToken, "application/json", []byte(`{"kind":"disable","version":"1.0.0","idempotency_key":"csrf"}`), true, false); status != 403 {
		t.Fatal("missing browser CSRF", status)
	}
	if status, _ := request("POST", path+"/commands", adminToken, "application/json", []byte(`{"kind":"disable","version":"1.0.0","idempotency_key":"x","owner_user_id":"2701002"}`), false, false); status != 400 {
		t.Fatal("client owner accepted", status)
	}
	releaseRaw, e := os.ReadFile(filepath.Join(work, "..", "lifecycle", "release.json"))
	if e != nil {
		t.Fatal(e)
	}
	packageRaw, e := os.ReadFile(filepath.Join(work, "..", "lifecycle", "package.tar.gz"))
	if e != nil {
		t.Fatal(e)
	}
	var upload bytes.Buffer
	writer := multipart.NewWriter(&upload)
	for _, part := range []struct {
		name string
		raw  []byte
	}{{"package", packageRaw}, {"release", releaseRaw}, {"idempotency_key", []byte("actual-import-" + sid)}} {
		field, e := writer.CreateFormField(part.name)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = field.Write(part.raw); e != nil {
			t.Fatal(e)
		}
	}
	writer.Close()
	if status, b := request("POST", "/admin/plugin-v5/packages", adminToken, writer.FormDataContentType(), upload.Bytes(), true, true); status != 200 {
		t.Fatalf("actual signed import: %d %s", status, b)
	}
	// Existing desired enable from the PG worker test is still pending: no host
	// observer or old Manager can publish it through the public runtime feed.
	status, b := request("GET", path+"/"+coordinate.Version, adminToken, "", nil, false, false)
	if status != 200 {
		t.Fatalf("detail %d %s", status, b)
	}
	var detail struct {
		Data plugin.V5InventoryEntry `json:"data"`
	}
	if json.Unmarshal(b, &detail) != nil {
		t.Fatal("detail decode")
	}
	global := detail.Data.Installation
	if global == nil || global.Observed == "ready" {
		t.Fatal("fake runtime readiness")
	}
	grant := detail.Data.Grant
	if grant == nil {
		t.Fatal("grant missing")
	}
	mutation(path, adminToken, plugin.V5Mutation{Kind: "grant", ExpectedRevision: grant.Revision, Approved: true, Consumes: []plugin.PluginV5Consume{}, IdempotencyKey: "actual-grant-" + sid})
	installed := mutation(userPath, userTokens["2701001"], plugin.V5Mutation{Kind: "install", IdempotencyKey: "actual-user-install-" + sid})
	if status, _ := request("GET", "/plugin-v5/operations/"+installed.ID, userTokens["2701002"], "", nil, false, false); status != 404 {
		t.Fatal("other owner operation disclosed", status)
	}
	if status, b := request("GET", "/plugin-v5/operations/"+installed.ID, userTokens["2701001"], "", nil, false, false); status != 200 || strings.Contains(string(b), "request_hash") || strings.Contains(string(b), "lease_generation") {
		t.Fatal("private operation internals disclosed", status)
	}
	mutation(userPath, userTokens["2701001"], plugin.V5Mutation{Kind: "configure", Values: json.RawMessage(`{"locale":"en-US"}`), IdempotencyKey: "actual-user-config-" + sid})
	status, b = request("GET", userPath+"/"+coordinate.Version, userTokens["2701002"], "", nil, false, false)
	if status != 200 || strings.Contains(string(b), `"locale":"en-US"`) {
		t.Fatal("other owner config leaked", status)
	}
	if status, _ := request("POST", "/plugins/obsolete/enable", adminToken, "application/json", []byte(`{}`), false, false); status != 410 {
		t.Fatal("legacy control still live", status)
	}
	if _, err := pool.Exec(ctx, `UPDATE sessions SET revoked_at=now(),revoke_reason='owned target' WHERE id=$1`, userSessions["2701001"]); err != nil {
		t.Fatal(err)
	}
	if status, _ := request("GET", "/plugin-v5", userTokens["2701001"], "", nil, false, false); status == 200 {
		t.Fatal("revoked user admitted")
	}
	if _, err := pool.Exec(ctx, `UPDATE admin_sessions SET mfa_authenticated_at=now()-interval '1 day' WHERE id=$1`, sid); err != nil {
		t.Fatal(err)
	}
	if status, _ := request("POST", path+"/commands", adminToken, "application/json", []byte(`{"kind":"purge_release","version":"`+coordinate.Version+`","idempotency_key":"expired-mfa"}`), false, false); status != 403 {
		t.Fatal("expired MFA command admitted", status)
	}
	if status, _ := request("GET", "/admin/plugin-v5", adminToken, "", nil, false, false); status != 200 {
		t.Fatal("MFA-free read unavailable", status)
	}
	if _, err := pool.Exec(ctx, `UPDATE admin_sessions SET revoked_at=now(),revoke_reason='owned target' WHERE id=$1`, sid); err != nil {
		t.Fatal(err)
	}
	if status, _ := request("GET", "/admin/plugin-v5", adminToken, "", nil, false, false); status == 200 {
		t.Fatal("revoked admin admitted")
	}
	var version int
	if err = pool.QueryRow(ctx, `SELECT max(version)::integer FROM schema_migrations`).Scan(&version); err != nil || version != 27 {
		t.Fatal("wrong target schema")
	}
	bytes, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(bytes)
	proof := map[string]any{"status": "passed", "actual_cmd_server": true, "server_binary_sha256": hex.EncodeToString(digest[:]), "migration": version, "installation_http": true, "checks": []string{"current signed Admin/User proofs, owner isolation, CSRF, MFA expiry and committed revocation", "actual multipart signed import and independent install/grant/configuration", "old Manager controls return410, pending enable not published", "actual server health with both legacy autoload flags set", "malformed legacy source and installed directories ignored", "public runtime external plugins empty", "compiled module contributions retained", "owned PostgreSQL27 startup passed"}}
	wire, _ := json.MarshalIndent(proof, "", "  ")
	if os.WriteFile(filepath.Join(work, "server-result.json"), append(wire, '\n'), 0600) != nil {
		t.Fatal("write proof")
	}
}
