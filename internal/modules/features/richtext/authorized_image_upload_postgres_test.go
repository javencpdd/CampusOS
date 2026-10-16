package richtext

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	corestorage "github.com/campusos/CampusOS/internal/modules/core/userstorage"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"reflect"
	"testing"
)

type inlineUploadPathHook struct {
	corestorage.Port
	after func()
}

func (s *inlineUploadPathHook) Path(owner string, parts ...string) (string, error) {
	path, err := s.Port.Path(owner, parts...)
	if s.after != nil {
		after := s.after
		s.after = nil
		after()
	}
	return path, err
}
func inlineUploadPG(t *testing.T, owner string) (*Service, *corestorage.ContentImageStore, *corestorage.LocalAdapter, *corestorage.ObjectService, *pgxpool.Pool, *ArticleResult, identityport.SessionPrincipalResolver, string) {
	t.Helper()
	ctx := context.Background()
	s, _, pool, proof, claims := articleCreatePostgres(t, owner)
	quotas := corestorage.NewPgQuotaRepository(pool)
	if _, err := quotas.Upsert(ctx, corestorage.QuotaRecord{UserID: owner, QuotaBytes: 1024 * 1024}); err != nil {
		t.Fatal(err)
	}
	adapter, err := corestorage.NewLocalAdapterWithQuotaRepository(t.TempDir(), 1024*1024, quotas)
	if err != nil {
		t.Fatal(err)
	}
	assets, err := NewLocalAssetStoreWithStorage(AssetStoreConfig{}, adapter)
	if err != nil {
		t.Fatal(err)
	}
	s.SetAssetStore(assets)
	objects, err := corestorage.NewObjectService(adapter, adapter, corestorage.NewPgObjectRepository(pool), 1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	s.SetObjectPort(objects)
	images, err := corestorage.NewContentImageStore(adapter, adapter, 1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	images.SetUploadCommand(s.reliable, objects)
	request := articleCreateRequest()
	request.CategoryID = "115111"
	draft, err := s.CreateDraftForPrincipal(ctx, "Owner", request, proof)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := objects.Usage(ctx, owner); err != nil {
		t.Fatal(err)
	}
	return s, images, adapter, objects, pool, draft, proof, claims.SessionID
}
func inlineUploadPGSnapshot(t *testing.T, pool *pgxpool.Pool, owner string) string {
	t.Helper()
	var raw string
	err := pool.QueryRow(context.Background(), `SELECT jsonb_build_object(
'account',(SELECT to_jsonb(a) FROM user_storage_accounts a WHERE a.user_id=$1),
'assets',(SELECT COALESCE(jsonb_agg(to_jsonb(a) ORDER BY a.id),'[]'::jsonb) FROM richtext_article_assets a WHERE uploader_id=$1),
'core',(SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY t.id),'[]'::jsonb) FROM threads t WHERE author_id=$1),
'articles',(SELECT COALESCE(jsonb_agg(to_jsonb(a) ORDER BY a.id),'[]'::jsonb) FROM richtext_article_contents a WHERE created_by=$1),
'audits',(SELECT COALESCE(jsonb_agg(to_jsonb(a) ORDER BY a.id),'[]'::jsonb) FROM platform_command_audits a WHERE actor_id=$1::text),
'outbox_count',(SELECT count(*) FROM platform_outbox))::text`, owner).Scan(&raw)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
func TestInlineImageUploadPostgreSQLRealLocksAndRequiredSQLRollback(t *testing.T) {
	ctx := context.Background()
	index := 0
	for _, scope := range []string{"content", "inventory", "article"} {
		failures := []string{"none", "platform_command_audits", "platform_outbox", "user_storage_accounts"}
		if scope != "content" {
			failures = append(failures, "richtext_article_assets")
		}
		for _, failure := range failures {
			t.Run(scope+"/"+failure, func(t *testing.T) {
				index++
				owner := fmt.Sprint(197201 + index*10)
				s, images, adapter, objects, pool, draft, proof, session := inlineUploadPG(t, owner)
				before := inlineUploadPGSnapshot(t, pool, owner)
				ran := false
				hook := &inlineUploadPathHook{Port: adapter, after: func() {
					ran = true
					rows := []struct{ query, id string }{{"SELECT id FROM users WHERE id=$1 FOR UPDATE NOWAIT", owner}, {"SELECT id FROM sessions WHERE id=$1 FOR UPDATE NOWAIT", session}, {"SELECT user_id FROM user_storage_quotas WHERE user_id=$1 FOR UPDATE NOWAIT", owner}, {"SELECT user_id FROM user_storage_accounts WHERE user_id=$1 FOR UPDATE NOWAIT", owner}}
					if scope == "article" {
						rows = append(rows, struct{ query, id string }{"SELECT id FROM threads WHERE id=$1 FOR UPDATE NOWAIT", draft.ThreadID}, struct{ query, id string }{"SELECT id FROM richtext_article_contents WHERE id=$1 FOR UPDATE NOWAIT", draft.Article.ID})
					}
					for _, row := range rows {
						assertAttachmentUploadPGLock(t, pool, row.query, row.id)
					}
					var acquired bool
					if err := pool.QueryRow(ctx, "SELECT pg_try_advisory_xact_lock(hashtextextended('storage-quota:' || $1::text,0))", owner).Scan(&acquired); err != nil || acquired {
						t.Fatal("current quota admission lock missing", acquired, err)
					}
				}}
				s.assets.storage = hook
				if scope == "content" {
					var err error
					images, err = corestorage.NewContentImageStore(hook, adapter, 1024*1024)
					if err != nil {
						t.Fatal(err)
					}
					images.SetUploadCommand(s.reliable, objects)
				}
				if failure != "none" {
					if _, err := pool.Exec(ctx, "CREATE FUNCTION inline_upload_reject() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'required inline upload record rejected'; END $$"); err != nil {
						t.Fatal(err)
					}
					verb := "INSERT"
					if failure == "user_storage_accounts" {
						verb = "UPDATE"
					}
					if _, err := pool.Exec(ctx, "CREATE TRIGGER inline_upload_reject BEFORE "+verb+" ON "+failure+" FOR EACH ROW EXECUTE FUNCTION inline_upload_reject()"); err != nil {
						t.Fatal(err)
					}
					defer func() {
						if _, err := pool.Exec(context.Background(), "DROP TRIGGER inline_upload_reject ON "+failure+"; DROP FUNCTION inline_upload_reject()"); err != nil {
							t.Fatal(err)
						}
					}()
				}
				var imageSize int64
				var err error
				if scope == "content" {
					image, e := images.SaveForPrincipal(ctx, bytes.NewReader(tinyPNG), proof)
					err = e
					if image != nil {
						imageSize = image.FileSize
					}
				} else {
					thread, article := "", ""
					if scope == "article" {
						thread, article = draft.ThreadID, draft.Article.ID
					}
					image, e := s.UploadAssetForPrincipal(ctx, bytes.NewReader(tinyPNG), thread, article, proof)
					err = e
					if image != nil {
						imageSize = image.FileSize
					}
				}
				usage, e := adapter.Usage(owner)
				if e != nil {
					t.Fatal(e)
				}
				ledger, e := objects.Usage(ctx, owner)
				if e != nil {
					t.Fatal(e)
				}
				if (err == nil) != (failure == "none") || ran != (failure != "user_storage_accounts") {
					t.Fatal("required SQL or lock probe not executed", scope, failure, err, ran)
				}
				if failure == "none" {
					if imageSize < 1 || usage != imageSize || ledger.UsedBytes != imageSize {
						t.Fatal("image bytes not allocated exactly once", usage, ledger, imageSize)
					}
				} else {
					var pgErr *pgconn.PgError
					if !errors.As(err, &pgErr) || pgErr.Code != "P0001" || errors.Is(err, corestorage.ErrInlineImageCleanupPending) {
						t.Fatal("actual required SQL failure missing", failure, err)
					}
					if usage != 0 || ledger.UsedBytes != 0 || inlineUploadPGSnapshot(t, pool, owner) != before {
						t.Fatal("required SQL rollback or physical cleanup incomplete", scope, failure, usage, ledger)
					}
				}
			})
		}
	}
}
func TestInlineImageUploadPostgreSQLCommittedAndActualTransactionChanges(t *testing.T) {
	ctx := context.Background()
	for index, mode := range []string{"session", "account_version", "strength", "core_body_aba", "article_summary_aba", "quota_lower", "quota_upper", "counter", "disabled", "after_core_aba", "after_article", "after_metadata"} {
		t.Run(mode, func(t *testing.T) {
			owner := fmt.Sprint(198201 + index*10)
			s, _, adapter, objects, pool, draft, proof, session := inlineUploadPG(t, owner)
			ran := false
			mutate := func(commandCtx context.Context, a *Asset) {
				ran = true
				db := transaction.ExecutorFor(commandCtx, pool)
				var err error
				switch mode {
				case "session":
					_, err = db.Exec(commandCtx, "UPDATE sessions SET revoked_at=NOW(),revoke_reason='image test' WHERE id=$1", session)
				case "account_version":
					_, err = db.Exec(commandCtx, "UPDATE users SET auth_version=auth_version+1 WHERE id=$1", owner)
				case "strength":
					_, err = db.Exec(commandCtx, "UPDATE sessions SET authentication_strength='mfa',mfa_authenticated_at=NOW() WHERE id=$1", session)
				case "core_body_aba", "after_core_aba":
					var content string
					err = db.QueryRow(commandCtx, "SELECT content FROM threads WHERE id=$1", draft.ThreadID).Scan(&content)
					if err == nil {
						_, err = db.Exec(commandCtx, "UPDATE threads SET content='image extra body' WHERE id=$1", draft.ThreadID)
					}
					if err == nil {
						_, err = db.Exec(commandCtx, "UPDATE threads SET content=$2 WHERE id=$1", draft.ThreadID, content)
					}
				case "article_summary_aba":
					var summary string
					err = db.QueryRow(commandCtx, "SELECT summary FROM richtext_article_contents WHERE id=$1", draft.Article.ID).Scan(&summary)
					if err == nil {
						_, err = db.Exec(commandCtx, "UPDATE richtext_article_contents SET summary='image extra summary' WHERE id=$1", draft.Article.ID)
					}
					if err == nil {
						_, err = db.Exec(commandCtx, "UPDATE richtext_article_contents SET summary=$2 WHERE id=$1", draft.Article.ID, summary)
					}
				case "after_article":
					_, err = db.Exec(commandCtx, "UPDATE richtext_article_contents SET summary='extra write' WHERE id=$1", draft.Article.ID)
				case "after_metadata":
					_, err = db.Exec(commandCtx, "UPDATE richtext_article_assets SET width=width+1 WHERE id=$1", a.ID)
				case "quota_lower", "quota_upper":
					quota := int64(1)
					if mode == "quota_upper" {
						quota = 2 * 1024 * 1024
					}
					_, err = corestorage.NewPgQuotaRepository(pool).Upsert(commandCtx, corestorage.QuotaRecord{UserID: owner, QuotaBytes: quota})
				case "counter":
					_, err = db.Exec(commandCtx, "UPDATE threads SET view_count=view_count+1 WHERE id=$1", draft.ThreadID)
				case "disabled":
					s.SetEnabledChecker(func() bool { return false })
				}
				if err != nil {
					t.Fatal("actual mutation failed", mode, err)
				}
			}
			before := inlineUploadPGSnapshot(t, pool, owner)
			reader := &inlineUploadReader{Reader: bytes.NewReader(tinyPNG)}
			if len(mode) > 5 && mode[:6] == "after_" {
				s.store = &inlineUploadStoreHook{Store: s.store, after: mutate}
			} else {
				reader.after = func() { mutate(ctx, nil) }
			}
			image, err := s.UploadAssetForPrincipal(ctx, reader, draft.ThreadID, draft.Article.ID, proof)
			usage, _ := adapter.Usage(owner)
			ledger, e := objects.Usage(ctx, owner)
			if e != nil {
				t.Fatal(e)
			}
			if !ran {
				t.Fatal("current source mutation did not execute", mode)
			}
			allowed := mode == "quota_upper" || mode == "counter"
			if (err == nil) != allowed {
				t.Fatal("stale allow or counter rejection", mode, image, err)
			}
			if allowed {
				if image == nil || usage != image.FileSize || ledger.UsedBytes != image.FileSize {
					t.Fatal(mode, image, usage, ledger)
				}
			} else {
				if image != nil || usage != 0 || ledger.UsedBytes != 0 {
					t.Fatal("changed facts retained bytes or allocation", mode, image, err, usage, ledger)
				}
				if len(mode) > 5 && mode[:6] == "after_" && !reflect.DeepEqual(inlineUploadPGSnapshot(t, pool, owner), before) {
					t.Fatal("actual extra writes not rolled back", mode)
				}
			}
		})
	}
}
