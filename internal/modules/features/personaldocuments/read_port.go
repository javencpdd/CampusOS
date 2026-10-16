package personaldocuments

import (
	"context"

	corestorage "github.com/campusos/CampusOS/internal/modules/core/userstorage"
)

// ReadPort is the built-in-module integration seam for private
// documents. Every method requires a fresh user-session resolver. It is deliberately
// not published through the External Plugin Host API or runtime AppContext:
// plugins must not enumerate or open a user's original document bytes.
type ReadPort interface {
	ListOwnDocuments(context.Context, DocumentPrincipalResolver, string) ([]DocumentDetail, error)
	GetOwnDocument(context.Context, DocumentPrincipalResolver, string) (DocumentDetail, error)
	OpenOwnDocument(context.Context, DocumentPrincipalResolver, string) (DocumentDetail, corestorage.ObjectReader, error)
	OpenOwnAttachmentSource(context.Context, DocumentPrincipalResolver, string) (DocumentAttachmentSource, error)
}

type readPort struct{ service *Service }

func (p readPort) ListOwnDocuments(ctx context.Context, resolve DocumentPrincipalResolver, status string) ([]DocumentDetail, error) {
	return p.service.ListForPrincipal(ctx, resolve, status)
}
func (p readPort) GetOwnDocument(ctx context.Context, resolve DocumentPrincipalResolver, id string) (DocumentDetail, error) {
	return p.service.GetForPrincipal(ctx, resolve, id)
}
func (p readPort) OpenOwnDocument(ctx context.Context, resolve DocumentPrincipalResolver, id string) (DocumentDetail, corestorage.ObjectReader, error) {
	return p.service.OpenVersionForPrincipal(ctx, resolve, id, "")
}
func (p readPort) OpenOwnAttachmentSource(ctx context.Context, resolve DocumentPrincipalResolver, id string) (DocumentAttachmentSource, error) {
	return p.service.openOwnAttachmentSource(ctx, resolve, id)
}

// ReadOnly exposes the checked reader only to trusted in-process Built-in Features.
// The returned value never changes the service's authorization policy.
func (s *Service) ReadOnly() ReadPort { return readPort{service: s} }
