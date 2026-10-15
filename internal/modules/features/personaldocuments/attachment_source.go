package personaldocuments

import (
	"context"
	"errors"
	"reflect"

	identitypolicy "github.com/campusos/CampusOS/internal/modules/core/identity/policy"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	corestorage "github.com/campusos/CampusOS/internal/modules/core/userstorage"
)

var ErrDocumentAttachmentFormat = errors.New("personal document attachment format is unsupported")

// DocumentAttachmentSource is a trusted in-process copy seam. The owning
// service retains immutable expected facts; callers cannot supply new facts.
// CheckCurrent is never a wire field or an external plugin capability.
type DocumentAttachmentSource struct {
	Document     DocumentDetail
	Object       corestorage.ObjectReader
	CheckCurrent func(context.Context, bool) error
}

func detachedAttachmentDocument(d DocumentDetail) DocumentDetail {
	if d.DeletedAt != nil {
		at := *d.DeletedAt
		d.DeletedAt = &at
	}
	if d.CurrentVersion != nil {
		v := *d.CurrentVersion
		d.CurrentVersion = &v
	}
	return d
}
func attachmentSourceObject(o corestorage.Object) corestorage.Object {
	o.CreatedAt, o.UpdatedAt = o.CreatedAt.UTC(), o.UpdatedAt.UTC()
	if o.DeletedAt != nil {
		at := o.DeletedAt.UTC()
		o.DeletedAt = &at
	}
	return o
}
func (s *Service) openOwnAttachmentSource(ctx context.Context, resolve DocumentPrincipalResolver, id string) (DocumentAttachmentSource, error) {
	if s == nil || nilDocumentAdapter(s.repository) || nilDocumentAdapter(s.objects) {
		return DocumentAttachmentSource{}, ErrDocumentCommandUnavailable
	}
	p, err := documentPrincipal(ctx, resolve)
	if err != nil {
		return DocumentAttachmentSource{}, err
	}
	documentLocker, ok := s.repository.(documentAuthorLocker)
	objectLocker, objectOK := s.objects.(corestorage.OwnedObjectCommandLocker)
	if !ok || !objectOK || nilDocumentAdapter(documentLocker) || nilDocumentAdapter(objectLocker) {
		return DocumentAttachmentSource{}, ErrDocumentCommandUnavailable
	}
	bound := func(ctx context.Context) (identityport.PrincipalContext, error) {
		current, err := documentPrincipal(ctx, resolve)
		if err != nil {
			return current, err
		}
		if !reflect.DeepEqual(current, p) {
			return identityport.PrincipalContext{}, identityport.ErrPolicyFactsChanged
		}
		return current, nil
	}
	metadata, err := s.GetForPrincipal(ctx, bound, id)
	if err != nil {
		return DocumentAttachmentSource{}, err
	}
	if metadata.Status != StatusActive {
		return DocumentAttachmentSource{}, ErrNotFound
	}
	if metadata.Format != FormatPDF && metadata.Format != FormatDOCX {
		return DocumentAttachmentSource{}, ErrDocumentAttachmentFormat
	}
	document, opened, err := s.OpenVersionForPrincipal(ctx, bound, id, "")
	if err != nil {
		return DocumentAttachmentSource{}, err
	}
	if !sameDocumentFacts(metadata, document) || opened.Reader == nil {
		if opened.Reader != nil {
			_ = opened.Reader.Close()
		}
		return DocumentAttachmentSource{}, identityport.ErrPolicyFactsChanged
	}
	expected := detachedAttachmentDocument(document)
	expectedObject := attachmentSourceObject(opened.Object)
	request, err := documentReadRequest(expected, p)
	if err != nil {
		_ = opened.Reader.Close()
		return DocumentAttachmentSource{}, err
	}
	decision, err := (identitypolicy.ResourcePolicy{}).DecideResource(request)
	if err != nil || decision.Effect != "allow" {
		_ = opened.Reader.Close()
		return DocumentAttachmentSource{}, errors.Join(ErrNotFound, err)
	}
	check := func(ctx context.Context, lock bool) error {
		currentP, err := bound(ctx)
		if err != nil {
			return err
		}
		if err := s.enabledError(); err != nil {
			return err
		}
		var current DocumentDetail
		if lock {
			current, err = documentLocker.LockDocumentForAuthorCommand(ctx, p.Actor.ID, id, expected.CurrentVersionID)
		} else {
			current, err = s.repository.Get(ctx, p.Actor.ID, id)
		}
		if err != nil {
			return err
		}
		if !sameDocumentFacts(expected, current) {
			return identityport.ErrPolicyFactsChanged
		}
		fresh, err := documentReadRequest(current, currentP)
		if err != nil {
			return err
		}
		fresh.RequestID = request.RequestID
		if err := identitypolicy.CheckResourceDecision(request, decision, fresh); err != nil {
			return err
		}
		selected, err := s.repository.Version(ctx, p.Actor.ID, id, expected.CurrentVersionID)
		if err != nil || !reflect.DeepEqual(canonicalDocumentVersion(selected), canonicalDocumentVersion(*expected.CurrentVersion)) {
			return errors.Join(identityport.ErrPolicyFactsChanged, err)
		}
		var object corestorage.Object
		if lock {
			object, err = objectLocker.LockOwnedObjectForCommand(ctx, p.Actor.ID, expectedObject.ID)
		} else {
			object, err = s.objects.Stat(ctx, p.Actor.ID, expectedObject.ID)
		}
		if err != nil {
			return err
		}
		if err := documentObjectMatches(object, p.Actor.ID, selected); err != nil {
			return err
		}
		if !reflect.DeepEqual(attachmentSourceObject(object), expectedObject) {
			return identityport.ErrPolicyFactsChanged
		}
		if _, err := bound(ctx); err != nil {
			return err
		}
		return s.enabledError()
	}
	if err := check(ctx, false); err != nil {
		_ = opened.Reader.Close()
		return DocumentAttachmentSource{}, err
	}
	return DocumentAttachmentSource{Document: detachedAttachmentDocument(expected), Object: opened, CheckCurrent: check}, nil
}
