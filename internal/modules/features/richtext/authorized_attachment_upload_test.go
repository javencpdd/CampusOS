package richtext

import (
	"bytes"
	"context"
	"errors"
	"io"
	"reflect"
	"testing"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	corestorage "github.com/campusos/CampusOS/internal/modules/core/userstorage"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
)

type attachmentUploadObjects struct {
	corestorage.ObjectPort
	puts, deletes int
	afterPut      func(corestorage.Object)
	deleteError   error
}

func (o *attachmentUploadObjects) Put(ctx context.Context, owner string, req corestorage.PutRequest) (corestorage.Object, error) {
	o.puts++
	object, err := o.ObjectPort.Put(ctx, owner, req)
	if err == nil && o.afterPut != nil {
		o.afterPut(object)
	}
	return object, err
}
func (o *attachmentUploadObjects) Delete(ctx context.Context, owner, id string, version int64) error {
	o.deletes++
	if ctx.Err() != nil {
		return errors.New("cleanup used canceled request")
	}
	deadline, ok := ctx.Deadline()
	if !ok || time.Until(deadline) > 5*time.Second {
		return errors.New("cleanup has no bounded deadline")
	}
	if o.deleteError != nil {
		return o.deleteError
	}
	return o.ObjectPort.Delete(ctx, owner, id, version)
}
func (o *attachmentUploadObjects) LockOwnedObjectForCommand(ctx context.Context, owner, id string) (corestorage.Object, error) {
	return o.ObjectPort.(corestorage.OwnedObjectCommandLocker).LockOwnedObjectForCommand(ctx, owner, id)
}

type attachmentUploadCountReader struct {
	io.Reader
	calls int
}

func (r *attachmentUploadCountReader) Read(p []byte) (int, error) {
	r.calls++
	return r.Reader.Read(p)
}
func attachmentUploadObjectFixture(t *testing.T, s *Service) (*attachmentUploadObjects, *corestorage.LocalAdapter, *corestorage.MemoryObjectRepository) {
	t.Helper()
	adapter, err := corestorage.NewLocalAdapterWithQuota(t.TempDir(), 1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	repo := corestorage.NewMemoryObjectRepository()
	objects, err := corestorage.NewObjectService(adapter, adapter, repo, MaxArticleAttachmentBytes)
	if err != nil {
		t.Fatal(err)
	}
	wrapper := &attachmentUploadObjects{ObjectPort: objects}
	s.SetObjectPort(wrapper)
	return wrapper, adapter, repo
}
func TestAttachmentUploadCurrentProofBeforeIOAndAuditRollback(t *testing.T) {
	for _, mode := range []string{"current", "other", "admin", "missing", "credential", "trashed", "disabled", "missing_audit", "audit_failure", "raw_gateway"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			s, threads, cats, _, events, core := articleCreateFixture(t)
			objects, adapter, _ := attachmentUploadObjectFixture(t, s)
			draft, err := s.CreateDraftForPrincipal(ctx, "Owner", articleCreateRequest(), articleCreateProof)
			if err != nil {
				t.Fatal(err)
			}
			proof := identityport.SessionPrincipalResolver(articleCreateProof)
			switch mode {
			case "other", "admin":
				proof = func(ctx context.Context) (identityport.PrincipalContext, error) {
					p, err := articleCreateProof(ctx)
					if mode == "other" {
						p.Actor.ID = "1002"
					} else {
						p.Actor.Kind, p.Audience, p.AuthenticationStrength = "admin", "admin", "mfa"
					}
					return p, err
				}
			case "missing":
				proof = nil
			case "credential":
				calls := 0
				proof = func(ctx context.Context) (identityport.PrincipalContext, error) {
					p, err := articleCreateProof(ctx)
					calls++
					if calls > 1 {
						p.CredentialID = "different-current-session"
					}
					return p, err
				}
			case "trashed":
				row, _ := threads.GetByID(ctx, draft.ThreadID)
				row.DeletionStatus = domain.DeletionStatusTrashed
				if err := threads.Update(ctx, row); err != nil {
					t.Fatal(err)
				}
			case "disabled":
				s.enabled = func() bool { return false }
			case "missing_audit":
				core.SetReliability(nil)
			case "audit_failure":
				durable := reliability.NewService(transaction.NewMemory(cats, s.store.(transaction.Snapshotter), events), articleCreateFailAudit{events})
				core.SetReliability(durable)
				s.SetReliability(durable)
			case "raw_gateway":
				s.community = articleRawGateway{s.community}
			}
			body := []byte("%PDF-1.7\n1 0 obj\n<<>>\nendobj\n")
			reader := &attachmentUploadCountReader{Reader: bytes.NewReader(body)}
			ack, err := s.UploadAttachmentForPrincipal(ctx, draft.ThreadID, "upload.pdf", "application/pdf", int64(len(body)), reader, proof)
			rows, readErr := s.store.LoadArticleBindingAuthorityFacts(ctx, draft.Article.ID, false)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if mode == "current" {
				if err != nil || ack == nil || ack.ThreadID != draft.ThreadID || ack.AttachmentID == "" || len(rows) != 1 || objects.puts != 1 {
					t.Fatal("current upload failed", ack, err, len(rows), objects.puts)
				}
				return
			}
			if err == nil || ack != nil || len(rows) != 0 {
				t.Fatal("denied upload changed metadata", ack, err, len(rows))
			}
			if mode == "audit_failure" {
				if objects.puts != 1 || objects.deletes != 1 {
					t.Fatal("staged audit failure did not compensate", objects.puts, objects.deletes)
				}
			} else if reader.calls != 0 || objects.puts != 0 {
				t.Fatal("failed preflight performed byte I/O", mode, reader.calls, objects.puts)
			}
			usage, usageErr := adapter.Usage("1001")
			if usageErr != nil || usage != 0 {
				t.Fatal("denied upload left provider bytes", usage, usageErr)
			}
		})
	}
}

func TestAttachmentUploadStagedFactsCancelAndChangedReceipt(t *testing.T) {
	for _, mode := range []string{"core_aba", "article_aba", "binding_aba", "credential", "disabled", "cancel", "changed_receipt", "cleanup_failure"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			s, threads, _, _, _, _ := articleCreateFixture(t)
			objects, adapter, repo := attachmentUploadObjectFixture(t, s)
			draft, err := s.CreateDraftForPrincipal(ctx, "Owner", articleCreateRequest(), articleCreateProof)
			if err != nil {
				t.Fatal(err)
			}
			body := []byte("%PDF-1.7\n1 0 obj\n<<>>\nendobj\n")
			old, err := s.UploadAttachment(ctx, "1001", draft.ThreadID, "old.pdf", "application/pdf", int64(len(body)), bytes.NewReader(body))
			if err != nil {
				t.Fatal(err)
			}
			objects.puts = 0
			p, _ := articleCreateProof(ctx)
			proof := func(context.Context) (identityport.PrincipalContext, error) { return p, nil }
			ran := false
			objects.afterPut = func(object corestorage.Object) {
				ran = true
				switch mode {
				case "core_aba":
					row, _ := threads.GetByID(ctx, draft.ThreadID)
					before := row.Content
					row.Content = "<p>changed</p>"
					if err := threads.Update(ctx, row); err != nil {
						t.Fatal(err)
					}
					row.Content = before
					if err := threads.Update(ctx, row); err != nil {
						t.Fatal(err)
					}
				case "article_aba":
					a, _ := s.store.GetArticleByThreadID(ctx, draft.ThreadID)
					before := a.Summary
					a.Summary = "changed"
					if err := s.store.UpdateArticle(ctx, a); err != nil {
						t.Fatal(err)
					}
					a.Summary = before
					if err := s.store.UpdateArticle(ctx, a); err != nil {
						t.Fatal(err)
					}
				case "binding_aba":
					if err := s.store.UpdateAttachmentDisplayName(ctx, draft.Article.ID, old.ID, "changed.pdf"); err != nil {
						t.Fatal(err)
					}
					if err := s.store.UpdateAttachmentDisplayName(ctx, draft.Article.ID, old.ID, old.DisplayName); err != nil {
						t.Fatal(err)
					}
				case "credential":
					p.CredentialID = "new-current-session"
				case "disabled":
					s.enabled = func() bool { return false }
				case "cancel":
					cancel()
				case "changed_receipt":
					if _, err := repo.PrepareDelete(ctx, "1001", object.ID, object.Version); err != nil {
						t.Fatal(err)
					}
				case "cleanup_failure":
					p.CredentialID = "new-current-session"
					objects.deleteError = errors.New("provider deletion refused")
				}
			}
			ack, err := s.UploadAttachmentForPrincipal(ctx, draft.ThreadID, "new.pdf", "application/pdf", int64(len(body)), bytes.NewReader(body), proof)
			if err == nil || ack != nil || !ran || objects.puts != 1 {
				t.Fatal("staged actual drift was not closed", ack, err, ran, objects.puts)
			}
			rows, readErr := s.store.LoadArticleBindingAuthorityFacts(context.Background(), draft.Article.ID, false)
			if readErr != nil || len(rows) != 1 {
				t.Fatal("staged failure published metadata", len(rows), readErr)
			}
			pending := mode == "changed_receipt" || mode == "cleanup_failure"
			if errors.Is(err, ErrAttachmentCompensationPending) != pending {
				t.Fatal("cleanup state was not explicit", mode, err)
			}
			usage, usageErr := adapter.Usage("1001")
			want := int64(len(body))
			if pending {
				want *= 2
			}
			if usageErr != nil || usage != want {
				t.Fatal("wrong receipt was cleaned or left bytes", mode, usage, want, usageErr)
			}
			if mode == "changed_receipt" && objects.deletes != 0 {
				t.Fatal("changed receipt was deleted")
			}
		})
	}
}

type attachmentUploadDriftStore struct {
	Store
	afterCreate func()
}

func (s *attachmentUploadDriftStore) LockArticleAuthority(ctx context.Context, id string) (*Article, error) {
	return s.Store.(ArticleAuthorityStore).LockArticleAuthority(ctx, id)
}
func (s *attachmentUploadDriftStore) UpdateArticleIfFacts(ctx context.Context, a *Article, v int64) error {
	return s.Store.(ArticleAuthorityStore).UpdateArticleIfFacts(ctx, a, v)
}
func (s *attachmentUploadDriftStore) CreateUserAsset(ctx context.Context, a *UserAsset) error {
	if err := s.Store.CreateUserAsset(ctx, a); err != nil {
		return err
	}
	s.afterCreate()
	return nil
}
func TestAttachmentUploadExtraMutationRollsBackWholeCommand(t *testing.T) {
	for _, mode := range []string{"core_flags", "article", "asset"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			s, threads, _, _, events, _ := articleCreateFixture(t)
			objects, adapter, _ := attachmentUploadObjectFixture(t, s)
			draft, err := s.CreateDraftForPrincipal(ctx, "Owner", articleCreateRequest(), articleCreateProof)
			if err != nil {
				t.Fatal(err)
			}
			store := s.store
			beforeThread, beforeStore, beforeEvents := threads.Snapshot(), store.(transaction.Snapshotter).Snapshot(), events.Snapshot()
			ran := false
			s.store = &attachmentUploadDriftStore{Store: store, afterCreate: func() {
				ran = true
				switch mode {
				case "core_flags":
					row, _ := threads.GetByID(ctx, draft.ThreadID)
					row.IsPinned = true
					if err := threads.Update(ctx, row); err != nil {
						t.Fatal(err)
					}
				case "article":
					row, _ := store.GetArticleByThreadID(ctx, draft.ThreadID)
					row.Summary = "unexpected"
					if err := store.UpdateArticle(ctx, row); err != nil {
						t.Fatal(err)
					}
				case "asset":
					page, err := store.ListUserAssetsByOwner(ctx, "1001", []string{AssetStatusActive})
					if err != nil || len(page) != 1 {
						t.Fatal(page, err)
					}
					if err := store.TransitionUserAsset(ctx, page[0].ID, "1001", []string{AssetStatusActive}, AssetStatusQuarantined, AssetLifecycleAudit{}); err != nil {
						t.Fatal(err)
					}
				}
			}}
			body := []byte("%PDF-1.7\n1 0 obj\n<<>>\nendobj\n")
			ack, err := s.UploadAttachmentForPrincipal(ctx, draft.ThreadID, "new.pdf", "application/pdf", int64(len(body)), bytes.NewReader(body), articleCreateProof)
			if err == nil || ack != nil || !ran || objects.deletes != 1 {
				t.Fatal("actual extra mutation did not fail and compensate", mode, ack, err, ran, objects.deletes)
			}
			if !reflect.DeepEqual(beforeThread, threads.Snapshot()) || !reflect.DeepEqual(beforeStore, store.(transaction.Snapshotter).Snapshot()) || !reflect.DeepEqual(beforeEvents, events.Snapshot()) {
				t.Fatal("whole command did not roll back", mode)
			}
			if usage, err := adapter.Usage("1001"); err != nil || usage != 0 {
				t.Fatal(usage, err)
			}
		})
	}
}
