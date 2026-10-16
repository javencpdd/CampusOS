package richtext

import (
	"context"
	"errors"
	corestorage "github.com/campusos/CampusOS/internal/modules/core/userstorage"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"reflect"
	"strings"
	"testing"
	"time"
)

type assetPurgeObjects struct {
	corestorage.ObjectPort
	corestorage.OwnedObjectLifecycleCommands
	afterDelete func()
}

type assetPurgeFailPhaseAudit struct {
	reliability.Store
	phase string
}

func (s assetPurgeFailPhaseAudit) RecordCommandAudit(ctx context.Context, a reliability.CommandAudit) error {
	if strings.HasSuffix(a.CommandCode, "."+s.phase) {
		return errors.New("required phase audit rejected")
	}
	return s.Store.RecordCommandAudit(ctx, a)
}
func TestAssetPurgeRequiredPhaseAuditFailureIsAtomicOrPending(t *testing.T) {
	for _, phase := range []string{"purge_started", "delete_bytes", "purged"} {
		t.Run(phase, func(t *testing.T) {
			g, a, objects := assetPurgeFixture(t, "adm.opaque.asset")
			ctx := context.Background()
			base := g.s.store
			initial, _ := base.GetUserAsset(ctx, a.AssetID)
			g.s.SetReliability(reliability.NewService(transaction.NewMemory(base.(transaction.Snapshotter), g.events, g.admins, g.sessions), assetPurgeFailPhaseAudit{Store: g.events, phase: phase}))
			ack, err := g.s.AdminPurgeAssetForPrincipal(ctx, a.AssetID, "required phase audit", g.proof)
			if err == nil || ack != nil {
				t.Fatal("missing required phase audit succeeded", ack, err)
			}
			after, _ := base.GetUserAsset(ctx, a.AssetID)
			receipt, e := objects.GetOwnedObjectLifecycle(ctx, a.Asset.OwnerID, a.Asset.StorageObjectID)
			if e != nil {
				t.Fatal(e)
			}
			if phase == "purge_started" {
				if !reflect.DeepEqual(initial, after) || receipt.Object.Status != corestorage.ObjectStatusReady || errors.Is(err, ErrAssetPurgePending) {
					t.Fatal("prepare failure not atomic", after, receipt, err)
				}
			} else if after.Status != AssetStatusPurging || receipt.Object.Status != corestorage.ObjectStatusDeleted || !errors.Is(err, ErrAssetPurgePending) {
				t.Fatal("post-byte failure falsely completed or restored", after, receipt, err)
			}
			if phase != "purge_started" {
				g.s.SetReliability(reliability.NewService(transaction.NewMemory(base.(transaction.Snapshotter), g.events, g.admins, g.sessions), g.events))
				before := receipt.Object
				ack, err = g.s.AdminPurgeAssetForPrincipal(ctx, a.AssetID, "fresh audit retry", g.proof)
				terminal, e := objects.GetOwnedObjectLifecycle(ctx, a.Asset.OwnerID, a.Asset.StorageObjectID)
				if err != nil || ack == nil || e != nil || !reflect.DeepEqual(before, terminal.Object) {
					t.Fatal("pending audit retry repeated bytes", ack, err, terminal, e)
				}
			}
		})
	}
}

func (o *assetPurgeObjects) DeleteOwnedObjectForCommand(ctx context.Context, r corestorage.ObjectLifecycleReceipt) error {
	err := o.OwnedObjectLifecycleCommands.DeleteOwnedObjectForCommand(ctx, r)
	if err == nil && o.afterDelete != nil {
		o.afterDelete()
	}
	return err
}
func assetPurgeFixture(t *testing.T, actor string) (articleGovernanceFixtureData, *ArticleAttachment, *assetPurgeObjects) {
	t.Helper()
	g, a := assetAdminFixture(t, actor, "restore")
	base := g.s.objects.(*attachmentUploadObjects).ObjectPort
	objects := &assetPurgeObjects{ObjectPort: base, OwnedObjectLifecycleCommands: base.(corestorage.OwnedObjectLifecycleCommands)}
	g.s.SetObjectPort(objects)
	return g, a, objects
}
func TestAssetPurgeCurrentAuthorityTypedActorsAndPhysicalResult(t *testing.T) {
	for _, actor := range []string{"1001", "adm.opaque.asset"} {
		t.Run(actor, func(t *testing.T) {
			g, a, objects := assetPurgeFixture(t, actor)
			ctx := context.Background()
			before, err := objects.GetOwnedObjectLifecycle(ctx, a.Asset.OwnerID, a.Asset.StorageObjectID)
			if err != nil {
				t.Fatal(err)
			}
			ack, err := g.s.AdminPurgeAssetForPrincipal(ctx, a.AssetID, "current confirmed purge", g.proof)
			if err != nil || ack == nil || ack.AssetID != a.AssetID || ack.Status != AssetStatusDeleted {
				t.Fatal(ack, err)
			}
			row, err := g.s.store.GetUserAsset(ctx, a.AssetID)
			if err != nil || row.Status != AssetStatusDeleted || row.DeletedAt == nil {
				t.Fatal(row, err)
			}
			terminal, err := objects.GetOwnedObjectLifecycle(ctx, a.Asset.OwnerID, a.Asset.StorageObjectID)
			if err != nil || terminal.Object.Status != corestorage.ObjectStatusDeleted || terminal.Object.Version != before.Object.Version+2 {
				t.Fatal(terminal, err)
			}
			mem := g.s.store.(*MemoryStore)
			mem.mu.RLock()
			audits := append([]AssetLifecycleAudit(nil), mem.audits...)
			mem.mu.RUnlock()
			if len(audits) != 3 || audits[1].Action != "purge_started" || audits[2].Action != "purged" || audits[2].ActorType != "admin" || audits[2].ActorID != actor {
				t.Fatal(audits)
			}
		})
	}
}

func TestAssetPurgeMissingAuthorityIneligibleStateAndNoByteMutation(t *testing.T) {
	for _, mode := range []string{"missing_proof", "user", "missing_reader", "missing_audit", "missing_lifecycle", "permission", "password", "expired", "disabled", "references", "active", "deleted", "object_terminal", "invalid_reason"} {
		t.Run(mode, func(t *testing.T) {
			g, a, objects := assetPurgeFixture(t, "adm.opaque.asset")
			ctx := context.Background()
			proof := g.proof
			reason := "actual purge"
			switch mode {
			case "missing_proof":
				proof = nil
			case "user":
				proof = articleCreateProof
			case "missing_reader":
				g.s.SetAdminExecutionAuthority(nil)
			case "missing_audit":
				g.s.SetReliability(nil)
			case "missing_lifecycle":
				g.s.objects = struct{ corestorage.ObjectPort }{objects.ObjectPort}
			case "permission":
				if err := g.admins.ReplaceAdminRolePermissionFacts(ctx, "security_admin", nil, "test"); err != nil {
					t.Fatal(err)
				}
			case "password", "expired":
				row, _ := g.sessions.GetSessionByID(ctx, g.claims.SessionID)
				strength, at := "mfa", time.Now().Add(-time.Hour)
				if mode == "password" {
					strength = "password"
				}
				if _, ok, err := g.sessions.UpgradeSessionStrengthCAS(ctx, row.ID, row.Version, strength, at, time.Now()); err != nil || !ok {
					t.Fatal(err, ok)
				}
			case "disabled":
				g.s.enabled = func() bool { return false }
			case "references":
				if err := g.s.store.CreateAttachment(ctx, a); err != nil {
					t.Fatal(err)
				}
			case "active", "deleted":
				status, action := AssetStatusActive, "restored"
				if mode == "deleted" {
					status, action = AssetStatusDeleted, "purged"
				}
				if err := g.s.store.TransitionUserAsset(ctx, a.AssetID, a.Asset.OwnerID, []string{AssetStatusQuarantined}, status, AssetLifecycleAudit{ActorID: g.actor, ActorType: "admin", Action: action}); err != nil {
					t.Fatal(err)
				}
			case "object_terminal":
				r, err := objects.GetOwnedObjectLifecycle(ctx, a.Asset.OwnerID, a.Asset.StorageObjectID)
				if err != nil {
					t.Fatal(err)
				}
				if err := objects.DeleteOwnedObjectForCommand(ctx, r); err != nil {
					t.Fatal(err)
				}
			case "invalid_reason":
				reason = " "
			}
			before, history := g.s.store.(*MemoryStore).Snapshot(), g.events.Snapshot()
			objectBefore, e := objects.GetOwnedObjectLifecycle(ctx, a.Asset.OwnerID, a.Asset.StorageObjectID)
			if e != nil {
				t.Fatal(e)
			}
			ack, err := g.s.AdminPurgeAssetForPrincipal(ctx, a.AssetID, reason, proof)
			if err == nil || ack != nil || errors.Is(err, ErrAssetPurgePending) {
				t.Fatal("early denial mutated/claimed bytes", mode, ack, err)
			}
			after, e := objects.GetOwnedObjectLifecycle(ctx, a.Asset.OwnerID, a.Asset.StorageObjectID)
			if e != nil || !reflect.DeepEqual(objectBefore, after) || !reflect.DeepEqual(before, g.s.store.(*MemoryStore).Snapshot()) || !reflect.DeepEqual(history, g.events.Snapshot()) {
				t.Fatal("early purge denial had side effect", mode, err, e)
			}
		})
	}
}

func TestAssetPurgePostByteChangesRemainPendingAndCurrentRetry(t *testing.T) {
	for _, mode := range []string{"disabled", "session", "reference", "object_metadata"} {
		t.Run(mode, func(t *testing.T) {
			g, a, objects := assetPurgeFixture(t, "adm.opaque.asset")
			ctx := context.Background()
			base := g.s.store
			ran := false
			objects.afterDelete = func() {
				ran = true
				switch mode {
				case "disabled":
					g.s.enabled = func() bool { return false }
				case "session":
					row, _ := g.sessions.GetSessionByID(ctx, g.claims.SessionID)
					if _, ok, err := g.sessions.RevokeSessionCAS(ctx, row.ID, row.Version, "test", time.Now()); err != nil || !ok {
						t.Fatal(err, ok)
					}
				case "reference":
					if err := base.CreateAttachment(ctx, a); err != nil {
						t.Fatal(err)
					}
				case "object_metadata":
					// Alter only the returned public view; the Storage binding must reject it.
					r, err := objects.GetOwnedObjectLifecycle(ctx, a.Asset.OwnerID, a.Asset.StorageObjectID)
					if err != nil {
						t.Fatal(err)
					}
					r.Object.OriginalName = "forged terminal"
					if err := objects.OwnedObjectLifecycleCommands.DeleteOwnedObjectForCommand(ctx, r); !errors.Is(err, corestorage.ErrObjectVersion) {
						t.Fatal(err)
					}
				}
			}
			ack, err := g.s.AdminPurgeAssetForPrincipal(ctx, a.AssetID, "post-byte proof", g.proof)
			wantPending := mode != "object_metadata"
			if !ran || (err != nil) != wantPending || wantPending && (!errors.Is(err, ErrAssetPurgePending) || ack != nil) {
				t.Fatal(mode, ack, err, ran)
			}
			row, _ := base.GetUserAsset(ctx, a.AssetID)
			if wantPending && row.Status != AssetStatusPurging {
				t.Fatal("pending purge restored availability", row)
			}
			terminal, e := objects.GetOwnedObjectLifecycle(ctx, a.Asset.OwnerID, a.Asset.StorageObjectID)
			if e != nil || terminal.Object.Status != corestorage.ObjectStatusDeleted {
				t.Fatal(terminal, e)
			}
			if mode == "disabled" {
				g.s.enabled = func() bool { return true }
				objects.afterDelete = nil
				before := terminal.Object
				ack, err = g.s.AdminPurgeAssetForPrincipal(ctx, a.AssetID, "fresh current retry", g.proof)
				after, e := objects.GetOwnedObjectLifecycle(ctx, a.Asset.OwnerID, a.Asset.StorageObjectID)
				if err != nil || ack == nil || ack.Status != AssetStatusDeleted || e != nil || !reflect.DeepEqual(before, after.Object) {
					t.Fatal("pending metadata retry repeated byte mutation", ack, err, after, e)
				}
			}
		})
	}
}
