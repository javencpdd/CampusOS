package plugin

import (
	"context"
	"encoding/json"
	"github.com/campusos/CampusOS/internal/platform/reliability"
)

// ReconcileStartup invalidates any previously ready generation before a new
// host could expose it. Pending/failed generations already have durable work.
// Batches are bounded and use stable keys; no volatile Manager state is loaded.
func (s *V5Platform) ReconcileStartup(ctx context.Context) error {
	if err := s.reconcilePackages(ctx); err != nil {
		return err
	}
	for _, kind := range []v5RecordKind{v5Global, v5User} {
		after := ""
		for {
			rows, err := s.store.list(ctx, kind, v5Query{After: after, Limit: 50})
			if err != nil {
				return err
			}
			for _, raw := range rows {
				var candidate V5Installation
				if json.Unmarshal(raw, &candidate) != nil {
					return ErrV5PlatformUnavailable
				}
				after = v5RecordID(kind, raw)
				if !candidate.Installed || candidate.Desired != "enabled" || candidate.Observed != "ready" {
					continue
				}
				op := V5Operation{V5Coordinate: V5Coordinate{candidate.Publisher, candidate.Key, candidate.DesiredVersion}, ID: v5RandomID(), ActorKind: "system", ActorID: "plugin.v5.reconciler", Owner: candidate.Owner, IdempotencyKey: "restart-" + v5Counter(candidate.Generation), RequestHash: v5BytesDigest(raw), Kind: "enable", Status: "queued"}
				err = s.reliable.Execute(ctx, reliability.Command{Code: "plugin.v5.restart", ActorType: "system", ActorID: op.ActorID, ResourceType: "plugin_v5_operation", ResourceID: op.ID, OperationCode: "plugin.v5.restart", EventFactory: func() (reliability.Event, error) {
					return reliability.NewEvent("plugin.lifecycle.changed", "plugin_v5_operation", op.ID, map[string]string{"operation_id": op.ID, "generation": v5Counter(op.Generation)})
				}}, func(tx context.Context) error {
					if err := s.store.lock(tx, op.family()); err != nil {
						return err
					}
					i, err := s.installation(tx, op.V5Coordinate, op.Owner)
					if err != nil {
						return err
					}
					if i.Generation != candidate.Generation || i.Observed != "ready" {
						return errV5Replay
					}
					if err := s.queueInstall(tx, &i, &op); err != nil {
						return err
					}
					return s.saveOp(tx, &op)
				})
				if err != nil && err != errV5Replay {
					return err
				}
			}
			if len(rows) < 50 {
				break
			}
		}
	}
	return nil
}

func (s *V5Platform) reconcilePackages(ctx context.Context) error {
	after := ""
	for {
		rows, err := s.store.list(ctx, v5Release, v5Query{After: after, Limit: 50})
		if err != nil {
			return err
		}
		for _, raw := range rows {
			var r V5PackageRelease
			if json.Unmarshal(raw, &r) != nil {
				return ErrV5PlatformUnavailable
			}
			after = r.id()
			if r.Storage == "purged" {
				continue
			}
			storage := "available"
			archive, readErr := s.files.Package(r.Digest)
			if readErr == ErrV5NotFound {
				storage = "missing"
			} else if readErr != nil {
				storage = "quarantined"
			} else if err := s.catalog.WithVerified(ctx, archive, r.Release, func(*PluginV5Package) error { return nil }); err != nil {
				storage = "quarantined"
			}
			if storage == r.Storage {
				continue
			}
			err = s.reliable.Execute(ctx, reliability.Command{Code: "plugin.v5.catalog.reconcile", ActorType: "system", ActorID: "plugin.v5.reconciler", ResourceType: "plugin_v5_release", ResourceID: r.id(), OperationCode: "plugin.v5.catalog.reconcile", EventFactory: func() (reliability.Event, error) {
				return reliability.NewEvent("plugin.lifecycle.changed", "plugin_v5_release", r.id(), map[string]string{"publisher": r.Publisher, "plugin_key": r.Key, "version": r.Version, "storage": storage})
			}}, func(tx context.Context) error {
				if err := s.store.lock(tx, r.family()); err != nil {
					return err
				}
				current, err := s.release(tx, r.V5Coordinate)
				if err != nil {
					return err
				}
				if current.Revision != r.Revision {
					return errV5Replay
				}
				current.Storage = storage
				current.Revision, err = s.store.next(tx)
				if err != nil {
					return err
				}
				return s.store.save(tx, v5Release, r.id(), current)
			})
			if err != nil && err != errV5Replay {
				return err
			}
		}
		if len(rows) < 50 {
			return nil
		}
	}
}
