package richtext

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	corestorage "github.com/campusos/CampusOS/internal/modules/core/userstorage"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type assetOwnerPGHook struct {
	Store
	after func(context.Context)
}

func (s *assetOwnerPGHook) LoadAssetAuthorityFacts(ctx context.Context, id string, lock bool) (json.RawMessage, error) {
	raw, err := s.Store.LoadAssetAuthorityFacts(ctx, id, lock)
	if err == nil && lock && s.after != nil {
		f := s.after
		s.after = nil
		f(ctx)
	}
	return raw, err
}
func assetOwnerPostgres(t *testing.T, owner, operation string) (*Service, *pgxpool.Pool, *ArticleResult, *ArticleAttachment, identityport.SessionPrincipalResolver, *corestorage.LocalAdapter) {
	t.Helper()
	s, pool, draft, a, proof, _, adapter, _ := attachmentUploadPostgres(t, owner)
	if err := s.store.RemoveAttachment(context.Background(), draft.Article.ID, a.ID); err != nil {
		t.Fatal(err)
	}
	if operation == "restore" {
		if err := s.TrashUserAsset(context.Background(), owner, a.AssetID); err != nil {
			t.Fatal(err)
		}
	}
	return s, pool, draft, a, proof, adapter
}
func assetOwnerPGSnapshot(t *testing.T, pool *pgxpool.Pool, asset string) string {
	t.Helper()
	var raw string
	err := pool.QueryRow(context.Background(), "SELECT jsonb_build_object('asset',(SELECT to_jsonb(a) FROM user_assets a WHERE id=$1::bigint),'asset_audits',(SELECT jsonb_agg(to_jsonb(x) ORDER BY id) FROM asset_lifecycle_audits x WHERE asset_id=$1::bigint),'audits',(SELECT count(*) FROM platform_command_audits WHERE resource_type='user_asset' AND resource_id=$1::text),'outbox',(SELECT count(*) FROM platform_outbox WHERE aggregate_type='user_asset' AND aggregate_id=$1::text))::text", asset).Scan(&raw)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
func TestAssetOwnerLifecyclePostgreSQLLocksFKAdmissionAndRequiredRollback(t *testing.T) {
	ctx := context.Background()
	for i, operation := range []string{"trash", "restore"} {
		for j, failure := range []string{"none", "asset_lifecycle_audits", "platform_command_audits", "platform_outbox"} {
			t.Run(operation+"/"+failure, func(t *testing.T) {
				owner := fmt.Sprint(163201 + i*100 + j*10)
				s, pool, draft, a, proof, adapter := assetOwnerPostgres(t, owner, operation)
				before := assetOwnerPGSnapshot(t, pool, a.AssetID)
				beforeUsage, err := adapter.Usage(owner)
				if err != nil {
					t.Fatal(err)
				}
				locked := false
				objectLocked := false
				if operation == "restore" {
					s.objects = &attachmentMetadataObjectHook{ObjectPort: s.objects, after: func() {
						objectLocked = true
						assertAttachmentUploadPGLock(t, pool, "SELECT id FROM storage_objects WHERE id=$1 FOR UPDATE NOWAIT", a.Asset.StorageObjectID)
					}}
				}
				s.store = &assetOwnerPGHook{Store: s.store, after: func(tx context.Context) {
					locked = true
					assertAttachmentUploadPGLock(t, pool, "SELECT id FROM user_assets WHERE id=$1 FOR UPDATE NOWAIT", a.AssetID)
					assertAttachmentUploadPGLock(t, pool, "SELECT id FROM users WHERE id=$1 FOR UPDATE NOWAIT", owner)
					// An empty inventory must not admit either actual FK relation
					// while this command holds its target Asset FOR UPDATE.
					block, cancel := context.WithTimeout(ctx, 80*time.Millisecond)
					_, err := pool.Exec(block, "INSERT INTO richtext_article_attachments(id,article_content_id,asset_id,display_name,display_order,created_at,updated_at) VALUES($1,$2,$3,'phantom.pdf',0,NOW(),NOW())", fmt.Sprint(164201+i*100+j*10), draft.Article.ID, a.AssetID)
					blocked := errors.Is(block.Err(), context.DeadlineExceeded)
					cancel()
					if err == nil || !blocked {
						t.Fatal("empty binding FK admission did not wait for Asset lock", err)
					}
					block, cancel = context.WithTimeout(ctx, 80*time.Millisecond)
					_, err = pool.Exec(block, "INSERT INTO richtext_article_assets(id,uploader_id,file_url,file_name,file_size,mime_type,created_at,asset_id) VALUES($1,$2,'/test','phantom.png',1,'image/png',NOW(),$3)", fmt.Sprint(164301+i*100+j*10), owner, a.AssetID)
					blocked = errors.Is(block.Err(), context.DeadlineExceeded)
					cancel()
					if err == nil || !blocked {
						t.Fatal("empty legacy-image FK admission did not wait for Asset lock", err)
					}
				}}
				if failure != "none" {
					if _, err := pool.Exec(ctx, "CREATE FUNCTION asset_owner_reject() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'required asset record rejected'; END $$"); err != nil {
						t.Fatal(err)
					}
					if _, err := pool.Exec(ctx, "CREATE TRIGGER asset_owner_reject BEFORE INSERT ON "+failure+" FOR EACH ROW EXECUTE FUNCTION asset_owner_reject()"); err != nil {
						t.Fatal(err)
					}
					defer func() {
						if _, err := pool.Exec(context.Background(), "DROP TRIGGER asset_owner_reject ON "+failure+"; DROP FUNCTION asset_owner_reject()"); err != nil {
							t.Fatal(err)
						}
					}()
				}
				var ack *AssetMutationAck
				if operation == "trash" {
					ack, err = s.TrashUserAssetForPrincipal(ctx, a.AssetID, proof)
				} else {
					ack, err = s.RestoreUserAssetForPrincipal(ctx, a.AssetID, proof)
				}
				if !locked || operation == "restore" && !objectLocked || (err == nil) != (failure == "none") {
					t.Fatal("actual lifecycle lock/SQL failure not reached", ack, err, locked)
				}
				if failure != "none" {
					var pgErr *pgconn.PgError
					if !errors.As(err, &pgErr) || pgErr.Code != "P0001" || ack != nil || assetOwnerPGSnapshot(t, pool, a.AssetID) != before {
						t.Fatal("required lifecycle record did not roll back", failure, ack, err)
					}
				} else {
					var actorKind, actorID, legacy string
					if err := pool.QueryRow(ctx, "SELECT actor_kind,actor_ref_id,COALESCE(actor_user_id::text,'') FROM asset_lifecycle_audits WHERE asset_id=$1 ORDER BY created_at DESC,id DESC LIMIT 1", a.AssetID).Scan(&actorKind, &actorID, &legacy); err != nil || actorKind != "user" || actorID != owner || legacy != owner {
						t.Fatal("user actor history changed domain", actorKind, actorID, legacy, err)
					}
				}
				if usage, err := adapter.Usage(owner); err != nil || usage != beforeUsage {
					t.Fatal("metadata transition changed bytes", usage, beforeUsage, err)
				}
			})
		}
	}
}

func TestAssetOwnerLifecyclePostgreSQLCurrentFactsAndIntegrityGuards(t *testing.T) {
	ctx := context.Background()
	for i, mode := range []string{"asset_aba", "session_revoke", "binding_fk_missing", "image_fk_disabled", "image_fk_duplicate", "facts_trigger_disabled", "object_changed", "counter"} {
		t.Run(mode, func(t *testing.T) {
			owner := fmt.Sprint(163501 + i*10)
			s, pool, _, a, base, _ := assetOwnerPostgres(t, owner, "restore")
			before := assetOwnerPGSnapshot(t, pool, a.AssetID)
			calls := 0
			proof := func(ctx context.Context) (identityport.PrincipalContext, error) {
				calls++
				if calls == 2 {
					switch mode {
					case "asset_aba":
						if _, err := pool.Exec(ctx, "UPDATE user_assets SET original_name='changed.pdf' WHERE id=$1", a.AssetID); err != nil {
							t.Fatal(err)
						}
						if _, err := pool.Exec(ctx, "UPDATE user_assets SET original_name=$2 WHERE id=$1", a.AssetID, a.Asset.OriginalName); err != nil {
							t.Fatal(err)
						}
					case "session_revoke":
						if _, err := pool.Exec(ctx, "UPDATE sessions SET revoked_at=NOW(),revoke_reason='test' WHERE user_id=$1", owner); err != nil {
							t.Fatal(err)
						}
					case "binding_fk_missing":
						if _, err := pool.Exec(ctx, "ALTER TABLE richtext_article_attachments DROP CONSTRAINT fk_richtext_article_attachments_asset"); err != nil {
							t.Fatal(err)
						}
					case "image_fk_disabled":
						var trigger string
						if err := pool.QueryRow(ctx, "SELECT t.tgname FROM pg_trigger t JOIN pg_constraint c ON c.oid=t.tgconstraint WHERE c.conname='fk_richtext_article_assets_user_asset' AND t.tgrelid='richtext_article_assets'::regclass LIMIT 1").Scan(&trigger); err != nil {
							t.Fatal(err)
						}
						if _, err := pool.Exec(ctx, "ALTER TABLE richtext_article_assets DISABLE TRIGGER "+pgx.Identifier{trigger}.Sanitize()); err != nil {
							t.Fatal(err)
						}
						t.Cleanup(func() {
							if _, err := pool.Exec(context.Background(), "ALTER TABLE richtext_article_assets ENABLE TRIGGER "+pgx.Identifier{trigger}.Sanitize()); err != nil {
								t.Error(err)
							}
						})
					case "facts_trigger_disabled":
						if _, err := pool.Exec(ctx, "ALTER TABLE user_assets DISABLE TRIGGER trg_user_assets_richtext_facts_version"); err != nil {
							t.Fatal(err)
						}
					case "image_fk_duplicate":
						if _, err := pool.Exec(ctx, "ALTER TABLE richtext_article_assets DROP CONSTRAINT fk_richtext_article_assets_user_asset; ALTER TABLE richtext_article_attachments ADD CONSTRAINT asset_owner_duplicate_fk FOREIGN KEY(asset_id) REFERENCES user_assets(id) ON DELETE RESTRICT"); err != nil {
							t.Fatal(err)
						}
						t.Cleanup(func() {
							if _, err := pool.Exec(context.Background(), "ALTER TABLE richtext_article_attachments DROP CONSTRAINT asset_owner_duplicate_fk; ALTER TABLE richtext_article_assets ADD CONSTRAINT fk_richtext_article_assets_user_asset FOREIGN KEY(asset_id) REFERENCES user_assets(id) ON DELETE SET NULL"); err != nil {
								t.Error(err)
							}
						})
					case "object_changed":
						if _, err := pool.Exec(ctx, "UPDATE storage_objects SET mime_type='changed' WHERE id=$1", a.Asset.StorageObjectID); err != nil {
							t.Fatal(err)
						}
					case "counter":
						if _, err := pool.Exec(ctx, "UPDATE threads SET view_count=view_count+1 WHERE author_id=$1", owner); err != nil {
							t.Fatal(err)
						}
					}
				}
				return base(ctx)
			}
			if mode == "binding_fk_missing" {
				defer func() {
					if _, err := pool.Exec(context.Background(), "ALTER TABLE richtext_article_attachments ADD CONSTRAINT fk_richtext_article_attachments_asset FOREIGN KEY(asset_id) REFERENCES user_assets(id) ON DELETE RESTRICT"); err != nil {
						t.Fatal(err)
					}
				}()
			}
			if mode == "facts_trigger_disabled" {
				defer func() {
					if _, err := pool.Exec(context.Background(), "ALTER TABLE user_assets ENABLE TRIGGER trg_user_assets_richtext_facts_version"); err != nil {
						t.Fatal(err)
					}
				}()
			}
			ack, err := s.RestoreUserAssetForPrincipal(ctx, a.AssetID, proof)
			if (err == nil) != (mode == "counter") {
				t.Fatal("actual current facts result wrong", mode, ack, err)
			}
			if mode != "counter" {
				row, readErr := s.store.GetUserAsset(ctx, a.AssetID)
				if readErr != nil || row.Status != AssetStatusTrashed || ack != nil {
					t.Fatal("denied lifecycle changed state", mode, row, readErr)
				}
				if mode != "asset_aba" && mode != "session_revoke" && mode != "object_changed" && assetOwnerPGSnapshot(t, pool, a.AssetID) != before {
					t.Fatal("failed guard changed metadata/audit", mode)
				}
			}
		})
	}
}
