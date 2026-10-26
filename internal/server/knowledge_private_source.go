package server

import (
	"context"
	"errors"

	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/modules/features/knowledge"
	"github.com/campusos/CampusOS/internal/modules/features/personaldocuments"
	"github.com/campusos/CampusOS/pkg/apperror"
)

// privateDocumentMetadataAdapter is a composition-only boundary. Knowledge
// receives current, owner-checked metadata, never an object reader, a path, the
// document repository, or original private bytes.
type privateDocumentMetadataAdapter struct {
	module  *personaldocuments.Module
	enabled func() bool
}

var _ knowledge.PrivateDocumentMetadataReader = privateDocumentMetadataAdapter{}

func (a privateDocumentMetadataAdapter) GetOwnDocumentMetadata(ctx context.Context, resolve identityport.SessionPrincipalResolver, id string) (knowledge.PrivateDocumentMetadata, error) {
	if a.enabled == nil {
		return knowledge.PrivateDocumentMetadata{}, knowledge.ErrPrivateUnavailable
	}
	if !a.enabled() {
		return knowledge.PrivateDocumentMetadata{}, knowledge.ErrDisabled
	}
	if a.module == nil || a.module.Service() == nil {
		return knowledge.PrivateDocumentMetadata{}, knowledge.ErrPrivateUnavailable
	}
	d, err := a.module.Service().ReadOnly().GetOwnDocument(ctx, resolve, id)
	if !a.enabled() {
		return knowledge.PrivateDocumentMetadata{}, knowledge.ErrDisabled
	}
	if err != nil {
		switch {
		case errors.Is(err, identityport.ErrUserSessionPrincipalUnavailable):
			return knowledge.PrivateDocumentMetadata{}, identityport.ErrUserSessionPrincipalUnavailable
		case errors.Is(err, identityport.ErrPolicyFactsChanged):
			return knowledge.PrivateDocumentMetadata{}, knowledge.ErrSourceChanged
		case errors.Is(err, personaldocuments.ErrNotFound):
			return knowledge.PrivateDocumentMetadata{}, knowledge.ErrPrivateSourceMissing
		case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
			return knowledge.PrivateDocumentMetadata{}, err
		default:
			var public *apperror.AppError
			if errors.As(err, &public) && public.Descriptor().MachineCode == apperror.PersonalDocumentDisabled.MachineCode {
				return knowledge.PrivateDocumentMetadata{}, knowledge.ErrDisabled
			}
			return knowledge.PrivateDocumentMetadata{}, knowledge.ErrPrivateUnavailable
		}
	}
	// Tombstone metadata is intentionally visible to its owner in Documents,
	// but it cannot become a private-index source. Versions and status are
	// revalidated by the owning module before this projection is returned.
	if d.Status != personaldocuments.StatusActive || d.DeletedAt != nil || d.CurrentVersion == nil ||
		d.ID != id || d.CurrentVersionID == "" || d.CurrentVersion.ID != d.CurrentVersionID ||
		d.CurrentVersion.DocumentID != d.ID || d.CurrentVersion.Format != d.Format {
		return knowledge.PrivateDocumentMetadata{}, knowledge.ErrPrivateSourceMissing
	}
	return knowledge.PrivateDocumentMetadata{DocumentID: d.ID, VersionID: d.CurrentVersionID, Format: d.Format}, nil
}
