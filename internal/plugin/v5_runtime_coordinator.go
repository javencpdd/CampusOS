package plugin

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	identity "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/platform/security"
	campusossdk "github.com/campusos/CampusOS/sdk/go"
)

type V5RuntimeLeader interface {
	Check(context.Context) error
	Release(context.Context) error
}
type v5CoordinatedRuntime struct {
	op         V5Operation
	credential V5RuntimeCredential
	running    V5RunningRuntime
}

// IO never runs inside a database transaction. Only a compiled, exclusively
// fenced host coordinator can turn verified preparation + actual readiness
// into observed ready and a managed publication.
type V5RuntimeCoordinator struct {
	platform *V5Platform
	hub      *V5ManagedRuntime
	factory  V5RuntimeFactory
	leader   V5RuntimeLeader
	mu       sync.Mutex
	active   map[string]v5CoordinatedRuntime
	after    string
	closed   bool
}

func NewV5RuntimeCoordinator(p *V5Platform, h *V5ManagedRuntime, f V5RuntimeFactory, l V5RuntimeLeader) *V5RuntimeCoordinator {
	return &V5RuntimeCoordinator{platform: p, hub: h, factory: f, leader: l, active: map[string]v5CoordinatedRuntime{}}
}

func (c *V5RuntimeCoordinator) broker(ctx context.Context, token, code string, raw json.RawMessage) (json.RawMessage, error) {
	if err := c.leader.Check(ctx); err != nil {
		return nil, ErrV5RuntimeMissing
	}
	i, err := c.hub.credentialForCall(ctx, token, true)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(5 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	wire, _ := json.Marshal(campusossdk.V5DispatchCall{RequestID: v5RandomID(), Contract: "campusos.plugin-dispatch/v1", Target: campusossdk.V5DispatchTarget{Kind: "host_api", ID: code}, InstanceGeneration: i.generation, DeadlineMS: deadline.UnixMilli(), RequestBytes: int64(len(raw)), ResponseLimitBytes: 65536})
	return c.hub.HostCall(ctx, token, wire, raw)
}

func (c *V5RuntimeCoordinator) ProcessOnce(ctx context.Context) (int, error) {
	if c == nil || c.platform == nil || c.hub == nil || c.factory == nil || c.leader == nil {
		return 0, ErrV5RuntimeMissing
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return 0, ErrV5RuntimeMissing
	}
	if err := c.leader.Check(ctx); err != nil {
		c.closed = true
		for _, a := range c.active {
			c.hub.retire(a.credential.ID, true)
			a.running.Abort()
			_ = c.platform.runtimeLost(ctx, a.op)
		}
		return 0, ErrV5RuntimeMissing
	}
	for key, a := range c.active {
		if ctx.Err() != nil {
			return 0, ctx.Err()
		}
		_, err := c.hub.current(ctx, a.credential.ID)
		if err == nil {
			if health, ok := a.running.(interface{ Health(context.Context) error }); ok {
				err = health.Health(ctx)
			}
		}
		select {
		case <-a.running.Done():
			err = ErrV5RuntimeMissing
		default:
		}
		if err != nil {
			c.hub.mu.Lock()
			instance := c.hub.instances[a.credential.ID]
			draining := instance != nil && instance.draining && len(instance.calls) > 0
			c.hub.mu.Unlock()
			if draining {
				continue
			}
			c.hub.retire(a.credential.ID, true)
			a.running.Abort()
			if err := a.running.Close(ctx); err != nil {
				return 0, err
			}
			_ = c.platform.runtimeLost(ctx, a.op)
			delete(c.active, key)
			continue
		}
		if err := c.hub.renew(ctx, a.credential.ID); err != nil {
			c.hub.retire(a.credential.ID, true)
			a.running.Abort()
		}
	}
	rows, err := c.platform.store.list(ctx, v5Operation, v5Query{After: c.after, Limit: 50})
	if err != nil {
		return 0, err
	}
	count := 0
	for _, raw := range rows {
		if ctx.Err() != nil {
			return count, ctx.Err()
		}
		var op V5Operation
		if json.Unmarshal(raw, &op) != nil {
			return count, ErrV5PlatformUnavailable
		}
		c.after = op.ID
		if op.Status != "waiting" || op.Preparation == nil || op.LeaseUntil <= c.platform.now().UnixMilli() {
			continue
		}
		spec, err := c.platform.runtimeSpec(ctx, op)
		if err == ErrPluginV5ManifestShape {
			continue
		}
		if err != nil {
			continue
		}
		deadline := time.UnixMilli(op.LeaseUntil - 20)
		if d := time.Now().Add(5 * time.Second); d.Before(deadline) {
			deadline = d
		}
		startCtx, cancel := context.WithDeadline(ctx, deadline)
		running, err := c.factory.Prepare(startCtx, spec, c.broker)
		cancel()
		observation := V5Observation{OperationID: op.ID, Lease: op.Lease, Generation: op.Generation, ConfigDigest: op.Preparation.ConfigDigest, Ready: err == nil}
		if err != nil {
			if ctx.Err() != nil {
				return count, ctx.Err()
			}
			_ = c.platform.Observe(ctx, observation)
			count++
			continue
		}
		if err = c.leader.Check(ctx); err == nil {
			err = c.platform.Observe(ctx, observation)
		}
		var credential V5RuntimeCredential
		if err == nil {
			credential, err = c.hub.Publish(ctx, spec.Coordinate, spec.Owner, spec.Generation, 5*time.Minute, running)
		}
		if err != nil {
			running.Abort()
			closeCtx, stop := context.WithTimeout(context.Background(), 3*time.Second)
			_ = running.Close(closeCtx)
			stop()
			_ = c.platform.runtimeLost(ctx, op)
			continue
		}
		c.active[op.ID] = v5CoordinatedRuntime{op, credential, running}
		count++
	}
	if len(rows) < 50 {
		c.after = ""
	}
	return count, nil
}

func (s *V5Platform) runtimeSpec(ctx context.Context, op V5Operation) (V5RuntimeSpec, error) {
	var out V5RuntimeSpec
	i, err := s.installation(ctx, op.V5Coordinate, op.Owner)
	if err != nil {
		return out, err
	}
	c := V5Coordinate{i.Publisher, i.Key, i.DesiredVersion}
	r, err := s.release(ctx, c)
	if err != nil {
		return out, err
	}
	archive, err := s.files.Package(r.Digest)
	if err != nil {
		return out, err
	}
	manifest, err := ParsePluginV5Manifest(r.Manifest)
	if err != nil {
		return out, err
	}
	var program V5WasmProgram
	var container V5ContainerProgram
	var requested PluginV5ManifestResources
	switch manifest.Runtime {
	case "wasm":
		program, err = s.catalog.CaptureWasmProgram(ctx, archive, r.Release)
		requested = program.Resources()
	case "container":
		container, err = s.catalog.CaptureContainerProgram(ctx, archive, r.Release)
		requested = container.Resources()
	default:
		return out, ErrPluginV5ManifestShape
	}
	if err != nil {
		return out, err
	}
	err = s.reliable.CheckedRead(ctx, func(tx context.Context) error {
		// Match HTTP session verification and prepare/observe: account first,
		// then family. preparation rechecks this same row under the lock. Taking
		// the family first deadlocks against a current owner reading its state.
		if op.Owner != "" {
			users, ok := s.Users.(identity.UserCommandReader)
			if !ok || users == nil {
				return ErrV5PlatformUnavailable
			}
			u, err := users.GetUserForUpdate(tx, op.Owner)
			if err != nil || u.ID != op.Owner || u.Status != "active" {
				return ErrV5Denied
			}
		}
		if err := s.store.lock(tx, c.family()); err != nil {
			return err
		}
		var current V5Operation
		if err := s.store.load(tx, v5Operation, op.ID, &current); err != nil {
			return err
		}
		if current.Status != "waiting" || current.Lease != op.Lease || current.Generation != op.Generation || current.LeaseUntil <= s.now().UnixMilli() {
			return ErrV5Conflict
		}
		i, err := s.installation(tx, c, op.Owner)
		if err != nil {
			return err
		}
		if !i.Installed || i.Desired != "enabled" || i.Generation != op.Generation {
			return ErrV5Conflict
		}
		if err := s.verifyUpgradeCheckpoint(tx, i, current); err != nil {
			return err
		}
		var m *PluginV5Manifest
		if m, err = ParsePluginV5Manifest(r.Manifest); err != nil {
			return err
		}
		binding, capsule, err := s.preparation(tx, i, m)
		if err != nil {
			return err
		}
		binding.ConfigDigest = v5BytesDigest(capsule)
		if current.Preparation == nil || binding != *current.Preparation {
			return ErrV5Conflict
		}
		if err := s.files.VerifyPrepared(c.Publisher, c.Key, i.Owner, i.Generation, binding.ConfigDigest); err != nil {
			return err
		}
		g, err := s.grant(tx, c)
		if err != nil || g.Resources == nil {
			return ErrV5Denied
		}
		ceiling := security.ResourceSet{NetworkTargets: []string{}, StorageBytes: 1 << 20, CPUMillis: 1000, MemoryMB: 256, MaxConcurrency: 1, TimeoutMS: 5000}
		expiry := g.ExpiresAtMS
		if expiry == 0 {
			expiry = s.now().Add(24 * time.Hour).UnixMilli()
		}
		decision, err := security.DecideResourcePolicy(security.ResourceDecisionInput{Contract: security.PluginHostResourcesContract, Requested: security.ResourceSet(requested), HostCeiling: ceiling, AdminGrant: security.ResourceSet(*g.Resources), GrantStatus: "granted", GrantExpiresAtMS: expiry, NowMS: s.now().UnixMilli()}, ceiling)
		if err != nil {
			return err
		}
		configuration := map[string]json.RawMessage{}
		if m.Data.ConfigScope != "user" {
			cfg, err := s.configuration(tx, c, "", m)
			if err != nil {
				return err
			}
			configuration["system"] = cfg.Values
		}
		if i.Owner != "" && m.Data.ConfigScope != "system" {
			cfg, err := s.configuration(tx, c, i.Owner, m)
			if err != nil {
				return err
			}
			configuration["user"] = cfg.Values
		}
		wire, err := json.Marshal(configuration)
		if err != nil {
			return err
		}
		out = V5RuntimeSpec{Program: program, Container: container, Coordinate: c, Owner: i.Owner, Generation: i.Generation, ExpiresAtMS: expiry, Effective: PluginV5ManifestResources(decision.Effective), Configuration: wire}
		return nil
	})
	return out, err
}

func (s *V5Platform) runtimeLost(ctx context.Context, op V5Operation) error {
	return s.system(ctx, op, "runtime_lost", func(tx context.Context, current *V5Operation) error {
		i, err := s.installation(tx, op.V5Coordinate, op.Owner)
		if err != nil {
			return err
		}
		if i.Generation != op.Generation || i.Observed != "ready" {
			return nil
		}
		i.Observed = "failed"
		i.ActiveGeneration = 0
		i.Error = "plugin.runtime_lost"
		current.Status = "failed"
		current.Error = i.Error
		return s.saveInstall(tx, &i)
	})
}

func (c *V5RuntimeCoordinator) Close(ctx context.Context) error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	for _, a := range c.active {
		c.hub.retire(a.credential.ID, true)
		a.running.Abort()
	}
	for _, a := range c.active {
		if err := a.running.Close(ctx); err != nil {
			return err
		}
	}
	c.active = map[string]v5CoordinatedRuntime{}
	return c.leader.Release(ctx)
}
