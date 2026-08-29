package personaldocuments

import (
	"context"

	corestorage "github.com/campusos/CampusOS/internal/modules/core/userstorage"
)

// ReadPort is the future built-in-module integration seam for private
// documents. Every method remains explicitly owner-scoped. It is deliberately
// not published through the External Plugin Host API or runtime AppContext:
// plugins must not enumerate or open a user's original document bytes.
type ReadPort interface {
	ListOwnDocuments(context.Context, string, string) ([]DocumentDetail, error)
	GetOwnDocument(context.Context, string, string) (DocumentDetail, error)
	OpenOwnDocument(context.Context, string, string) (DocumentDetail, corestorage.ObjectReader, error)
}

type readPort struct{ service *Service }

func (p readPort) ListOwnDocuments(ctx context.Context, owner, status string) ([]DocumentDetail, error) {
	return p.service.List(ctx, owner, status)
}
func (p readPort) GetOwnDocument(ctx context.Context, owner, id string) (DocumentDetail, error) {
	return p.service.Get(ctx, owner, id)
}
func (p readPort) OpenOwnDocument(ctx context.Context, owner, id string) (DocumentDetail, corestorage.ObjectReader, error) {
	return p.service.OpenCurrent(ctx, owner, id)
}

// ReadOnly exposes the draft only to trusted in-process Built-in Features.
// The returned value never changes the service's authorization policy.
func (s *Service) ReadOnly() ReadPort { return readPort{service: s} }
