package richtext

import (
	"context"
	"reflect"

	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	corestorage "github.com/campusos/CampusOS/internal/modules/core/userstorage"
)

type invocationResourceSnapshot struct {
	Kind            string
	Attachment      *attachmentReadSnapshot
	Asset           *personalAssetReadSnapshot
	DocumentID      string
	DocumentVersion int64
	DocumentName    string
	DocumentObject  *corestorage.Object
}

func (s *Service) invocationResourceSnapshot(ctx context.Context, p identityport.PrincipalContext, request PluginUIInvocationRequest) (invocationResourceSnapshot, error) {
	snapshot := invocationResourceSnapshot{Kind: request.ResourceType}
	switch request.ResourceType {
	case InvocationContextArticleAttachment:
		attachment, err := s.attachmentReadSnapshot(ctx, p, request.ThreadID, request.ResourceID)
		if err != nil {
			return snapshot, err
		}
		snapshot.Attachment = &attachment
	case InvocationContextPersonalAsset:
		if request.ThreadID != "" {
			return snapshot, ErrInvocationNotFound
		}
		asset, err := s.personalAssetReadSnapshot(ctx, p, request.ResourceID)
		if err != nil {
			return snapshot, err
		}
		snapshot.Asset = &asset
	case InvocationContextPersonalDocument:
		if request.ThreadID != "" {
			return snapshot, ErrInvocationNotFound
		}
		document, err := s.openPersonalDocumentPDF(ctx, p.Actor.ID, request.ResourceID)
		if err != nil {
			return snapshot, err
		}
		if document.Object.Reader != nil {
			defer document.Object.Reader.Close()
		}
		if document.ID != request.ResourceID || document.Version < 1 || document.Format != "pdf" || document.Object.Reader == nil || document.Object.Object.OwnerID != p.Actor.ID || document.Object.Object.Status != corestorage.ObjectStatusReady || document.Object.Object.Version < 1 {
			return snapshot, ErrInvocationNotFound
		}
		snapshot.DocumentID, snapshot.DocumentVersion, snapshot.DocumentName, snapshot.DocumentObject = document.ID, document.Version, document.Name, &document.Object.Object
	default:
		return snapshot, ErrInvocationNotFound
	}
	return snapshot, nil
}

func (s *Service) CreateInvocationForPrincipal(ctx context.Context, request PluginUIInvocationRequest, resolve identityport.SessionPrincipalResolver) (*PluginUIInvocation, error) {
	return identityport.ReadBoundProjection(ctx, resolve, false,
		func(ctx context.Context, p identityport.PrincipalContext) (invocationResourceSnapshot, error) {
			return s.invocationResourceSnapshot(ctx, p, request)
		},
		func(ctx context.Context, p identityport.PrincipalContext, _ invocationResourceSnapshot) (*PluginUIInvocation, error) {
			return s.CreateResourceInvocation(ctx, p.Actor.ID, request)
		}, nil)
}

type invocationReadSnapshot struct {
	Invocation PluginUIInvocation
	Resource   invocationResourceSnapshot
}

func (s *Service) invocationReadSnapshot(ctx context.Context, p identityport.PrincipalContext, id string) (invocationReadSnapshot, error) {
	v, err := s.store.GetInvocation(ctx, id)
	if err != nil {
		return invocationReadSnapshot{}, err
	}
	if v == nil || v.ID != id || v.UserID != p.Actor.ID || v.RevokedAt != nil || v.PluginKey == "" || v.SurfaceID == "" {
		return invocationReadSnapshot{}, ErrInvocationNotFound
	}
	request := PluginUIInvocationRequest{ResourceType: v.ContextKind}
	switch v.ContextKind {
	case InvocationContextArticleAttachment:
		article, err := s.store.GetArticleByContentID(ctx, v.ArticleContentID)
		if err != nil {
			return invocationReadSnapshot{}, err
		}
		if article == nil || article.ID != v.ArticleContentID || v.AttachmentID == "" || v.AssetID == "" || v.PersonalDocumentID != "" {
			return invocationReadSnapshot{}, ErrInvocationNotFound
		}
		request.ThreadID, request.ResourceID = article.ThreadID, v.AttachmentID
	case InvocationContextPersonalAsset:
		if v.ArticleContentID != "" || v.AttachmentID != "" || v.PersonalDocumentID != "" {
			return invocationReadSnapshot{}, ErrInvocationNotFound
		}
		request.ResourceID = v.AssetID
	case InvocationContextPersonalDocument:
		if v.ArticleContentID != "" || v.AttachmentID != "" || v.AssetID != "" {
			return invocationReadSnapshot{}, ErrInvocationNotFound
		}
		request.ResourceID = v.PersonalDocumentID
	default:
		return invocationReadSnapshot{}, ErrInvocationNotFound
	}
	resource, err := s.invocationResourceSnapshot(ctx, p, request)
	if err != nil {
		return invocationReadSnapshot{}, err
	}
	if resource.Attachment != nil && (resource.Attachment.Attachment.AssetID != v.AssetID || resource.Attachment.Attachment.ArticleContentID != v.ArticleContentID) {
		return invocationReadSnapshot{}, ErrInvocationNotFound
	}
	copy := *v
	copy.OpenedAt = nil // A successful open marks its own observation time.
	return invocationReadSnapshot{Invocation: copy, Resource: resource}, nil
}

type checkedInvocationOpen struct {
	Opened     AttachmentOpen
	Invocation *PluginUIInvocation
}

func (s *Service) OpenInvocationForPrincipal(ctx context.Context, id string, download bool, resolve identityport.SessionPrincipalResolver) (AttachmentOpen, *PluginUIInvocation, error) {
	result, err := identityport.ReadBoundProjection(ctx, resolve, false,
		func(ctx context.Context, p identityport.PrincipalContext) (invocationReadSnapshot, error) {
			return s.invocationReadSnapshot(ctx, p, id)
		},
		func(ctx context.Context, p identityport.PrincipalContext, snapshot invocationReadSnapshot) (checkedInvocationOpen, error) {
			var result checkedInvocationOpen
			var err error
			if download {
				result.Opened, err = s.DownloadPDFInvocation(ctx, p.Actor.ID, id)
				copy := snapshot.Invocation
				result.Invocation = &copy
			} else {
				result.Opened, result.Invocation, err = s.OpenPDFInvocation(ctx, p.Actor.ID, id)
			}
			if err != nil {
				return result, err
			}
			object := snapshot.Resource.DocumentObject
			if snapshot.Resource.Attachment != nil {
				object = &snapshot.Resource.Attachment.Object
			}
			if snapshot.Resource.Asset != nil {
				object = &snapshot.Resource.Asset.Object
			}
			if object == nil || result.Opened.Reader == nil || !reflect.DeepEqual(*object, result.Opened.Object) {
				return result, identityport.ErrPolicyFactsChanged
			}
			return result, nil
		}, func(result checkedInvocationOpen) { closeCheckedAttachment(result.Opened) })
	if err != nil {
		return AttachmentOpen{}, nil, err
	}
	return result.Opened, result.Invocation, nil
}
