package richtext

import (
	"context"
	"errors"
	"fmt"
	corestorage "github.com/campusos/CampusOS/internal/modules/core/userstorage"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/jackc/pgx/v5/pgconn"
	"strings"
	"testing"
	"time"
)

type assetPurgePGObjects struct {
	*assetPurgeObjects
	afterGet func(context.Context, corestorage.ObjectLifecycleReceipt)
}

func (o *assetPurgePGObjects) GetOwnedObjectLifecycle(ctx context.Context, owner, id string) (corestorage.ObjectLifecycleReceipt, error) {
	r, err := o.assetPurgeObjects.GetOwnedObjectLifecycle(ctx, owner, id)
	if err == nil && o.afterGet != nil {
		o.afterGet(ctx, r)
	}
	return r, err
}
func assetPurgePostgres(t *testing.T, seed int, opaque bool) (assetAdminPG, *assetPurgePGObjects) {
	t.Helper()
	g := assetAdminPostgres(t, seed, opaque, "restore")
	base := g.s.objects.(*attachmentUploadObjects).ObjectPort
	objects := &assetPurgePGObjects{assetPurgeObjects: &assetPurgeObjects{ObjectPort: base, OwnedObjectLifecycleCommands: base.(corestorage.OwnedObjectLifecycleCommands)}}
	g.s.SetObjectPort(objects)
	return g, objects
}
func TestAssetPurgePostgreSQLRequiredPhasesAndTypedPhysicalResult(t *testing.T) {
	ctx := context.Background()
	for k, opaque := range []bool{false, true} {
		for i, mode := range []string{"none", "prepare_asset", "prepare_audit", "prepare_outbox", "guard_audit", "guard_outbox", "final_asset", "final_audit", "final_outbox"} {
			t.Run(fmt.Sprint(opaque, "/", mode), func(t *testing.T) {
				g, objects := assetPurgePostgres(t, 170201+k*200+i*10, opaque)
				before := assetOwnerPGSnapshot(t, g.pool, g.a.AssetID)
				original, err := objects.GetOwnedObjectLifecycle(ctx, g.a.Asset.OwnerID, g.a.Asset.StorageObjectID)
				if err != nil {
					t.Fatal(err)
				}
				if mode != "none" {
					table, predicate := "", ""
					switch {
					case strings.HasSuffix(mode, "_asset"):
						table = "asset_lifecycle_audits"
						action := "purge_started"
						if strings.HasPrefix(mode, "final") {
							action = "purged"
						}
						predicate = fmt.Sprintf("NEW.asset_id=%s AND NEW.action='%s'", g.a.AssetID, action)
					case strings.HasSuffix(mode, "_audit"):
						table = "platform_command_audits"
						action := "purge_started"
						if strings.HasPrefix(mode, "guard") {
							action = "delete_bytes"
						}
						if strings.HasPrefix(mode, "final") {
							action = "purged"
						}
						predicate = fmt.Sprintf("NEW.resource_id='%s' AND NEW.command_code='feature.richtext.asset.purge.%s'", g.a.AssetID, action)
					case strings.HasSuffix(mode, "_outbox"):
						table = "platform_outbox"
						count := 0
						if strings.HasPrefix(mode, "guard") {
							count = 1
						}
						if strings.HasPrefix(mode, "final") {
							count = 2
						}
						predicate = fmt.Sprintf("NEW.aggregate_type='user_asset' AND NEW.aggregate_id='%s' AND (SELECT count(*) FROM platform_outbox WHERE aggregate_type='user_asset' AND aggregate_id=NEW.aggregate_id)=%d", g.a.AssetID, count)
					}
					if _, err := g.pool.Exec(ctx, "CREATE FUNCTION asset_purge_reject() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF "+predicate+" THEN RAISE EXCEPTION 'required purge phase record rejected'; END IF; RETURN NEW; END $$; CREATE TRIGGER asset_purge_reject BEFORE INSERT ON "+table+" FOR EACH ROW EXECUTE FUNCTION asset_purge_reject()"); err != nil {
						t.Fatal(err)
					}
					defer func() {
						if _, err := g.pool.Exec(context.Background(), "DROP TRIGGER asset_purge_reject ON "+table+"; DROP FUNCTION asset_purge_reject()"); err != nil {
							t.Error(err)
						}
					}()
				}
				ack, err := g.s.AdminPurgeAssetForPrincipal(ctx, g.a.AssetID, "actual physical purge", g.proof)
				if (err == nil) != (mode == "none") {
					t.Fatal(mode, ack, err)
				}
				row, readErr := g.s.store.GetUserAsset(ctx, g.a.AssetID)
				if readErr != nil {
					t.Fatal(readErr)
				}
				receipt, readErr := objects.GetOwnedObjectLifecycle(ctx, g.a.Asset.OwnerID, g.a.Asset.StorageObjectID)
				if readErr != nil {
					t.Fatal(readErr)
				}
				if mode != "none" {
					var pe *pgconn.PgError
					if !errors.As(err, &pe) || pe.Code != "P0001" || ack != nil {
						t.Fatal("required actual SQL phase failure not reached", mode, ack, err)
					}
					if strings.HasPrefix(mode, "prepare") {
						if row.Status != AssetStatusQuarantined || receipt.Object.Status != corestorage.ObjectStatusReady || assetOwnerPGSnapshot(t, g.pool, g.a.AssetID) != before || errors.Is(err, ErrAssetPurgePending) {
							t.Fatal("prepare failure partial", row, receipt, err)
						}
					} else if row.Status != AssetStatusPurging || receipt.Object.Status != corestorage.ObjectStatusDeleted || !errors.Is(err, ErrAssetPurgePending) {
						t.Fatal("post-byte failure falsely restored/completed", row, receipt, err)
					}
				} else {
					if ack == nil || row.Status != AssetStatusDeleted || row.DeletedAt == nil || receipt.Object.Status != corestorage.ObjectStatusDeleted || receipt.Object.Version != original.Object.Version+2 {
						t.Fatal(ack, row, receipt)
					}
					var kind, actor, legacy string
					if err := g.pool.QueryRow(ctx, "SELECT actor_kind,actor_ref_id,COALESCE(actor_user_id::text,'') FROM asset_lifecycle_audits WHERE asset_id=$1 AND action='purged' ORDER BY id DESC LIMIT 1", g.a.AssetID).Scan(&kind, &actor, &legacy); err != nil || kind != "admin" || actor != g.actor || legacy != "" {
						t.Fatal("Admin physical purge wrote User history", kind, actor, legacy, err)
					}
				}
				want := g.a.Asset.SizeBytes
				if !strings.HasPrefix(mode, "prepare") || mode == "none" {
					want = 0
				}
				if usage, e := g.adapter.Usage(g.a.Asset.OwnerID); e != nil || usage != want {
					t.Fatal("physical state does not match phase", mode, usage, want, e)
				}
			})
		}
	}
}

func TestAssetPurgePostgreSQLByteGuardLocksEmptyReferencesAndCurrentRetry(t *testing.T) {
	ctx := context.Background()
	g, objects := assetPurgePostgres(t, 170801, true)
	metadataLocked, guardRan := false, false
	objects.afterGet = func(tx context.Context, r corestorage.ObjectLifecycleReceipt) {
		if metadataLocked || !transaction.Active(tx) {
			return
		}
		metadataLocked = true
		assertAttachmentUploadPGLock(t, g.pool, "SELECT id FROM storage_objects WHERE id=$1 FOR UPDATE NOWAIT", r.Object.ID)
	}
	objects.afterDelete = func() {
		guardRan = true
		for _, q := range []struct{ sql, id string }{
			{"SELECT id FROM admin_accounts WHERE id=$1 FOR UPDATE NOWAIT", g.actor},
			{"SELECT id FROM admin_sessions WHERE id=$1 FOR UPDATE NOWAIT", g.claims.SessionID},
			{"SELECT id FROM admin_roles WHERE id=$1 FOR UPDATE NOWAIT", g.role},
			{"SELECT id FROM admin_role_bindings WHERE role_id=$1 FOR UPDATE NOWAIT", g.role},
			{"SELECT role_id FROM admin_role_permissions WHERE role_id=$1 FOR UPDATE NOWAIT", g.role},
			{"SELECT id FROM user_assets WHERE id=$1 FOR UPDATE NOWAIT", g.a.AssetID},
		} {
			assertAttachmentUploadPGLock(t, g.pool, q.sql, q.id)
		}
		// Object transactions have committed separately; its row must not be
		// held by the Asset guard or those transactions would deadlock.
		if _, err := g.pool.Exec(ctx, "SELECT id FROM storage_objects WHERE id=$1 FOR UPDATE NOWAIT", g.a.Asset.StorageObjectID); err != nil {
			t.Fatal("byte guard retained Object transaction lock", err)
		}
		for _, q := range []struct {
			sql  string
			args []any
		}{
			{"INSERT INTO richtext_article_attachments(id,article_content_id,asset_id,display_name,display_order,created_at,updated_at) VALUES($1,$2,$3,'blocked.pdf',0,NOW(),NOW())", []any{"171201", g.a.ArticleContentID, g.a.AssetID}},
			{"INSERT INTO richtext_article_assets(id,uploader_id,file_url,file_name,file_size,mime_type,created_at,asset_id) VALUES($1,$2,'/blocked','blocked.png',1,'image/png',NOW(),$3)", []any{"171202", g.a.Asset.OwnerID, g.a.AssetID}},
		} {
			block, cancel := context.WithTimeout(ctx, 80*time.Millisecond)
			_, err := g.pool.Exec(block, q.sql, q.args...)
			blocked := errors.Is(block.Err(), context.DeadlineExceeded)
			cancel()
			if err == nil || !blocked {
				t.Fatal("empty references admitted during bytes", err, blocked)
			}
		}
		g.s.enabled = func() bool { return false }
	}
	ack, err := g.s.AdminPurgeAssetForPrincipal(ctx, g.a.AssetID, "byte guard current facts", g.proof)
	if !metadataLocked || !guardRan || ack != nil || !errors.Is(err, ErrAssetPurgePending) {
		t.Fatal(metadataLocked, guardRan, ack, err)
	}
	row, _ := g.s.store.GetUserAsset(ctx, g.a.AssetID)
	if row.Status != AssetStatusPurging {
		t.Fatal("byte failure restored Asset", row)
	}
	before, e := objects.GetOwnedObjectLifecycle(ctx, g.a.Asset.OwnerID, g.a.Asset.StorageObjectID)
	if e != nil || before.Object.Status != corestorage.ObjectStatusDeleted {
		t.Fatal(before, e)
	}
	objects.afterDelete = nil
	g.s.enabled = func() bool { return true }
	ack, err = g.s.AdminPurgeAssetForPrincipal(ctx, g.a.AssetID, "fresh current retry", g.proof)
	after, e := objects.GetOwnedObjectLifecycle(ctx, g.a.Asset.OwnerID, g.a.Asset.StorageObjectID)
	if err != nil || ack == nil || ack.Status != AssetStatusDeleted || e != nil || after.Object.Version != before.Object.Version {
		t.Fatal("current retry duplicated byte mutation", ack, err, after, e)
	}
}
