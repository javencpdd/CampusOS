package space

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
)

func runOwnerStyle(ctx context.Context, s *Service, op string, pkg StylePackage, proof identityport.SessionPrincipalResolver) (*SpaceMutationAck, error) {
	switch op {
	case "apply":
		return s.ApplyOwnSpaceStylePackage(ctx, pkg, proof)
	case "default":
		return s.RestoreOwnSpaceDefaultStyle(ctx, proof)
	case "rollback":
		return s.RollbackOwnSpaceStyle(ctx, proof)
	default:
		return nil, errors.New("unknown test operation")
	}
}
func seedOwnerStyleSnapshot(t *testing.T, repo *MemoryRepository) {
	t.Helper()
	manifest := validStylePackage().Manifest
	if err := repo.SaveStyleSnapshot(context.Background(), &StyleSnapshot{ID: "3001", UserID: "1001", SnapshotType: "before_apply", Theme: manifest.Name, Layout: manifest.Layout, StyleName: manifest.Name, StyleVersion: manifest.Version, StyleManifest: cloneManifest(&manifest), CreatedAt: time.Now().UTC().Truncate(time.Microsecond)}); err != nil {
		t.Fatal(err)
	}
}
func TestSpaceStyleOwnerMetadataApplyDefaultRollbackAndRequiredAudit(t *testing.T) {
	ctx := context.Background()
	for _, existing := range []bool{false, true} {
		for _, op := range []string{"apply", "default", "rollback"} {
			for _, mode := range []string{"current", "audit", "outbox", "admin", "proof", "disabled", "missing_audit", "empty_snapshot", "invalid"} {
				t.Run(map[bool]string{false: "default", true: "existing"}[existing]+"/"+op+"/"+mode, func(t *testing.T) {
					s, repo, events := spaceOwnerFixture(t, existing, mode)
					if op == "rollback" && mode != "empty_snapshot" {
						seedOwnerStyleSnapshot(t, repo)
					}
					pkg := validStylePackage()
					proof := identityport.SessionPrincipalResolver(spaceOwnerProof)
					switch mode {
					case "admin":
						proof = func(ctx context.Context) (identityport.PrincipalContext, error) {
							p, e := spaceOwnerProof(ctx)
							p.Actor.Kind, p.Audience = "admin", "admin"
							return p, e
						}
					case "proof":
						proof = nil
					case "disabled":
						if !existing {
							return
						}
						at := time.Now()
						if err := repo.SetDisabled(ctx, "1001", &at, "", "disabled"); err != nil {
							t.Fatal(err)
						}
					case "missing_audit":
						s.reliable = nil
					case "invalid":
						if op != "apply" {
							return
						}
						pkg.Manifest.Components[0].Type = "script"
					case "empty_snapshot":
						if op != "rollback" {
							return
						}
					}
					before, beforeEvents := repo.Snapshot(), events.Snapshot()
					ack, err := runOwnerStyle(ctx, s, op, pkg, proof)
					if mode != "current" {
						if err == nil || ack != nil || !reflect.DeepEqual(before, repo.Snapshot()) || !reflect.DeepEqual(beforeEvents, events.Snapshot()) {
							t.Fatal("style denial/rollback escaped", ack, err)
						}
						return
					}
					if err != nil || ack == nil || ack.OwnerID != "1001" || ack.SpaceID == "" {
						t.Fatal(ack, err)
					}
					stored, err := repo.GetByUserID(ctx, "1001")
					if err != nil {
						t.Fatal(err)
					}
					if op == "default" {
						if stored.Theme != "default" || stored.Layout != "blog" || stored.StyleManifest != nil {
							t.Fatal(stored)
						}
					} else if stored.StyleName != pkg.Manifest.Name || stored.StyleManifest == nil {
						t.Fatal("intended style missing", stored)
					}
					rows, err := repo.LoadStyleSnapshotAuthority(ctx, "1001", false)
					if err != nil || len(rows) != 1 {
						t.Fatal("wrong history inventory", len(rows), err)
					}
					raw, _ := json.Marshal(ack)
					var fields map[string]json.RawMessage
					if json.Unmarshal(raw, &fields) != nil || len(fields) != 2 {
						t.Fatal("unsafe style ack", string(raw))
					}
				})
			}
		}
	}
}

func TestSpaceStyleOwnerExactHistoryAndUnexpectedWritesRollback(t *testing.T) {
	ctx := context.Background()
	for _, mode := range []string{"extra_profile", "extra_actor", "extra_snapshot", "changed_snapshot", "credential"} {
		t.Run(mode, func(t *testing.T) {
			s, repo, events := spaceOwnerFixture(t, true, "")
			seedOwnerStyleSnapshot(t, repo)
			before, beforeEvents := repo.Snapshot(), events.Snapshot()
			ran := false
			s.repo = &spaceOwnerRepoHook{MemoryRepository: repo, after: func(tx context.Context, space *Space) {
				ran = true
				switch mode {
				case "extra_profile":
					space.Bio = "extra"
					if err := repo.Upsert(tx, space); err != nil {
						t.Fatal(err)
					}
				case "extra_actor":
					space.DisabledActorKind, space.DisabledActorID = "admin", "opaque-admin"
					if err := repo.Upsert(tx, space); err != nil {
						t.Fatal(err)
					}
				case "extra_snapshot":
					if err := repo.SaveStyleSnapshot(tx, &StyleSnapshot{ID: "3009", UserID: "1001", Theme: "extra", Layout: "blog", CreatedAt: time.Now()}); err != nil {
						t.Fatal(err)
					}
				case "changed_snapshot":
					repo.mu.Lock()
					snapshot := repo.snapshots["1001"][0]
					snapshot.Theme = "extra"
					snapshot.FactsVersion, _ = repo.nextFactsVersion()
					repo.mu.Unlock()
				}
			}}
			proof := func(ctx context.Context) (identityport.PrincipalContext, error) {
				p, e := spaceOwnerProof(ctx)
				if ran && mode == "credential" {
					p.CredentialID = "changed"
				}
				return p, e
			}
			ack, err := s.ApplyOwnSpaceStylePackage(ctx, validStylePackage(), proof)
			if !ran || err == nil || ack != nil || !reflect.DeepEqual(before, repo.Snapshot()) || !reflect.DeepEqual(beforeEvents, events.Snapshot()) {
				t.Fatal("actual extra style write escaped", mode, ack, err, ran)
			}
		})
	}
}

func TestSpaceStyleOwnerDetachedInputAndLatestSnapshotTie(t *testing.T) {
	ctx := context.Background()
	s, repo, _ := spaceOwnerFixture(t, true, "")
	pkg := validStylePackage()
	calls := 0
	proof := func(ctx context.Context) (identityport.PrincipalContext, error) {
		p, e := spaceOwnerProof(ctx)
		calls++
		if calls == 2 {
			pkg.Manifest.Components[0].Props["show_avatar"] = false
		}
		return p, e
	}
	ack, err := s.ApplyOwnSpaceStylePackage(ctx, pkg, proof)
	if err != nil || ack == nil {
		t.Fatal(ack, err)
	}
	stored, err := repo.GetByUserID(ctx, "1001")
	if err != nil || stored.StyleManifest.Components[0].Props["show_avatar"] != true {
		t.Fatal("caller map alias changed intended command", stored, err)
	}
	at := time.Now().UTC().Truncate(time.Microsecond)
	for _, id := range []string{"3008", "3009"} {
		if err := repo.SaveStyleSnapshot(ctx, &StyleSnapshot{ID: id, UserID: "1001", Theme: id, Layout: "blog", CreatedAt: at}); err != nil {
			t.Fatal(err)
		}
	}
	ack, err = s.RollbackOwnSpaceStyle(ctx, spaceOwnerProof)
	if err != nil || ack == nil {
		t.Fatal(ack, err)
	}
	stored, err = repo.GetByUserID(ctx, "1001")
	if err != nil || stored.Theme != "3009" {
		t.Fatal("created-at tie ignored current nonce", stored, err)
	}
}
