package plugin_test

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/campusos/CampusOS/internal/plugin"
	"github.com/campusos/CampusOS/internal/plugin/wasm"
	campusossdk "github.com/campusos/CampusOS/sdk/go"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func wasmU(n uint64) []byte {
	var b []byte
	for {
		c := byte(n & 127)
		n >>= 7
		if n != 0 {
			c |= 128
		}
		b = append(b, c)
		if n == 0 {
			return b
		}
	}
}
func wasmS(n int64) []byte {
	var b []byte
	for {
		c := byte(n & 127)
		n >>= 7
		done := n == 0 && c&64 == 0 || n == -1 && c&64 != 0
		if !done {
			c |= 128
		}
		b = append(b, c)
		if done {
			return b
		}
	}
}
func wasmName(s string) []byte { return append(wasmU(uint64(len(s))), []byte(s)...) }
func wasmSec(id byte, b []byte) []byte {
	return append(append([]byte{id}, wasmU(uint64(len(b)))...), b...)
}
func wasmConst(n int64) []byte { return append([]byte{0x41}, wasmS(n)...) }

// A real, reviewable guest: the import must succeed before Chat can answer;
// event delivery rejects the uppercase P in the private-body source marker.
// No mock provider, compiler toolchain or synthetic observed ready is used.
func actualWasmProgram() []byte {
	host := []byte(`{"capability":"community.thread.public.read","payload":{"thread_id":"505003"}}`)
	suffix := []byte(`,"text":"real-wasm","input_tokens":1,"output_tokens":1,"finish_reason":"completed"}`)
	code := []byte{0, 'a', 's', 'm', 1, 0, 0, 0}
	code = append(code, wasmSec(1, []byte{3, 0x60, 4, 0x7f, 0x7f, 0x7f, 0x7f, 1, 0x7f, 0x60, 2, 0x7f, 0x7f, 1, 0x7e, 0x60, 2, 0x7f, 0x7f, 1, 0x7f})...)
	imports := append(append([]byte{1}, wasmName("campusos")...), wasmName("host_call")...)
	code = append(code, wasmSec(2, append(imports, 0, 0))...)
	code = append(code, wasmSec(3, []byte{3, 1, 2, 2})...)
	code = append(code, wasmSec(5, []byte{1, 0, 4})...)
	exports := append(append([]byte{4}, wasmName("memory")...), 2, 0)
	for n, name := range []string{"campusos_invoke", "campusos_event", "campusos_configure"} {
		exports = append(append(exports, wasmName(name)...), 0, byte(n+1))
	}
	code = append(code, wasmSec(7, exports)...)
	invoke := []byte{1, 2, 0x7f}
	for _, n := range []int64{65536, int64(len(host)), 196608, 4096} {
		invoke = append(invoke, wasmConst(n)...)
	}
	invoke = append(invoke, 0x10, 0, 0x41, 0x7f, 0x46, 0x04, 0x40, 0, 0x0b)
	// Scan three commas in the host-validated correlation prefix.
	invoke = append(invoke, 0x02, 0x40, 0x03, 0x40, 0x20, 2, 0x20, 1, 0x4f, 0x04, 0x40, 0, 0x0b,
		0x20, 2, 0x2d, 0, 0, 0x41, 44, 0x46, 0x04, 0x40, 0x20, 3, 0x41, 1, 0x6a, 0x21, 3, 0x0b,
		0x20, 2, 0x41, 1, 0x6a, 0x21, 2, 0x20, 3, 0x41, 3, 0x46, 0x0d, 1, 0x0c, 0, 0x0b, 0x0b,
		0x20, 2, 0x41, 1, 0x6b)
	invoke = append(invoke, wasmConst(131072)...)
	invoke = append(invoke, wasmConst(int64(len(suffix)))...)
	invoke = append(invoke, 0xfc, 0x0a, 0, 0, 0x20, 2, 0x41, 1, 0x6b)
	invoke = append(invoke, wasmConst(int64(len(suffix)))...)
	invoke = append(invoke, 0x6a, 0xad, 0x0b)
	event := []byte{1, 1, 0x7f, 0x02, 0x40, 0x03, 0x40, 0x20, 2, 0x20, 1, 0x4f, 0x0d, 1, 0x20, 2, 0x2d, 0, 0}
	event = append(event, wasmConst(80)...)
	event = append(event, 0x46, 0x04, 0x40, 0x41, 1, 0x0f, 0x0b, 0x20, 2, 0x41, 1, 0x6a, 0x21, 2, 0x0c, 0, 0x0b, 0x0b, 0x41, 0, 0x0b)
	configure := []byte{0, 0x20, 0, 0x2d, 0, 0}
	configure = append(configure, wasmConst(123)...)
	configure = append(configure, 0x47, 0x0b)
	bodies := []byte{3}
	for _, body := range [][]byte{invoke, event, configure} {
		bodies = append(append(bodies, wasmU(uint64(len(body)))...), body...)
	}
	code = append(code, wasmSec(10, bodies)...)
	data := []byte{2}
	for n, raw := range [][]byte{host, suffix} {
		data = append(data, 0)
		data = append(data, wasmConst(int64(65536*(n+1)))...)
		data = append(data, 0x0b)
		data = append(append(data, wasmU(uint64(len(raw)))...), raw...)
	}
	return append(code, wasmSec(11, data)...)
}

type afterV5Preparation struct {
	plugin.V5RuntimeFactory
	after func()
}

func (f afterV5Preparation) Prepare(ctx context.Context, spec plugin.V5RuntimeSpec, broker plugin.V5RuntimeBroker) (plugin.V5RunningRuntime, error) {
	h, err := f.V5RuntimeFactory.Prepare(ctx, spec, broker)
	if err == nil {
		f.after()
	}
	return h, err
}

func TestV5WasmRuntimeActualPostgres(t *testing.T) {
	dsn, image := os.Getenv("CAMPUSOS_V5_WASM_DATABASE_URL"), os.Getenv("CAMPUSOS_WASM_WORKER_TEST_IMAGE")
	if dsn == "" || image == "" {
		t.Skip("requires owned05 PostgreSQL and fixed helper image")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil || cfg.ConnConfig.Database != "campusos_v12_05_wasm" {
		t.Fatal("requires exact owned05 database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	h := plugin.NewV5WasmTestHarness(t, ctx, pool, actualWasmProgram())
	factory, err := wasm.NewV5IsolatedFactory(image)
	if err != nil {
		t.Fatal(err)
	}
	store := plugin.NewPgV5StateStore(pool)
	leader, err := store.AcquireRuntimeLeader(ctx)
	if err != nil {
		t.Fatal(err)
	}
	rollbackReadiness := false
	checkedFactory := afterV5Preparation{V5RuntimeFactory: factory, after: func() {
		if rollbackReadiness {
			h.SetAuditFault(true)
		}
	}}
	coord := plugin.NewV5RuntimeCoordinator(h.Platform, h.Hub, checkedFactory, leader)
	defer func() { _ = coord.Close(ctx) }()
	q := plugin.V5ContributionQuery{Point: plugin.V5AIChat, ContractVersion: "v1", Capability: "text"}
	drive := func(owner string) {
		t.Helper()
		until := time.Now().Add(20 * time.Second)
		for {
			if _, err := h.Platform.ProcessOnce(ctx, 16); err != nil {
				t.Fatal(err)
			}
			if _, err := coord.ProcessOnce(ctx); err != nil {
				t.Fatal(err)
			}
			if h.Current(owner).Observed == "ready" {
				return
			}
			if time.Now().After(until) {
				t.Fatal("actual worker never ready", h.Current(owner))
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
	invoke := func(b plugin.V5ManagedBinding) (json.RawMessage, error) {
		payload := []byte(fmt.Sprintf(`{"request_id":"actual-1","contribution_id":"chat","instance_generation":%d,"deadline_ms":4000,"prompt":"explicit user prompt","context_class":"none"}`, b.Descriptor().InstanceGeneration))
		wire, _ := json.Marshal(campusossdk.V5DispatchCall{RequestID: "actual-1", Contract: "campusos.plugin-dispatch/v1", Target: campusossdk.V5DispatchTarget{Kind: "contribution", ID: "chat"}, InstanceGeneration: b.Descriptor().InstanceGeneration, DeadlineMS: time.Now().Add(4 * time.Second).UnixMilli(), RequestBytes: int64(len(payload)), ResponseLimitBytes: 4096})
		return h.Hub.Invoke(ctx, h.Consumer, b, wire, payload, h.User)
	}
	t.Run("pending-is-not-published", func(t *testing.T) {
		i := h.Current("")
		h.Mutate(plugin.V5Mutation{Kind: "enable", ExpectedGeneration: i.Generation}, true)
		if _, err := h.Hub.Select(ctx, "", q, nil); err == nil {
			t.Fatal("pending published")
		}
	})
	t.Run("actual-readiness-audit-rollback-never-publishes", func(t *testing.T) {
		if _, err := h.Platform.ProcessOnce(ctx, 16); err != nil {
			t.Fatal(err)
		}
		rollbackReadiness = true
		if _, err := coord.ProcessOnce(ctx); err != nil {
			t.Fatal(err)
		}
		h.SetAuditFault(false)
		rollbackReadiness = false
		if h.Current("").Observed == "ready" || len(factory.Workers()) != 0 {
			t.Fatal("rolled-back ready left a native publication")
		}
		if _, err := h.Hub.Select(ctx, "", q, nil); err == nil {
			t.Fatal("audit rollback published")
		}
	})
	t.Run("actual-readiness-and-exclusive-host", func(t *testing.T) {
		drive("")
		if other, err := store.AcquireRuntimeLeader(ctx); err == nil {
			other.Release(ctx)
			t.Fatal("two execution budget leaders")
		}
	})
	userOp := h.Mutate(plugin.V5Mutation{Kind: "install"}, false)
	h.Mutate(plugin.V5Mutation{Kind: "enable", ExpectedGeneration: userOp.Generation}, false)
	drive("2801001")
	b, err := h.Hub.Select(ctx, "2801001", q, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Run("real-user-typed-guest-and-core-host-read", func(t *testing.T) {
		out, err := invoke(b)
		if err != nil || !strings.Contains(string(out), "real-wasm") || strings.Contains(string(out), "PRIVATE") {
			t.Fatal(string(out), err)
		}
	})
	t.Run("current-core-private-and-aba", func(t *testing.T) {
		h.SetPublic(false)
		if out, err := invoke(b); err == nil || len(out) > 0 {
			t.Fatal("private Host resource released guest output", err)
		}
		h.SetPublic(true)
		if _, err := invoke(b); err != nil {
			t.Fatal("current public resource not restored", err)
		}
	})
	t.Run("metadata-native-event-ack", func(t *testing.T) {
		id := h.PublishEvent()
		if err := h.Events.CaptureCommitted(ctx, id); err != nil {
			t.Fatal(err)
		}
		if _, err := h.Events.ProcessOnce(ctx, 16); err != nil {
			t.Fatal(err)
		}
		var n int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM plugin_v5_event_deliveries WHERE state->>'source_event_id'=$1 AND state->>'status'='done'`, id).Scan(&n); err != nil || n < 1 {
			t.Fatal("actual guest metadata ACK missing", n, err)
		}
	})
	t.Run("configuration-fences-old-generation", func(t *testing.T) {
		cfg := h.Configuration("2801001")
		h.Mutate(plugin.V5Mutation{Kind: "configure", ConfigurationPart: "values", Values: json.RawMessage(`{"message":"ordinary-v2"}`), ExpectedRevision: cfg.Revision}, false)
		if out, err := invoke(b); err == nil || len(out) > 0 {
			t.Fatal("old config generation executed")
		}
		drive("2801001")
		b, err = h.Hub.Select(ctx, "2801001", q, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := invoke(b); err != nil {
			t.Fatal("real configured worker", err)
		}
	})
	t.Run("current-user-session-required", func(t *testing.T) {
		h.RevokeUser()
		if out, err := invoke(b); err == nil || len(out) > 0 {
			t.Fatal("revoked session released output")
		}
		h.RefreshUser()
		if _, err := invoke(b); err != nil {
			t.Fatal("fresh current session", err)
		}
	})
	t.Run("forced-native-exit-is-observed-and-recoverable", func(t *testing.T) {
		var name string
		for _, w := range factory.Workers() {
			if w.Owner == "2801001" {
				name = w.Name
			}
		}
		if name == "" {
			t.Fatal("no owned native worker")
		}
		if err := exec.CommandContext(ctx, "/usr/bin/docker", "kill", name).Run(); err != nil {
			t.Fatal("owned worker kill failed")
		}
		until := time.Now().Add(8 * time.Second)
		for h.Current("2801001").Observed != "failed" {
			_, _ = coord.ProcessOnce(ctx)
			if time.Now().After(until) {
				t.Fatal("native death did not fail observation")
			}
			time.Sleep(30 * time.Millisecond)
		}
		if out, err := invoke(b); err == nil || len(out) > 0 {
			t.Fatal("dead worker released output")
		}
		i := h.Current("2801001")
		h.Mutate(plugin.V5Mutation{Kind: "enable", ExpectedGeneration: i.Generation}, false)
		drive("2801001")
		b, err = h.Hub.Select(ctx, "2801001", q, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := invoke(b); err != nil {
			t.Fatal("isolated worker recovery", err)
		}
	})
	t.Run("actual-process-loss-and-recovery", func(t *testing.T) {
		if err := coord.Close(ctx); err != nil {
			t.Fatal(err)
		}
		if _, err := h.Hub.Select(ctx, "2801001", q, nil); err == nil {
			t.Fatal("closed runtime publication")
		}
		if err := h.Platform.ReconcileStartup(ctx); err != nil {
			t.Fatal(err)
		}
		h.Hub = plugin.NewV5ManagedRuntime(plugin.NewV5ContributionDirectory(h.Platform))
		h.BindHosts(h.Hub)
		leader, err = store.AcquireRuntimeLeader(ctx)
		if err != nil {
			t.Fatal(err)
		}
		coord = plugin.NewV5RuntimeCoordinator(h.Platform, h.Hub, factory, leader)
		drive("")
		drive("2801001")
		if out, err := invoke(b); err == nil || len(out) > 0 {
			t.Fatal("restart revived old handle")
		}
		b, err = h.Hub.Select(ctx, "2801001", q, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := invoke(b); err != nil {
			t.Fatal("actual restart recovery", err)
		}
	})
	t.Run("lost-postgres-leader-fences-and-reaps-native-workers", func(t *testing.T) {
		var n int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM (SELECT pg_terminate_backend(pid) FROM pg_locks WHERE locktype='advisory' AND classid=1128353104::oid AND objid=1463898957::oid AND objsubid=2 AND granted) x`).Scan(&n); err != nil || n != 1 {
			t.Fatal("owned leader termination", n, err)
		}
		if _, err := coord.ProcessOnce(ctx); err == nil {
			t.Fatal("lost leader admitted")
		}
		if out, err := invoke(b); err == nil || len(out) > 0 {
			t.Fatal("lost leader released output")
		}
		if err := coord.Close(ctx); err != nil {
			t.Fatal(err)
		}
		if len(factory.Workers()) != 0 {
			t.Fatal("lost leader leaked workers")
		}
		if err := h.Platform.ReconcileStartup(ctx); err != nil {
			t.Fatal(err)
		}
		h.Hub = plugin.NewV5ManagedRuntime(plugin.NewV5ContributionDirectory(h.Platform))
		h.BindHosts(h.Hub)
		leader, err = store.AcquireRuntimeLeader(ctx)
		if err != nil {
			t.Fatal(err)
		}
		coord = plugin.NewV5RuntimeCoordinator(h.Platform, h.Hub, factory, leader)
		// A known runtime loss is durable failed intent; retry explicitly.
		h.Mutate(plugin.V5Mutation{Kind: "enable", ExpectedGeneration: h.Current("").Generation}, true)
		h.Mutate(plugin.V5Mutation{Kind: "enable", ExpectedGeneration: h.Current("2801001").Generation}, false)
		drive("")
		drive("2801001")
		b, err = h.Hub.Select(ctx, "2801001", q, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := invoke(b); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("trust-revocation-stops-current-publication", func(t *testing.T) {
		h.SetTrust(false)
		if out, err := invoke(b); err == nil || len(out) > 0 {
			t.Fatal("revoked package trust executed")
		}
		_, _ = coord.ProcessOnce(ctx)
		if _, err := h.Hub.Select(ctx, "2801001", q, nil); err == nil {
			t.Fatal("trust-revoked runtime published")
		}
		h.SetTrust(true)
	})
	t.Run("real-grant-expiry-withdraws-and-reaps", func(t *testing.T) {
		expiry := time.Now().Add(8 * time.Second)
		h.GrantUntil(expiry.UnixMilli())
		h.Mutate(plugin.V5Mutation{Kind: "enable", ExpectedGeneration: h.Current("").Generation}, true)
		h.Mutate(plugin.V5Mutation{Kind: "enable", ExpectedGeneration: h.Current("2801001").Generation}, false)
		drive("")
		drive("2801001")
		b, err = h.Hub.Select(ctx, "2801001", q, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := invoke(b); err != nil {
			t.Fatal("finite grant did not admit actual guest", err)
		}
		timer := time.NewTimer(time.Until(expiry) + 40*time.Millisecond)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
		if out, err := invoke(b); err == nil || len(out) > 0 {
			t.Fatal("expired grant released guest output")
		}
		if _, err := coord.ProcessOnce(ctx); err != nil {
			t.Fatal(err)
		}
		if len(factory.Workers()) != 0 {
			t.Fatal("expired workers not reaped")
		}
	})

}
