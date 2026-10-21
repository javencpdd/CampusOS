package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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

	communitydomain "github.com/campusos/CampusOS/internal/modules/core/community/domain"
	communityrepo "github.com/campusos/CampusOS/internal/modules/core/community/repository"
	identitydomain "github.com/campusos/CampusOS/internal/modules/core/identity/domain"
	identityrepo "github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	"github.com/campusos/CampusOS/internal/projectaudit"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestKnowledgeCollectionsActualServerBrowser(t *testing.T) {
	raw := os.Getenv("CAMPUSOS_KNOWLEDGE_EXIT_DSN")
	binary := os.Getenv("CAMPUSOS_KNOWLEDGE_SERVER_BINARY")
	if raw == "" || binary == "" {
		t.Skip("owned Admin SPA database and actual server binary not configured")
	}
	config, err := pgxpool.ParseConfig(raw)
	if err != nil {
		t.Fatal(err)
	}
	if config.ConnConfig.Database != "campusos_v12_09a_exit" {
		t.Fatal("requires owned Admin SPA database")
	}
	root, err := projectaudit.FindRepositoryRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	password := adminSPABrowserSecret(t)
	mfaKey := adminSPABrowserSecret(t)
	jwtKey := adminSPABrowserSecret(t)
	controlKey := adminSPABrowserSecret(t)
	if err := SeedAdmin(pool, adminSeedOptions{Environment: "test", PasswordHashEnabled: true, BootstrapAdminSecret: password}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	admins := identityrepo.NewPgAdminDomainRepository(pool)
	account := "adm.knowledge.browser"
	role := "knowledge_browser_manager"
	if err := admins.CreateAccount(ctx, identityrepo.AdminDomainAccount{ID: account, Username: "knowledge-operator", Email: "knowledge-browser@example.test", DisplayName: "独立盘点管理员"}); err != nil {
		t.Fatal(err)
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := admins.SetCredentialCAS(ctx, account, hash, 0, now); err != nil || !ok {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO admin_roles(id,name) VALUES($1,'内容盘点员')`, role); err != nil {
		t.Fatal(err)
	}
	if err := admins.BindRole(ctx, identityrepo.AdminDomainRoleBinding{ID: account + ".binding", AccountID: account, RoleID: role, GrantedBy: account}); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO admin_role_permissions(role_id,permission_code,created_by) VALUES($1,'knowledge.collection.read','browser'),($1,'knowledge.collection.manage','browser')`, role); err != nil {
		t.Fatal(err)
	}
	users := identityrepo.NewPgUserRepository(pool)
	for _, u := range []struct{ id, name string }{{"7200001", "integration_alice"}, {"7200002", "integration_bob"}} {
		if err := users.Create(ctx, &identitydomain.User{ID: u.id, Username: u.name, Nickname: u.name, Email: u.name + "@example.test", Status: identitydomain.UserStatusActive, CreatedAt: now, UpdatedAt: now}); err != nil {
			t.Fatal(err)
		}
		if err := users.CreateVerifiedAccount(ctx, u.id, u.name+"@example.test", hash); err != nil {
			t.Fatal(err)
		}
	}
	category := "7200017"
	if err := communityrepo.NewPgCategoryRepository(pool).Create(ctx, &communitydomain.Category{ID: category, Name: "浏览器验收板块", Slug: "integration-browser-owned", DefaultTags: []string{}, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := communityrepo.NewPgThreadTypePolicyRepository(pool).Replace(ctx, category, []communitydomain.ThreadType{communitydomain.ThreadTypeDiscussion}); err != nil {
		t.Fatal(err)
	}
	threads := communityrepo.NewPgThreadRepository(pool)
	for i, pub := range []communitydomain.PublicationStatus{"published", "private"} {
		id := fmt.Sprint(7200021 + i)
		marker := "PUBLIC_BROWSER_CONTENT"
		if i == 1 {
			marker = "PRIVATE_BROWSER_CONTENT"
		}
		if err := threads.Create(ctx, &communitydomain.Thread{ID: id, AuthorID: "7200001", CategoryID: category, ThreadType: communitydomain.ThreadTypeDiscussion, Title: marker, Content: marker + " body", PublicationStatus: pub, ModerationStatus: "clear", DeletionStatus: "active", CreatedAt: now, UpdatedAt: now}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, `INSERT INTO builtin_feature_states(feature_id,desired_enabled,effective_enabled) VALUES('knowledge',true,true) ON CONFLICT(feature_id) DO UPDATE SET desired_enabled=true,effective_enabled=true`); err != nil {
		t.Fatal(err)
	}
	control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.Header.Get("Authorization") != "Bearer "+controlKey {
			w.WriteHeader(403)
			return
		}
		var err error
		switch r.URL.Path {
		case "/make-private":
			_, err = pool.Exec(r.Context(), `UPDATE threads SET publication_status='private',content='PRIVATE_AFTER_WHITELIST' WHERE id=7200021`)
		case "/make-public":
			_, err = pool.Exec(r.Context(), `UPDATE threads SET publication_status='published',content='PUBLIC_BROWSER_CONTENT body' WHERE id=7200021`)
		case "/withdraw-permission":
			_, err = pool.Exec(r.Context(), `DELETE FROM admin_role_permissions WHERE role_id=$1 AND permission_code='knowledge.collection.manage'`, role)
		case "/restore-permission":
			_, err = pool.Exec(r.Context(), `INSERT INTO admin_role_permissions(role_id,permission_code,created_by) VALUES($1,'knowledge.collection.manage','browser')`, role)
		case "/disable-feature":
			// A normal hot-gate command is exercised in the native composition target;
			// process restart reads the persisted defaultoff state separately.
			_, err = pool.Exec(r.Context(), `UPDATE builtin_feature_states SET desired_enabled=false,effective_enabled=false WHERE feature_id='knowledge'`)
		case "/suspend-admin":
			_, err = pool.Exec(r.Context(), `UPDATE admin_accounts SET status='suspended',version=version+1,status_reason='owned browser test' WHERE id=$1`, account)
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
	work := os.Getenv("CAMPUSOS_KNOWLEDGE_BROWSER_OUTPUT")
	if work == "" {
		work = t.TempDir()
	}
	if err := os.MkdirAll(work, 0700); err != nil {
		t.Fatal(err)
	}
	processDir := t.TempDir()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	serverLog, err := os.OpenFile(filepath.Join(work, "actual-server.log"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer serverLog.Close()
	command := exec.Command(binary)
	command.Dir = processDir
	env := map[string]string{}
	for _, entry := range os.Environ() {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			env[key] = value
		}
	}
	for key, value := range map[string]string{"DATABASE_DSN": raw, "SERVER_HOST": "127.0.0.1", "SERVER_PORT": fmt.Sprint(port), "CAMPUSOS_ENV": "test", "REDIS_ENABLED": "false", "NATS_URL": "nats://127.0.0.1:1", "HOST_API_ENABLED": "false", "AUTH_PASSWORD_HASH_ENABLED": "true", "AUTH_ALLOW_DEVELOPMENT_DEFAULT_ADMIN": "false", "AUTH_BOOTSTRAP_ADMIN_SECRET": "", "AUTH_REFRESH_BODY_COMPAT": "false", "JWT_SECRET": jwtKey, "AUTH_MFA_ACTIVE_KEY_ID": "browser", "AUTH_MFA_ENCRYPTION_KEYS": "browser:" + mfaKey, "PLUGIN_DATA_DIR": filepath.Join(processDir, "plugins"), "EMAIL_PROVIDER": "fake", "AI_ENABLED": "false", "OBSERVABILITY_PROMETHEUS_ENABLED": "false", "GIN_MODE": "release"} {
		env[key] = value
	}
	for key, value := range env {
		command.Env = append(command.Env, key+"="+value)
	}
	command.Stdout = serverLog
	command.Stderr = serverLog
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait(); close(done) }()
	defer func() {
		_ = command.Process.Signal(syscall.SIGTERM)
		select {
		case <-done:
		case <-time.After(12 * time.Second):
			_ = command.Process.Kill()
			<-done
		}
	}()
	client := &http.Client{Timeout: time.Second}
	ready := false
	for i := 0; i < 150; i++ {
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
			t.Fatal("actual server stopped before health; inspect retained actual-server.log")
		default:
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !ready {
		t.Fatal("actual cmd/server did not become healthy; inspect retained actual-server.log")
	}
	browserConfig := map[string]any{"serverLog": filepath.Join(work, "actual-server.log"), "backend": base, "control": control.URL, "controlKey": controlKey, "password": password, "adminUsername": "knowledge-browser@example.test", "account": account, "alice": "integration_alice@example.test", "bob": "integration_bob@example.test", "thread": "7200021", "privateThread": "7200022", "result": filepath.Join(work, "browser-result.json")}
	cfgPath := filepath.Join(processDir, "browser-private-config.json")
	encoded, _ := json.Marshal(browserConfig)
	if err := os.WriteFile(cfgPath, encoded, 0600); err != nil {
		t.Fatal(err)
	}
	nodeCtx, cancelNode := context.WithTimeout(ctx, 180*time.Second)
	defer cancelNode()
	node := exec.CommandContext(nodeCtx, "node", filepath.Join(root, "scripts/v12-09a-browser.mjs"), cfgPath)
	node.Dir = root
	if os.PathSeparator == '/' {
		// Chrome's SingletonSocket is limited to a short Unix socket path;
		// retained acceptance paths may exceed that limit. Only browser scratch
		// files use /tmp; config, server state and evidence retain owned paths.
		node.Env = append(os.Environ(), "TMPDIR=/tmp")
	}
	output, err := node.CombinedOutput()
	if err := os.WriteFile(filepath.Join(work, "browser.log"), output, 0600); err != nil {
		t.Fatal(err)
	}
	if err != nil {
		t.Fatalf("actual backend browser failed; inspect retained browser.log: %v", err)
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
	if json.Unmarshal(data, &report) != nil || report.Status != "passed" || report.Mocked || len(report.Checks) < 10 {
		t.Fatal("incomplete real backend browser result")
	}
	var audits int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM platform_command_audits WHERE command_code LIKE 'knowledge.%' AND actor_type='admin' AND actor_id=$1`, account).Scan(&audits); err != nil || audits < 8 {
		t.Fatal("browser lifecycle missing actual typed audits", audits, err)
	}
	var rawSecrets int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM information_schema.columns WHERE table_name IN ('knowledge_collections','knowledge_source_selections','knowledge_collection_sources') AND column_name IN ('token','secret','raw_token')`).Scan(&rawSecrets); err != nil || rawSecrets != 0 {
		t.Fatal("secret persistence", err)
	}
	logData, err := os.ReadFile(filepath.Join(work, "actual-server.log"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(logData), "PostgreSQL 基础设施连接成功") || (strings.Contains(string(logData), "业务装配将使用内存") || strings.Contains(string(logData), "PostgreSQL 基础设施不可用")) {
		t.Fatal("actual server did not use PostgreSQL")
	}
	for _, secret := range []string{password, mfaKey, jwtKey, controlKey} {
		if strings.Contains(string(logData), secret) || strings.Contains(string(output), secret) || strings.Contains(string(data), secret) {
			t.Fatal("ephemeral credential leaked into acceptance evidence")
		}
	}
	f, err := os.Open(binary)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	digest := sha256.New()
	if _, err := io.Copy(digest, f); err != nil {
		t.Fatal(err)
	}
	meta, _ := json.MarshalIndent(map[string]any{"server_binary_sha256": hex.EncodeToString(digest.Sum(nil)), "actual_cmd_server": true, "database": "owned PostgreSQL16", "isolated_process_files": true, "user_domain_fallback": false, "actual_pg_lifecycle_and_typed_audit": true, "browser_checks": len(report.Checks), "knowledge_ui": "not_claimed_API_stage_09a", "admin_login_spa": true}, "", "  ")
	if err := os.WriteFile(filepath.Join(work, "runtime-proof.json"), append(meta, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
}
