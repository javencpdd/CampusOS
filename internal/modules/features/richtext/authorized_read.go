package richtext

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	communityport "github.com/campusos/CampusOS/internal/modules/core/community/port"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	corestorage "github.com/campusos/CampusOS/internal/modules/core/userstorage"
)

type articleReadSnapshot struct {
	BindingFacts []AssetBindingAuthorityFact
	Version      int64
	AuthorID     string
	CategoryID   string
	Publication  domain.PublicationStatus
	Moderation   domain.ModerationStatus
	Deletion     domain.DeletionStatus
	Article      *Article
}

func (s *Service) articleReadSnapshot(ctx context.Context, p identityport.PrincipalContext, threadID string) (articleReadSnapshot, error) {
	if err := s.ensureEnabled(); err != nil {
		return articleReadSnapshot{}, err
	}
	query, ok := s.community.(communityport.AuthenticatedThreadQuery)
	if !ok || nilReadAdapter(query) {
		return articleReadSnapshot{}, identityport.ErrPolicyDecisionMismatch
	}
	thread, err := query.GetThreadForPrincipal(ctx, threadID, identityport.SessionPrincipalResolverFromContext(ctx))
	if err != nil {
		if errors.Is(err, communityport.ErrThreadNotFound) {
			return articleReadSnapshot{}, ErrArticleNotFound
		}
		return articleReadSnapshot{}, err
	}
	if thread == nil || thread.ID != threadID || thread.ThreadType != domain.ThreadTypeArticle || thread.FactsVersion < 1 || thread.DeletionStatus != domain.DeletionStatusActive {
		return articleReadSnapshot{}, ErrArticleNotFound
	}
	article, err := s.store.GetArticleByThreadID(ctx, threadID)
	if err != nil {
		return articleReadSnapshot{}, err
	}
	if article == nil || article.ID == "" || article.ThreadID != threadID || article.CreatedBy != thread.AuthorID || article.Title != thread.Title {
		return articleReadSnapshot{}, identityport.ErrPolicyDecisionMismatch
	}
	var bindingFacts []AssetBindingAuthorityFact
	if p.Actor.Kind == "user" {
		bindingFacts, err = s.store.LoadArticleBindingAuthorityFacts(ctx, article.ID, false)
		if err != nil {
			return articleReadSnapshot{}, err
		}
		attachments := make([]ArticleAttachment, 0, len(bindingFacts))
		for _, fact := range bindingFacts {
			attachment, err := attachmentFromAuthorityFact(fact)
			if err != nil {
				return articleReadSnapshot{}, err
			}
			attachments = append(attachments, *attachment)
		}
		seen := make(map[string]bool, len(attachments))
		for _, a := range attachments {
			if seen[a.ID] || a.ID == "" || a.ArticleContentID != article.ID || a.AssetID != a.Asset.ID ||
				a.Asset.OwnerID != thread.AuthorID || a.Asset.Status != AssetStatusActive || a.Asset.Version < 1 || a.Asset.StorageObjectID == "" || a.Asset.SizeBytes < 0 {
				return articleReadSnapshot{}, identityport.ErrPolicyDecisionMismatch
			}
			seen[a.ID] = true
		}
		article.Attachments = attachments
	} else {
		article.Attachments = nil
	}
	return articleReadSnapshot{BindingFacts: bindingFacts, Version: thread.FactsVersion, AuthorID: thread.AuthorID, CategoryID: thread.CategoryID, Publication: thread.PublicationStatus, Moderation: thread.ModerationStatus, Deletion: thread.DeletionStatus, Article: article}, nil
}

func nilReadAdapter(v any) bool {
	if v == nil {
		return true
	}
	r := reflect.ValueOf(v)
	switch r.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return r.IsNil()
	}
	return false
}

func (s *Service) GetArticleForPrincipal(ctx context.Context, threadID string, resolve identityport.SessionPrincipalResolver) (*Article, error) {
	return identityport.ReadBoundProjection(ctx, resolve, true,
		func(ctx context.Context, p identityport.PrincipalContext) (articleReadSnapshot, error) {
			return s.articleReadSnapshot(ctx, p, threadID)
		},
		func(_ context.Context, _ identityport.PrincipalContext, snapshot articleReadSnapshot) (*Article, error) {
			article := cloneArticle(snapshot.Article)
			applyArticleThreadState(article, &domain.Thread{PublicationStatus: snapshot.Publication, ModerationStatus: snapshot.Moderation, DeletionStatus: snapshot.Deletion})
			return article, nil
		}, nil)
}

func (s *Service) ListAttachmentsForPrincipal(ctx context.Context, threadID string, resolve identityport.SessionPrincipalResolver) ([]ArticleAttachment, error) {
	if resolve == nil {
		return nil, identityport.ErrUserSessionPrincipalUnavailable
	}
	article, err := s.GetArticleForPrincipal(ctx, threadID, resolve)
	if err != nil {
		return nil, err
	}
	return append([]ArticleAttachment{}, article.Attachments...), nil
}

type attachmentReadSnapshot struct {
	Article    articleReadSnapshot
	Attachment ArticleAttachment
	Object     corestorage.Object
}

func (s *Service) attachmentReadSnapshot(ctx context.Context, p identityport.PrincipalContext, threadID, attachmentID string) (attachmentReadSnapshot, error) {
	article, err := s.articleReadSnapshot(ctx, p, threadID)
	if err != nil {
		return attachmentReadSnapshot{}, err
	}
	for _, attachment := range article.Article.Attachments {
		if attachment.ID != attachmentID {
			continue
		}
		object, err := s.assetReadObject(ctx, &attachment.Asset)
		if err != nil {
			return attachmentReadSnapshot{}, err
		}
		return attachmentReadSnapshot{Article: article, Attachment: attachment, Object: object}, nil
	}
	return attachmentReadSnapshot{}, ErrAttachmentNotFound
}

func (s *Service) assetReadObject(ctx context.Context, asset *UserAsset) (corestorage.Object, error) {
	if asset == nil || asset.ID == "" || asset.OwnerID == "" || asset.Status != AssetStatusActive || asset.Version < 1 || asset.StorageObjectID == "" || asset.SizeBytes < 0 {
		return corestorage.Object{}, ErrAssetNotFound
	}
	if nilReadAdapter(s.objects) {
		return corestorage.Object{}, ErrAssetUnavailable
	}
	object, err := s.objects.Stat(ctx, asset.OwnerID, asset.StorageObjectID)
	if err != nil {
		return corestorage.Object{}, err
	}
	if object.ID != asset.StorageObjectID || object.OwnerID != asset.OwnerID || object.Status != corestorage.ObjectStatusReady || object.Version < 1 || object.DeletedAt != nil || object.SizeBytes != asset.SizeBytes || object.MimeType != asset.MimeType {
		return corestorage.Object{}, identityport.ErrPolicyDecisionMismatch
	}
	return object, nil
}

func (s *Service) OpenAttachmentForPrincipal(ctx context.Context, threadID, attachmentID string, resolve identityport.SessionPrincipalResolver) (AttachmentOpen, error) {
	return identityport.ReadBoundProjection(ctx, resolve, false,
		func(ctx context.Context, p identityport.PrincipalContext) (attachmentReadSnapshot, error) {
			return s.attachmentReadSnapshot(ctx, p, threadID, attachmentID)
		},
		func(ctx context.Context, _ identityport.PrincipalContext, snapshot attachmentReadSnapshot) (AttachmentOpen, error) {
			object, err := s.objects.Open(ctx, snapshot.Object.OwnerID, snapshot.Object.ID)
			result := AttachmentOpen{Attachment: snapshot.Attachment, Object: object.Object, Reader: object.Reader}
			if err != nil {
				return result, err
			}
			if object.Reader == nil || !reflect.DeepEqual(object.Object, snapshot.Object) {
				return result, identityport.ErrPolicyFactsChanged
			}
			return result, nil
		}, closeCheckedAttachment)
}

func closeCheckedAttachment(value AttachmentOpen) {
	if value.Reader != nil {
		_ = value.Reader.Close()
	}
}

type personalAssetReadSnapshot struct {
	Facts  json.RawMessage
	Asset  UserAsset
	Object corestorage.Object
}

func (s *Service) personalAssetReadSnapshot(ctx context.Context, p identityport.PrincipalContext, assetID string) (personalAssetReadSnapshot, error) {
	if err := s.ensureEnabled(); err != nil {
		return personalAssetReadSnapshot{}, err
	}
	facts, err := s.store.LoadAssetAuthorityFacts(ctx, strings.TrimSpace(assetID), false)
	if err != nil {
		return personalAssetReadSnapshot{}, err
	}
	asset, err := assetFromAuthorityFact(facts)
	if err != nil {
		return personalAssetReadSnapshot{}, err
	}
	if asset == nil || asset.ID != strings.TrimSpace(assetID) || asset.OwnerID != p.Actor.ID {
		return personalAssetReadSnapshot{}, ErrAssetNotFound
	}
	object, err := s.assetReadObject(ctx, asset)
	if err != nil {
		return personalAssetReadSnapshot{}, err
	}
	return personalAssetReadSnapshot{Facts: facts, Asset: *asset, Object: object}, nil
}

func (s *Service) OpenUserAssetForPrincipal(ctx context.Context, assetID string, resolve identityport.SessionPrincipalResolver) (AttachmentOpen, error) {
	return identityport.ReadBoundProjection(ctx, resolve, false,
		func(ctx context.Context, p identityport.PrincipalContext) (personalAssetReadSnapshot, error) {
			return s.personalAssetReadSnapshot(ctx, p, assetID)
		},
		func(ctx context.Context, _ identityport.PrincipalContext, snapshot personalAssetReadSnapshot) (AttachmentOpen, error) {
			object, err := s.objects.Open(ctx, snapshot.Asset.OwnerID, snapshot.Object.ID)
			result := AttachmentOpen{Attachment: ArticleAttachment{ID: snapshot.Asset.ID, AssetID: snapshot.Asset.ID, DisplayName: snapshot.Asset.OriginalName, Asset: snapshot.Asset}, Object: object.Object, Reader: object.Reader}
			if err != nil {
				return result, err
			}
			if object.Reader == nil || !reflect.DeepEqual(snapshot.Object, object.Object) {
				return result, identityport.ErrPolicyFactsChanged
			}
			return result, nil
		}, closeCheckedAttachment)
}

type assetPageReadSnapshot struct {
	Page  UserAssetPage
	Facts []json.RawMessage
}

func (s *Service) UserAssetPageForPrincipal(ctx context.Context, status, cursor string, limit int, resolve identityport.SessionPrincipalResolver) (UserAssetPage, error) {
	return identityport.ReadBoundProjection(ctx, resolve, false,
		func(ctx context.Context, p identityport.PrincipalContext) (assetPageReadSnapshot, error) {
			if err := s.ensureEnabled(); err != nil {
				return assetPageReadSnapshot{}, err
			}
			if nilReadAdapter(s.store) {
				return assetPageReadSnapshot{}, ErrAssetBindingFactsUnavailable
			}
			if err := s.store.CheckAssetBindingAuthority(ctx); err != nil {
				return assetPageReadSnapshot{}, err
			}
			page, err := s.ListMyUserAssetPage(ctx, p.Actor.ID, status, cursor, limit)
			if err != nil {
				return assetPageReadSnapshot{}, err
			}
			expectedStatus := status
			if expectedStatus == "" {
				expectedStatus = AssetStatusActive
			}
			facts := make([]json.RawMessage, 0, len(page.Items))
			seen := make(map[string]bool, len(page.Items))
			for _, asset := range page.Items {
				if asset == nil || asset.ID == "" || seen[asset.ID] || asset.OwnerID != p.Actor.ID || asset.Version < 1 || (asset.Status != AssetStatusActive && asset.Status != AssetStatusTrashed) || asset.Status != expectedStatus {
					return assetPageReadSnapshot{}, identityport.ErrPolicyDecisionMismatch
				}
				raw, err := s.store.LoadAssetAuthorityFacts(ctx, asset.ID, false)
				if err != nil {
					return assetPageReadSnapshot{}, err
				}
				actual, err := assetFromAuthorityFact(raw)
				if err != nil {
					return assetPageReadSnapshot{}, err
				}
				if !reflect.DeepEqual(actual, canonicalReadAsset(asset)) {
					return assetPageReadSnapshot{}, identityport.ErrPolicyFactsChanged
				}
				facts = append(facts, raw)
				seen[asset.ID] = true
			}
			if err := s.store.CheckAssetBindingAuthority(ctx); err != nil {
				return assetPageReadSnapshot{}, err
			}
			return assetPageReadSnapshot{Page: page, Facts: facts}, nil
		}, func(_ context.Context, _ identityport.PrincipalContext, snapshot assetPageReadSnapshot) (UserAssetPage, error) {
			return snapshot.Page, nil
		}, nil)
}
