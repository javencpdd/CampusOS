package richtext

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	corestorage "github.com/campusos/CampusOS/internal/modules/core/userstorage"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"io"
	"reflect"
	"testing"
)

type inlineUploadReader struct {
	io.Reader
	calls int
	after func()
}

func (r *inlineUploadReader) Read(p []byte) (int, error) {
	r.calls++
	n, e := r.Reader.Read(p)
	if r.after != nil {
		after := r.after
		r.after = nil
		after()
	}
	return n, e
}

type inlineUploadAuditFailure struct {
	reliability.Store
	mode string
}

func (s inlineUploadAuditFailure) RecordCommandAudit(ctx context.Context, a reliability.CommandAudit) error {
	if s.mode == "audit" {
		return errors.New("required image audit failed")
	}
	return s.Store.RecordCommandAudit(ctx, a)
}
func (s inlineUploadAuditFailure) Enqueue(ctx context.Context, e *reliability.Event) (*reliability.Event, error) {
	if s.mode == "outbox" {
		return nil, errors.New("required image event failed")
	}
	return s.Store.Enqueue(ctx, e)
}

type inlineUploadStoreHook struct {
	Store
	after func(context.Context, *Asset)
}

func (s *inlineUploadStoreHook) SaveAsset(ctx context.Context, a *Asset) error {
	if err := s.Store.SaveAsset(ctx, a); err != nil {
		return err
	}
	if s.after != nil {
		s.after(ctx, a)
	}
	return nil
}
func (s *inlineUploadStoreHook) GetInlineImage(ctx context.Context, owner, name string) (*Asset, error) {
	return s.Store.(inlineImageStore).GetInlineImage(ctx, owner, name)
}
func (s *inlineUploadStoreHook) LoadInlineImageAuthority(ctx context.Context, id string) (json.RawMessage, error) {
	return s.Store.(inlineImageAuthorityReader).LoadInlineImageAuthority(ctx, id)
}
func (s *inlineUploadStoreHook) LockArticleAuthority(ctx context.Context, id string) (*Article, error) {
	return s.Store.(ArticleAuthorityStore).LockArticleAuthority(ctx, id)
}
func (s *inlineUploadStoreHook) UpdateArticleIfFacts(ctx context.Context, a *Article, version int64) error {
	return s.Store.(ArticleAuthorityStore).UpdateArticleIfFacts(ctx, a, version)
}

func inlineUploadStorage(t *testing.T, s *Service) (*corestorage.LocalAdapter, *corestorage.ObjectService) {
	t.Helper()
	adapter, err := corestorage.NewLocalAdapterWithQuota(t.TempDir(), 1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	assets, err := NewLocalAssetStoreWithStorage(AssetStoreConfig{}, adapter)
	if err != nil {
		t.Fatal(err)
	}
	s.SetAssetStore(assets)
	objects, err := corestorage.NewObjectService(adapter, adapter, corestorage.NewMemoryObjectRepository(), 1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	s.SetObjectPort(objects)
	return adapter, objects
}
func TestInlineImageUploadRichTextCurrentBindingsAndRequiredRollback(t *testing.T) {
	for _, bound := range []bool{false, true} {
		for _, mode := range []string{"current", "admin", "missing", "credential", "audit", "outbox", "quota_lower", "disabled", "article_without_thread", "missing_account", "other_owner", "wrong_article", "trashed"} {
			t.Run(map[bool]string{false: "inventory", true: "article"}[bound]+"/"+mode, func(t *testing.T) {
				ctx := context.Background()
				s, threads, cats, _, events, core := articleCreateFixture(t)
				adapter, objects := inlineUploadStorage(t, s)
				draft, err := s.CreateDraftForPrincipal(ctx, "Owner", articleCreateRequest(), articleCreateProof)
				if err != nil {
					t.Fatal(err)
				}
				thread, article := "", ""
				if bound {
					thread, article = draft.ThreadID, draft.Article.ID
				}
				proof := identityport.SessionPrincipalResolver(articleCreateProof)
				reader := &inlineUploadReader{Reader: bytes.NewReader(tinyPNG)}
				switch mode {
				case "admin":
					proof = func(ctx context.Context) (identityport.PrincipalContext, error) {
						p, e := articleCreateProof(ctx)
						p.Actor.Kind, p.Audience = "admin", "admin"
						return p, e
					}
				case "missing":
					proof = nil
				case "missing_account":
					s.SetObjectPort(nil)
				case "credential":
					changed := false
					reader.after = func() { changed = true }
					proof = func(ctx context.Context) (identityport.PrincipalContext, error) {
						p, e := articleCreateProof(ctx)
						if changed {
							p.CredentialID = "different-session"
						}
						return p, e
					}
				case "audit", "outbox":
					durable := reliability.NewService(transaction.NewMemory(threads, cats, s.store.(transaction.Snapshotter), events), inlineUploadAuditFailure{Store: events, mode: mode})
					s.SetReliability(durable)
					core.SetReliability(durable)
				case "quota_lower":
					if _, err := adapter.SetQuota(ctx, "1001", 1, ""); err != nil {
						t.Fatal(err)
					}
				case "disabled":
					s.SetEnabledChecker(func() bool { return false })
				case "article_without_thread":
					thread, article = "", draft.Article.ID
				case "other_owner":
					if !bound {
						return
					}
					proof = func(ctx context.Context) (identityport.PrincipalContext, error) {
						p, e := articleCreateProof(ctx)
						p.Actor.ID = "1002"
						return p, e
					}
				case "wrong_article":
					if !bound {
						return
					}
					article = "999999"
				case "trashed":
					if !bound {
						return
					}
					row, _ := threads.GetByID(ctx, thread)
					row.DeletionStatus = domain.DeletionStatusTrashed
					if err := threads.Update(ctx, row); err != nil {
						t.Fatal(err)
					}
				}
				before := s.store.(transaction.Snapshotter).Snapshot()
				beforeEvents := events.Snapshot()
				asset, err := s.UploadAssetForPrincipal(ctx, reader, thread, article, proof)
				usage, _ := adapter.Usage("1001")
				ledger, e := objects.Usage(ctx, "1001")
				if e != nil {
					t.Fatal(e)
				}
				if mode == "current" {
					if err != nil || asset == nil || asset.UploaderID != "1001" || asset.ThreadID != thread || asset.ArticleContentID != article || usage != asset.FileSize || ledger.UsedBytes != asset.FileSize {
						t.Fatal(bound, asset, err, usage, ledger)
					}
				} else {
					if err == nil || asset != nil || usage != 0 || ledger.UsedBytes != 0 || !reflect.DeepEqual(before, s.store.(transaction.Snapshotter).Snapshot()) || !reflect.DeepEqual(beforeEvents, events.Snapshot()) {
						t.Fatal("failed image command retained data", bound, mode, asset, err, usage, ledger)
					}
				}
			})
		}
	}
}
func TestInlineImageUploadRichTextPostDecodeAndActualWriteChanges(t *testing.T) {
	for _, mode := range []string{"core_body", "core_counter", "article_title", "credential", "disabled", "after_title", "after_article", "after_session", "after_disabled", "after_metadata"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			s, threads, _, _, events, _ := articleCreateFixture(t)
			adapter, objects := inlineUploadStorage(t, s)
			draft, err := s.CreateDraftForPrincipal(ctx, "Owner", articleCreateRequest(), articleCreateProof)
			if err != nil {
				t.Fatal(err)
			}
			beforeEvents := events.Snapshot()
			reader := &inlineUploadReader{Reader: bytes.NewReader(tinyPNG)}
			changed, ran := false, false
			proof := func(ctx context.Context) (identityport.PrincipalContext, error) {
				p, e := articleCreateProof(ctx)
				if changed && (mode == "credential" || mode == "after_session") {
					return p, errors.New("current session revoked")
				}
				return p, e
			}
			mutate := func(ctx context.Context, a *Asset) {
				ran, changed = true, true
				switch mode {
				case "core_body", "after_title":
					row, e := threads.GetByID(ctx, draft.ThreadID)
					if e != nil {
						t.Fatal(e)
					}
					if mode == "after_title" {
						row.Title = "extra title"
					} else {
						row.Content = "extra body"
					}
					if e = threads.Update(ctx, row); e != nil {
						t.Fatal(e)
					}
				case "core_counter":
					if _, e := threads.IncrementViewCount(ctx, draft.ThreadID); e != nil {
						t.Fatal(e)
					}
				case "article_title", "after_article":
					article, e := s.store.GetArticleByThreadID(ctx, draft.ThreadID)
					if e != nil {
						t.Fatal(e)
					}
					article.Summary = "extra metadata"
					if e = s.store.UpdateArticle(ctx, article); e != nil {
						t.Fatal(e)
					}
				case "disabled", "after_disabled":
					s.SetEnabledChecker(func() bool { return false })
				case "after_metadata":
					copy := cloneAsset(a)
					copy.Width++
					if e := s.store.(*inlineUploadStoreHook).Store.SaveAsset(ctx, copy); e != nil {
						t.Fatal(e)
					}
				}
			}
			if len(mode) > 5 && mode[:6] == "after_" {
				s.store = &inlineUploadStoreHook{Store: s.store, after: mutate}
			} else {
				reader.after = func() { mutate(ctx, nil) }
			}
			asset, err := s.UploadAssetForPrincipal(ctx, reader, draft.ThreadID, draft.Article.ID, proof)
			usage, _ := adapter.Usage("1001")
			ledger, e := objects.Usage(context.Background(), "1001")
			if e != nil {
				t.Fatal(e)
			}
			if !ran {
				t.Fatal("mutation did not execute", mode)
			}
			if mode == "core_counter" {
				if err != nil || asset == nil || usage != asset.FileSize || ledger.UsedBytes != asset.FileSize {
					t.Fatal(mode, asset, err, usage, ledger)
				}
			} else {
				if err == nil || asset != nil || usage != 0 || ledger.UsedBytes != 0 || !reflect.DeepEqual(events.Snapshot(), beforeEvents) {
					t.Fatal("staged/current change accepted", mode, asset, err, usage, ledger)
				}
			}
		})
	}
}
