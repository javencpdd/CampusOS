package richtext

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	communityport "github.com/campusos/CampusOS/internal/modules/core/community/port"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	corestorage "github.com/campusos/CampusOS/internal/modules/core/userstorage"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/campusos/CampusOS/pkg/idgen"
)

var ErrAttachmentCompensationPending = errors.New("attachment compensation is pending")

type attachmentUploadFacts struct {
	Metadata *attachmentMetadataFacts
	Object   corestorage.Object
}
type attachmentUploadPart struct {
	attachmentMetadataPart
	preflight   *attachmentMetadataFacts
	object      corestorage.Object
	sourceCheck func(context.Context, bool) error
}

func canonicalAttachmentObject(o corestorage.Object) corestorage.Object {
	o.CreatedAt, o.UpdatedAt = o.CreatedAt.UTC(), o.UpdatedAt.UTC()
	if o.DeletedAt != nil {
		at := o.DeletedAt.UTC()
		o.DeletedAt = &at
	}
	return o
}
func (p *attachmentUploadPart) LoadAuthorFacts(ctx context.Context, t *domain.Thread, lock bool) (any, error) {
	facts, err := p.attachmentMetadataPart.LoadAuthorFacts(ctx, t, lock)
	if err != nil {
		return nil, err
	}
	metadata := facts.(*attachmentMetadataFacts)
	if !reflect.DeepEqual(metadata, p.preflight) {
		return nil, fmt.Errorf("%w: attachment upload metadata changed", identityport.ErrPolicyFactsChanged)
	}
	var current corestorage.Object
	if lock {
		reader, ok := p.s.objects.(corestorage.OwnedObjectCommandLocker)
		if !ok || nilReadAdapter(reader) {
			return nil, ErrAssetUnavailable
		}
		current, err = reader.LockOwnedObjectForCommand(ctx, t.AuthorID, p.object.ID)
	} else {
		current, err = p.s.objects.Stat(ctx, t.AuthorID, p.object.ID)
	}
	if err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(canonicalAttachmentObject(current), p.object) {
		return nil, fmt.Errorf("%w: attachment upload Object receipt changed", identityport.ErrPolicyFactsChanged)
	}
	if p.sourceCheck != nil {
		if err := p.sourceCheck(ctx, lock); err != nil {
			return nil, err
		}
	}
	return attachmentUploadFacts{metadata, p.object}, nil
}
func (p *attachmentUploadPart) ApplyAuthorCommand(ctx context.Context, t *domain.Thread) error {
	if p.initial == nil || p.object.OwnerID != t.AuthorID {
		return identityport.ErrPolicyDecisionMismatch
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	asset := &UserAsset{ID: fmt.Sprint(idgen.New()), OwnerID: t.AuthorID, Kind: AssetKindAttachment,
		OriginalName: p.object.OriginalName, StorageObjectID: p.object.ID, MimeType: p.object.MimeType,
		SizeBytes: p.object.SizeBytes, Status: AssetStatusActive, Version: 1, CreatedAt: now, UpdatedAt: now}
	if err := p.s.store.CreateUserAsset(ctx, asset); err != nil {
		return err
	}
	raw, err := p.s.store.LoadAssetAuthorityFacts(ctx, asset.ID, true)
	if err != nil {
		return err
	}
	actual, err := assetFromAuthorityFact(raw)
	if err != nil || !reflect.DeepEqual(actual, asset) {
		return errors.Join(err, fmt.Errorf("%w: new attachment Asset proof differs", identityport.ErrPolicyFactsChanged))
	}
	// The new row must exactly match the server's staged receipt. The old
	// aggregate stays immutable; only the expected selected asset is added.
	expected := *p.initial
	expected.Selected, expected.Object = raw, &p.object
	p.initial = &expected
	p.operation, p.assetID, p.name = "bind", asset.ID, asset.OriginalName
	if err := p.attachmentMetadataPart.ApplyAuthorCommand(ctx, t); err != nil {
		return fmt.Errorf("new attachment Binding result: %w", err)
	}
	return nil
}
func (p *attachmentUploadPart) CheckAuthorResult(ctx context.Context, t *domain.Thread) error {
	if err := p.attachmentMetadataPart.CheckAuthorResult(ctx, t); err != nil {
		return fmt.Errorf("final attachment upload proof: %w", err)
	}
	if p.sourceCheck != nil {
		return p.sourceCheck(ctx, true)
	}
	return nil
}

func (s *Service) discardAttachmentObject(ctx context.Context, owner string, object corestorage.Object) error {
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	current, err := s.objects.Stat(cleanup, owner, object.ID)
	if err != nil || !reflect.DeepEqual(canonicalAttachmentObject(current), canonicalAttachmentObject(object)) {
		return errors.Join(ErrAttachmentCompensationPending, err)
	}
	if err := s.objects.Delete(cleanup, owner, object.ID, object.Version); err != nil {
		return errors.Join(ErrAttachmentCompensationPending, err)
	}
	return nil
}

func (s *Service) UploadAttachmentForPrincipal(ctx context.Context, thread, originalName, claimedMIME string, size int64, reader io.Reader, resolve identityport.SessionPrincipalResolver) (ack *AttachmentMutationAck, returnErr error) {
	return s.uploadAttachmentForPrincipal(ctx, thread, originalName, claimedMIME, size, reader, resolve, nil, nil, nil)
}

func (s *Service) uploadAttachmentForPrincipal(ctx context.Context, thread, originalName, claimedMIME string, size int64, reader io.Reader, resolve identityport.SessionPrincipalResolver, expected *attachmentMetadataFacts, sourceCheck func(context.Context, bool) error, sourceObject *corestorage.Object) (ack *AttachmentMutationAck, returnErr error) {
	preflight, ok := s.community.(communityport.AuthenticatedThreadAuthorEditPreflight)
	command, commandOK := s.community.(communityport.AuthenticatedThreadAuthorCommands)
	if !ok || !commandOK || nilReadAdapter(preflight) || nilReadAdapter(command) || nilReadAdapter(s.store) || nilReadAdapter(s.objects) || s.reliable == nil || transaction.Active(ctx) {
		return nil, communityport.ErrAuthorCommandUnavailable
	}
	if resolve == nil {
		return nil, identityport.ErrUserSessionPrincipalUnavailable
	}
	if size <= 0 || reader == nil {
		return nil, ErrAttachmentContent
	}
	if size > MaxArticleAttachmentBytes {
		return nil, ErrAssetTooLarge
	}
	principal, err := resolve(ctx)
	if err != nil || principal.Validate() != nil {
		return nil, errors.Join(identityport.ErrUserSessionPrincipalUnavailable, err)
	}
	if principal.Actor.Kind != "user" || principal.Audience != "user" {
		return nil, communityport.ErrAuthorCommandDenied
	}
	bound := func(ctx context.Context) (identityport.PrincipalContext, error) {
		if err := ctx.Err(); err != nil {
			return identityport.PrincipalContext{}, err
		}
		current, err := resolve(ctx)
		if err != nil {
			return current, errors.Join(identityport.ErrUserSessionPrincipalUnavailable, err)
		}
		if current.Validate() != nil || !reflect.DeepEqual(principal, current) {
			return identityport.PrincipalContext{}, identityport.ErrPolicyFactsChanged
		}
		return current, nil
	}
	snapshot := func() (*attachmentMetadataFacts, error) {
		t, err := preflight.LoadThreadAuthorEditProof(ctx, thread, bound)
		if err != nil {
			return nil, err
		}
		if t == nil || t.ThreadType != domain.ThreadTypeArticle || t.AuthorID != principal.Actor.ID {
			return nil, communityport.ErrAuthorCommandDenied
		}
		part := &attachmentMetadataPart{s: s, operation: "upload"}
		facts, err := part.LoadAuthorFacts(ctx, t, false)
		if err != nil {
			return nil, err
		}
		f := facts.(*attachmentMetadataFacts)
		if len(f.Bindings) >= MaxArticleAttachmentCount {
			return nil, ErrAttachmentLimit
		}
		var total int64
		for _, row := range f.Bindings {
			a, err := attachmentFromAuthorityFact(row)
			if err != nil || a.Asset.SizeBytes < 0 || a.Asset.SizeBytes > MaxArticleAttachmentTotalBytes {
				return nil, identityport.ErrPolicyDecisionMismatch
			}
			total += a.Asset.SizeBytes
		}
		if total+size > MaxArticleAttachmentTotalBytes {
			return nil, ErrAttachmentTotalSize
		}
		if _, err := bound(ctx); err != nil {
			return nil, err
		}
		return f, nil
	}
	initial, err := snapshot()
	if err != nil {
		return nil, err
	}
	if expected != nil && !reflect.DeepEqual(expected, initial) {
		return nil, identityport.ErrPolicyFactsChanged
	}
	if sourceCheck != nil {
		if err := sourceCheck(ctx, false); err != nil {
			return nil, err
		}
	}
	buffered := bufio.NewReaderSize(reader, 8192)
	mimeType, err := validateAttachment(buffered, originalName, claimedMIME)
	if err != nil {
		return nil, err
	}
	if current, err := snapshot(); err != nil || !reflect.DeepEqual(current, initial) {
		return nil, errors.Join(identityport.ErrPolicyFactsChanged, err)
	}
	if sourceCheck != nil {
		if err := sourceCheck(ctx, false); err != nil {
			return nil, err
		}
	}
	object, err := s.objects.Put(ctx, principal.Actor.ID, corestorage.PutRequest{Namespace: "richtext", Purpose: "article_attachment",
		OriginalName: filepath.Base(strings.TrimSpace(originalName)), MimeType: mimeType, SizeHint: size, Reader: buffered})
	if err != nil {
		if errors.Is(err, corestorage.ErrObjectQuota) {
			return nil, errors.Join(ErrAssetQuotaExceeded, err)
		}
		return nil, err
	}
	object = canonicalAttachmentObject(object)
	published := false
	defer func() {
		if !published {
			returnErr = errors.Join(returnErr, s.discardAttachmentObject(ctx, principal.Actor.ID, object))
		}
	}()
	if object.ID == "" || object.OwnerID != principal.Actor.ID || object.Namespace != "richtext" || object.Purpose != "article_attachment" || object.Status != corestorage.ObjectStatusReady || object.Version < 1 || object.DeletedAt != nil || object.SizeBytes <= 0 || object.SizeBytes > size || object.MimeType != mimeType || object.OriginalName != filepath.Base(strings.TrimSpace(originalName)) || object.SHA256 == "" {
		return nil, identityport.ErrPolicyDecisionMismatch
	}
	if sourceObject != nil && (object.SizeBytes != sourceObject.SizeBytes || object.SHA256 != sourceObject.SHA256 || object.MimeType != sourceObject.MimeType) {
		return nil, identityport.ErrPolicyFactsChanged
	}
	if err := s.validateOOXMLObject(ctx, principal.Actor.ID, object, mimeType); err != nil {
		return nil, err
	}
	if current, err := snapshot(); err != nil || !reflect.DeepEqual(current, initial) {
		return nil, errors.Join(identityport.ErrPolicyFactsChanged, err)
	}
	if sourceCheck != nil {
		if err := sourceCheck(ctx, false); err != nil {
			return nil, err
		}
	}
	part := &attachmentUploadPart{attachmentMetadataPart: attachmentMetadataPart{s: s, operation: "upload"}, preflight: initial, object: object, sourceCheck: sourceCheck}
	code := "feature.richtext.attachment.upload"
	if sourceObject != nil {
		code = "feature.richtext.attachment.copy_document"
	}
	if err := command.ExecuteThreadAuthorCommand(ctx, thread, "edit", code, "richtext.attachment.updated", bound, part); err != nil {
		return nil, err
	}
	published = true
	return &part.ack, nil
}
