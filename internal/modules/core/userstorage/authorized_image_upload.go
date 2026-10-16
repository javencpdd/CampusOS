package storage

import (
	"context"
	"errors"
	"fmt"
	identitypolicy "github.com/campusos/CampusOS/internal/modules/core/identity/policy"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/campusos/CampusOS/pkg/idgen"
	"io"
)

func (s *ContentImageStore) SetUploadCommand(reliable *reliability.Service, account InlineImageAccounting) {
	s.reliable, s.account = reliable, account
}

func (s *ContentImageStore) SaveForPrincipal(ctx context.Context, reader io.Reader, resolve identityport.SessionPrincipalResolver) (result *ContentImage, returnErr error) {
	if s == nil || s.reliable == nil || NilImageAdapter(s.account) || transaction.Active(ctx) {
		return nil, ErrObjectUnavailable
	}
	if resolve == nil {
		return nil, identityport.ErrUserSessionPrincipalUnavailable
	}
	p, err := resolve(ctx)
	if err != nil || p.Validate() != nil {
		return nil, errors.Join(identityport.ErrUserSessionPrincipalUnavailable, err)
	}
	request := identityport.InlineImageUploadRequest{RequestID: fmt.Sprint(idgen.New()), PolicyVersion: identityport.InlineImageUploadPolicyVersion, Namespace: "content", OwnerID: p.Actor.ID, Principal: p}
	decision, err := (identitypolicy.InlineImageUploadPolicy{}).DecideInlineImageUpload(request)
	if err != nil {
		return nil, err
	}
	if decision.Effect != "allow" {
		return nil, ErrInlineImageDenied
	}
	file, err := PrepareInlineImage(ctx, reader, s.maxBytes)
	if err != nil {
		return nil, err
	}
	published := false
	undo := func() error { return nil }
	defer func() {
		if !published {
			returnErr = errors.Join(returnErr, file.Discard(), undo())
		}
	}()
	bound := func(ctx context.Context) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		current, err := resolve(ctx)
		if err != nil {
			return errors.Join(identityport.ErrUserSessionPrincipalUnavailable, err)
		}
		fresh := request
		fresh.Principal = current
		return identitypolicy.CheckInlineImageUploadDecision(request, decision, fresh)
	}
	id := fmt.Sprint(idgen.New())
	err = s.reliable.Execute(ctx, reliability.Command{Code: "storage.content_image.upload", ActorType: "user", ActorID: p.Actor.ID, ResourceType: "inline_image", ResourceID: id, OperationCode: "storage.content_image.upload", EventFactory: func() (reliability.Event, error) {
		return reliability.NewEvent("storage.inline_image.created", "inline_image", id, map[string]string{"image_id": id})
	}}, func(tx context.Context) error {
		if err := bound(tx); err != nil {
			return err
		}
		var err error
		undo, err = s.account.AccountInlineImageForCommand(tx, p.Actor.ID, file.Size())
		if err != nil {
			return err
		}
		if err := file.Write(tx, s.storage, p.Actor.ID, "content", ContentImageURLPrefix); err != nil {
			return err
		}
		if err := bound(tx); err != nil {
			return err
		}
		if err := file.Check(tx); err != nil {
			return err
		}
		if err := bound(tx); err != nil {
			return err
		}
		metadata := file.Metadata()
		result = &metadata
		return nil
	})
	if err != nil {
		return nil, err
	}
	published = true
	return result, nil
}
