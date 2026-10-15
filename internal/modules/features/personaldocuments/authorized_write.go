package personaldocuments

import (
	"context"
	"errors"
	"io"
	"reflect"
	"time"

	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	corestorage "github.com/campusos/CampusOS/internal/modules/core/userstorage"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/pkg/apperror"
)

var ErrDocumentCommandUnavailable = errors.New("document author command is unavailable")
var ErrDocumentCompensationPending = errors.New("document staged object cleanup is pending")

type documentWriteKey struct{}
type documentWriteProof struct {
	principal identityport.PrincipalContext
	resolve   DocumentPrincipalResolver
	operation string
	initial   *DocumentDetail
	selected  *DocumentVersion
	source    *corestorage.Object
	staged    *corestorage.Object
}

// This is an owner-write rule, separate from personal.document.read. Ingress
// provides only verified session resolution; all resource facts come from the
// owning repository. A staged object is outside SQL and is compensated on any
// failed command using its exact receipt. A changed receipt is retained for
// reconciliation; document/version/preview/audit/outbox remain atomic.
func (s *Service) documentWriteContext(ctx context.Context, resolve DocumentPrincipalResolver, operation, id string, expected int64) (context.Context, *documentWriteProof, error) {
	p, err := documentPrincipal(ctx, resolve)
	if err != nil {
		return ctx, nil, err
	}
	if err = s.enabledError(); err != nil {
		return ctx, nil, err
	}
	if s.reliable == nil {
		return ctx, nil, ErrDocumentCommandUnavailable
	}
	if _, ok := s.repository.(documentAuthorLocker); !ok {
		return ctx, nil, ErrDocumentCommandUnavailable
	}
	if _, ok := s.objects.(corestorage.OwnedObjectCommandLocker); !ok {
		return ctx, nil, ErrDocumentCommandUnavailable
	}
	proof := &documentWriteProof{principal: p, resolve: resolve, operation: operation}
	if id != "" {
		d, err := s.repository.Get(ctx, p.Actor.ID, id)
		if err != nil {
			return ctx, nil, s.translateError(err)
		}
		if _, err = documentRevision(d); err != nil {
			return ctx, nil, identityport.ErrPolicyFactsChanged
		}
		if d.ID != id || d.OwnerID != p.Actor.ID {
			return ctx, nil, ErrNotFound
		}
		if expected < 1 {
			return ctx, nil, s.public(ErrInvalid, apperror.PersonalDocumentInvalid, map[string]any{"field": "expected_version"})
		}
		if d.Version != expected {
			return ctx, nil, s.translateError(ErrConflict)
		}
		required := StatusActive
		if operation == "restore" {
			required = StatusTrashed
		}
		if d.Status != required {
			return ctx, nil, s.translateError(ErrNotEditable)
		}
		proof.initial = &d
	}
	return context.WithValue(ctx, documentWriteKey{}, proof), proof, nil
}

func (s *Service) CreateTextForPrincipal(ctx context.Context, resolve DocumentPrincipalResolver, req CreateRequest) (DocumentDetail, error) {
	ctx, p, err := s.documentWriteContext(ctx, resolve, "create", "", 0)
	if err != nil {
		return DocumentDetail{}, err
	}
	return s.CreateText(ctx, p.principal.Actor.ID, req)
}

func (s *Service) UploadForPrincipal(ctx context.Context, resolve DocumentPrincipalResolver, name, format, mime string, size int64, reader io.Reader) (DocumentDetail, error) {
	ctx, p, err := s.documentWriteContext(ctx, resolve, "upload", "", 0)
	if err != nil {
		return DocumentDetail{}, err
	}
	return s.Upload(ctx, p.principal.Actor.ID, name, format, mime, size, reader)
}

func (s *Service) SaveForPrincipal(ctx context.Context, resolve DocumentPrincipalResolver, id string, req SaveRequest) (DocumentDetail, error) {
	ctx, p, err := s.documentWriteContext(ctx, resolve, "save", id, req.ExpectedVersion)
	if err != nil {
		return DocumentDetail{}, err
	}
	return s.Save(ctx, p.principal.Actor.ID, id, req)
}

func (s *Service) RestoreVersionForPrincipal(ctx context.Context, resolve DocumentPrincipalResolver, id, versionID string, expected int64) (DocumentDetail, error) {
	ctx, p, err := s.documentWriteContext(ctx, resolve, "restore_version", id, expected)
	if err != nil {
		return DocumentDetail{}, err
	}
	v, err := s.repository.Version(ctx, p.principal.Actor.ID, id, versionID)
	if err != nil {
		return DocumentDetail{}, s.translateError(err)
	}
	if v.ID != versionID || v.DocumentID != id || v.Format != p.initial.Format || v.VersionNumber < 1 || v.SizeBytes < 0 || v.SizeBytes > limitFor(v.Format) || v.SHA256 == "" {
		return DocumentDetail{}, identityport.ErrPolicyFactsChanged
	}
	o, err := s.objects.Stat(ctx, p.principal.Actor.ID, v.SourceObjectID)
	if err != nil {
		return DocumentDetail{}, s.objectError(err, limitFor(v.Format), p.principal.Actor.ID, v.SizeBytes)
	}
	if o.OwnerID != p.principal.Actor.ID || o.ID != v.SourceObjectID || o.Namespace != "personal-documents" || o.Purpose != "source."+v.Format || o.Status != corestorage.ObjectStatusReady || o.DeletedAt != nil || o.Version < 1 || o.SizeBytes != v.SizeBytes || o.SHA256 != v.SHA256 {
		return DocumentDetail{}, identityport.ErrPolicyFactsChanged
	}
	p.selected, p.source = &v, &o
	return s.RestoreVersion(ctx, p.principal.Actor.ID, id, versionID, expected)
}

func (s *Service) SetStatusForPrincipal(ctx context.Context, resolve DocumentPrincipalResolver, id string, expected int64, status string) (DocumentDetail, error) {
	operation := "trash"
	if status == StatusActive {
		operation = "restore"
	} else if status != StatusTrashed {
		return DocumentDetail{}, s.public(ErrInvalid, apperror.PersonalDocumentInvalid, nil)
	}
	ctx, p, err := s.documentWriteContext(ctx, resolve, operation, id, expected)
	if err != nil {
		return DocumentDetail{}, err
	}
	owner := p.principal.Actor.ID
	var result DocumentDetail
	err = s.reliable.Execute(ctx, reliability.Command{Code: "personal_documents.author." + operation, ActorID: owner, ActorType: "user", ResourceType: "personal_document", ResourceID: id, OperationCode: "document.author." + operation}, func(txCtx context.Context) error {
		if err := s.checkDocumentWrite(txCtx, p, owner, true); err != nil {
			return err
		}
		var err error
		result, err = s.repository.SetStatus(txCtx, owner, id, expected, status)
		if err != nil {
			return err
		}
		if _, err = documentRevision(result); err != nil {
			return identityport.ErrPolicyFactsChanged
		}
		want := *p.initial
		want.Status = status
		want.Version++
		want.UpdatedAt = result.UpdatedAt
		want.DeletedAt = result.DeletedAt
		if !sameDocumentFacts(want, result) {
			return identityport.ErrPolicyFactsChanged
		}
		actual, err := s.repository.Get(txCtx, owner, id)
		if err != nil || !sameDocumentFacts(result, actual) {
			return identityport.ErrPolicyFactsChanged
		}
		return s.checkDocumentWrite(txCtx, p, owner, false)
	})
	if err != nil {
		return DocumentDetail{}, s.translateError(err)
	}
	if status == StatusTrashed {
		result.CurrentVersion = nil
		result.CurrentVersionID = ""
	}
	return result, nil
}

func (s *Service) checkDocumentWrite(ctx context.Context, proof *documentWriteProof, owner string, lock bool) error {
	if proof == nil || owner != proof.principal.Actor.ID {
		return identityport.ErrPolicyFactsChanged
	}
	p, err := documentPrincipal(ctx, proof.resolve)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(p, proof.principal) {
		return identityport.ErrUserSessionPrincipalUnavailable
	}
	if err = s.enabledError(); err != nil {
		return err
	}
	if lock && proof.initial != nil {
		selected := ""
		if proof.selected != nil {
			selected = proof.selected.ID
		}
		actual, err := s.repository.(documentAuthorLocker).LockDocumentForAuthorCommand(ctx, owner, proof.initial.ID, selected)
		if err != nil {
			return err
		}
		if !sameDocumentFacts(*proof.initial, actual) {
			return identityport.ErrPolicyFactsChanged
		}
		if proof.selected != nil {
			v, err := s.repository.Version(ctx, owner, actual.ID, selected)
			if err != nil || !reflect.DeepEqual(canonicalDocumentVersion(v), canonicalDocumentVersion(*proof.selected)) {
				return identityport.ErrPolicyFactsChanged
			}
		}
	}
	locker := s.objects.(corestorage.OwnedObjectCommandLocker)
	for _, object := range []*corestorage.Object{proof.source, proof.staged} {
		if object == nil {
			continue
		}
		var current corestorage.Object
		if lock {
			current, err = locker.LockOwnedObjectForCommand(ctx, owner, object.ID)
		} else {
			current, err = s.objects.Stat(ctx, owner, object.ID)
		}
		if err != nil || !reflect.DeepEqual(*object, current) {
			return identityport.ErrPolicyFactsChanged
		}
	}
	return nil
}

func canonicalDocumentVersion(v DocumentVersion) DocumentVersion {
	v.CreatedAt = v.CreatedAt.UTC().Truncate(time.Microsecond)
	return v
}
func canonicalDocument(d DocumentDetail) DocumentDetail {
	d.CreatedAt = d.CreatedAt.UTC().Truncate(time.Microsecond)
	d.UpdatedAt = d.UpdatedAt.UTC().Truncate(time.Microsecond)
	if d.DeletedAt != nil {
		x := d.DeletedAt.UTC().Truncate(time.Microsecond)
		d.DeletedAt = &x
	}
	if d.CurrentVersion != nil {
		v := canonicalDocumentVersion(*d.CurrentVersion)
		d.CurrentVersion = &v
	}
	return d
}
func sameDocumentFacts(a, b DocumentDetail) bool {
	return reflect.DeepEqual(canonicalDocument(a), canonicalDocument(b))
}

func (s *Service) checkDocumentVersionResult(ctx context.Context, p *documentWriteProof, owner, id string, v DocumentVersion, result DocumentDetail) error {
	if p.staged == nil || result.ID != id || result.OwnerID != owner || result.Status != StatusActive || result.CurrentVersion == nil {
		return identityport.ErrPolicyFactsChanged
	}
	if _, err := documentRevision(result); err != nil {
		return identityport.ErrPolicyFactsChanged
	}
	current := result.CurrentVersion
	if current.ID != v.ID || current.DocumentID != id || current.SourceObjectID != p.staged.ID || current.Format != v.Format || current.SizeBytes != p.staged.SizeBytes || current.SHA256 != p.staged.SHA256 || current.RestoredFromVersionID != v.RestoredFromVersionID {
		return identityport.ErrPolicyFactsChanged
	}
	if p.initial == nil {
		if result.Version != 1 || current.VersionNumber != 1 {
			return identityport.ErrPolicyFactsChanged
		}
	} else if result.Version != p.initial.Version+1 || current.VersionNumber <= p.initial.CurrentVersion.VersionNumber {
		return identityport.ErrPolicyFactsChanged
	}
	if p.selected != nil && (current.SHA256 != p.selected.SHA256 || current.SizeBytes != p.selected.SizeBytes || current.RestoredFromVersionID != p.selected.ID) {
		return identityport.ErrPolicyFactsChanged
	}
	actual, err := s.repository.Get(ctx, owner, id)
	if err != nil || !sameDocumentFacts(result, actual) {
		return identityport.ErrPolicyFactsChanged
	}
	return s.checkDocumentWrite(ctx, p, owner, false)
}

func (s *Service) discardStagedObject(ctx context.Context, owner string, object corestorage.Object) error {
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := s.objects.Delete(cleanup, owner, object.ID, object.Version); err != nil {
		return errors.Join(ErrDocumentCompensationPending, err)
	}
	return nil
}
