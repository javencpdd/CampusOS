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
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type attachmentMetadataObjectHook struct {
	corestorage.ObjectPort
	after func()
}

func (s *attachmentMetadataObjectHook) LockOwnedObjectForCommand(ctx context.Context, owner, id string) (corestorage.Object, error) {
	o, err := s.ObjectPort.(corestorage.OwnedObjectCommandLocker).LockOwnedObjectForCommand(ctx, owner, id)
	if err == nil && s.after != nil {
		f := s.after
		s.after = nil
		f()
	}
	return o, err
}

type attachmentMetadataPGHook struct {
	Store
	afterLocked func()
}

func (s *attachmentMetadataPGHook) LockArticleAuthority(ctx context.Context, id string) (*Article, error) {
	return s.Store.(ArticleAuthorityStore).LockArticleAuthority(ctx, id)
}
func (s *attachmentMetadataPGHook) UpdateArticleIfFacts(ctx context.Context, a *Article, v int64) error {
	return s.Store.(ArticleAuthorityStore).UpdateArticleIfFacts(ctx, a, v)
}
func (s *attachmentMetadataPGHook) LoadArticleBindingAuthorityFacts(ctx context.Context, id string, lock bool) ([]AssetBindingAuthorityFact, error) {
	rows, err := s.Store.LoadArticleBindingAuthorityFacts(ctx, id, lock)
	if err == nil && lock && s.afterLocked != nil {
		f := s.afterLocked
		s.afterLocked = nil
		f()
	}
	return rows, err
}
func attachmentMetadataPGSetup(t *testing.T, owner, operation string) (*Service, *pgxpool.Pool, *ArticleResult, []*ArticleAttachment, identityport.SessionPrincipalResolver) {
	t.Helper()
	s, pool, draft, a, proof := assetReadPostgres(t, owner)
	pdf := []byte("%PDF-1.7\n1 0 obj\n<<>>\nendobj\n")
	b, err := s.UploadAttachment(context.Background(), owner, draft.ThreadID, "second.pdf", "application/pdf", int64(len(pdf)), bytes.NewReader(pdf))
	if err != nil {
		t.Fatal(err)
	}
	if operation == "bind" {
		if err := s.store.RemoveAttachment(context.Background(), draft.Article.ID, b.ID); err != nil {
			t.Fatal(err)
		}
	}
	return s, pool, draft, []*ArticleAttachment{a, b}, proof
}
func attachmentMetadataPGExecute(ctx context.Context, s *Service, d *ArticleResult, a []*ArticleAttachment, op string, p identityport.SessionPrincipalResolver) error {
	switch op {
	case "bind":
		_, err := s.AttachExistingForPrincipal(ctx, d.ThreadID, a[1].AssetID, "bound.pdf", p)
		return err
	case "remove":
		return s.RemoveAttachmentForPrincipal(ctx, d.ThreadID, a[0].ID, p)
	case "rename":
		_, err := s.RenameAttachmentForPrincipal(ctx, d.ThreadID, a[0].ID, "renamed.pdf", p)
		return err
	case "reorder":
		_, err := s.ReorderAttachmentsForPrincipal(ctx, d.ThreadID, []string{a[1].ID, a[0].ID}, p)
		return err
	}
	return errors.New("unknown operation")
}
func attachmentMetadataPGSnapshot(t *testing.T, pool *pgxpool.Pool, d *ArticleResult) string {
	t.Helper()
	var snapshot string
	err := pool.QueryRow(context.Background(), "SELECT jsonb_build_object('thread',(SELECT to_jsonb(t) FROM threads t WHERE id=$1),'article',(SELECT to_jsonb(a) FROM richtext_article_contents a WHERE thread_id=$1),'bindings',(SELECT jsonb_agg(to_jsonb(b) ORDER BY b.id) FROM richtext_article_attachments b WHERE article_content_id=$2),'audits',(SELECT count(*) FROM platform_command_audits WHERE resource_id=$1::text),'outbox',(SELECT count(*) FROM platform_outbox WHERE aggregate_id=$1::text))::text", d.ThreadID, d.Article.ID).Scan(&snapshot)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}
func TestAttachmentMetadataPostgreSQLLocksAndRequiredRollback(t *testing.T) {
	ctx := context.Background()
	for i, op := range []string{"bind", "remove", "rename", "reorder"} {
		t.Run(op, func(t *testing.T) {
			for j, failure := range []string{"none", "platform_command_audits", "platform_outbox"} {
				t.Run(failure, func(t *testing.T) {
					owner := fmt.Sprint(158201 + i*100 + j*10)
					s, pool, draft, items, proof := attachmentMetadataPGSetup(t, owner, op)
					base := s.store
					before := attachmentMetadataPGSnapshot(t, pool, draft)
					ran := false
					objectLocked := false
					if op == "bind" {
						s.objects = &attachmentMetadataObjectHook{ObjectPort: s.objects, after: func() {
							objectLocked = true
							for _, q := range []struct{ query, id string }{
								{"SELECT id FROM user_assets WHERE id=$1 FOR UPDATE NOWAIT", items[1].AssetID},
								{"SELECT id FROM storage_objects WHERE id=$1 FOR UPDATE NOWAIT", items[1].Asset.StorageObjectID},
							} {
								_, err := pool.Exec(ctx, q.query, q.id)
								var e *pgconn.PgError
								if !errors.As(err, &e) || e.Code != "55P03" {
									t.Fatal("selected actual Asset/Object not locked", q, err)
								}
							}
						}}
					}
					s.store = &attachmentMetadataPGHook{Store: base, afterLocked: func() {
						ran = true
						for _, q := range []struct{ query, id string }{
							{"SELECT id FROM users WHERE id=$1 FOR UPDATE NOWAIT", owner},
							{"SELECT id FROM threads WHERE id=$1 FOR UPDATE NOWAIT", draft.ThreadID},
							{"SELECT id FROM richtext_article_contents WHERE id=$1 FOR UPDATE NOWAIT", draft.Article.ID},
							{"SELECT id FROM richtext_article_attachments WHERE id=$1 FOR UPDATE NOWAIT", items[0].ID},
							{"SELECT id FROM user_assets WHERE id=$1 FOR UPDATE NOWAIT", items[0].AssetID},
						} {
							_, err := pool.Exec(ctx, q.query, q.id)
							var e *pgconn.PgError
							if !errors.As(err, &e) || e.Code != "55P03" {
								t.Fatal("actual row not locked", q, err)
							}
						}
						var acquired bool
						if err := pool.QueryRow(ctx, "SELECT pg_try_advisory_xact_lock(hashtextextended('richtext-attachment:' || $1::text,0))", draft.Article.ID).Scan(&acquired); err != nil || acquired {
							t.Fatal("aggregate admission not held", acquired, err)
						}
					}}
					if failure != "none" {
						if _, err := pool.Exec(ctx, "CREATE FUNCTION attachment_metadata_reject() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'required attachment record rejected'; END $$"); err != nil {
							t.Fatal(err)
						}
						if _, err := pool.Exec(ctx, "CREATE TRIGGER attachment_metadata_reject BEFORE INSERT ON "+failure+" FOR EACH ROW EXECUTE FUNCTION attachment_metadata_reject()"); err != nil {
							t.Fatal(err)
						}
						defer func() {
							if _, err := pool.Exec(context.Background(), "DROP TRIGGER attachment_metadata_reject ON "+failure+"; DROP FUNCTION attachment_metadata_reject()"); err != nil {
								t.Fatal(err)
							}
						}()
					}
					err := attachmentMetadataPGExecute(ctx, s, draft, items, op, proof)
					if !ran || op == "bind" && !objectLocked || (err == nil) != (failure == "none") {
						t.Fatal("actual execution", ran, err)
					}
					if failure != "none" {
						if attachmentMetadataPGSnapshot(t, pool, draft) != before {
							t.Fatal("required failure left resource/audit/outbox changes")
						}
					} else {
						var payload []byte
						if err := pool.QueryRow(ctx, "SELECT payload FROM platform_outbox WHERE aggregate_id=$1 AND event_type='richtext.attachment.updated' ORDER BY created_at DESC LIMIT 1", draft.ThreadID).Scan(&payload); err != nil {
							t.Fatal(err)
						}
						var fields map[string]json.RawMessage
						if json.Unmarshal(payload, &fields) != nil || len(fields) > 2 || len(fields) < 1 {
							t.Fatal("event payload", string(payload))
						}
						for name := range fields {
							if name != "thread_id" && name != "attachment_id" {
								t.Fatal("private event field", name)
							}
						}
					}
				})
			}
		})
	}
}
func TestAttachmentMetadataPostgreSQLCommittedFactsAndFinalChecks(t *testing.T) {
	ctx := context.Background()
	for i, mode := range []string{"binding_aba", "asset_aba", "article_aba", "core_aba", "session_revoke", "guard_disabled", "counter"} {
		t.Run(mode, func(t *testing.T) {
			owner := fmt.Sprint(158801 + i*10)
			s, pool, draft, items, base := attachmentMetadataPGSetup(t, owner, "rename")
			calls := 0
			proof := func(ctx context.Context) (identityport.PrincipalContext, error) {
				calls++
				if calls == 2 {
					richReadCommit(t, pool, func(tx pgx.Tx) error {
						exec := func(q string, args ...any) error { _, err := tx.Exec(ctx, q, args...); return err }
						switch mode {
						case "binding_aba":
							if err := exec("UPDATE richtext_article_attachments SET display_name='external.pdf' WHERE id=$1", items[0].ID); err != nil {
								return err
							}
							return exec("UPDATE richtext_article_attachments SET display_name=$2 WHERE id=$1", items[0].ID, items[0].DisplayName)
						case "asset_aba":
							if err := exec("UPDATE user_assets SET original_name='external.pdf' WHERE id=$1", items[0].AssetID); err != nil {
								return err
							}
							return exec("UPDATE user_assets SET original_name=$2 WHERE id=$1", items[0].AssetID, items[0].Asset.OriginalName)
						case "article_aba":
							if err := exec("UPDATE richtext_article_contents SET summary='external' WHERE id=$1", draft.Article.ID); err != nil {
								return err
							}
							return exec("UPDATE richtext_article_contents SET summary=$2 WHERE id=$1", draft.Article.ID, draft.Article.Summary)
						case "core_aba":
							if err := exec("UPDATE threads SET content=content||'x' WHERE id=$1", draft.ThreadID); err != nil {
								return err
							}
							return exec("UPDATE threads SET content=left(content,length(content)-1) WHERE id=$1", draft.ThreadID)
						case "session_revoke":
							return exec("UPDATE sessions SET revoked_at=NOW(),revoke_reason='test' WHERE user_id=$1", owner)
						case "guard_disabled":
							return exec("ALTER TABLE richtext_article_attachments DISABLE TRIGGER trg_article_attachments_richtext_facts_version")
						case "counter":
							return exec("UPDATE threads SET view_count=view_count+1,updated_at=NOW() WHERE id=$1", draft.ThreadID)
						}
						return errors.New("unknown mode")
					})
				}
				return base(ctx)
			}
			if mode == "guard_disabled" {
				defer func() {
					if _, err := pool.Exec(context.Background(), "ALTER TABLE richtext_article_attachments ENABLE TRIGGER trg_article_attachments_richtext_facts_version"); err != nil {
						t.Fatal(err)
					}
				}()
			}
			err := attachmentMetadataPGExecute(ctx, s, draft, items, "rename", proof)
			if (err == nil) != (mode == "counter") {
				t.Fatal(mode, err)
			}
			actual, _ := s.store.GetAttachment(ctx, draft.Article.ID, items[0].ID)
			if mode != "counter" && actual.DisplayName != items[0].DisplayName {
				t.Fatal("denied command changed binding", actual)
			}
		})
	}
	// The final proof must reject extra writes performed inside the transaction.
	s, pool, draft, items, proof := attachmentMetadataPGSetup(t, "158991", "rename")
	store := s.store
	before := attachmentMetadataPGSnapshot(t, pool, draft)
	ran := false
	s.store = &attachmentMetadataTxDriftStore{Store: store, after: func(tx context.Context) {
		ran = true
		if _, err := store.(*PgStore).db(tx).Exec(tx, "UPDATE threads SET is_pinned=TRUE WHERE id=$1", draft.ThreadID); err != nil {
			t.Fatal(err)
		}
	}}
	if err := attachmentMetadataPGExecute(ctx, s, draft, items, "rename", proof); err == nil || !ran {
		t.Fatal("actual final Core change accepted or callback skipped", ran, err)
	}
	if attachmentMetadataPGSnapshot(t, pool, draft) != before {
		t.Fatal("final rejection did not roll back actual Core/binding/audit/outbox")
	}
}

type attachmentMetadataTxDriftStore struct {
	Store
	after func(context.Context)
}

func (s *attachmentMetadataTxDriftStore) LockArticleAuthority(ctx context.Context, id string) (*Article, error) {
	return s.Store.(ArticleAuthorityStore).LockArticleAuthority(ctx, id)
}
func (s *attachmentMetadataTxDriftStore) UpdateArticleIfFacts(ctx context.Context, a *Article, v int64) error {
	return s.Store.(ArticleAuthorityStore).UpdateArticleIfFacts(ctx, a, v)
}
func (s *attachmentMetadataTxDriftStore) UpdateAttachmentDisplayName(ctx context.Context, article, id, name string) error {
	if err := s.Store.UpdateAttachmentDisplayName(ctx, article, id, name); err != nil {
		return err
	}
	if s.after != nil {
		s.after(ctx)
	}
	return nil
}
