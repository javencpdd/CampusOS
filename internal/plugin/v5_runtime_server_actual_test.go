package plugin_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/campusos/CampusOS/internal/plugin"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestV5WasmActualServer(t *testing.T) {
	dsn, image, binary := os.Getenv("CAMPUSOS_V5_WASM_DATABASE_URL"), os.Getenv("CAMPUSOS_WASM_WORKER_TEST_IMAGE"), os.Getenv("CAMPUSOS_V5_WASM_SERVER_BINARY")
	if dsn == "" || image == "" || binary == "" {
		t.Skip("requires owned05 PG, image and actual server")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil || cfg.ConnConfig.Database != "campusos_v12_05_wasm" {
		t.Fatal("requires exact owned05 database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Second)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	h := plugin.NewV5WasmTestHarness(t, ctx, pool, actualWasmProgram())
	h.Mutate(plugin.V5Mutation{Kind: "enable", ExpectedGeneration: h.Current("").Generation}, true)
	p, err := h.Admin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var random [48]byte
	if _, err := rand.Read(random[:]); err != nil {
		t.Fatal(err)
	}
	secret := hex.EncodeToString(random[:32])
	jwt := auth.NewJWTManager(auth.JWTConfig{Secret: auth.DeriveDomainSecret(secret, "campusos-admin-domain-v1"), Issuer: "campusos-admin", Audience: "admin-api", KeyID: "admin-key", AccessTTL: time.Hour})
	token, err := jwt.GenerateAccessToken(p.Actor.ID, p.Actor.ID, auth.AccessTokenContext{SessionID: p.CredentialID, AuthVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()
	launch := func(image string) (string, func(bool)) {
		t.Helper()
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		port := l.Addr().(*net.TCPAddr).Port
		_ = l.Close()
		log, err := os.CreateTemp(work, "server-*.log")
		if err != nil {
			t.Fatal(err)
		}
		command := exec.Command(binary)
		command.Dir = work
		env := map[string]string{"PATH": "/usr/bin:/bin", "CAMPUSOS_SKIP_DOTENV": "true", "DATABASE_DSN": dsn, "SERVER_HOST": "127.0.0.1", "SERVER_PORT": fmt.Sprint(port), "CAMPUSOS_ENV": "test", "REDIS_ENABLED": "false", "NATS_URL": "nats://127.0.0.1:1", "HOST_API_ENABLED": "false", "AUTH_PASSWORD_HASH_ENABLED": "true", "AUTH_ALLOW_DEVELOPMENT_DEFAULT_ADMIN": "false", "AUTH_BOOTSTRAP_ADMIN_SECRET": secret, "JWT_SECRET": secret, "AUTH_MFA_ACTIVE_KEY_ID": "owned05", "AUTH_MFA_ENCRYPTION_KEYS": "owned05:" + hex.EncodeToString(random[:32]), "PLUGIN_DATA_DIR": h.ManagedRoot, "CAMPUSOS_PLUGIN_V5_TRUST_STORE": h.TrustPath, "CAMPUSOS_PLUGIN_WASM_IMAGE": image, "EMAIL_PROVIDER": "fake", "AI_ENABLED": "false", "OBSERVABILITY_PROMETHEUS_ENABLED": "false", "GIN_MODE": "release"}
		for k, v := range env {
			command.Env = append(command.Env, k+"="+v)
		}
		command.Stdout, command.Stderr = log, log
		if err := command.Start(); err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() { done <- command.Wait(); close(done) }()
		stopped := false
		stop := func(force bool) {
			if stopped {
				return
			}
			stopped = true
			if force {
				_ = command.Process.Kill()
			} else {
				_ = command.Process.Signal(syscall.SIGTERM)
			}
			select {
			case err := <-done:
				if !force && err != nil {
					t.Error("actual server did not exit cleanly", err)
				}
			case <-time.After(12 * time.Second):
				_ = command.Process.Kill()
				<-done
				t.Error("actual server shutdown timeout")
			}
			_ = log.Close()
			raw, err := os.ReadFile(log.Name())
			if err != nil || strings.Contains(string(raw), "DATA RACE") {
				t.Error("actual server race log failed verification")
			}
		}
		t.Cleanup(func() { stop(false) })
		base := fmt.Sprintf("http://127.0.0.1:%d", port)
		client := &http.Client{Timeout: time.Second}
		until := time.Now().Add(20 * time.Second)
		for {
			r, e := client.Get(base + "/api/v1/health")
			if e == nil {
				_ = r.Body.Close()
				if r.StatusCode == 200 {
					return base, stop
				}
			}
			select {
			case <-done:
				raw, _ := os.ReadFile(log.Name())
				t.Log(string(raw))
				t.Fatal("actual server failed before health")
			default:
			}
			if time.Now().After(until) {
				t.Fatal("server health timeout")
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
	ready := func() {
		t.Helper()
		until := time.Now().Add(25 * time.Second)
		for h.Current("").Observed != "ready" {
			if time.Now().After(until) {
				t.Fatal("actual cmd/server never ready", h.Current(""))
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
	workers := func() []string {
		raw, err := exec.CommandContext(ctx, "/usr/bin/docker", "ps", "--all", "--filter=label=campusos.wasm.coordinate="+h.Coordinate.Publisher+"/"+h.Coordinate.Key+"/"+h.Coordinate.Version, "--format={{.Names}}").Output()
		if err != nil {
			t.Fatal(err)
		}
		return strings.Fields(string(raw))
	}
	t.Run("invalid-image-is-fail-closed", func(t *testing.T) {
		_, stop := launch("invalid:tag")
		defer stop(false)
		time.Sleep(1300 * time.Millisecond)
		if h.Current("").Observed == "ready" || len(workers()) != 0 {
			t.Fatal("invalid runner falsely ready")
		}
	})
	base, stop := launch(image)
	ready()
	oldGeneration := h.Current("").Generation
	oldWorkers := workers()
	if len(oldWorkers) != 1 {
		t.Fatal("actual server worker missing", len(oldWorkers))
	}
	t.Run("actual-admin-route-observes-native-ready", func(t *testing.T) {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/v1/admin/plugin-v5/"+h.Coordinate.Publisher+"/"+h.Coordinate.Key+"/"+h.Coordinate.Version, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		r, err := (&http.Client{Timeout: time.Second}).Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Body.Close()
		raw, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		var body map[string]any
		if r.StatusCode != 200 || json.Unmarshal(raw, &body) != nil || !strings.Contains(string(raw), `"ready"`) {
			t.Fatal("actual admin ready route", r.StatusCode, string(raw))
		}
	})
	t.Run("killed-host-orphans-fenced-before-restart", func(t *testing.T) {
		stop(true)
		if len(workers()) != 1 {
			t.Fatal("expected actual orphan before fencing")
		}
		_, stop = launch(image)
		ready()
		if h.Current("").Generation <= oldGeneration {
			t.Fatal("restart kept generation")
		}
		names := workers()
		if len(names) != 1 || names[0] == oldWorkers[0] {
			t.Fatal("old worker was not fenced", names)
		}
	})
	t.Run("graceful-shutdown-reaps-owned-worker", func(t *testing.T) {
		stop(false)
		if len(workers()) != 0 {
			t.Fatal("shutdown leaked native worker")
		}
	})
}
