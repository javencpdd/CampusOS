package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"time"

	identity "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/platform/reliability"
)

// Observation is a compiled host Port, never an HTTP/plugin callback. Runner
// and Dispatcher implementations arrive in03c/05/06; production has no fake
// observer that could publish a ready instance before those checks exist.
type V5Observation struct {
	OperationID       string
	Lease, Generation int64
	ConfigDigest      string
	Ready             bool
}

func (s *V5Platform) system(ctx context.Context, op V5Operation, action string, apply func(context.Context, *V5Operation) error) error {
	return s.reliable.Execute(ctx, reliability.Command{Code: "plugin.v5.worker." + action, ActorType: "system", ActorID: "plugin.v5.reconciler", ResourceType: "plugin_v5_operation", ResourceID: op.ID, OperationCode: "plugin.v5.worker." + action, EventFactory: func() (reliability.Event, error) {
		return reliability.NewEvent("plugin.lifecycle.changed", "plugin_v5_operation", op.ID, map[string]string{"operation_id": op.ID, "status": op.Status, "generation": v5Counter(op.Generation)})
	}}, func(tx context.Context) error {
		// This lock order matches current User session verification followed by
		// the plugin family lock in user commands. Stops never require a live user.
		if op.Owner != "" && (action == "prepare" || action == "observe") {
			users, ok := s.Users.(identity.UserCommandReader)
			if !ok || users == nil {
				return ErrV5PlatformUnavailable
			}
			if _, err := users.GetUserForUpdate(tx, op.Owner); err != nil {
				return ErrV5Denied
			}
		}
		if err := s.store.lock(tx, op.family()); err != nil {
			return err
		}
		var current V5Operation
		if err := s.store.load(tx, v5Operation, op.ID, &current); err != nil {
			return err
		}
		if err := apply(tx, &current); err != nil {
			return err
		}
		if err := s.saveOp(tx, &current); err != nil {
			return err
		}
		op = current
		return nil
	})
}
func (s *V5Platform) ProcessOnce(ctx context.Context, limit int) (int, error) {
	if s == nil || s.store == nil || s.reliable == nil {
		return 0, ErrV5PlatformUnavailable
	}
	if limit < 1 || limit > 50 {
		limit = 16
	}
	rows, err := s.store.list(ctx, v5Operation, v5Query{Limit: limit, Pending: true, Now: s.now().UnixMilli()})
	if err != nil {
		return 0, err
	}
	count := 0
	for _, raw := range rows {
		var op V5Operation
		if json.Unmarshal(raw, &op) != nil {
			return count, ErrV5PlatformUnavailable
		}
		var claimed V5Operation
		err := s.system(ctx, op, "claim", func(tx context.Context, current *V5Operation) error {
			if current.Status != "queued" && current.Status != "working" && current.Status != "waiting" || current.LeaseUntil > s.now().UnixMilli() {
				return errV5Replay
			}
			current.Lease, err = s.store.next(tx)
			if err != nil {
				return err
			}
			current.LeaseUntil = s.now().Add(30 * time.Second).UnixMilli()
			current.Status = "working"
			current.Attempts++
			current.Preparation = nil
			claimed = *current
			return nil
		})
		if err == errV5Replay {
			continue
		}
		if err != nil {
			return count, err
		}
		count++
		if err := s.processClaim(ctx, claimed); err != nil && err != errV5Replay {
			if e := s.failClaim(ctx, claimed, err); e != nil && e != errV5Replay {
				return count, e
			}
		}
	}
	return count, nil
}
func (s *V5Platform) claimed(ctx context.Context, current *V5Operation, expected V5Operation) (V5Installation, error) {
	if current.Status != "working" || current.Lease != expected.Lease {
		return V5Installation{}, errV5Replay
	}
	i, err := s.installation(ctx, current.V5Coordinate, current.Owner)
	if err != nil {
		return i, err
	}
	if i.Generation != current.Generation {
		current.Status = "superseded"
		current.Error = ""
		return i, nil
	}
	return i, nil
}
func (s *V5Platform) processClaim(ctx context.Context, op V5Operation) error {
	if op.Kind == "purge_release" {
		return s.system(ctx, op, "purge_release", func(tx context.Context, current *V5Operation) error {
			if current.Status != "working" || current.Lease != op.Lease {
				return errV5Replay
			}
			r, err := s.release(tx, op.V5Coordinate)
			if err != nil {
				return err
			}
			if r.Revision != op.StorageRevision {
				current.Status = "superseded"
				return nil
			}
			referenced, err := s.store.referenced(tx, op.V5Coordinate)
			if err != nil {
				return err
			}
			if referenced {
				return ErrV5Conflict
			}
			if err := s.files.RemovePackage(r.Digest); err != nil {
				return err
			}
			r.Storage = "purged"
			r.Revision, err = s.store.next(tx)
			if err != nil {
				return err
			}
			if err := s.store.save(tx, v5Release, r.id(), r); err != nil {
				return err
			}
			current.Status = "done"
			return nil
		})
	}
	i, err := s.installation(ctx, op.V5Coordinate, op.Owner)
	if err != nil {
		return err
	}
	if i.Generation != op.Generation {
		return s.system(ctx, op, "supersede", func(tx context.Context, current *V5Operation) error {
			if current.Lease != op.Lease {
				return errV5Replay
			}
			current.Status = "superseded"
			return nil
		})
	}
	if !i.Installed || i.Desired != "enabled" {
		return s.system(ctx, op, "stop", func(tx context.Context, current *V5Operation) error {
			i, err := s.claimed(tx, current, op)
			if err != nil {
				return err
			}
			if current.Status == "superseded" {
				return nil
			}
			if current.Purge {
				if err := s.files.PurgeOwner(i.Publisher, i.Key, i.Owner); err != nil {
					return err
				}
				more, err := s.clearConfigurations(tx, current)
				if err != nil {
					return err
				}
				if more {
					current.Status = "queued"
					current.LeaseUntil = 0
					return nil
				}
			}
			if i.Installed {
				i.Version = i.DesiredVersion
			}
			i.Observed = "stopped"
			i.ObservedGeneration = i.Generation
			i.ActiveGeneration = 0
			i.Error = ""
			current.Status = "done"
			current.Error = ""
			return s.saveInstall(tx, &i)
		})
	}
	c := V5Coordinate{i.Publisher, i.Key, i.DesiredVersion}
	release, err := s.release(ctx, c)
	if err != nil {
		return err
	}
	archive, err := s.files.Package(release.Digest)
	if err != nil {
		return err
	}
	return s.catalog.WithVerified(ctx, archive, release.Release, func(p *PluginV5Package) error {
		return s.system(ctx, op, "prepare", func(tx context.Context, current *V5Operation) error {
			i, err := s.claimed(tx, current, op)
			if err != nil {
				return err
			}
			if current.Status == "superseded" {
				return nil
			}
			if !i.Installed || i.Desired != "enabled" || i.DesiredVersion != c.Version {
				return ErrV5Conflict
			}
			binding, capsule, err := s.preparation(tx, i, p.Manifest)
			if err != nil {
				return err
			}
			binding.ConfigDigest, err = s.files.Prepare(c.Publisher, c.Key, i.Owner, i.Generation, capsule)
			if err != nil {
				return err
			}
			i.ConfigDigest = binding.ConfigDigest
			i.Observed = "pending"
			i.ObservedGeneration = i.Generation
			i.ActiveGeneration = 0
			i.Error = "plugin.runtime_unavailable"
			current.Preparation = &binding
			current.Status = "waiting"
			current.Error = i.Error
			current.LeaseUntil = s.now().Add(10 * time.Second).UnixMilli()
			return s.saveInstall(tx, &i)
		})
	})
}
func (s *V5Platform) preparation(ctx context.Context, i V5Installation, m *PluginV5Manifest) (V5Preparation, []byte, error) {
	c := V5Coordinate{i.Publisher, i.Key, i.DesiredVersion}
	binding := V5Preparation{}
	g, err := s.grant(ctx, c)
	if err != nil || !v5GrantCompleteAt(m, g, s.now().UnixMilli()) || !s.consentOK(ctx, c, i.Owner, m, g) {
		return binding, nil, ErrV5Denied
	}
	global, err := s.installation(ctx, c, "")
	if err != nil || !global.Installed {
		return binding, nil, ErrV5Denied
	}
	binding.GlobalGeneration = global.Generation
	binding.GrantRevision = g.Revision
	if i.Owner != "" {
		users, ok := s.Users.(identity.UserCommandReader)
		if !ok {
			return binding, nil, ErrV5PlatformUnavailable
		}
		u, err := users.GetUserForUpdate(ctx, i.Owner)
		if err != nil || u.Status != "active" || u.ID != i.Owner {
			return binding, nil, ErrV5Denied
		}
		if global.Version != c.Version || global.Desired != "enabled" || global.Observed != "ready" || global.Generation != global.ActiveGeneration {
			return binding, nil, ErrV5Denied
		}
	}
	r, err := s.release(ctx, c)
	if err != nil || r.Storage != "available" {
		return binding, nil, ErrV5PlatformUnavailable
	}
	binding.PackageDigest = r.Digest
	var system *V5Configuration
	if m.Data.ConfigScope != "user" {
		value, err := s.configuration(ctx, c, "", m)
		if err != nil {
			return binding, nil, err
		}
		if err := s.validateConfig(ctx, c, "", m, value); err != nil {
			return binding, nil, err
		}
		binding.SystemConfigRevision = value.Revision
		system = &value
	}
	var user *V5Configuration
	if i.Owner != "" && m.Data.ConfigScope != "system" {
		v, err := s.configuration(ctx, c, i.Owner, m)
		if err != nil {
			return binding, nil, err
		}
		if err := s.validateConfig(ctx, c, i.Owner, m, v); err != nil {
			return binding, nil, err
		}
		binding.UserConfigRevision = v.Revision
		user = &v
	}
	capsule, err := json.Marshal(struct {
		Coordinate V5Coordinate     `json:"release"`
		Owner      string           `json:"owner"`
		Generation string           `json:"generation"`
		System     *V5Configuration `json:"system_configuration,omitempty"`
		User       *V5Configuration `json:"user_configuration,omitempty"`
	}{c, i.Owner, v5Counter(i.Generation), system, user})
	return binding, capsule, err
}
func (s *V5Platform) Observe(ctx context.Context, r V5Observation) error {
	if r.Lease < 1 || r.Generation < 1 || !v5Digest(r.ConfigDigest) {
		return ErrV5Invalid
	}
	var op V5Operation
	if err := s.store.load(ctx, v5Operation, r.OperationID, &op); err != nil {
		return err
	}
	i, err := s.installation(ctx, op.V5Coordinate, op.Owner)
	if err != nil {
		return err
	}
	c := V5Coordinate{i.Publisher, i.Key, i.DesiredVersion}
	release, err := s.release(ctx, c)
	if err != nil {
		return err
	}
	archive, err := s.files.Package(release.Digest)
	if err != nil {
		return err
	}
	return s.catalog.WithVerified(ctx, archive, release.Release, func(p *PluginV5Package) error {
		return s.system(ctx, op, "observe", func(tx context.Context, current *V5Operation) error {
			if current.Status != "waiting" || current.Lease != r.Lease || current.LeaseUntil <= s.now().UnixMilli() || current.Generation != r.Generation || current.Preparation == nil || current.Preparation.ConfigDigest != r.ConfigDigest {
				return ErrV5Conflict
			}
			i, err := s.installation(tx, current.V5Coordinate, current.Owner)
			if err != nil {
				return err
			}
			if !i.Installed || i.Desired != "enabled" || i.Generation != r.Generation || i.DesiredVersion != c.Version {
				return ErrV5Conflict
			}
			binding, capsule, err := s.preparation(tx, i, p.Manifest)
			if err != nil {
				return err
			}
			binding.ConfigDigest = v5BytesDigest(capsule)
			if !reflect.DeepEqual(binding, *current.Preparation) {
				return ErrV5Conflict
			}
			if err := s.files.VerifyPrepared(c.Publisher, c.Key, i.Owner, i.Generation, binding.ConfigDigest); err != nil {
				return err
			}
			if r.Ready {
				i.Version = i.DesiredVersion
				i.Observed = "ready"
				i.ActiveGeneration = i.Generation
				i.Error = ""
				current.Status = "done"
				current.Error = ""
			} else {
				i.Observed = "failed"
				i.ActiveGeneration = 0
				i.Error = "plugin.runtime_prepare_failed"
				current.Status = "failed"
				current.Error = i.Error
			}
			i.ObservedGeneration = i.Generation
			return s.saveInstall(tx, &i)
		})
	})
}
func (s *V5Platform) failClaim(ctx context.Context, op V5Operation, cause error) error {
	return s.system(ctx, op, "failure", func(tx context.Context, current *V5Operation) error {
		if current.Status != "working" || current.Lease != op.Lease {
			return errV5Replay
		}
		current.Status = "failed"
		current.Error = "plugin.reconcile_failed"
		if errors.Is(cause, ErrV5Denied) {
			current.Error = "plugin.authorization_changed"
		}
		if errors.Is(cause, ErrPluginV5PackageSignature) {
			current.Error = "plugin.package_signature_invalid"
		}
		if errors.Is(cause, ErrV5NotFound) || errors.Is(cause, ErrPluginV5PackageDigest) {
			current.Error = "plugin.package_unavailable"
		}
		i, err := s.installation(tx, op.V5Coordinate, op.Owner)
		if err == ErrV5NotFound {
			return nil
		}
		if err != nil {
			return err
		}
		if i.Generation != op.Generation {
			current.Status = "superseded"
			return nil
		}
		i.Observed = "failed"
		i.ObservedGeneration = i.Generation
		i.ActiveGeneration = 0
		i.Error = current.Error
		return s.saveInstall(tx, &i)
	})
}
func (s *V5Platform) clearConfigurations(ctx context.Context, op *V5Operation) (bool, error) {
	rows, err := s.store.list(ctx, v5Config, v5Query{After: op.CleanupAfter, Limit: 51, Publisher: op.Publisher, Key: op.Key, Owner: op.Owner})
	if err != nil {
		return false, err
	}
	for n, raw := range rows {
		if n == 50 {
			return true, nil
		}
		var cfg V5Configuration
		if json.Unmarshal(raw, &cfg) != nil {
			return false, ErrV5PlatformUnavailable
		}
		op.CleanupAfter = v5RecordID(v5Config, raw)
		if cfg.Publisher != op.Publisher || cfg.Key != op.Key || cfg.Owner != op.Owner {
			continue
		}
		r, err := s.release(ctx, cfg.V5Coordinate)
		if err != nil {
			return false, err
		}
		m, err := ParsePluginV5Manifest(r.Manifest)
		if err != nil {
			return false, err
		}
		wire, _ := json.Marshal(m)
		def, err := parsePluginV5ConfigDefinition(wire)
		if err != nil {
			return false, err
		}
		cfg.Values = def.Defaults
		cfg.SecretRefs = map[string]string{}
		cfg.Revision, err = s.store.next(ctx)
		if err != nil {
			return false, err
		}
		if err := s.store.save(ctx, v5Config, v5VersionOwnerID(cfg.V5Coordinate, cfg.Owner), cfg); err != nil {
			return false, err
		}
	}
	return false, nil
}

// ExecutionFacts is an admission prerequisite for the future Dispatcher,
// never a resource ACL/Host Capability authorization or publication decision.
type V5ExecutionFacts struct {
	Installation     V5Installation `json:"installation"`
	Global           V5Installation `json:"global"`
	GrantRevision    string         `json:"grant_revision"`
	ReadyForDispatch bool           `json:"ready_for_dispatch"`
}

func (s *V5Platform) ExecutionFacts(ctx context.Context, c V5Coordinate, owner string) (V5ExecutionFacts, error) {
	out := V5ExecutionFacts{}
	if !c.valid() || !v5OwnerValid(owner) {
		return out, ErrV5Invalid
	}
	r, err := s.release(ctx, c)
	if err != nil {
		return out, err
	}
	archive, err := s.files.Package(r.Digest)
	if err != nil {
		return out, err
	}
	err = s.catalog.WithVerified(ctx, archive, r.Release, func(p *PluginV5Package) error {
		return s.reliable.CheckedRead(ctx, func(tx context.Context) error {
			if owner != "" {
				users, ok := s.Users.(identity.UserCommandReader)
				if !ok {
					return ErrV5PlatformUnavailable
				}
				u, err := users.GetUserForUpdate(tx, owner)
				if err != nil || u.Status != "active" {
					return ErrV5Denied
				}
			}
			if err := s.store.lock(tx, c.family()); err != nil {
				return err
			}
			i, err := s.installation(tx, c, owner)
			if err != nil {
				return err
			}
			g, err := s.grant(tx, c)
			if err != nil {
				return err
			}
			global, err := s.installation(tx, c, "")
			if err != nil {
				return err
			}
			out.Installation = i
			out.Global = global
			out.GrantRevision = v5Counter(g.Revision)
			ready := func(x V5Installation) bool {
				return x.Installed && x.Version == c.Version && x.DesiredVersion == c.Version && x.Desired == "enabled" && x.Observed == "ready" && x.Generation == x.ActiveGeneration && x.Generation == x.ObservedGeneration
			}
			out.ReadyForDispatch = ready(i) && ready(global) && v5GrantCompleteAt(p.Manifest, g, s.now().UnixMilli()) && s.consentOK(tx, c, owner, p.Manifest, g)
			if out.ReadyForDispatch {
				_, capsule, err := s.preparation(tx, i, p.Manifest)
				if err != nil {
					return err
				}
				if v5BytesDigest(capsule) != i.ConfigDigest {
					return ErrV5Conflict
				}
				if err := s.files.VerifyPrepared(c.Publisher, c.Key, owner, i.Generation, i.ConfigDigest); err != nil {
					return err
				}
			}
			return nil
		})
	})
	return out, err
}
