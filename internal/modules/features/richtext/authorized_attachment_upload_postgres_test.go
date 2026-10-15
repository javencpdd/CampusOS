package richtext

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"

	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	corestorage "github.com/campusos/CampusOS/internal/modules/core/userstorage"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func attachmentUploadPostgres(t *testing.T, owner string) (*Service, *pgxpool.Pool, *ArticleResult, *ArticleAttachment, identityport.SessionPrincipalResolver, *attachmentUploadObjects, *corestorage.LocalAdapter, *corestorage.PgObjectRepository) {
	t.Helper()
	s, _, pool, proof, _ := articleCreatePostgres(t, owner)
	quotas := corestorage.NewPgQuotaRepository(pool)
	if _, err := quotas.Upsert(context.Background(), corestorage.QuotaRecord{UserID: owner, QuotaBytes: 1024 * 1024}); err != nil {
		t.Fatal(err)
	}
	adapter, err := corestorage.NewLocalAdapterWithQuotaRepository(t.TempDir(), 1024*1024, quotas)
	if err != nil {
		t.Fatal(err)
	}
	repo := corestorage.NewPgObjectRepository(pool)
	objects, err := corestorage.NewObjectService(adapter, adapter, repo, MaxArticleAttachmentBytes)
	if err != nil {
		t.Fatal(err)
	}
	wrapper := &attachmentUploadObjects{ObjectPort: objects}
	s.SetObjectPort(wrapper)
	request := articleCreateRequest()
	request.CategoryID = "115111"
	draft, err := s.CreateDraftForPrincipal(context.Background(), "Owner", request, proof)
	if err != nil {
		t.Fatal(err)
	}
	body := []byte("%PDF-1.7\n1 0 obj\n<<>>\nendobj\n")
	old, err := s.UploadAttachment(context.Background(), owner, draft.ThreadID, "old.pdf", "application/pdf", int64(len(body)), bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	wrapper.puts = 0
	return s, pool, draft, old, proof, wrapper, adapter, repo
}
func assertAttachmentUploadPGLock(t *testing.T, pool *pgxpool.Pool, query, id string) {
	t.Helper()
	_, err := pool.Exec(context.Background(), query, id)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "55P03" {
		t.Fatal("actual upload row lock missing", query, id, err)
	}
}
func TestAttachmentUploadPostgreSQLLocksMetadataAndRequiredRollback(t *testing.T) {
	ctx := context.Background()
	body := []byte("%PDF-1.7\n1 0 obj\n<<>>\nendobj\n")
	for i, failure := range []string{"none", "platform_command_audits", "platform_outbox", "user_assets", "richtext_article_attachments"} {
		t.Run(failure, func(t *testing.T) {
			owner := fmt.Sprint(160201 + i*10)
			s, pool, draft, old, proof, wrapper, adapter, repo := attachmentUploadPostgres(t, owner)
			before := attachmentMetadataPGSnapshot(t, pool, draft)
			locked, objectLocked := false, false
			base := s.store
			s.store = &attachmentMetadataPGHook{Store: base, afterLocked: func() {
				locked = true
				for _, row := range []struct{ q, id string }{
					{"SELECT id FROM users WHERE id=$1 FOR UPDATE NOWAIT", owner},
					{"SELECT id FROM threads WHERE id=$1 FOR UPDATE NOWAIT", draft.ThreadID},
					{"SELECT id FROM richtext_article_contents WHERE id=$1 FOR UPDATE NOWAIT", draft.Article.ID},
					{"SELECT id FROM richtext_article_attachments WHERE id=$1 FOR UPDATE NOWAIT", old.ID},
					{"SELECT id FROM user_assets WHERE id=$1 FOR UPDATE NOWAIT", old.AssetID},
				} {
					assertAttachmentUploadPGLock(t, pool, row.q, row.id)
				}
				var acquired bool
				if err := pool.QueryRow(ctx, "SELECT pg_try_advisory_xact_lock(hashtextextended('richtext-attachment:' || $1::text,0))", draft.Article.ID).Scan(&acquired); err != nil || acquired {
					t.Fatal("actual inventory admission lock missing", acquired, err)
				}
			}}
			wrapper.afterPut = func(object corestorage.Object) {
				s.objects = &attachmentMetadataObjectHook{ObjectPort: wrapper, after: func() {
					objectLocked = true
					assertAttachmentUploadPGLock(t, pool, "SELECT id FROM storage_objects WHERE id=$1 FOR UPDATE NOWAIT", object.ID)
				}}
			}
			if failure != "none" {
				if _, err := pool.Exec(ctx, "CREATE FUNCTION attachment_upload_reject() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'required upload record rejected'; END $$"); err != nil {
					t.Fatal(err)
				}
				if _, err := pool.Exec(ctx, "CREATE TRIGGER attachment_upload_reject BEFORE INSERT ON "+failure+" FOR EACH ROW EXECUTE FUNCTION attachment_upload_reject()"); err != nil {
					t.Fatal(err)
				}
				defer func() {
					if _, err := pool.Exec(context.Background(), "DROP TRIGGER attachment_upload_reject ON "+failure+"; DROP FUNCTION attachment_upload_reject()"); err != nil {
						t.Fatal(err)
					}
				}()
			}
			ack, err := s.UploadAttachmentForPrincipal(ctx, draft.ThreadID, "new.pdf", "application/pdf", int64(len(body)), bytes.NewReader(body), proof)
			if !locked || !objectLocked || wrapper.puts != 1 || (err == nil) != (failure == "none") {
				t.Fatal("actual transaction/rollback was not exercised", failure, ack, err, locked, objectLocked, wrapper.puts)
			}
			assets, readErr := base.ListUserAssetsByOwner(ctx, owner, []string{AssetStatusActive})
			if readErr != nil {
				t.Fatal(readErr)
			}
			want := 1
			if failure == "none" {
				want = 2
			}
			if len(assets) != want {
				t.Fatal("failed transaction leaked Asset", failure, len(assets))
			}
			if failure != "none" {
				if ack != nil || attachmentMetadataPGSnapshot(t, pool, draft) != before || wrapper.deletes != 1 || errors.Is(err, ErrAttachmentCompensationPending) {
					t.Fatal("failed required record did not roll back and compensate", failure, ack, err, wrapper.deletes)
				}
			}
			account, exists, readErr := repo.Account(ctx, owner)
			if readErr != nil || !exists || account.ReservedBytes != 0 || account.UsedBytes != int64(want*len(body)) {
				t.Fatal("wrong quota receipt after rollback", account, exists, readErr)
			}
			if usage, readErr := adapter.Usage(owner); readErr != nil || usage != int64(want*len(body)) {
				t.Fatal("wrong physical receipt after rollback", usage, readErr)
			}
			if failure == "none" && (ack == nil || ack.ThreadID != draft.ThreadID || ack.AttachmentID == "") {
				t.Fatal("success missing safe receipt", ack)
			}
		})
	}
	// An actual mutation inside the command must also fail the whole final proof.
	s, pool, draft, _, proof, wrapper, adapter, _ := attachmentUploadPostgres(t, "160291")
	before := attachmentMetadataPGSnapshot(t, pool, draft)
	ran := false
	s.store = &attachmentUploadPGTxDrift{Store: s.store, after: func(tx context.Context) {
		ran = true
		db, ok := transaction.FromContext(tx)
		if !ok {
			t.Fatal("mutation did not join actual SQL transaction")
		}
		if _, err := db.Exec(tx, "UPDATE threads SET is_pinned=TRUE WHERE id=$1", draft.ThreadID); err != nil {
			t.Fatal(err)
		}
	}}
	ack, err := s.UploadAttachmentForPrincipal(ctx, draft.ThreadID, "new.pdf", "application/pdf", int64(len(body)), bytes.NewReader(body), proof)
	if err == nil || ack != nil || !ran || wrapper.deletes != 1 || attachmentMetadataPGSnapshot(t, pool, draft) != before {
		t.Fatal("actual extra transaction mutation escaped", ack, err, ran, wrapper.deletes)
	}
	if usage, err := adapter.Usage("160291"); err != nil || usage != int64(len(body)) {
		t.Fatal(usage, err)
	}
}

type attachmentUploadPGTxDrift struct {
	Store
	after func(context.Context)
}

func (s *attachmentUploadPGTxDrift) LockArticleAuthority(ctx context.Context, id string) (*Article, error) {
	return s.Store.(ArticleAuthorityStore).LockArticleAuthority(ctx, id)
}
func (s *attachmentUploadPGTxDrift) UpdateArticleIfFacts(ctx context.Context, a *Article, v int64) error {
	return s.Store.(ArticleAuthorityStore).UpdateArticleIfFacts(ctx, a, v)
}
func (s *attachmentUploadPGTxDrift) CreateUserAsset(ctx context.Context, a *UserAsset) error {
	if err := s.Store.CreateUserAsset(ctx, a); err != nil {
		return err
	}
	s.after(ctx)
	return nil
}

func TestAttachmentUploadPostgreSQLStagedCommittedChangesAndReceipts(t *testing.T) {
	body := []byte("%PDF-1.7\n1 0 obj\n<<>>\nendobj\n")
	for i, mode := range []string{"core_aba", "article_aba", "binding_aba", "asset_aba", "session_revoke", "guard_disabled", "cancel", "changed_receipt", "cleanup_failure", "counter"} {
		t.Run(mode, func(t *testing.T) {
			owner := fmt.Sprint(160301 + i*10)
			s, pool, draft, old, proof, wrapper, adapter, repo := attachmentUploadPostgres(t, owner)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			ran := false
			wrapper.afterPut = func(object corestorage.Object) {
				ran = true
				if mode == "cancel" {
					cancel()
					return
				}
				if mode == "changed_receipt" {
					if _, err := repo.PrepareDelete(context.Background(), owner, object.ID, object.Version); err != nil {
						t.Fatal(err)
					}
					return
				}
				if mode == "cleanup_failure" {
					wrapper.deleteError = errors.New("test cleanup unavailable")
				}
				richReadCommit(t, pool, func(tx pgx.Tx) error {
					exec := func(query string, args ...any) error {
						_, err := tx.Exec(context.Background(), query, args...)
						return err
					}
					switch mode {
					case "core_aba":
						if err := exec("UPDATE threads SET content=content||'x' WHERE id=$1", draft.ThreadID); err != nil {
							return err
						}
						return exec("UPDATE threads SET content=left(content,length(content)-1) WHERE id=$1", draft.ThreadID)
					case "article_aba":
						if err := exec("UPDATE richtext_article_contents SET summary='changed' WHERE id=$1", draft.Article.ID); err != nil {
							return err
						}
						return exec("UPDATE richtext_article_contents SET summary=$2 WHERE id=$1", draft.Article.ID, draft.Article.Summary)
					case "binding_aba":
						if err := exec("UPDATE richtext_article_attachments SET display_name='changed.pdf' WHERE id=$1", old.ID); err != nil {
							return err
						}
						return exec("UPDATE richtext_article_attachments SET display_name=$2 WHERE id=$1", old.ID, old.DisplayName)
					case "asset_aba":
						if err := exec("UPDATE user_assets SET original_name='changed.pdf' WHERE id=$1", old.AssetID); err != nil {
							return err
						}
						return exec("UPDATE user_assets SET original_name=$2 WHERE id=$1", old.AssetID, old.Asset.OriginalName)
					case "session_revoke", "cleanup_failure":
						return exec("UPDATE sessions SET revoked_at=NOW(),revoke_reason='test' WHERE user_id=$1", owner)
					case "guard_disabled":
						return exec("ALTER TABLE richtext_article_attachments DISABLE TRIGGER trg_article_attachments_richtext_facts_version")
					case "counter":
						return exec("UPDATE threads SET view_count=view_count+1,updated_at=NOW() WHERE id=$1", draft.ThreadID)
					}
					return errors.New("unexpected upload drift")
				})
			}
			if mode == "guard_disabled" {
				defer func() {
					if _, err := pool.Exec(context.Background(), "ALTER TABLE richtext_article_attachments ENABLE TRIGGER trg_article_attachments_richtext_facts_version"); err != nil {
						t.Fatal(err)
					}
				}()
			}
			ack, err := s.UploadAttachmentForPrincipal(ctx, draft.ThreadID, "new.pdf", "application/pdf", int64(len(body)), bytes.NewReader(body), proof)
			if !ran || wrapper.puts != 1 || (err == nil) != (mode == "counter") {
				t.Fatal("actual staged change was not checked", mode, ack, err, ran, wrapper.puts)
			}
			pending := mode == "changed_receipt" || mode == "cleanup_failure"
			if errors.Is(err, ErrAttachmentCompensationPending) != pending {
				t.Fatal("changed or failed receipt not explicit", mode, err)
			}
			assets, readErr := s.store.ListUserAssetsByOwner(context.Background(), owner, []string{AssetStatusActive})
			wantAssets := 1
			if mode == "counter" {
				wantAssets = 2
			}
			if readErr != nil || len(assets) != wantAssets {
				t.Fatal("staged rejection published Asset", len(assets), readErr)
			}
			wantBytes := int64(len(body))
			if pending || mode == "counter" {
				wantBytes *= 2
			}
			account, exists, readErr := repo.Account(context.Background(), owner)
			if readErr != nil || !exists || account.ReservedBytes != 0 || account.UsedBytes != wantBytes {
				t.Fatal("wrong actual receipt ledger", mode, account, readErr)
			}
			if usage, readErr := adapter.Usage(owner); readErr != nil || usage != wantBytes {
				t.Fatal("wrong actual receipt bytes", mode, usage, wantBytes, readErr)
			}
			if mode == "changed_receipt" && wrapper.deletes != 0 {
				t.Fatal("changed receipt deleted")
			}
		})
	}
}
