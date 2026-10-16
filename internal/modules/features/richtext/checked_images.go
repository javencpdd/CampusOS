package richtext

import (
	"context"
	"errors"
	"fmt"

	editor "github.com/campusos/CampusOS/internal/modules/core/contenteditor"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	corestorage "github.com/campusos/CampusOS/internal/modules/core/userstorage"
)

type inlineImageStore interface {
	GetInlineImage(context.Context, string, string) (*Asset, error)
}

func (s *PgStore) GetInlineImage(ctx context.Context, owner, name string) (*Asset, error) {
	rows, err := s.db(ctx).Query(ctx, `SELECT id::text, COALESCE(thread_id::text,''), COALESCE(article_content_id::text,''),
	 uploader_id::text, file_url, file_name, file_size, mime_type, width, height, created_at
	 FROM richtext_article_assets WHERE uploader_id=$1 AND file_name=$2 LIMIT 2`, owner, name)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result *Asset
	for rows.Next() {
		if result != nil {
			return nil, identityport.ErrPolicyDecisionMismatch
		}
		result = &Asset{}
		if err := rows.Scan(&result.ID, &result.ThreadID, &result.ArticleContentID, &result.UploaderID, &result.FileURL, &result.FileName, &result.FileSize, &result.MimeType, &result.Width, &result.Height, &result.CreatedAt); err != nil {
			return nil, err
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if result == nil {
		return nil, ErrAssetNotFound
	}
	return result, nil
}

func (s *MemoryStore) GetInlineImage(_ context.Context, owner, name string) (*Asset, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var result *Asset
	for _, a := range s.assets {
		if a.UploaderID != owner || a.FileName != name {
			continue
		}
		if result != nil {
			return nil, identityport.ErrPolicyDecisionMismatch
		}
		result = cloneAsset(a)
	}
	if result == nil {
		return nil, ErrAssetNotFound
	}
	return result, nil
}

type richImageSnapshot struct {
	Article articleReadSnapshot
	Asset   *Asset
	Image   corestorage.ImageBytes
}

// ReadInlineImageForPrincipal authorizes the stored article binding for public
// media, or a fresh owner's independent upload inventory when unbound.
func (s *Service) ReadInlineImageForPrincipal(ctx context.Context, owner, name, threadID, kind string, resolve identityport.SessionPrincipalResolver) (corestorage.ImageBytes, error) {
	if kind == "" {
		kind = "richtext"
	}
	if !corestorage.SafeSegment(owner) || !corestorage.SafeSegment(name) || (kind != "content" && kind != "richtext") {
		return corestorage.ImageBytes{}, ErrAssetNotFound
	}
	return identityport.ReadBoundProjection(ctx, resolve, true,
		func(ctx context.Context, p identityport.PrincipalContext) (richImageSnapshot, error) {
			if err := s.ensureEnabled(); err != nil {
				return richImageSnapshot{}, err
			}
			var snap richImageSnapshot
			url := corestorage.ContentImageURLPrefix + "/" + owner + "/" + name
			if kind == "richtext" {
				if s.assets == nil {
					return snap, ErrAssetUnavailable
				}
				store, ok := s.store.(inlineImageStore)
				if !ok || nilReadAdapter(store) {
					return snap, identityport.ErrPolicyDecisionMismatch
				}
				a, err := store.GetInlineImage(ctx, owner, name)
				if err != nil {
					return snap, err
				}
				url = fmt.Sprintf("%s/%s/%s", s.assets.cfg.URLPrefix, owner, name)
				if a == nil || a.ID == "" || a.UploaderID != owner || a.FileName != name || a.FileURL != url {
					return snap, ErrAssetNotFound
				}
				snap.Asset = a
			}
			if threadID == "" {
				if p.Actor.Kind != "user" || p.Actor.ID != owner {
					return snap, ErrAssetNotFound
				}
			} else {
				article, err := s.articleReadSnapshot(ctx, p, threadID)
				if err != nil {
					return snap, err
				}
				if article.AuthorID != owner || (!editor.ReferencesImage(article.Article.SanitizedHTML, url) && article.Article.CoverURL != url) {
					return snap, ErrAssetNotFound
				}
				snap.Article = article
			}
			var path string
			var err error
			var max int64
			if kind == "content" {
				if s.contentImages == nil {
					return snap, ErrAssetUnavailable
				}
				path, err = s.contentImages.Path(owner, name)
				max = s.contentImages.MaxBytes()
			} else {
				path, err = s.assets.Path(owner, name)
				max = s.assets.MaxAssetBytes()
			}
			if err != nil {
				return snap, ErrAssetNotFound
			}
			snap.Image, err = corestorage.ReadImageBytes(path, max)
			if err != nil {
				if errors.Is(err, corestorage.ErrImageNotFound) {
					return snap, ErrAssetNotFound
				}
				return snap, err
			}
			if snap.Asset != nil && (snap.Asset.FileSize != int64(len(snap.Image.Data)) || snap.Asset.MimeType != snap.Image.MimeType) {
				return snap, identityport.ErrPolicyDecisionMismatch
			}
			return snap, nil
		}, func(_ context.Context, _ identityport.PrincipalContext, snap richImageSnapshot) (corestorage.ImageBytes, error) {
			image := snap.Image
			image.Data = append([]byte(nil), image.Data...)
			return image, nil
		}, nil)
}

func (s *Service) ListInlineImagesForPrincipal(ctx context.Context, resolve identityport.SessionPrincipalResolver) ([]*Asset, error) {
	return identityport.ReadBoundProjection(ctx, resolve, false,
		func(ctx context.Context, p identityport.PrincipalContext) ([]*Asset, error) {
			items, err := s.ListMyAssets(ctx, p.Actor.ID)
			if err != nil {
				return nil, err
			}
			seen := map[string]bool{}
			for _, a := range items {
				if a == nil || a.ID == "" || a.UploaderID != p.Actor.ID || seen[a.ID] || a.FileSize < 1 {
					return nil, identityport.ErrPolicyDecisionMismatch
				}
				seen[a.ID] = true
			}
			return items, nil
		}, func(_ context.Context, _ identityport.PrincipalContext, items []*Asset) ([]*Asset, error) {
			return items, nil
		}, nil)
}
