package plugin_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	identity "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/plugin"
	container "github.com/campusos/CampusOS/internal/plugin/container"
	"github.com/campusos/CampusOS/internal/plugin/wasm"
	sdk "github.com/campusos/CampusOS/sdk/go"
	"github.com/jackc/pgx/v5/pgxpool"
)

type v5LifecycleFault struct {
	plugin.V5RuntimeFactory
	fail     bool
	version  string
	injected int
	after    func()
}

func (f *v5LifecycleFault) Prepare(ctx context.Context, s plugin.V5RuntimeSpec, b plugin.V5RuntimeBroker) (plugin.V5RunningRuntime, error) {
	r, err := f.V5RuntimeFactory.Prepare(ctx, s, b)
	if err == nil && f.after != nil {
		after := f.after
		f.after = nil
		after()
	}
	if err != nil || !f.fail || s.Coordinate.Version != f.version {
		return r, err
	}
	r.Abort()
	closeCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := r.Close(closeCtx); err != nil {
		return nil, err
	}
	f.injected++
	return nil, plugin.ErrV5RuntimeMissing
}
func TestV5LifecycleActualPostgres(t *testing.T) {
	dsn, image, wasmImage := os.Getenv("CAMPUSOS_V5_LIFECYCLE_DATABASE_URL"), os.Getenv("CAMPUSOS_V5_LIFECYCLE_REFERENCE"), os.Getenv("CAMPUSOS_WASM_WORKER_TEST_IMAGE")
	if dsn == "" || image == "" || wasmImage == "" {
		t.Skip("requires owned07 lifecycle PostgreSQL and both real native runtime images")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil || cfg.ConnConfig.Database != "campusos_v12_07_upgrade" || !strings.HasPrefix(image, "localhost/campusos-owned-v12-07-upgrade@sha256:") {
		t.Fatal("requires exact owned07 lifecycle resources")
	}
	for _, kind := range []string{"container-http", "container-grpc", "wasm"} {
		t.Run(kind, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
			defer cancel()
			pool, err := pgxpool.NewWithConfig(ctx, cfg)
			if err != nil {
				t.Fatal(err)
			}
			defer pool.Close()
			var h *plugin.V5WasmTestHarness
			var factory plugin.V5RuntimeFactory
			var workers func() int
			var closeFactory func(context.Context) error
			if kind == "wasm" {
				h = plugin.NewV5LifecycleTestHarness(t, ctx, pool, actualWasmProgram(), "", "")
				f, err := wasm.NewV5IsolatedFactoryForDeployment(wasmImage, strings.Repeat("e", 64))
				if err != nil {
					t.Fatal(err)
				}
				factory = f
				workers = func() int { return len(f.Workers()) }
				closeFactory = func(context.Context) error { return nil }
			} else {
				transport := strings.TrimPrefix(kind, "container-")
				h = plugin.NewV5LifecycleTestHarness(t, ctx, pool, nil, image, transport)
				f, err := container.NewV5Factory(strings.Repeat("e", 64))
				if err != nil {
					t.Fatal(err)
				}
				factory = f
				workers = func() int { return len(f.Workers()) }
				closeFactory = f.Close
			}
			defer closeFactory(ctx)
			leader, err := plugin.NewPgV5StateStore(pool).AcquireRuntimeLeader(ctx)
			if err != nil {
				t.Fatal(err)
			}
			fault := &v5LifecycleFault{V5RuntimeFactory: factory, version: "8.2.0"}
			coordinator := plugin.NewV5RuntimeCoordinator(h.Platform, h.Hub, fault, leader)
			defer coordinator.Close(ctx)
			a := h.Coordinate
			users := map[string]identity.SessionPrincipalResolver{"2801001": h.User, "2801002": h.UserSession(t, "2801002")}
			sequence := 0
			id := func() string { sequence++; return a.Key + "-" + fmt.Sprint(sequence) }
			detail := func(tt *testing.T, c plugin.V5Coordinate, owner string) plugin.V5InventoryEntry {
				tt.Helper()
				proof, admin := h.Admin, true
				if owner != "" {
					proof, admin = users[owner], false
				}
				v, err := h.Platform.Detail(ctx, c, admin, proof)
				if err != nil {
					tt.Fatal("detail", err)
				}
				return v
			}
			command := func(tt *testing.T, c plugin.V5Coordinate, owner string, r plugin.V5Mutation) plugin.V5Operation {
				tt.Helper()
				proof, admin := h.Admin, true
				if owner != "" {
					proof, admin = users[owner], false
				}
				d := detail(tt, c, owner)
				r.Version = c.Version
				r.IdempotencyKey = id()
				switch r.Kind {
				case "install", "enable", "disable", "uninstall", "upgrade":
					if d.Installation != nil {
						r.ExpectedGeneration = d.Installation.Generation
					}
				case "configure":
					if d.Configuration != nil {
						r.ExpectedRevision = d.Configuration.Revision
					}
				case "grant", "revoke":
					if d.Grant != nil {
						r.ExpectedRevision = d.Grant.Revision
					}
				case "consent":
					if d.Consent != nil {
						r.ExpectedRevision = d.Consent.Revision
					}
					r.PurposeHash = d.PurposeHash
				}
				op, err := h.Platform.Mutate(ctx, c.Publisher, c.Key, r, admin, proof)
				if err != nil {
					tt.Fatal(r.Kind, c.Version, owner, err)
				}
				return op
			}
			deny := func(tt *testing.T, c plugin.V5Coordinate, owner, action string, expected error) {
				tt.Helper()
				proof, admin := h.Admin, true
				if owner != "" {
					proof, admin = users[owner], false
				}
				i := h.Installation(tt, c, owner)
				_, err := h.Platform.Mutate(ctx, c.Publisher, c.Key, plugin.V5Mutation{Kind: action, Version: c.Version, ExpectedGeneration: i.Generation, IdempotencyKey: id()}, admin, proof)
				if err != expected {
					tt.Fatal("expected current authority/recovery rejection", action, owner, err)
				}
			}
			approve := func(tt *testing.T, c plugin.V5Coordinate) {
				tt.Helper()
				m, err := plugin.ParsePluginV5Manifest(detail(tt, c, "").Release.Manifest)
				if err != nil {
					tt.Fatal(err)
				}
				command(tt, c, "", plugin.V5Mutation{Kind: "grant", Approved: true, Consumes: m.Consumes, Resources: m.HostResources})
			}
			config := func(tt *testing.T, c plugin.V5Coordinate, owner, message string) {
				tt.Helper()
				raw, _ := json.Marshal(map[string]string{"message": message})
				command(tt, c, owner, plugin.V5Mutation{Kind: "configure", ConfigurationPart: "values", Values: raw})
			}
			drive := func(tt *testing.T, c plugin.V5Coordinate, owner, observed string) {
				tt.Helper()
				until := time.Now().Add(25 * time.Second)
				for {
					if _, err := h.Platform.ProcessOnce(ctx, 32); err != nil {
						tt.Fatal(err)
					}
					if _, err := coordinator.ProcessOnce(ctx); err != nil {
						tt.Fatal(err)
					}
					if h.Installation(tt, c, owner).Observed == observed {
						return
					}
					if time.Now().After(until) {
						tt.Fatal("lifecycle target did not converge", owner, observed, h.Installation(tt, c, owner))
					}
					time.Sleep(50 * time.Millisecond)
				}
			}
			write := func(tt *testing.T, c plugin.V5Coordinate, owner, value string) {
				tt.Helper()
				root := h.PrivateFiles(tt, c, owner)
				defer root.Close()
				if err := root.WriteFile("state", []byte(value), 0600); err != nil {
					tt.Fatal(err)
				}
			}
			read := func(tt *testing.T, c plugin.V5Coordinate, owner string) string {
				tt.Helper()
				root := h.PrivateFiles(tt, c, owner)
				defer root.Close()
				raw, err := root.ReadFile("state")
				if os.IsNotExist(err) {
					return ""
				}
				if err != nil {
					tt.Fatal(err)
				}
				return string(raw)
			}
			q := plugin.V5ContributionQuery{Point: plugin.V5AIChat, ContractVersion: "v1", Capability: "text"}
			selectBinding := func(tt *testing.T, owner string, c plugin.V5Coordinate) plugin.V5ManagedBinding {
				tt.Helper()
				ref := &plugin.V5ContributionRef{V5Coordinate: c, ID: "chat"}
				b, err := h.Hub.Select(ctx, owner, q, ref)
				if err != nil {
					tt.Fatal(err)
				}
				return b
			}
			invoke := func(b plugin.V5ManagedBinding, proof identity.SessionPrincipalResolver) (json.RawMessage, error) {
				p, _ := json.Marshal(sdk.V5ChatRequest{RequestID: "lifecycle07", ContributionID: "chat", InstanceGeneration: b.Descriptor().InstanceGeneration, DeadlineMS: 4000, Prompt: "configuration", ContextClass: "none"})
				w, _ := json.Marshal(sdk.V5DispatchCall{RequestID: "lifecycle07", Contract: "campusos.plugin-dispatch/v1", Target: sdk.V5DispatchTarget{Kind: "contribution", ID: "chat"}, InstanceGeneration: b.Descriptor().InstanceGeneration, DeadlineMS: time.Now().Add(4 * time.Second).UnixMilli(), RequestBytes: int64(len(p)), ResponseLimitBytes: 8192})
				return h.Hub.Invoke(ctx, h.Consumer, b, w, p, proof)
			}
			checkCall := func(tt *testing.T, c plugin.V5Coordinate, owner, system, user string) {
				tt.Helper()
				out, err := invoke(selectBinding(tt, owner, c), users[owner])
				if err != nil {
					tt.Fatal("actual typed invocation", err)
				}
				if kind == "wasm" {
					if !strings.Contains(string(out), "real-wasm") {
						tt.Fatal("real Wasm guest not invoked")
					}
					return
				}
				var response sdk.V5ChatResponse
				if err := json.Unmarshal(out, &response); err != nil {
					tt.Fatal(err)
				}
				var values map[string]map[string]string
				if err := json.Unmarshal([]byte(response.Text), &values); err != nil {
					tt.Fatal(err)
				}
				if values["system"]["message"] != system || values["user"]["message"] != user {
					tt.Fatal("wrong actual version/owner configuration")
				}
			}
			installUser := func(tt *testing.T, c plugin.V5Coordinate, owner, message string) {
				tt.Helper()
				command(tt, c, owner, plugin.V5Mutation{Kind: "install"})
				command(tt, c, owner, plugin.V5Mutation{Kind: "consent", Accepted: true})
				config(tt, c, owner, message)
				command(tt, c, owner, plugin.V5Mutation{Kind: "enable"})
				drive(tt, c, owner, "ready")
			}
			var b, other plugin.V5Coordinate
			var otherBinding plugin.V5ManagedBinding
			t.Run("expanded-review-and-two-owner-instances", func(t *testing.T) {
				config(t, a, "", "global-a")
				command(t, a, "", plugin.V5Mutation{Kind: "enable"})
				drive(t, a, "", "ready")
				write(t, a, "", "global-private-a")
				for _, owner := range []string{"2801001", "2801002"} {
					installUser(t, a, owner, "owner-"+owner+"-a")
					write(t, a, owner, "private-"+owner+"-a")
					checkCall(t, a, owner, "global-a", "owner-"+owner+"-a")
				}
				other = h.ImportVariant(a.Version, func(m *plugin.PluginV5Manifest) { m.Key = a.Key + "-other" })
				command(t, other, "", plugin.V5Mutation{Kind: "install"})
				approve(t, other)
				command(t, other, "", plugin.V5Mutation{Kind: "enable"})
				drive(t, other, "", "ready")
				installUser(t, other, "2801001", "other-user")
				otherBinding = selectBinding(t, "2801001", other)
				if workers() != 5 {
					t.Fatal("wrong isolated instance count", workers())
				}
				b = h.ImportVariant("8.2.0", func(m *plugin.PluginV5Manifest) {
					m.HostResources.CPUMillis = 750
					m.Consumes[0].Purpose = "Expanded release purpose requires an independent review"
				})
				if detail(t, b, "").Grant != nil {
					t.Fatal("candidate inherited grant")
				}
				deny(t, b, "", "upgrade", plugin.ErrV5Denied)
				approve(t, b)
				config(t, b, "", "global-b")
				unsafe := h.ImportVariant("9.0.0", func(m *plugin.PluginV5Manifest) {
					m.Data.Upgrade.SchemaVersion++
					m.Data.Upgrade.Mode = "reset"
					m.Data.Upgrade.Rollback = "forbidden"
				})
				approve(t, unsafe)
				deny(t, unsafe, "", "upgrade", plugin.ErrV5Rollback)
			})
			if t.Failed() {
				return
			}
			t.Run("candidate-failure-isolation-and-explicit-recovery", func(t *testing.T) {
				fault.fail = true
				command(t, b, "", plugin.V5Mutation{Kind: "upgrade"})
				drive(t, a, "", "failed")
				fault.fail = false
				i := h.Installation(t, a, "")
				if i.Version != a.Version || i.ActiveGeneration != 0 || fault.injected != 1 || read(t, a, "") != "global-private-a" {
					t.Fatal("failed candidate changed committed version/data")
				}
				if out, err := invoke(otherBinding, users["2801001"]); err != nil || len(out) == 0 {
					t.Fatal("failed plugin disrupted independent plugin", err)
				}
				command(t, a, "", plugin.V5Mutation{Kind: "disable"})
				drive(t, a, "", "stopped")
				command(t, a, "", plugin.V5Mutation{Kind: "enable"})
				drive(t, a, "", "ready")
				for _, owner := range []string{"2801001", "2801002"} {
					command(t, a, owner, plugin.V5Mutation{Kind: "disable"})
					drive(t, a, owner, "stopped")
					command(t, a, owner, plugin.V5Mutation{Kind: "enable"})
					drive(t, a, owner, "ready")
				}
			})
			if t.Failed() {
				return
			}
			t.Run("ready-cutover-and-verified-rollback", func(t *testing.T) {
				old := selectBinding(t, "2801001", a)
				command(t, a, "2801002", plugin.V5Mutation{Kind: "disable"})
				drive(t, a, "2801002", "stopped")
				up := command(t, b, "", plugin.V5Mutation{Kind: "upgrade"})
				if up.Checkpoint.ID == "" {
					t.Fatal("missing immutable checkpoint")
				}
				drive(t, b, "", "ready")
				if out, err := invoke(old, users["2801001"]); err == nil || len(out) != 0 {
					t.Fatal("old-generation output released after upgrade")
				}
				for _, owner := range []string{"2801001", "2801002"} {
					deny(t, b, owner, "upgrade", plugin.ErrV5Denied)
					command(t, b, owner, plugin.V5Mutation{Kind: "consent", Accepted: true})
					config(t, b, owner, "owner-"+owner+"-b")
					command(t, b, owner, plugin.V5Mutation{Kind: "upgrade"})
					if owner == "2801002" {
						drive(t, b, owner, "stopped")
						i := h.Installation(t, b, owner)
						if i.Version != a.Version || i.DesiredVersion != b.Version || i.ActiveGeneration != 0 {
							t.Fatal("stopped upgrade promoted without readiness")
						}
						command(t, b, owner, plugin.V5Mutation{Kind: "enable"})
					}
					drive(t, b, owner, "ready")
					write(t, b, owner, "private-"+owner+"-b")
					checkCall(t, b, owner, "global-b", "owner-"+owner+"-b")
				}
				write(t, b, "", "global-private-b")
				config(t, a, "", "offline-edit-a")
				command(t, a, "", plugin.V5Mutation{Kind: "revoke"})
				deny(t, a, "", "upgrade", plugin.ErrV5Denied)
				approve(t, a)
				rollback := command(t, a, "", plugin.V5Mutation{Kind: "upgrade"})
				if _, err := h.Platform.ProcessOnce(ctx, 32); err != nil {
					t.Fatal(err)
				}
				auditInjected := false
				fault.after = func() { auditInjected = true; h.SetAuditFault(true) }
				untilAudit := time.Now().Add(8 * time.Second)
				for !auditInjected {
					_, err := coordinator.ProcessOnce(ctx)
					h.SetAuditFault(false)
					if err != nil {
						t.Fatal(err)
					}
					if time.Now().After(untilAudit) {
						t.Fatal("actual prepared rollback never reached PG audit fault")
					}
				}
				if i := h.Installation(t, a, ""); i.Version != b.Version || i.ActiveGeneration != 0 || read(t, a, "") != "global-private-b" {
					t.Fatal("PG audit rollback published restored version/data")
				}
				operation, err := h.Platform.Operation(ctx, rollback.ID, true, h.Admin)
				if err != nil {
					t.Fatal(err)
				}
				revision, err := strconv.ParseInt(operation.Revision, 10, 64)
				if err != nil {
					t.Fatal(err)
				}
				command(t, a, "", plugin.V5Mutation{Kind: "retry", OperationID: rollback.ID, ExpectedRevision: revision})
				drive(t, a, "", "ready")
				if read(t, a, "") != "global-private-a" || detail(t, a, "").Configuration.Values == nil {
					t.Fatal("global checkpoint not restored")
				}
				var values map[string]string
				if err := json.Unmarshal(detail(t, a, "").Configuration.Values, &values); err != nil || values["message"] != "global-a" {
					t.Fatal("restored config differs from checkpoint")
				}
				for _, owner := range []string{"2801001", "2801002"} {
					deny(t, a, owner, "upgrade", plugin.ErrV5Denied)
					if read(t, b, owner) != "private-"+owner+"-b" {
						t.Fatal("other-owner data switched before own rollback")
					}
					command(t, a, owner, plugin.V5Mutation{Kind: "consent", Accepted: true})
					op := command(t, a, owner, plugin.V5Mutation{Kind: "upgrade"})
					drive(t, a, owner, "ready")
					if owner == "2801001" {
						if _, err := h.Platform.Operation(ctx, op.ID, false, users["2801002"]); err != plugin.ErrV5NotFound {
							t.Fatal("cross-owner operation exposed", err)
						}
					}
					if read(t, a, owner) != "private-"+owner+"-a" {
						t.Fatal("owner checkpoint mixed data")
					}
					checkCall(t, a, owner, "global-a", "owner-"+owner+"-a")
				}
				deny(t, b, "", "purge_release", plugin.ErrV5Conflict)
			})
			if t.Failed() {
				return
			}
			t.Run("repeated-reload-event-dedup-and-uninstall", func(t *testing.T) {
				beforeLoss := selectBinding(t, "2801001", a)
				current := h.Installation(t, a, "2801001")
				if current.DataGeneration == 0 {
					t.Fatal("rollback did not persist a data view")
				}
				ownedView := filepath.Join(h.ManagedRoot, "v5", "restored-data", a.Publisher, a.Key, "user-2801001", strconv.FormatInt(current.DataGeneration, 10))
				if err := os.Rename(ownedView, ownedView+".owned-loss"); err != nil {
					t.Fatal(err)
				}
				if out, err := invoke(beforeLoss, users["2801001"]); err == nil || len(out) != 0 {
					t.Fatal("missing active private view admitted runtime output")
				}
				if _, err := os.Stat(ownedView); !os.IsNotExist(err) {
					t.Fatal("missing private view silently recreated", err)
				}
				if err := os.Rename(ownedView+".owned-loss", ownedView); err != nil {
					t.Fatal(err)
				}
				command(t, a, "2801001", plugin.V5Mutation{Kind: "enable"})
				drive(t, a, "2801001", "ready")
				checkCall(t, a, "2801001", "global-a", "owner-2801001-a")
				for n := 0; n < 2; n++ {
					old := selectBinding(t, "2801001", a)
					command(t, a, "2801001", plugin.V5Mutation{Kind: "disable"})
					drive(t, a, "2801001", "stopped")
					command(t, a, "2801001", plugin.V5Mutation{Kind: "enable"})
					drive(t, a, "2801001", "ready")
					if out, err := invoke(old, users["2801001"]); err == nil || len(out) != 0 {
						t.Fatal("reloaded old binding released output")
					}
					source := h.PublishEvent()
					for repeat := 0; repeat < 3; repeat++ {
						if err := h.Events.CaptureCommitted(ctx, source); err != nil {
							t.Fatal(err)
						}
					}
					if _, err := h.Events.ProcessOnce(ctx, 32); err != nil {
						t.Fatal(err)
					}
					if err := h.Events.CaptureCommitted(ctx, source); err != nil {
						t.Fatal(err)
					}
					var total, done int
					if err := pool.QueryRow(ctx, `SELECT count(*),count(*) FILTER(WHERE state->>'status'='done') FROM plugin_v5_event_deliveries WHERE source_event_id=$1`, source).Scan(&total, &done); err != nil || total != 5 || done != 5 || workers() != 5 {
						t.Fatal("duplicate/failed native subscriptions", total, done, workers(), err)
					}
				}
				before := detail(t, a, "2801001").Configuration
				command(t, a, "2801001", plugin.V5Mutation{Kind: "uninstall"})
				drive(t, a, "2801001", "stopped")
				if read(t, a, "2801001") != "private-2801001-a" || !reflect.DeepEqual(detail(t, a, "2801001").Configuration, before) {
					t.Fatal("default uninstall lost retained data/config")
				}
				command(t, a, "2801001", plugin.V5Mutation{Kind: "install"})
				command(t, a, "2801001", plugin.V5Mutation{Kind: "enable"})
				drive(t, a, "2801001", "ready")
				checkCall(t, a, "2801001", "global-a", "owner-2801001-a")
				command(t, a, "2801001", plugin.V5Mutation{Kind: "uninstall", Purge: true})
				drive(t, a, "2801001", "stopped")
				if read(t, a, "2801001") != "" || read(t, a, "2801002") != "private-2801002-a" || read(t, a, "") != "global-private-a" {
					t.Fatal("purge escaped owner or retained private data")
				}
				for _, c := range []plugin.V5Coordinate{a, b} {
					var values map[string]string
					configuration := h.ConfigurationAt(t, c, "2801001")
					if err := json.Unmarshal(configuration.Values, &values); err != nil || values["message"] != "ordinary-v1" || len(configuration.SecretRefs) != 0 {
						t.Fatal("purge retained version configuration", err)
					}
				}
				if _, err := h.Platform.Detail(ctx, b, false, users["2801001"]); err != plugin.ErrV5NotFound {
					t.Fatal("purge exposed a hidden old-version user detail", err)
				}
				if i := h.Installation(t, a, "2801001"); i.Rollback.ID != "" || i.DataGeneration != 0 {
					t.Fatal("purge retained recovery pointer")
				}
				checkCall(t, a, "2801002", "global-a", "owner-2801002-a")
				if out, err := invoke(otherBinding, users["2801001"]); err != nil || len(out) == 0 {
					t.Fatal("owner purge disrupted independent plugin", err)
				}
				command(t, a, "2801002", plugin.V5Mutation{Kind: "uninstall", Purge: true})
				drive(t, a, "2801002", "stopped")
				command(t, a, "", plugin.V5Mutation{Kind: "uninstall", Purge: true})
				drive(t, a, "", "stopped")
				for _, c := range []plugin.V5Coordinate{a, b} {
					op := command(t, c, "", plugin.V5Mutation{Kind: "purge_release"})
					if _, err := h.Platform.ProcessOnce(ctx, 32); err != nil {
						t.Fatal(err)
					}
					view, err := h.Platform.Operation(ctx, op.ID, true, h.Admin)
					if err != nil || view.Status != "done" || detail(t, c, "").Release.Storage != "purged" {
						t.Fatal("unreferenced release cleanup failed", err)
					}
				}
				if _, err := coordinator.ProcessOnce(ctx); err != nil {
					t.Fatal(err)
				}
				if workers() != 2 {
					t.Fatal("uninstall left target worker or removed sibling", workers())
				}
				blocked := h.ImportVariant("8.2.1", func(m *plugin.PluginV5Manifest) { m.Key = other.Key; m.Data.Upgrade.Rollback = "forbidden" })
				approve(t, blocked)
				command(t, other, "", plugin.V5Mutation{Kind: "disable"})
				drive(t, other, "", "stopped")
				command(t, blocked, "", plugin.V5Mutation{Kind: "upgrade"})
				drive(t, blocked, "", "stopped")
				if i := h.Installation(t, blocked, ""); i.Version != other.Version || i.DesiredVersion != blocked.Version || i.ActiveGeneration != 0 {
					t.Fatal("stopped global upgrade enabled implicitly")
				}
				command(t, blocked, "", plugin.V5Mutation{Kind: "enable"})
				drive(t, blocked, "", "ready")
				deny(t, other, "", "upgrade", plugin.ErrV5Rollback)
				var body string
				if err := pool.QueryRow(ctx, `SELECT content FROM threads WHERE id=505003`).Scan(&body); err != nil || !strings.Contains(body, "PRIVATE-BODY") {
					t.Fatal("plugin lifecycle changed host business data", err)
				}
			})
			if err := coordinator.Close(ctx); err != nil || workers() != 0 {
				t.Fatal("native lifecycle cleanup", err)
			}
		})
	}
}
