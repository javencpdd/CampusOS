package richtext

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	corestorage "github.com/campusos/CampusOS/internal/modules/core/userstorage"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func assetReadPostgres(t *testing.T, owner string) (*Service, *pgxpool.Pool, *ArticleResult, *ArticleAttachment, identityport.SessionPrincipalResolver) {
	t.Helper()
	s, _, pool, proof, _ := articleCreatePostgres(t, owner)
	adapter, err := corestorage.NewLocalAdapterWithQuota(t.TempDir(), 1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	objects, err := corestorage.NewObjectService(adapter, adapter, corestorage.NewPgObjectRepository(pool), MaxArticleAttachmentBytes)
	if err != nil {
		t.Fatal(err)
	}
	s.SetObjectPort(objects)
	req := articleCreateRequest()
	req.CategoryID = "115111"
	draft, err := s.CreateDraftForPrincipal(context.Background(), "Owner", req, proof)
	if err != nil {
		t.Fatal(err)
	}
	pdf := []byte("%PDF-1.7\n1 0 obj\n<<>>\nendobj\n")
	attachment, err := s.UploadAttachment(context.Background(), owner, draft.ThreadID, "original.pdf", "application/pdf", int64(len(pdf)), bytes.NewReader(pdf))
	if err != nil {
		t.Fatal(err)
	}
	return s, pool, draft, attachment, proof
}

func TestAssetBindingReadPostgreSQLCommittedABAAndCounter(t *testing.T) {
	ctx := context.Background()
	for i, mode := range []string{"binding_name", "binding_order", "binding_reinsert", "asset_name", "asset_status", "asset_reference", "asset_reinsert", "asset_owner", "asset_name_personal", "counter"} {
		t.Run(mode, func(t *testing.T) {
			owner := fmt.Sprint(157201 + i*10)
			s, pool, draft, attachment, proof := assetReadPostgres(t, owner)
			if _, err := pool.Exec(ctx, "INSERT INTO users(id,username,nickname,email,status,auth_version) VALUES(157999,'asset-read-other','Other','asset-read-other@example.test','active',1) ON CONFLICT(id) DO NOTHING"); err != nil {
				t.Fatal(err)
			}
			pdf := []byte("%PDF-1.7\n1 0 obj\n<<>>\nendobj\n")
			other, err := s.objects.Put(ctx, owner, corestorage.PutRequest{Namespace: "richtext", Purpose: "article_attachment", OriginalName: "other.pdf", MimeType: "application/pdf", SizeHint: int64(len(pdf)), Reader: bytes.NewReader(pdf)})
			if err != nil {
				t.Fatal(err)
			}
			objects := &richReadObjects{ObjectPort: s.objects}
			objects.afterOpen = func() {
				richReadCommit(t, pool, func(tx pgx.Tx) error {
					exec := func(q string, args ...any) error { _, err := tx.Exec(ctx, q, args...); return err }
					switch mode {
					case "binding_name":
						if err := exec("UPDATE richtext_article_attachments SET display_name='changed.pdf' WHERE id=$1", attachment.ID); err != nil {
							return err
						}
						return exec("UPDATE richtext_article_attachments SET display_name=$2 WHERE id=$1", attachment.ID, attachment.DisplayName)
					case "binding_order":
						if err := exec("UPDATE richtext_article_attachments SET display_order=100 WHERE id=$1", attachment.ID); err != nil {
							return err
						}
						return exec("UPDATE richtext_article_attachments SET display_order=$2 WHERE id=$1", attachment.ID, attachment.DisplayOrder)
					case "binding_reinsert", "asset_reinsert":
						if err := exec("CREATE TEMP TABLE saved_read_binding ON COMMIT DROP AS SELECT * FROM richtext_article_attachments WHERE id=$1", attachment.ID); err != nil {
							return err
						}
						if err := exec("DELETE FROM richtext_article_attachments WHERE id=$1", attachment.ID); err != nil {
							return err
						}
						if mode == "asset_reinsert" {
							if err := exec("CREATE TEMP TABLE saved_read_asset ON COMMIT DROP AS SELECT * FROM user_assets WHERE id=$1", attachment.AssetID); err != nil {
								return err
							}
							if err := exec("DELETE FROM user_assets WHERE id=$1", attachment.AssetID); err != nil {
								return err
							}
							if err := exec("INSERT INTO user_assets SELECT * FROM saved_read_asset"); err != nil {
								return err
							}
						}
						return exec("INSERT INTO richtext_article_attachments SELECT * FROM saved_read_binding")
					case "asset_name", "asset_name_personal":
						if err := exec("UPDATE user_assets SET original_name='changed.pdf' WHERE id=$1", attachment.AssetID); err != nil {
							return err
						}
						return exec("UPDATE user_assets SET original_name=$2 WHERE id=$1", attachment.AssetID, attachment.Asset.OriginalName)
					case "asset_status":
						if err := exec("UPDATE user_assets SET status='quarantined' WHERE id=$1", attachment.AssetID); err != nil {
							return err
						}
						return exec("UPDATE user_assets SET status='active' WHERE id=$1", attachment.AssetID)
					case "asset_reference":
						if err := exec("UPDATE user_assets SET storage_object_id=$2 WHERE id=$1", attachment.AssetID, other.ID); err != nil {
							return err
						}
						return exec("UPDATE user_assets SET storage_object_id=$2 WHERE id=$1", attachment.AssetID, attachment.Asset.StorageObjectID)
					case "asset_owner":
						if err := exec("UPDATE user_assets SET owner_user_id=157999 WHERE id=$1", attachment.AssetID); err != nil {
							return err
						}
						return exec("UPDATE user_assets SET owner_user_id=$2 WHERE id=$1", attachment.AssetID, owner)
					case "counter":
						return exec("UPDATE threads SET view_count=view_count+1,updated_at=NOW() WHERE id=$1", draft.ThreadID)
					}
					return errors.New("unexpected mode")
				})
			}
			s.objects = objects
			var opened AttachmentOpen
			if mode == "asset_name_personal" {
				opened, err = s.OpenUserAssetForPrincipal(ctx, attachment.AssetID, proof)
			} else {
				opened, err = s.OpenAttachmentForPrincipal(ctx, draft.ThreadID, attachment.ID, proof)
			}
			if mode == "counter" {
				if err != nil || opened.Reader == nil {
					t.Fatal("counter-only changed authority", err)
				}
				opened.Reader.Close()
			} else if !errors.Is(err, identityport.ErrPolicyFactsChanged) || opened.Reader != nil || objects.closes != 1 {
				t.Fatal("committed actual metadata ABA not closed", mode, err, opened.Reader != nil, objects.closes)
			}
		})
	}
}

type assetReadPageDriftStore struct {
	Store
	afterFacts func()
}

func (s *assetReadPageDriftStore) LoadAssetAuthorityFacts(ctx context.Context, id string, lock bool) (json.RawMessage, error) {
	raw, err := s.Store.LoadAssetAuthorityFacts(ctx, id, lock)
	if err == nil && s.afterFacts != nil {
		after := s.afterFacts
		s.afterFacts = nil
		after()
	}
	return raw, err
}

func TestAssetBindingReadPostgreSQLWholePageEmptyAndGuards(t *testing.T) {
	ctx := context.Background()
	s, pool, draft, attachment, proof := assetReadPostgres(t, "157401")
	store := s.store
	page, err := s.UserAssetPageForPrincipal(ctx, "", "", 10, proof)
	if err != nil || len(page.Items) != 1 || page.Items[0].ID != attachment.AssetID {
		t.Fatal(page, err)
	}
	s.store = &assetReadPageDriftStore{Store: store, afterFacts: func() {
		richReadCommit(t, pool, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, "UPDATE user_assets SET original_name='page changed.pdf' WHERE id=$1", attachment.AssetID); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, "UPDATE user_assets SET original_name=$2 WHERE id=$1", attachment.AssetID, attachment.Asset.OriginalName)
			return err
		})
	}}
	if page, err := s.UserAssetPageForPrincipal(ctx, "", "", 10, proof); !errors.Is(err, identityport.ErrPolicyFactsChanged) || len(page.Items) != 0 {
		t.Fatal("whole page ABA accepted", page, err)
	}
	s.store = store
	for _, statement := range []struct{ disable, enable string }{
		{"ALTER TABLE user_assets DISABLE TRIGGER trg_user_assets_richtext_facts_version", "ALTER TABLE user_assets ENABLE TRIGGER trg_user_assets_richtext_facts_version"},
		{"ALTER TABLE richtext_article_attachments DISABLE TRIGGER trg_article_attachments_richtext_facts_version", "ALTER TABLE richtext_article_attachments ENABLE TRIGGER trg_article_attachments_richtext_facts_version"},
		{"ALTER SEQUENCE richtext_asset_binding_facts_version_seq CYCLE", "ALTER SEQUENCE richtext_asset_binding_facts_version_seq NO CYCLE"},
	} {
		func() {
			if _, err := pool.Exec(ctx, statement.disable); err != nil {
				t.Fatal(err)
			}
			defer func() {
				if _, err := pool.Exec(context.Background(), statement.enable); err != nil {
					t.Fatal(err)
				}
			}()
			for _, status := range []string{AssetStatusActive, AssetStatusTrashed} {
				if page, err := s.UserAssetPageForPrincipal(ctx, status, "", 10, proof); !errors.Is(err, ErrAssetBindingFactsUnavailable) || len(page.Items) != 0 {
					t.Fatal("populated/empty page ignored guard", status, page, err)
				}
			}
			if article, err := s.GetArticleForPrincipal(ctx, draft.ThreadID, proof); !errors.Is(err, ErrAssetBindingFactsUnavailable) || article != nil {
				t.Fatal("Article binding inventory ignored guard", article, err)
			}
			if opened, err := s.OpenUserAssetForPrincipal(ctx, attachment.AssetID, proof); !errors.Is(err, ErrAssetBindingFactsUnavailable) || opened.Reader != nil {
				t.Fatal("bytes ignored guard", opened, err)
			}
		}()
	}
}
