package richtext

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func assetBindingPostgres(t *testing.T, owner string) (*PgStore, *pgxpool.Pool, string, string, []string, []string) {
	t.Helper()
	s, _, pool, proof, _ := articleCreatePostgres(t, owner)
	ctx := context.Background()
	req := articleCreateRequest()
	req.CategoryID = "115111"
	draft, err := s.CreateDraftForPrincipal(ctx, "Owner", req, proof)
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.CreateDraftForPrincipal(ctx, "Owner", req, proof)
	if err != nil {
		t.Fatal(err)
	}
	store := s.store.(*PgStore)
	n, _ := strconv.ParseInt(owner, 10, 64)
	assets, bindings := []string{}, []string{}
	for i := int64(0); i < 3; i++ {
		objectID := strconv.FormatInt(n+1000+i, 10)
		assetID := strconv.FormatInt(n+2000+i, 10)
		bindingID := strconv.FormatInt(n+3000+i, 10)
		_, err := pool.Exec(ctx, "INSERT INTO storage_objects(id,owner_user_id,namespace,purpose,storage_key,original_name,mime_type,size_bytes,sha256,status) VALUES($1,$2,'richtext','attachment',$3,'original.pdf','application/pdf',100,'test-sha','ready')", objectID, owner, "owned-test/"+objectID)
		if err != nil {
			t.Fatal(err)
		}
		now := time.Now().UTC()
		asset := &UserAsset{ID: assetID, OwnerID: owner, Kind: AssetKindAttachment, OriginalName: "original.pdf", StorageObjectID: objectID, MimeType: "application/pdf", SizeBytes: 100, Status: AssetStatusActive, Version: 7, CreatedAt: now, UpdatedAt: now}
		if err := store.CreateUserAsset(ctx, asset); err != nil {
			t.Fatal(err)
		}
		article := draft.Article.ID
		if i == 2 {
			article = other.Article.ID
		}
		binding := &ArticleAttachment{ID: bindingID, ArticleContentID: article, AssetID: assetID, DisplayName: "display.pdf", CreatedAt: now, UpdatedAt: now, Asset: *asset}
		if err := store.CreateAttachment(ctx, binding); err != nil {
			t.Fatal(err)
		}
		assets, bindings = append(assets, assetID), append(bindings, bindingID)
	}
	return store, pool, draft.Article.ID, other.Article.ID, assets, bindings
}

func assetBindingNonce(t *testing.T, raw json.RawMessage) int64 {
	t.Helper()
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	var n int64
	if err := json.Unmarshal(fields["facts_version"], &n); err != nil || n <= 0 {
		t.Fatal("missing positive storage nonce", n, err, string(raw))
	}
	return n
}

func assetBindingOriginal(t *testing.T, raw json.RawMessage) []byte {
	t.Helper()
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	delete(fields, "facts_version")
	delete(fields, "updated_at")
	value, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestAssetBindingAuthorityPostgreSQLScopeSnapshotsAndLocks(t *testing.T) {
	store, pool, article, other, assets, bindings := assetBindingPostgres(t, "156201")
	ctx := context.Background()
	raw, err := store.LoadAssetAuthorityFacts(ctx, assets[0], false)
	if err != nil {
		t.Fatal(err)
	}
	assetBindingNonce(t, raw)
	var full map[string]json.RawMessage
	if err := json.Unmarshal(raw, &full); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"id", "owner_user_id", "kind", "original_name", "storage_object_id", "mime_type", "size_bytes", "status", "version", "created_at", "updated_at", "trashed_at", "deleted_at"} {
		if _, ok := full[field]; !ok {
			t.Fatal("incomplete internal asset snapshot", field)
		}
	}
	raw[0] = '!'
	again, err := store.LoadAssetAuthorityFacts(ctx, assets[0], false)
	if err != nil || !json.Valid(again) {
		t.Fatal("snapshot aliased", err)
	}
	rows, err := store.LoadArticleBindingAuthorityFacts(ctx, article, false)
	if err != nil || len(rows) != 2 {
		t.Fatal(rows, err)
	}
	for i, row := range rows {
		assetBindingNonce(t, row.Binding)
		assetBindingNonce(t, row.Asset)
		var binding map[string]json.RawMessage
		if err := json.Unmarshal(row.Binding, &binding); err != nil || string(binding["id"]) != bindings[i] || string(binding["article_content_id"]) != article || string(binding["asset_id"]) != assets[i] {
			t.Fatal("scope/order/relationship lost", row, err)
		}
	}
	otherRows, err := store.LoadArticleBindingAuthorityFacts(ctx, other, false)
	if err != nil || len(otherRows) != 1 || bytes.Equal(otherRows[0].Binding, rows[0].Binding) {
		t.Fatal("article inventory leaked", otherRows, err)
	}
	if _, err := store.LoadAssetAuthorityFacts(ctx, assets[0], true); !errors.Is(err, ErrAssetBindingFactsUnavailable) {
		t.Fatal("lock outside transaction", err)
	}
	if _, err := store.LoadArticleBindingAuthorityFacts(ctx, article, true); !errors.Is(err, ErrAssetBindingFactsUnavailable) {
		t.Fatal("aggregate lock outside transaction", err)
	}
	err = transaction.NewPostgreSQL(pool).Within(ctx, func(tx context.Context) error {
		if _, err := store.LoadArticleBindingAuthorityFacts(tx, article, true); err != nil {
			return err
		}
		for _, item := range []struct{ query, id string }{
			{"SELECT id FROM richtext_article_attachments WHERE id=$1 FOR UPDATE NOWAIT", bindings[0]},
			{"SELECT id FROM richtext_article_attachments WHERE id=$1 FOR UPDATE NOWAIT", bindings[1]},
			{"SELECT id FROM user_assets WHERE id=$1 FOR UPDATE NOWAIT", assets[0]},
			{"SELECT id FROM user_assets WHERE id=$1 FOR UPDATE NOWAIT", assets[1]},
		} {
			_, err := pool.Exec(ctx, item.query, item.id)
			var pgerr *pgconn.PgError
			if !errors.As(err, &pgerr) || pgerr.Code != "55P03" {
				t.Fatal("actual row not locked", item, err)
			}
		}
		if _, err := pool.Exec(ctx, "SELECT id FROM user_assets WHERE id=$1 FOR UPDATE NOWAIT", assets[2]); err != nil {
			t.Fatal("unrelated article asset locked", err)
		}
		var admitted bool
		if err := pool.QueryRow(ctx, "SELECT pg_try_advisory_xact_lock(hashtextextended('richtext-attachment:' || $1::text,0))", article).Scan(&admitted); err != nil || admitted {
			t.Fatal("aggregate admission not locked", admitted, err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.LoadAssetAuthorityFacts(ctx, "9223372036854775807", false); !errors.Is(err, ErrAssetNotFound) {
		t.Fatal(err)
	}
	for _, invalid := range []string{"0", "01", "+1", " 1", "-1"} {
		if _, err := store.LoadAssetAuthorityFacts(ctx, invalid, false); !errors.Is(err, ErrAssetBindingFactsUnavailable) {
			t.Fatal("noncanonical asset ID accepted", invalid, err)
		}
		if _, err := store.LoadArticleBindingAuthorityFacts(ctx, invalid, false); !errors.Is(err, ErrAssetBindingFactsUnavailable) {
			t.Fatal("noncanonical article ID accepted", invalid, err)
		}
	}
	if err := transaction.NewPostgreSQL(pool).Within(ctx, func(tx context.Context) error {
		if _, err := store.LoadAssetAuthorityFacts(tx, assets[2], true); err != nil {
			return err
		}
		_, err := pool.Exec(ctx, "SELECT id FROM user_assets WHERE id=$1 FOR UPDATE NOWAIT", assets[2])
		var pgerr *pgconn.PgError
		if !errors.As(err, &pgerr) || pgerr.Code != "55P03" {
			t.Fatal("individual asset row not locked", err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// Even an empty inventory holds the admission lock. The parent command
	// independently proves that the corresponding Article actually exists.
	if err := transaction.NewPostgreSQL(pool).Within(ctx, func(tx context.Context) error {
		rows, err := store.LoadArticleBindingAuthorityFacts(tx, "9223372036854775807", true)
		if err != nil || len(rows) != 0 {
			t.Fatal(rows, err)
		}
		var admitted bool
		err = pool.QueryRow(ctx, "SELECT pg_try_advisory_xact_lock(hashtextextended('richtext-attachment:' || $1::text,0))", "9223372036854775807").Scan(&admitted)
		if err != nil || admitted {
			t.Fatal("empty aggregate unlocked", admitted, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestAssetBindingAuthorityPostgreSQLABAHighWaterAndGuards(t *testing.T) {
	store, pool, article, _, assets, bindings := assetBindingPostgres(t, "156301")
	ctx := context.Background()
	for _, mode := range []string{"asset_name", "asset_status", "asset_version", "asset_date", "binding_name", "binding_order", "binding_reinsert", "asset_reinsert"} {
		t.Run(mode, func(t *testing.T) {
			before, err := store.LoadArticleBindingAuthorityFacts(ctx, article, false)
			if err != nil {
				t.Fatal(err)
			}
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(ctx)
			exec := func(q string, args ...any) {
				if _, err := tx.Exec(ctx, q, args...); err != nil {
					t.Fatal(mode, err)
				}
			}
			switch mode {
			case "asset_name":
				exec("UPDATE user_assets SET original_name='changed.pdf' WHERE id=$1", assets[0])
				exec("UPDATE user_assets SET original_name='original.pdf' WHERE id=$1", assets[0])
			case "asset_status":
				exec("UPDATE user_assets SET status='quarantined' WHERE id=$1", assets[0])
				exec("UPDATE user_assets SET status='active' WHERE id=$1", assets[0])
			case "asset_version":
				exec("UPDATE user_assets SET version=version+1 WHERE id=$1", assets[0])
				exec("UPDATE user_assets SET version=7 WHERE id=$1", assets[0])
			case "asset_date":
				exec("UPDATE user_assets SET trashed_at='2026-01-01T00:00:00Z' WHERE id=$1", assets[0])
				exec("UPDATE user_assets SET trashed_at=NULL WHERE id=$1", assets[0])
			case "binding_name":
				exec("UPDATE richtext_article_attachments SET display_name='changed.pdf' WHERE id=$1", bindings[0])
				exec("UPDATE richtext_article_attachments SET display_name='display.pdf' WHERE id=$1", bindings[0])
			case "binding_order":
				exec("UPDATE richtext_article_attachments SET display_order=100 WHERE id=$1", bindings[0])
				exec("UPDATE richtext_article_attachments SET display_order=0 WHERE id=$1", bindings[0])
			case "binding_reinsert":
				exec("CREATE TEMP TABLE saved_binding ON COMMIT DROP AS SELECT * FROM richtext_article_attachments WHERE id=$1", bindings[0])
				exec("DELETE FROM richtext_article_attachments WHERE id=$1", bindings[0])
				exec("INSERT INTO richtext_article_attachments SELECT * FROM saved_binding")
			case "asset_reinsert":
				exec("CREATE TEMP TABLE saved_bindings ON COMMIT DROP AS SELECT * FROM richtext_article_attachments WHERE asset_id=$1", assets[0])
				exec("CREATE TEMP TABLE saved_asset ON COMMIT DROP AS SELECT * FROM user_assets WHERE id=$1", assets[0])
				exec("DELETE FROM richtext_article_attachments WHERE asset_id=$1", assets[0])
				exec("DELETE FROM user_assets WHERE id=$1", assets[0])
				exec("INSERT INTO user_assets SELECT * FROM saved_asset")
				exec("INSERT INTO richtext_article_attachments SELECT * FROM saved_bindings")
			}
			if err := tx.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			after, err := store.LoadArticleBindingAuthorityFacts(ctx, article, false)
			if err != nil || len(after) != len(before) {
				t.Fatal(after, err)
			}
			assetChanged := len(mode) >= 6 && mode[:6] == "asset_"
			bindingChanged := !assetChanged || mode == "asset_reinsert"
			if assetChanged && assetBindingNonce(t, after[0].Asset) <= assetBindingNonce(t, before[0].Asset) {
				t.Fatal("asset ABA nonce reused")
			}
			if bindingChanged && assetBindingNonce(t, after[0].Binding) <= assetBindingNonce(t, before[0].Binding) {
				t.Fatal("binding ABA nonce reused")
			}
			if !bytes.Equal(assetBindingOriginal(t, before[0].Asset), assetBindingOriginal(t, after[0].Asset)) || !bytes.Equal(assetBindingOriginal(t, before[0].Binding), assetBindingOriginal(t, after[0].Binding)) {
				t.Fatal("roundtrip original fields changed")
			}
		})
	}
	before, _ := store.LoadAssetAuthorityFacts(ctx, assets[0], false)
	if _, err := pool.Exec(ctx, "UPDATE user_assets SET updated_at=NOW(),facts_version=1 WHERE id=$1", assets[0]); err != nil {
		t.Fatal(err)
	}
	after, _ := store.LoadAssetAuthorityFacts(ctx, assets[0], false)
	if assetBindingNonce(t, after) != assetBindingNonce(t, before) {
		t.Fatal("caller nonce/timestamp accepted")
	}
	var high int64
	if err := transaction.NewPostgreSQL(pool).Within(ctx, func(tx context.Context) error {
		if _, err := store.db(tx).Exec(tx, "UPDATE user_assets SET original_name='rollback.pdf' WHERE id=$1", assets[0]); err != nil {
			return err
		}
		raw, err := store.LoadAssetAuthorityFacts(tx, assets[0], true)
		if err != nil {
			return err
		}
		high = assetBindingNonce(t, raw)
		return errors.New("rollback")
	}); err == nil {
		t.Fatal("rollback ignored")
	}
	rolledBack, _ := store.LoadAssetAuthorityFacts(ctx, assets[0], false)
	if !bytes.Equal(rolledBack, after) {
		t.Fatal("rollback changed facts")
	}
	if _, err := pool.Exec(ctx, "UPDATE user_assets SET original_name='next.pdf' WHERE id=$1", assets[0]); err != nil {
		t.Fatal(err)
	}
	next, _ := store.LoadAssetAuthorityFacts(ctx, assets[0], false)
	if assetBindingNonce(t, next) <= high {
		t.Fatal("rollback high water reused")
	}
	for _, guard := range []struct{ disable, enable string }{
		{"ALTER TABLE user_assets DISABLE TRIGGER trg_user_assets_richtext_facts_version", "ALTER TABLE user_assets ENABLE TRIGGER trg_user_assets_richtext_facts_version"},
		{"ALTER TABLE richtext_article_attachments DISABLE TRIGGER trg_article_attachments_richtext_facts_version", "ALTER TABLE richtext_article_attachments ENABLE TRIGGER trg_article_attachments_richtext_facts_version"},
		{"ALTER SEQUENCE richtext_asset_binding_facts_version_seq CYCLE", "ALTER SEQUENCE richtext_asset_binding_facts_version_seq NO CYCLE"},
	} {
		func() {
			if _, err := pool.Exec(ctx, guard.disable); err != nil {
				t.Fatal(err)
			}
			defer func() {
				if _, err := pool.Exec(context.Background(), guard.enable); err != nil {
					t.Fatal(err)
				}
			}()
			if _, err := store.LoadAssetAuthorityFacts(ctx, assets[0], false); !errors.Is(err, ErrAssetBindingFactsUnavailable) {
				t.Fatal("disabled guard accepted asset", err)
			}
			if _, err := store.LoadArticleBindingAuthorityFacts(ctx, article, false); !errors.Is(err, ErrAssetBindingFactsUnavailable) {
				t.Fatal("disabled guard accepted aggregate", err)
			}
			if _, err := store.LoadArticleBindingAuthorityFacts(ctx, "9223372036854775807", false); !errors.Is(err, ErrAssetBindingFactsUnavailable) {
				t.Fatal("disabled guard accepted empty inventory", err)
			}
		}()
	}
}
