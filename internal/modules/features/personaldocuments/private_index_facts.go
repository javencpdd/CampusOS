package personaldocuments

import (
	"context"
	"errors"
	corestorage "github.com/campusos/CampusOS/internal/modules/core/userstorage"
)

// PrivateIndexRevision returns no content or object capability. A trusted
// first-party reconciler uses it only to retire previously selected derivatives.
// User-facing reads must still use ReadOnly/current session Policy.
func (s *Service) PrivateIndexRevision(ctx context.Context, owner, id string) (string, error) {
	if e := s.enabledError(); e != nil {
		return "", e
	}
	d, e := s.repository.Get(ctx, owner, id)
	if e != nil {
		return "", e
	}
	if d.OwnerID != owner || d.Status != StatusActive || d.DeletedAt != nil {
		return "", ErrNotFound
	}
	if d.CurrentVersion == nil {
		return "", ErrNotFound
	}
	object, e := s.objects.Stat(ctx, owner, d.CurrentVersion.SourceObjectID)
	if errors.Is(e, corestorage.ErrObjectNotFound) {
		return "", ErrNotFound
	}
	if e != nil {
		return "", e
	}
	if e = documentObjectMatches(object, owner, *d.CurrentVersion); e != nil {
		return "", ErrNotFound
	}
	return documentRevision(d)
}
func PrivateIndexDocumentRevision(d DocumentDetail) (string, error) { return documentRevision(d) }

// PrivateIndexMetadataForPrincipal checks the same document/version/Object
// policy as original-byte reads without opening the original bytes.
func (s *Service) PrivateIndexMetadataForPrincipal(ctx context.Context, resolve DocumentPrincipalResolver, id string) (DocumentDetail, error) {
	var out DocumentDetail
	e := s.authorizeDocumentRead(ctx, resolve, id, "", func(_ string, d DocumentDetail, _ DocumentVersion) error { out = d; return nil })
	if e != nil {
		return DocumentDetail{}, e
	}
	return out, nil
}
