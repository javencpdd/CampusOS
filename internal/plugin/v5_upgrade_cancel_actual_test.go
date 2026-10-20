package plugin_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/campusos/CampusOS/internal/plugin"
	container "github.com/campusos/CampusOS/internal/plugin/container"
	campusossdk "github.com/campusos/CampusOS/sdk/go"
	"github.com/jackc/pgx/v5/pgxpool"
)

// The candidate is a real configured container. Pause only the coordinator's
// return from Prepare so a normal, concurrent command can cancel its generation
// before Observe; this barrier cannot manufacture readiness or authorization.
type v5UpgradeCandidateBarrier struct {
	plugin.V5RuntimeFactory
	version string
	ready   chan plugin.V5RuntimeSpec
	release <-chan struct{}
	handoff chan error
	started atomic.Bool
}

func (f *v5UpgradeCandidateBarrier) Prepare(ctx context.Context, spec plugin.V5RuntimeSpec, broker plugin.V5RuntimeBroker) (plugin.V5RunningRuntime, error) {
	running, err := f.V5RuntimeFactory.Prepare(ctx, spec, broker)
	if err != nil || spec.Coordinate.Version != f.version {
		return running, err
	}
	f.started.Store(true)
	f.ready <- spec
	select {
	case <-f.release:
		f.handoff <- nil
		return running, nil
	case <-ctx.Done():
		running.Abort()
		cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = running.Close(cleanup)
		f.handoff <- ctx.Err()
		return nil, ctx.Err()
	}
}

func TestV5UpgradeCancelActualPostgres(t *testing.T) {
	dsn := os.Getenv("CAMPUSOS_V5_UPGRADE_DATABASE_URL")
	image := os.Getenv("CAMPUSOS_V5_UPGRADE_REFERENCE")
	if dsn == "" || image == "" {
		t.Skip("requires owned07 PG and executable Container image")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil || cfg.ConnConfig.Database != "campusos_v12_07_upgrade" || !strings.HasPrefix(image, "localhost/campusos-owned-v12-07-upgrade@sha256:") {
		t.Fatal("requires exact owned07 database/image")
	}
	for _, transport := range []string{"http", "grpc"} {
		t.Run(transport, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			pool, err := pgxpool.NewWithConfig(ctx, cfg)
			if err != nil {
				t.Fatal(err)
			}
			defer pool.Close()
			h := plugin.NewV5ContainerTestHarness(t, ctx, pool, image, transport)
			original := h.Coordinate
			factory, err := container.NewV5Factory(strings.Repeat("d", 64))
			if err != nil {
				t.Fatal(err)
			}
			defer factory.Close(ctx)
			leader, err := plugin.NewPgV5StateStore(pool).AcquireRuntimeLeader(ctx)
			if err != nil {
				t.Fatal(err)
			}
			release := make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			barrier := &v5UpgradeCandidateBarrier{V5RuntimeFactory: factory, version: "8.2.0", ready: make(chan plugin.V5RuntimeSpec, 1), release: release, handoff: make(chan error, 1)}
			coordinator := plugin.NewV5RuntimeCoordinator(h.Platform, h.Hub, barrier, leader)
			defer coordinator.Close(ctx)
			defer unblock()
			mutate := func(c plugin.V5Coordinate, r plugin.V5Mutation) (plugin.V5Operation, error) {
				r.Version = c.Version
				return h.Platform.Mutate(ctx, c.Publisher, c.Key, r, true, h.Admin)
			}
			mustMutate := func(tt *testing.T, c plugin.V5Coordinate, r plugin.V5Mutation) plugin.V5Operation {
				tt.Helper()
				op, err := mutate(c, r)
				if err != nil {
					tt.Fatal(r.Kind, err)
				}
				return op
			}
			detail := func(tt *testing.T, c plugin.V5Coordinate) plugin.V5InventoryEntry {
				tt.Helper()
				entry, err := h.Platform.Detail(ctx, c, true, h.Admin)
				if err != nil || entry.Configuration == nil || entry.Grant == nil {
					tt.Fatal("current Admin detail", err)
				}
				return entry
			}
			drive := func(tt *testing.T, owner, observed string) {
				tt.Helper()
				until := time.Now().Add(15 * time.Second)
				for {
					if _, err := h.Platform.ProcessOnce(ctx, 16); err != nil {
						tt.Fatal(err)
					}
					if _, err := coordinator.ProcessOnce(ctx); err != nil {
						tt.Fatal(err)
					}
					if h.Current(owner).Observed == observed {
						return
					}
					if time.Now().After(until) {
						tt.Fatal("owned07 reconciliation timeout", h.Current(owner))
					}
					time.Sleep(30 * time.Millisecond)
				}
			}
			q := plugin.V5ContributionQuery{Point: plugin.V5AIChat, ContractVersion: "v1", Capability: "text"}
			invoke := func(binding plugin.V5ManagedBinding) (json.RawMessage, error) {
				generation := binding.Descriptor().InstanceGeneration
				payload := []byte(fmt.Sprintf(`{"request_id":"upgrade07","contribution_id":"chat","instance_generation":%d,"deadline_ms":4000,"prompt":"configuration","context_class":"none"}`, generation))
				wire, _ := json.Marshal(campusossdk.V5DispatchCall{RequestID: "upgrade07", Contract: "campusos.plugin-dispatch/v1", Target: campusossdk.V5DispatchTarget{Kind: "contribution", ID: "chat"}, InstanceGeneration: generation, DeadlineMS: time.Now().Add(4 * time.Second).UnixMilli(), RequestBytes: int64(len(payload)), ResponseLimitBytes: 4096})
				return h.Hub.Invoke(ctx, h.Consumer, binding, wire, payload, h.User)
			}
			mustMutate(t, original, plugin.V5Mutation{Kind: "enable", ExpectedGeneration: h.Current("").Generation, IdempotencyKey: "07-original-enable"})
			drive(t, "", "ready")
			oldBinding, err := h.Hub.Select(ctx, "", q, nil)
			if err != nil {
				t.Fatal(err)
			}
			oldConfig := *detail(t, original).Configuration
			candidate := h.ImportVersion(barrier.version)
			candidateEntry, err := h.Platform.Detail(ctx, candidate, true, h.Admin)
			if err != nil {
				t.Fatal(err)
			}
			if candidateEntry.Grant != nil {
				t.Fatal("candidate inherited original grant")
			}
			if _, err := mutate(candidate, plugin.V5Mutation{Kind: "upgrade", ExpectedGeneration: h.Current("").Generation, IdempotencyKey: "07-unapproved-upgrade"}); err != plugin.ErrV5Denied {
				t.Fatal("unreviewed version upgrade admitted", err)
			}
			originalGrant := detail(t, original).Grant
			mustMutate(t, candidate, plugin.V5Mutation{Kind: "grant", Approved: true, Consumes: originalGrant.Consumes, Resources: originalGrant.Resources, IdempotencyKey: "07-candidate-review"})
			mustMutate(t, candidate, plugin.V5Mutation{Kind: "configure", Values: json.RawMessage(`{"message":"ordinary-candidate"}`), IdempotencyKey: "07-candidate-config"})
			candidateConfig := *detail(t, candidate).Configuration
			upgrade := mustMutate(t, candidate, plugin.V5Mutation{Kind: "upgrade", ExpectedGeneration: h.Current("").Generation, IdempotencyKey: "07-upgrade"})
			if _, err := h.Platform.ProcessOnce(ctx, 16); err != nil {
				t.Fatal(err)
			}
			prepared := h.Operation(upgrade.ID)
			if prepared.Status != "waiting" || prepared.Preparation == nil {
				t.Fatal("candidate not waiting for native readiness")
			}
			result := make(chan error, 1)
			go func() {
				for {
					_, err := coordinator.ProcessOnce(ctx)
					if err != nil || barrier.started.Load() {
						result <- err
						return
					}
					time.Sleep(10 * time.Millisecond)
				}
			}()
			select {
			case spec := <-barrier.ready:
				if spec.Coordinate != candidate || !strings.Contains(string(spec.Configuration), "ordinary-candidate") {
					t.Fatal("wrong actual candidate configuration")
				}
			case err := <-result:
				t.Fatal("actual candidate never reached readiness barrier", err)
			case <-time.After(8 * time.Second):
				t.Fatal("actual candidate preparation timed out")
			}
			t.Run("disable-cancels-real-ready-candidate-before-observe", func(t *testing.T) {
				disabled := mustMutate(t, original, plugin.V5Mutation{Kind: "disable", ExpectedGeneration: upgrade.Generation, IdempotencyKey: "07-disable-pending"})
				unblock()
				select {
				case err := <-barrier.handoff:
					if err != nil {
						t.Fatal("candidate preparation expired before cancellation handoff", err)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("candidate cancellation handoff timed out")
				}
				select {
				case err := <-result:
					if err != nil {
						t.Fatal(err)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("cancelled candidate reconciliation timed out")
				}
				drive(t, "", "stopped")
				i := h.Current("")
				if i.Version != original.Version || i.DesiredVersion != original.Version || i.ActiveGeneration != 0 || len(factory.Workers()) != 0 {
					t.Fatal("unready candidate promoted or native worker leaked", i)
				}
				if _, err := h.Hub.Select(ctx, "", q, nil); err == nil {
					t.Fatal("disabled contribution remained published")
				}
				if out, err := invoke(oldBinding); err == nil || len(out) != 0 {
					t.Fatal("old generation released output")
				}
				if err := h.Platform.Observe(ctx, plugin.V5Observation{OperationID: prepared.ID, Lease: prepared.Lease, Generation: prepared.Generation, ConfigDigest: prepared.Preparation.ConfigDigest, Ready: true}); err != plugin.ErrV5Conflict {
					t.Fatal("late cancelled observation accepted", err)
				}
				replay := mustMutate(t, original, plugin.V5Mutation{Kind: "disable", ExpectedGeneration: upgrade.Generation, IdempotencyKey: "07-disable-pending"})
				if replay.ID != disabled.ID || h.Current("").Generation != disabled.Generation {
					t.Fatal("replayed disable changed generation")
				}
			})
			t.Run("version-configurations-retained-and-current-grant-required", func(t *testing.T) {
				for c, expected := range map[plugin.V5Coordinate]plugin.V5Configuration{original: oldConfig, candidate: candidateConfig} {
					current := detail(t, c).Configuration
					if current.Revision != expected.Revision || string(current.Values) != string(expected.Values) {
						t.Fatal("version configuration changed during cancellation", c.Version)
					}
				}
				grant := detail(t, original).Grant
				mustMutate(t, original, plugin.V5Mutation{Kind: "revoke", ExpectedRevision: grant.Revision, IdempotencyKey: "07-revoke-original"})
				drive(t, "", "stopped")
				if _, err := mutate(original, plugin.V5Mutation{Kind: "enable", ExpectedGeneration: h.Current("").Generation, IdempotencyKey: "07-revoked-enable"}); err != plugin.ErrV5Denied {
					t.Fatal("cancellation restored revoked authorization", err)
				}
				if !detail(t, candidate).Grant.Approved || len(factory.Workers()) != 0 {
					t.Fatal("unrelated candidate grant substituted for current grant")
				}
				currentGrant := detail(t, original).Grant
				mustMutate(t, original, plugin.V5Mutation{Kind: "grant", Approved: true, Consumes: grant.Consumes, Resources: grant.Resources, ExpectedRevision: currentGrant.Revision, IdempotencyKey: "07-explicit-original-review"})
				drive(t, "", "stopped")
				mustMutate(t, original, plugin.V5Mutation{Kind: "enable", ExpectedGeneration: h.Current("").Generation, IdempotencyKey: "07-current-enable"})
				drive(t, "", "ready")
				binding, err := h.Hub.Select(ctx, "", q, nil)
				if err != nil || binding.Descriptor().Version != original.Version || binding.Descriptor().InstanceGeneration <= upgrade.Generation || len(factory.Workers()) != 1 {
					t.Fatal("original runtime did not recover once", err)
				}
				if out, err := invoke(binding); err != plugin.ErrV5Denied || len(out) != 0 {
					t.Fatal("ordinary user invoked a global binding", err)
				}
				// A consumer needs its own installation. This post-cancellation
				// probe reads the retained system configuration through the normal
				// user dispatcher; it does not claim user-upgrade cancellation coverage.
				userMutation := func(r plugin.V5Mutation) plugin.V5Operation {
					t.Helper()
					r.Version = original.Version
					r.IdempotencyKey = original.Key + "-" + r.IdempotencyKey
					op, err := h.Platform.Mutate(ctx, original.Publisher, original.Key, r, false, h.User)
					if err != nil {
						t.Fatal(r.Kind, err)
					}
					return op
				}
				installed := userMutation(plugin.V5Mutation{Kind: "install", IdempotencyKey: "07-consumer-install"})
				userMutation(plugin.V5Mutation{Kind: "enable", ExpectedGeneration: installed.Generation, IdempotencyKey: "07-consumer-enable"})
				drive(t, "2801001", "ready")
				binding, err = h.Hub.Select(ctx, "2801001", q, nil)
				if err != nil || binding.Descriptor().Version != original.Version || len(factory.Workers()) != 2 {
					t.Fatal("current user consumer did not become ready", err)
				}
				out, err := invoke(binding)
				if err != nil || !strings.Contains(string(out), "ordinary-v1") || strings.Contains(string(out), "ordinary-candidate") {
					t.Fatal("recovered runtime used wrong version configuration", err)
				}
				var response campusossdk.V5ChatResponse
				if err := json.Unmarshal(out, &response); err != nil {
					t.Fatal(err)
				}
				var configuration map[string]json.RawMessage
				if err := json.Unmarshal([]byte(response.Text), &configuration); err != nil {
					t.Fatal(err)
				}
				var actualSystem, expectedSystem any
				if err := json.Unmarshal(configuration["system"], &actualSystem); err != nil {
					t.Fatal("system configuration missing from actual guest response", err)
				}
				if err := json.Unmarshal(oldConfig.Values, &expectedSystem); err != nil || !reflect.DeepEqual(actualSystem, expectedSystem) {
					t.Fatal("actual guest system configuration differs from retained original", err)
				}
			})
			if err := coordinator.Close(ctx); err != nil || len(factory.Workers()) != 0 {
				t.Fatal("owned07 native cleanup", err)
			}
		})
	}
}
