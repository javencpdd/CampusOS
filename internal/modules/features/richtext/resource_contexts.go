package richtext

import (
	"context"
	"fmt"

	"github.com/campusos/CampusOS/internal/platform/pluginui"
)

// SurfaceValidator is a host-composition callback, not a plugin extension.
type SurfaceValidator func(context.Context, *PluginUIInvocation, string) (string, error)

func resourceFingerprint(opened AttachmentOpen) string {
	return fmt.Sprintf("%s:%d:%s", opened.Object.ID, opened.Object.Version, opened.Object.SHA256)
}

func (s *Service) SetSurfaceValidator(check SurfaceValidator) { s.surfaceValidator = check }

func (s *Service) validateSurface(ctx context.Context, v *PluginUIInvocation, action string) (string, error) {
	if v.PluginKey == PDFViewerPluginKey {
		if err := s.ensurePDFViewerEnabled(); err != nil {
			return "", err
		}
	}
	if s.surfaceValidator != nil {
		return s.surfaceValidator(ctx, v, action)
	}
	// Feature-only test composition cannot mint contexts for arbitrary plugins.
	if v.PluginKey != PDFViewerPluginKey || v.SurfaceID != PDFViewerSurfaceID || action != "" {
		return "", ErrPluginAuthorization
	}
	return "", nil
}

func (s *Service) resourceContexts() pluginui.Service {
	return pluginui.Service{
		Store:   s.store,
		Surface: func(ctx context.Context, v *PluginUIInvocation) (string, error) { return s.validateSurface(ctx, v, "") },
		Resolve: func(ctx context.Context, user string, v *PluginUIInvocation) (string, error) {
			opened, err := s.openInvocationResource(ctx, user, v)
			if err != nil {
				return "", err
			}
			_ = opened.Reader.Close()
			return resourceFingerprint(opened), nil
		},
		Authorize: func(ctx context.Context, v *PluginUIInvocation, operation string) error {
			capability, owner := "personal_space_file.self.read", v.UserID
			if v.ContextKind == InvocationContextArticleAttachment {
				capability, owner = "article_attachment.self.preview", ""
			}
			if err := s.authorizePDFViewer(ctx, PDFViewerAuthorizationInput{PluginKey: v.PluginKey, UserID: v.UserID, ResourceOwnerID: owner, CapabilityCode: capability, OperationCode: operation, Purpose: v.Purpose}); err != nil {
				return err
			}
			return s.authorizePDFViewer(ctx, PDFViewerAuthorizationInput{PluginKey: v.PluginKey, UserID: v.UserID, ResourceOwnerID: v.UserID, CapabilityCode: "plugin_ui.surface.open", OperationCode: "surface.open", Purpose: v.Purpose})
		},
	}
}

// CreateResourceInvocation only accepts the three host-owned resource types.
// The resolver reuses business ACLs; neither SQL nor paths come from a plugin.
func (s *Service) CreateResourceInvocation(ctx context.Context, user string, request PluginUIInvocationRequest) (*PluginUIInvocation, error) {
	if user == "" {
		return nil, ErrPermissionDenied
	}
	if request.ResourceID == "" || !allowedPresentation(request.Presentation) {
		return nil, ErrInvocationPresentation
	}
	v := PluginUIInvocation{UserID: user, PluginKey: request.PluginKey, SurfaceID: request.SurfaceID, ContextKind: request.ResourceType, Presentation: request.Presentation}
	switch request.ResourceType {
	case InvocationContextArticleAttachment:
		article, err := s.store.GetArticleByThreadID(ctx, request.ThreadID)
		if err != nil {
			return nil, err
		}
		attachment, err := s.store.GetAttachment(ctx, article.ID, request.ResourceID)
		if err != nil {
			return nil, err
		}
		v.ArticleContentID, v.AttachmentID, v.AssetID = article.ID, attachment.ID, attachment.AssetID
	case InvocationContextPersonalAsset:
		if request.ThreadID != "" {
			return nil, ErrInvocationNotFound
		}
		v.AssetID = request.ResourceID
	case InvocationContextPersonalDocument:
		if request.ThreadID != "" {
			return nil, ErrInvocationNotFound
		}
		v.PersonalDocumentID = request.ResourceID
	default:
		return nil, ErrInvocationNotFound
	}
	if request.ActionID != "" {
		if _, err := s.validateSurface(ctx, &v, request.ActionID); err != nil {
			return nil, err
		}
	}
	return s.resourceContexts().Issue(ctx, v)
}
