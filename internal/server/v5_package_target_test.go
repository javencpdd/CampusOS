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
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPluginV5ActualServerIgnoresRetiredPackages(t *testing.T) {
	dsn, binary, work := os.Getenv("CAMPUSOS_V5_SERVER_DATABASE_URL"), os.Getenv("CAMPUSOS_V5_SERVER_BINARY"), os.Getenv("CAMPUSOS_V5_SERVER_OUTPUT")
	if dsn == "" || binary == "" || work == "" {
		t.Skip("owned v5 server target not configured")
	}
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil || config.ConnConfig.Database != "campusos_v12_03a_target" {
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
	env := map[string]string{"PATH": os.Getenv("PATH"), "CAMPUSOS_SKIP_DOTENV": "true", "DATABASE_DSN": dsn, "SERVER_HOST": "127.0.0.1", "SERVER_PORT": fmt.Sprint(port), "CAMPUSOS_ENV": "test", "REDIS_ENABLED": "false", "NATS_URL": "nats://127.0.0.1:1", "HOST_API_ENABLED": "false", "AUTH_PASSWORD_HASH_ENABLED": "true", "AUTH_ALLOW_DEVELOPMENT_DEFAULT_ADMIN": "false", "JWT_SECRET": adminSPABrowserSecret(t), "AUTH_MFA_ACTIVE_KEY_ID": "test", "AUTH_MFA_ENCRYPTION_KEYS": "test:" + adminSPABrowserSecret(t), "PLUGIN_DATA_DIR": filepath.Join(processDir, "data"), "PLUGINS_DIR": legacy, "CAMPUSOS_PLUGIN_V4_DIR": legacy, "CAMPUSOS_PLUGIN_V4_DEV_SOURCE": "true", "EMAIL_PROVIDER": "fake", "AI_ENABLED": "false", "OBSERVABILITY_PROMETHEUS_ENABLED": "false", "GIN_MODE": "release"}
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
	var version int
	if err = pool.QueryRow(ctx, `SELECT max(version)::integer FROM schema_migrations`).Scan(&version); err != nil || version != 25 {
		t.Fatal("wrong target schema")
	}
	bytes, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(bytes)
	proof := map[string]any{"status": "passed", "actual_cmd_server": true, "server_binary_sha256": hex.EncodeToString(digest[:]), "migration": version, "checks": []string{"actual server health with both legacy autoload flags set", "malformed legacy source and installed directories ignored", "public runtime external plugins empty", "compiled module contributions retained", "owned PostgreSQL25 startup passed"}}
	wire, _ := json.MarshalIndent(proof, "", "  ")
	if os.WriteFile(filepath.Join(work, "server-result.json"), append(wire, '\n'), 0600) != nil {
		t.Fatal("write proof")
	}
}
