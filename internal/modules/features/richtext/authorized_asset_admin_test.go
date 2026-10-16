package richtext

import (
	"context"
	identityservice "github.com/campusos/CampusOS/internal/modules/core/identity/service"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"reflect"
	"testing"
	"time"
)

func assetAdminFixture(t *testing.T, actor, operation string) (articleGovernanceFixtureData, *ArticleAttachment) {
	t.Helper()
	g := articleGovernanceFixture(t, actor, "take_down")
	s, a, _, _, events := assetOwnerFixture(t, "trash")
	g.s, g.events = s, events
	s.SetAdminExecutionAuthority(identityservice.NewBoardGovernanceAuthorityService(nil, g.admins, g.current))
	ctx := context.Background()
	if err := g.admins.ReplaceAdminRolePermissionFacts(ctx, "security_admin", []string{"personal_space.asset.manage"}, "asset-test"); err != nil {
		t.Fatal(err)
	}
	if operation == "restore" {
		if err := s.store.TransitionUserAsset(ctx, a.AssetID, a.Asset.OwnerID, []string{AssetStatusActive}, AssetStatusQuarantined, AssetLifecycleAudit{ActorID: actor, ActorType: "admin", Action: "quarantined"}); err != nil {
			t.Fatal(err)
		}
	}
	s.SetReliability(reliability.NewService(transaction.NewMemory(s.store.(transaction.Snapshotter), events, g.admins, g.sessions), events))
	return g, a
}

func TestAssetAdminLifecycleCurrentAuthorityTypedActorsAndAtomicity(t *testing.T) {
	for _, actor := range []string{"1001", "adm.opaque.asset"} {
		for _, op := range []string{"quarantine", "restore"} {
			for _, mode := range []string{"current", "missing_proof", "user", "missing_reader", "missing_audit", "permission", "password", "expired", "disabled", "bad_state", "references", "object_missing", "invalid_reason", "audit", "outbox"} {
				t.Run(actor+"/"+op+"/"+mode, func(t *testing.T) {
					g, a := assetAdminFixture(t, actor, op)
					ctx := context.Background()
					base, proof := g.s.store, g.proof
					reason := "actual asset governance"
					switch mode {
					case "missing_proof":
						proof = nil
					case "user":
						proof = articleCreateProof
					case "missing_reader":
						g.s.SetAdminExecutionAuthority(nil)
					case "missing_audit":
						g.s.SetReliability(nil)
					case "permission":
						if err := g.admins.ReplaceAdminRolePermissionFacts(ctx, "security_admin", []string{"community.thread.trash"}, "test"); err != nil {
							t.Fatal(err)
						}
					case "password", "expired":
						row, err := g.sessions.GetSessionByID(ctx, g.claims.SessionID)
						if err != nil {
							t.Fatal(err)
						}
						strength, at := "mfa", time.Now().Add(-time.Hour)
						if mode == "password" {
							strength = "password"
						}
						if _, ok, err := g.sessions.UpgradeSessionStrengthCAS(ctx, row.ID, row.Version, strength, at, time.Now()); err != nil || !ok {
							t.Fatal(err, ok)
						}
					case "disabled":
						g.s.enabled = func() bool { return false }
					case "bad_state":
						row, _ := base.GetUserAsset(ctx, a.AssetID)
						if err := base.TransitionUserAsset(ctx, row.ID, row.OwnerID, []string{row.Status}, AssetStatusPurging, AssetLifecycleAudit{}); err != nil {
							t.Fatal(err)
						}
					case "references":
						if err := base.CreateAttachment(ctx, a); err != nil {
							t.Fatal(err)
						}
					case "object_missing":
						o, err := g.s.objects.Stat(ctx, a.Asset.OwnerID, a.Asset.StorageObjectID)
						if err != nil {
							t.Fatal(err)
						}
						if err := g.s.objects.(*attachmentUploadObjects).ObjectPort.Delete(ctx, o.OwnerID, o.ID, o.Version); err != nil {
							t.Fatal(err)
						}
					case "invalid_reason":
						reason = " "
					case "audit", "outbox":
						var audit reliability.Store = g.events
						if mode == "audit" {
							audit = articleCreateFailAudit{audit}
						} else {
							audit = articleGovernanceFailOutbox{audit}
						}
						g.s.SetReliability(reliability.NewService(transaction.NewMemory(base.(transaction.Snapshotter), g.events, g.admins, g.sessions), audit))
					}
					before, history := base.(transaction.Snapshotter).Snapshot(), g.events.Snapshot()
					ack, err := g.s.AdminAssetLifecycleForPrincipal(ctx, a.AssetID, op, reason, proof)
					want := mode == "current" || mode == "references" || op == "quarantine" && mode == "object_missing"
					if (err == nil) != want {
						t.Fatal(ack, err)
					}
					if !want {
						if ack != nil || !reflect.DeepEqual(before, base.(transaction.Snapshotter).Snapshot()) || !reflect.DeepEqual(history, g.events.Snapshot()) {
							t.Fatal("failed Admin asset command leaked", ack, err)
						}
						return
					}
					status := AssetStatusQuarantined
					if op == "restore" {
						status = AssetStatusActive
					}
					if ack == nil || ack.AssetID != a.AssetID || ack.Status != status {
						t.Fatal(ack)
					}
					mem := base.(*MemoryStore)
					mem.mu.RLock()
					last := mem.audits[len(mem.audits)-1]
					mem.mu.RUnlock()
					if last.ActorType != "admin" || last.ActorID != actor {
						t.Fatal("Admin written as User", last)
					}
				})
			}
		}
	}
}

func TestAssetAdminLifecycleRejectsActualExtraMutationAndFinalAuthority(t *testing.T) {
	for _, mode := range []string{"extra_status", "new_reference", "session", "permission_aba", "disabled"} {
		t.Run(mode, func(t *testing.T) {
			g, a := assetAdminFixture(t, "adm.opaque.asset", "quarantine")
			ctx := context.Background()
			base := g.s.store
			before, history, admins, sessions := base.(transaction.Snapshotter).Snapshot(), g.events.Snapshot(), g.admins.Snapshot(), g.sessions.Snapshot()
			ran := false
			g.s.store = &assetOwnerMutationStore{Store: base, after: func(tx context.Context, row *UserAsset) {
				ran = true
				switch mode {
				case "extra_status":
					if err := base.TransitionUserAsset(tx, row.ID, row.OwnerID, []string{row.Status}, AssetStatusPurging, AssetLifecycleAudit{}); err != nil {
						t.Fatal(err)
					}
				case "new_reference":
					if err := base.CreateAttachment(tx, a); err != nil {
						t.Fatal(err)
					}
				case "session":
					session, _ := g.sessions.GetSessionByID(tx, g.claims.SessionID)
					if _, ok, err := g.sessions.RevokeSessionCAS(tx, session.ID, session.Version, "test", time.Now()); err != nil || !ok {
						t.Fatal(ok, err)
					}
				case "permission_aba":
					if err := g.admins.ReplaceAdminRolePermissionFacts(tx, "security_admin", nil, "test"); err != nil {
						t.Fatal(err)
					}
					if err := g.admins.ReplaceAdminRolePermissionFacts(tx, "security_admin", []string{"personal_space.asset.manage"}, "test"); err != nil {
						t.Fatal(err)
					}
				case "disabled":
					g.s.enabled = func() bool { return false }
				}
			}}
			ack, err := g.s.AdminAssetLifecycleForPrincipal(ctx, a.AssetID, "quarantine", "actual final proof", g.proof)
			if err == nil || ack != nil || !ran {
				t.Fatal("actual mutation not rejected", ack, err, ran)
			}
			if !reflect.DeepEqual(before, base.(transaction.Snapshotter).Snapshot()) || !reflect.DeepEqual(history, g.events.Snapshot()) || !reflect.DeepEqual(admins, g.admins.Snapshot()) || !reflect.DeepEqual(sessions, g.sessions.Snapshot()) {
				t.Fatal("partial final proof rollback")
			}
		})
	}
}
