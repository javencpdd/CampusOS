package richtext

import (
	"context"
	"errors"
	"reflect"
	"strings"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	communityport "github.com/campusos/CampusOS/internal/modules/core/community/port"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	corestorage "github.com/campusos/CampusOS/internal/modules/core/userstorage"
	"github.com/campusos/CampusOS/internal/platform/transaction"
)

func (s *Service) AttachPersonalDocumentForPrincipal(ctx context.Context, thread, id, name string, resolve identityport.SessionPrincipalResolver) (*AttachmentMutationAck, error) {
	if s == nil || nilReadAdapter(s.store) || nilReadAdapter(s.objects) {
		return nil, communityport.ErrAuthorCommandUnavailable
	}
	if resolve == nil {
		return nil, identityport.ErrUserSessionPrincipalUnavailable
	}
	if transaction.Active(ctx) || nilReadAdapter(s.personalDocumentAttachmentReader) {
		return nil, communityport.ErrAuthorCommandUnavailable
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, ErrAssetNotFound
	}
	p, err := resolve(ctx)
	if err != nil || p.Validate() != nil {
		return nil, errors.Join(identityport.ErrUserSessionPrincipalUnavailable, err)
	}
	if p.Actor.Kind != "user" || p.Audience != "user" {
		return nil, communityport.ErrAuthorCommandDenied
	}
	bound := func(ctx context.Context) (identityport.PrincipalContext, error) {
		if err := ctx.Err(); err != nil {
			return identityport.PrincipalContext{}, err
		}
		current, err := resolve(ctx)
		if err != nil {
			return current, err
		}
		if current.Validate() != nil || !reflect.DeepEqual(p, current) {
			return identityport.PrincipalContext{}, identityport.ErrPolicyFactsChanged
		}
		return current, nil
	}
	preflight, ok := s.community.(communityport.AuthenticatedThreadAuthorEditPreflight)
	if !ok || nilReadAdapter(preflight) || s.reliable == nil {
		return nil, communityport.ErrAuthorCommandUnavailable
	}
	t, err := preflight.LoadThreadAuthorEditProof(ctx, thread, bound)
	if err != nil {
		return nil, err
	}
	if t == nil || t.ThreadType != domain.ThreadTypeArticle || t.AuthorID != p.Actor.ID {
		return nil, communityport.ErrAuthorCommandDenied
	}
	metadataPart := &attachmentMetadataPart{s: s, operation: "upload"}
	facts, err := metadataPart.LoadAuthorFacts(ctx, t, false)
	if err != nil {
		return nil, err
	}
	expected := facts.(*attachmentMetadataFacts)
	source, err := s.personalDocumentAttachmentReader.OpenOwnDocumentForAttachment(identityport.WithSessionPrincipalResolver(ctx, bound), p.Actor.ID, id)
	if err != nil {
		return nil, err
	}
	if source.Object.Reader != nil {
		defer source.Object.Reader.Close()
	}
	if source.Object.Reader == nil || source.CheckCurrent == nil || source.Version < 1 || source.ID != id {
		return nil, ErrAssetUnavailable
	}
	if source.Format != "pdf" && source.Format != "docx" {
		return nil, ErrAttachmentType
	}
	o := source.Object.Object
	if o.OwnerID != p.Actor.ID || o.ID == "" || o.Namespace != "personal-documents" || o.Purpose != "source."+source.Format || o.Status != corestorage.ObjectStatusReady || o.Version < 1 || o.SizeBytes <= 0 || o.SHA256 == "" || o.DeletedAt != nil {
		return nil, identityport.ErrPolicyDecisionMismatch
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = source.Name
	}
	return s.uploadAttachmentForPrincipal(ctx, thread, name, o.MimeType, o.SizeBytes, source.Object.Reader, bound, expected, source.CheckCurrent, &o)
}
