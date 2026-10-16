package richtext

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"

	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	corestorage "github.com/campusos/CampusOS/internal/modules/core/userstorage"
	personaldocuments "github.com/campusos/CampusOS/internal/modules/features/personaldocuments"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func attachmentCopyPostgres(t *testing.T, owner string) (*Service, *pgxpool.Pool, *ArticleResult, identityport.SessionPrincipalResolver, *personaldocuments.Service, personaldocuments.DocumentDetail, *attachmentUploadObjects, *corestorage.LocalAdapter, *corestorage.PgObjectRepository) {
	t.Helper()
	s, pool, draft, _, proof, objects, adapter, repo := attachmentUploadPostgres(t, owner)
	documents, err := personaldocuments.NewService(personaldocuments.NewPgRepository(pool), objects)
	if err != nil {
		t.Fatal(err)
	}
	documents.SetReliability(reliability.NewService(transaction.NewPostgreSQL(pool), reliability.NewPostgreSQLStore(pool)))
	body := []byte("%PDF-1.4\n1 0 obj\n<< /Type /Catalog >>\nendobj\ntrailer\n<< /Root 1 0 R >>\n%%EOF")
	d, err := documents.UploadForPrincipal(context.Background(), proof, "source.pdf", personaldocuments.FormatPDF, "application/pdf", int64(len(body)), bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	s.SetPersonalDocumentAttachmentReader(&attachmentCopyReader{documents: documents})
	objects.puts = 0
	return s, pool, draft, proof, documents, d, objects, adapter, repo
}

func TestAttachmentCopyPostgreSQLSourceLocksAndRequiredRollback(t *testing.T) {
	ctx := context.Background()
	for i, failure := range []string{"none", "platform_command_audits", "platform_outbox", "richtext_article_attachments", "source_extra_mutation"} {
		t.Run(failure, func(t *testing.T) {
			owner := fmt.Sprint(162201 + i*10)
			s, pool, draft, proof, documents, d, objects, adapter, repo := attachmentCopyPostgres(t, owner)
			before := attachmentMetadataPGSnapshot(t, pool, draft)
			beforeUsage, err := adapter.Usage(owner)
			if err != nil {
				t.Fatal(err)
			}
			var beforeDoc string
			if err := pool.QueryRow(ctx, "SELECT to_jsonb(d)::text FROM personal_documents d WHERE id=$1", d.ID).Scan(&beforeDoc); err != nil {
				t.Fatal(err)
			}
			locked, mutated := false, false
			reader := s.personalDocumentAttachmentReader.(*attachmentCopyReader)
			reader.afterCheck = func(tx context.Context, lock bool) {
				if !lock {
					return
				}
				if !locked {
					locked = true
					for _, row := range []struct{ q, id string }{
						{"SELECT id FROM personal_documents WHERE id=$1 FOR UPDATE NOWAIT", d.ID},
						{"SELECT id FROM personal_document_versions WHERE id=$1 FOR UPDATE NOWAIT", d.CurrentVersionID},
						{"SELECT id FROM storage_objects WHERE id=$1 FOR UPDATE NOWAIT", d.CurrentVersion.SourceObjectID},
					} {
						assertAttachmentUploadPGLock(t, pool, row.q, row.id)
					}
				}
				if failure == "source_extra_mutation" && !mutated {
					mutated = true
					db, ok := transaction.FromContext(tx)
					if !ok {
						t.Fatal("source mutation did not join actual SQL transaction")
					}
					if _, err := db.Exec(tx, "UPDATE personal_documents SET name='unexpected.pdf' WHERE id=$1", d.ID); err != nil {
						t.Fatal(err)
					}
				}
			}
			if failure != "none" && failure != "source_extra_mutation" {
				if _, err := pool.Exec(ctx, "CREATE FUNCTION attachment_copy_reject() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'required copy record rejected'; END $$"); err != nil {
					t.Fatal(err)
				}
				if _, err := pool.Exec(ctx, "CREATE TRIGGER attachment_copy_reject BEFORE INSERT ON "+failure+" FOR EACH ROW EXECUTE FUNCTION attachment_copy_reject()"); err != nil {
					t.Fatal(err)
				}
				defer func() {
					if _, err := pool.Exec(context.Background(), "DROP TRIGGER attachment_copy_reject ON "+failure+"; DROP FUNCTION attachment_copy_reject()"); err != nil {
						t.Fatal(err)
					}
				}()
			}
			ack, err := s.AttachPersonalDocumentForPrincipal(ctx, draft.ThreadID, d.ID, "published.pdf", proof)
			if !locked || objects.puts != 1 || (err == nil) != (failure == "none") {
				t.Fatal("actual source locks/result not exercised", failure, ack, err, locked, objects.puts)
			}
			if failure != "none" {
				if ack != nil || objects.deletes != 1 || attachmentMetadataPGSnapshot(t, pool, draft) != before || errors.Is(err, ErrAttachmentCompensationPending) {
					t.Fatal("source copy rollback/compensation incomplete", failure, ack, err, objects.deletes)
				}
				if failure != "source_extra_mutation" {
					var pgErr *pgconn.PgError
					if !errors.As(err, &pgErr) || pgErr.Code != "P0001" {
						t.Fatal("required SQL rejection was not reached", failure, err)
					}
				}
			}
			if failure == "source_extra_mutation" && !mutated {
				t.Fatal("actual source mutation did not run")
			}
			var afterDoc string
			if err := pool.QueryRow(ctx, "SELECT to_jsonb(d)::text FROM personal_documents d WHERE id=$1", d.ID).Scan(&afterDoc); err != nil || afterDoc != beforeDoc {
				t.Fatal("source read or failed command changed private source", err)
			}
			want := beforeUsage
			if failure == "none" {
				want += d.CurrentVersion.SizeBytes
			}
			if usage, err := adapter.Usage(owner); err != nil || usage != want {
				t.Fatal("copy physical receipt wrong", usage, want, err)
			}
			account, exists, err := repo.Account(ctx, owner)
			if err != nil || !exists || account.ReservedBytes != 0 || account.UsedBytes != want {
				t.Fatal("copy ledger receipt wrong", account, exists, err)
			}
			if current, err := documents.ReadOnly().GetOwnDocument(ctx, proof, d.ID); err != nil || current.Version != d.Version || current.CurrentVersionID != d.CurrentVersionID {
				t.Fatal("copy changed current private version", current, err)
			}
		})
	}
}

func TestAttachmentCopyPostgreSQLStagedSourceChangesAndIndependentBytes(t *testing.T) {
	for i, mode := range []string{"status_aba", "source_namespace", "source_version_sha", "session_revoke", "source_disabled", "cancel", "counter"} {
		t.Run(mode, func(t *testing.T) {
			owner := fmt.Sprint(162301 + i*10)
			s, pool, draft, proof, documents, d, objects, adapter, repo := attachmentCopyPostgres(t, owner)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			beforeUsage, err := adapter.Usage(owner)
			if err != nil {
				t.Fatal(err)
			}
			ran := false
			objects.afterPut = func(corestorage.Object) {
				ran = true
				switch mode {
				case "status_aba":
					d, err = documents.SetStatusForPrincipal(ctx, proof, d.ID, d.Version, personaldocuments.StatusTrashed)
					if err != nil {
						t.Fatal(err)
					}
					d, err = documents.SetStatusForPrincipal(ctx, proof, d.ID, d.Version, personaldocuments.StatusActive)
					if err != nil {
						t.Fatal(err)
					}
				case "source_namespace":
					_, err = pool.Exec(ctx, "UPDATE storage_objects SET namespace='changed' WHERE id=$1", d.CurrentVersion.SourceObjectID)
				case "source_version_sha":
					_, err = pool.Exec(ctx, "UPDATE personal_document_versions SET sha256=repeat('0',64) WHERE id=$1", d.CurrentVersionID)
				case "session_revoke":
					_, err = pool.Exec(ctx, "UPDATE sessions SET revoked_at=NOW(),revoke_reason='test' WHERE user_id=$1", owner)
				case "source_disabled":
					documents.SetEnabledChecker(func() bool { return false })
				case "cancel":
					cancel()
				case "counter":
					_, err = pool.Exec(ctx, "UPDATE threads SET view_count=view_count+1,updated_at=NOW() WHERE id=$1", draft.ThreadID)
				}
				if err != nil {
					t.Fatal(err)
				}
			}
			ack, err := s.AttachPersonalDocumentForPrincipal(ctx, draft.ThreadID, d.ID, "published.pdf", proof)
			if !ran || objects.puts != 1 || (err == nil) != (mode == "counter") || errors.Is(err, ErrAttachmentCompensationPending) {
				t.Fatal("actual staged source change accepted or not compensated", mode, ack, err, ran, objects.puts)
			}
			want := beforeUsage
			if mode == "counter" {
				want += d.CurrentVersion.SizeBytes
			}
			if usage, err := adapter.Usage(owner); err != nil || usage != want {
				t.Fatal("wrong source/staged physical receipt", mode, usage, want, err)
			}
			account, exists, err := repo.Account(context.Background(), owner)
			if err != nil || !exists || account.ReservedBytes != 0 || account.UsedBytes != want {
				t.Fatal("wrong source/staged ledger", mode, account, err)
			}
			assets, err := s.store.ListUserAssetsByOwner(context.Background(), owner, []string{AssetStatusActive})
			wantAssets := 1
			if mode == "counter" {
				wantAssets = 2
			}
			if err != nil || len(assets) != wantAssets {
				t.Fatal("staged source failure published Asset", mode, len(assets), err)
			}
			if mode == "counter" {
				objects.afterPut = nil
				if _, err := documents.SetStatusForPrincipal(context.Background(), proof, d.ID, d.Version, personaldocuments.StatusTrashed); err != nil {
					t.Fatal(err)
				}
				opened, err := s.OpenAttachmentForPrincipal(context.Background(), draft.ThreadID, ack.AttachmentID, proof)
				if err != nil || opened.Object.ID == d.CurrentVersion.SourceObjectID {
					t.Fatal("independent copy depends on private source", err)
				}
				opened.Reader.Close()
			}
		})
	}
}
