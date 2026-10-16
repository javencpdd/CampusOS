package richtext

import (
	"bytes"
	"context"
	"reflect"
	"testing"

	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	corestorage "github.com/campusos/CampusOS/internal/modules/core/userstorage"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
)

type attachmentMetadataDriftStore struct {
	Store
	after func()
}

func (s *attachmentMetadataDriftStore) LockArticleAuthority(ctx context.Context, id string) (*Article, error) {
	return s.Store.(ArticleAuthorityStore).LockArticleAuthority(ctx, id)
}
func (s *attachmentMetadataDriftStore) UpdateArticleIfFacts(ctx context.Context, a *Article, version int64) error {
	return s.Store.(ArticleAuthorityStore).UpdateArticleIfFacts(ctx, a, version)
}

func (s *attachmentMetadataDriftStore) UpdateAttachmentDisplayName(ctx context.Context, article, id, name string) error {
	if err := s.Store.UpdateAttachmentDisplayName(ctx, article, id, name); err != nil {
		return err
	}
	if s.after != nil {
		s.after()
	}
	return nil
}

func TestAttachmentMetadataRejectsExtraCoreArticleAndInventoryChanges(t *testing.T) {
	for _, mode := range []string{"core_body", "core_flags", "article", "other_binding", "asset_status", "disabled"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			s, threads, _, _, events, _ := articleCreateFixture(t)
			adapter, err := corestorage.NewLocalAdapterWithQuota(t.TempDir(), 1024*1024)
			if err != nil {
				t.Fatal(err)
			}
			objects, err := corestorage.NewObjectService(adapter, adapter, corestorage.NewMemoryObjectRepository(), MaxArticleAttachmentBytes)
			if err != nil {
				t.Fatal(err)
			}
			s.SetObjectPort(objects)
			draft, err := s.CreateDraftForPrincipal(ctx, "Owner", articleCreateRequest(), articleCreateProof)
			if err != nil {
				t.Fatal(err)
			}
			pdf := []byte("%PDF-1.7\n1 0 obj\n<<>>\nendobj\n")
			items := []*ArticleAttachment{}
			for _, name := range []string{"a.pdf", "b.pdf"} {
				item, err := s.UploadAttachment(ctx, "1001", draft.ThreadID, name, "application/pdf", int64(len(pdf)), bytes.NewReader(pdf))
				if err != nil {
					t.Fatal(err)
				}
				items = append(items, item)
			}
			store := s.store
			persisted := store.(transaction.Snapshotter)
			beforeThread, beforeArticle, beforeEvents := threads.Snapshot(), persisted.Snapshot(), events.Snapshot()
			ran := false
			s.store = &attachmentMetadataDriftStore{Store: store, after: func() {
				ran = true
				switch mode {
				case "core_body", "core_flags":
					row, _ := threads.GetByID(ctx, draft.ThreadID)
					if mode == "core_body" {
						row.Content = "<p>unexpected body</p>"
					} else {
						row.IsPinned = true
					}
					if err := threads.Update(ctx, row); err != nil {
						t.Fatal(err)
					}
				case "article":
					a, _ := store.GetArticleByThreadID(ctx, draft.ThreadID)
					a.Summary = "unexpected"
					if err := store.UpdateArticle(ctx, a); err != nil {
						t.Fatal(err)
					}
				case "other_binding":
					if err := store.UpdateAttachmentDisplayName(ctx, draft.Article.ID, items[1].ID, "unexpected.pdf"); err != nil {
						t.Fatal(err)
					}
				case "asset_status":
					if err := store.TransitionUserAsset(ctx, items[0].AssetID, "1001", []string{AssetStatusActive}, AssetStatusQuarantined, AssetLifecycleAudit{}); err != nil {
						t.Fatal(err)
					}
				case "disabled":
					s.enabled = func() bool { return false }
				}
			}}
			if ack, err := s.RenameAttachmentForPrincipal(ctx, draft.ThreadID, items[0].ID, "rename.pdf", articleCreateProof); err == nil || ack != nil {
				t.Fatal("extra change accepted", mode, ack, err)
			}
			if !ran {
				t.Fatal("actual mutation callback was not exercised")
			}
			if !reflect.DeepEqual(beforeThread, threads.Snapshot()) || !reflect.DeepEqual(beforeArticle, persisted.Snapshot()) || !reflect.DeepEqual(beforeEvents, events.Snapshot()) {
				t.Fatal("failed final check left changes", mode)
			}
		})
	}
}

func TestAttachmentMetadataCurrentPrincipalAndAuditAtomicity(t *testing.T) {
	for _, operation := range []string{"bind", "remove", "rename", "reorder"} {
		for _, mode := range []string{"current", "other", "admin", "missing", "credential", "audit", "disabled", "trashed"} {
			t.Run(operation+"/"+mode, func(t *testing.T) {
				ctx := context.Background()
				s, threads, cats, _, events, core := articleCreateFixture(t)
				adapter, err := corestorage.NewLocalAdapterWithQuota(t.TempDir(), 1024*1024)
				if err != nil {
					t.Fatal(err)
				}
				objects, err := corestorage.NewObjectService(adapter, adapter, corestorage.NewMemoryObjectRepository(), MaxArticleAttachmentBytes)
				if err != nil {
					t.Fatal(err)
				}
				s.SetObjectPort(objects)
				draft, err := s.CreateDraftForPrincipal(ctx, "Owner", articleCreateRequest(), articleCreateProof)
				if err != nil {
					t.Fatal(err)
				}
				items := []*ArticleAttachment{}
				pdf := []byte("%PDF-1.7\n1 0 obj\n<<>>\nendobj\n")
				for _, name := range []string{"a.pdf", "b.pdf"} {
					item, err := s.UploadAttachment(ctx, "1001", draft.ThreadID, name, "application/pdf", int64(len(pdf)), bytes.NewReader(pdf))
					if err != nil {
						t.Fatal(err)
					}
					items = append(items, item)
				}
				if operation == "bind" {
					if err := s.store.RemoveAttachment(ctx, draft.Article.ID, items[1].ID); err != nil {
						t.Fatal(err)
					}
				}
				proof := identityport.SessionPrincipalResolver(articleCreateProof)
				switch mode {
				case "other":
					proof = func(ctx context.Context) (identityport.PrincipalContext, error) {
						p, _ := articleCreateProof(ctx)
						p.Actor.ID = "1002"
						return p, nil
					}
				case "admin":
					proof = func(ctx context.Context) (identityport.PrincipalContext, error) {
						p, _ := articleCreateProof(ctx)
						p.Actor.Kind, p.Audience, p.AuthenticationStrength = "admin", "admin", "mfa"
						return p, nil
					}
				case "missing":
					proof = nil
				case "credential":
					calls := 0
					proof = func(ctx context.Context) (identityport.PrincipalContext, error) {
						calls++
						p, _ := articleCreateProof(ctx)
						if calls > 1 {
							p.CredentialID = "changed"
						}
						return p, nil
					}
				case "disabled":
					s.enabled = func() bool { return false }
				case "trashed":
					row, _ := threads.GetByID(ctx, draft.ThreadID)
					row.DeletionStatus = "trashed"
					if err := threads.Update(ctx, row); err != nil {
						t.Fatal(err)
					}
				}
				persisted := s.store.(transaction.Snapshotter)
				beforeThread, beforeArticle, beforeEvents := threads.Snapshot(), persisted.Snapshot(), events.Snapshot()
				if mode == "audit" {
					durable := reliability.NewService(transaction.NewMemory(cats, persisted, events), articleCreateFailAudit{events})
					core.SetReliability(durable)
					s.SetReliability(durable)
				}
				var ack *AttachmentMutationAck
				switch operation {
				case "bind":
					ack, err = s.AttachExistingForPrincipal(ctx, draft.ThreadID, items[1].AssetID, "new.pdf", proof)
				case "remove":
					err = s.RemoveAttachmentForPrincipal(ctx, draft.ThreadID, items[0].ID, proof)
				case "rename":
					ack, err = s.RenameAttachmentForPrincipal(ctx, draft.ThreadID, items[0].ID, "renamed.pdf", proof)
				case "reorder":
					ack, err = s.ReorderAttachmentsForPrincipal(ctx, draft.ThreadID, []string{items[1].ID, items[0].ID}, proof)
				}
				if (err == nil) != (mode == "current") {
					t.Fatal(operation, mode, err)
				}
				if mode != "current" {
					if ack != nil || !reflect.DeepEqual(beforeThread, threads.Snapshot()) || !reflect.DeepEqual(beforeArticle, persisted.Snapshot()) || !reflect.DeepEqual(beforeEvents, events.Snapshot()) {
						t.Fatal("failed command changed resource/audit/outbox", mode)
					}
					return
				}
				if operation != "remove" && (ack == nil || ack.ThreadID != draft.ThreadID) {
					t.Fatal("unsafe or missing ack", ack)
				}
				if !reflect.DeepEqual(beforeThread, threads.Snapshot()) {
					t.Fatal("metadata command changed Core")
				}
				rows, err := s.store.ListAttachments(ctx, draft.Article.ID)
				if err != nil {
					t.Fatal(err)
				}
				switch operation {
				case "bind":
					if len(rows) != 2 || rows[1].AssetID != items[1].AssetID || rows[1].DisplayName != "new.pdf" {
						t.Fatal(rows)
					}
				case "remove":
					if len(rows) != 1 || rows[0].ID != items[1].ID {
						t.Fatal(rows)
					}
				case "rename":
					if rows[0].DisplayName != "renamed.pdf" {
						t.Fatal(rows)
					}
				case "reorder":
					if rows[0].ID != items[1].ID || rows[1].ID != items[0].ID {
						t.Fatal(rows)
					}
				}
			})
		}
	}
}
