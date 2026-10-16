package richtext

import (
	"bytes"
	"context"
	"testing"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	corestorage "github.com/campusos/CampusOS/internal/modules/core/userstorage"
)

func inlineImageFixture(t *testing.T, s *Service, owner, category string) (*Asset, *corestorage.ContentImage, *ArticleResult) {
	t.Helper()
	ctx := context.Background()
	adapter, err := corestorage.NewLocalAdapter(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s.contentImages, err = corestorage.NewContentImageStore(adapter, adapter, corestorage.DefaultMaxContentImage)
	if err != nil {
		t.Fatal(err)
	}
	assets, err := NewLocalAssetStoreWithStorage(AssetStoreConfig{}, adapter)
	if err != nil {
		t.Fatal(err)
	}
	s.SetAssetStore(assets)
	a, err := s.UploadAsset(ctx, owner, "legacy.png", bytes.NewReader(tinyPNG), "", "")
	if err != nil {
		t.Fatal(err)
	}
	core, err := s.contentImages.Save(owner, "core.png", bytes.NewReader(tinyPNG))
	if err != nil {
		t.Fatal(err)
	}
	draft, err := s.CreateDraft(ctx, owner, "Image owner", SaveArticleRequest{Title: "Bound image", CategoryID: category, ContentHTML: `<p>private-image-body</p><img src="` + a.FileURL + `"><img src="` + core.FileURL + `">`, CoverURL: a.FileURL})
	if err != nil {
		t.Fatal(err)
	}
	return a, core, draft
}

func TestRichTextInlineImageStateMatrix(t *testing.T) {
	ctx := context.Background()
	s, repo := newTestService(true)
	a, core, draft := inlineImageFixture(t, s, "1001", "1")
	for _, publication := range []domain.PublicationStatus{"draft", "private", "published"} {
		for _, moderation := range []domain.ModerationStatus{"clear", "pending", "rejected", "taken_down"} {
			for _, deletion := range []domain.DeletionStatus{"active", "trashed", "purged"} {
				t.Run(string(publication)+"/"+string(moderation)+"/"+string(deletion), func(t *testing.T) {
					thread, _ := repo.GetByID(ctx, draft.ThreadID)
					thread.PublicationStatus, thread.ModerationStatus, thread.DeletionStatus = publication, moderation, deletion
					if err := repo.Update(ctx, thread); err != nil {
						t.Fatal(err)
					}
					for _, image := range []struct{ name, kind string }{{a.FileName, "richtext"}, {core.FileName, "content"}} {
						pub, err := s.ReadInlineImageForPrincipal(ctx, "1001", image.name, draft.ThreadID, image.kind, nil)
						allow := publication == "published" && moderation == "clear" && deletion == "active"
						if (err == nil) != allow || (err != nil && len(pub.Data) != 0) {
							t.Fatalf("public %s bytes=%d err=%v", image.kind, len(pub.Data), err)
						}
						own, err := s.ReadInlineImageForPrincipal(ctx, "1001", image.name, draft.ThreadID, image.kind, richReadProof("1001"))
						if (err == nil) != (deletion == "active") || (err != nil && len(own.Data) != 0) {
							t.Fatalf("owner %s bytes=%d err=%v", image.kind, len(own.Data), err)
						}
					}
				})
			}
		}
	}
}

func TestRichTextInlineImageBindingsAndBodyRace(t *testing.T) {
	ctx := context.Background()
	for _, mode := range []string{"body", "cover", "metadata", "missing", "duplicate"} {
		t.Run(mode, func(t *testing.T) {
			s, _ := newTestService(true)
			a, _, draft := inlineImageFixture(t, s, "1001", "1")
			if _, err := s.Publish(ctx, draft.ThreadID, "1001"); err != nil {
				t.Fatal(err)
			}
			base := s.store
			if mode == "missing" {
				s.store.(*MemoryStore).mu.Lock()
				delete(s.store.(*MemoryStore).assets, a.ID)
				s.store.(*MemoryStore).mu.Unlock()
			}
			if mode == "duplicate" {
				copy := *a
				copy.ID = "another-image"
				if err := s.store.SaveAsset(ctx, &copy); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "metadata" {
				copy := *a
				copy.FileSize++
				if err := s.store.SaveAsset(ctx, &copy); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "body" || mode == "cover" {
				wrapper := &inlineHookStore{Store: base}
				wrapper.after = func(n int) {
					if n != 1 {
						return
					}
					article, _ := base.GetArticleByThreadID(ctx, draft.ThreadID)
					article.SanitizedHTML = "<p>binding removed</p>"
					article.CoverURL = ""
					if err := base.UpdateArticle(ctx, article); err != nil {
						t.Fatal(err)
					}
				}
				s.store = wrapper
			}
			img, err := s.ReadInlineImageForPrincipal(ctx, "1001", a.FileName, draft.ThreadID, "richtext", nil)
			if err == nil || len(img.Data) != 0 {
				t.Fatalf("stale bytes=%d err=%v", len(img.Data), err)
			}
		})
	}
	s, _ := newTestService(true)
	a, _, draft := inlineImageFixture(t, s, "1001", "1")
	if img, err := s.ReadInlineImageForPrincipal(ctx, "1001", a.FileName, "", "richtext", nil); err == nil || len(img.Data) != 0 {
		t.Fatal("anonymous unbound")
	}
	if img, err := s.ReadInlineImageForPrincipal(ctx, "1001", a.FileName, "", "richtext", richReadProof("1001")); err != nil || len(img.Data) == 0 {
		t.Fatal(err)
	}
	if _, err := s.ReadInlineImageForPrincipal(ctx, "1001", a.FileName, draft.ThreadID, "other", richReadProof("1001")); err == nil {
		t.Fatal("unknown store")
	}
	if _, err := s.ListInlineImagesForPrincipal(ctx, nil); err == nil {
		t.Fatal("unbound inventory")
	}
}

type inlineHookStore struct {
	Store
	gets  int
	after func(int)
}

func (s *inlineHookStore) GetInlineImage(ctx context.Context, owner, name string) (*Asset, error) {
	return s.Store.(inlineImageStore).GetInlineImage(ctx, owner, name)
}
func (s *inlineHookStore) GetArticleByThreadID(ctx context.Context, id string) (*Article, error) {
	a, e := s.Store.GetArticleByThreadID(ctx, id)
	s.gets++
	s.after(s.gets)
	return a, e
}
