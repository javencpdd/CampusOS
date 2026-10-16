package community

import (
	"context"
	"errors"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	communityport "github.com/campusos/CampusOS/internal/modules/core/community/port"
	"github.com/campusos/CampusOS/internal/modules/core/community/service"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
)

func createFeatureForPrincipal(ctx context.Context, svc *service.ThreadService, name string, request domain.CreateThreadRequest, opts communityport.ThreadCreateOptions, participant communityport.StructuredThreadParticipant, resolve identityport.SessionPrincipalResolver) (*domain.Thread, error) {
	if svc == nil {
		return nil, communityport.ErrAuthorCommandUnavailable
	}
	result, err := svc.CreateStructuredThreadForPrincipal(ctx, name, request, service.CreateThreadOptions{Status: opts.Status, ContentFormat: opts.ContentFormat, ThreadType: opts.ThreadType, CommandCode: opts.CommandCode, EventType: opts.EventType}, participant, resolve)
	if errors.Is(err, service.ErrThreadOwnerPrincipalUnavailable) {
		return nil, identityport.ErrUserSessionPrincipalUnavailable
	}
	return result, err
}

func (g *moduleContentGateway) CreateStructuredThreadForPrincipal(ctx context.Context, name string, request domain.CreateThreadRequest, opts communityport.ThreadCreateOptions, participant communityport.StructuredThreadParticipant, resolve identityport.SessionPrincipalResolver) (*domain.Thread, error) {
	delegate, err := g.gateway()
	if err != nil {
		return nil, communityport.ErrAuthorCommandUnavailable
	}
	return createFeatureForPrincipal(ctx, delegate.threadSvc, name, request, opts, participant, resolve)
}

func (g *contentGateway) CreateStructuredThreadForPrincipal(ctx context.Context, name string, request domain.CreateThreadRequest, opts communityport.ThreadCreateOptions, participant communityport.StructuredThreadParticipant, resolve identityport.SessionPrincipalResolver) (*domain.Thread, error) {
	if g == nil {
		return nil, communityport.ErrAuthorCommandUnavailable
	}
	return createFeatureForPrincipal(ctx, g.threadSvc, name, request, opts, participant, resolve)
}

func (g *moduleContentGateway) ExecuteThreadAuthorCommand(ctx context.Context, id, operation, code, event string, resolve identityport.SessionPrincipalResolver, part communityport.ThreadAuthorParticipant) error {
	delegate, err := g.gateway()
	if err != nil {
		return communityport.ErrAuthorCommandUnavailable
	}
	return executeFeatureAuthorCommand(ctx, delegate.threadSvc, id, operation, code, event, resolve, part)
}
func (g *contentGateway) ExecuteThreadAuthorCommand(ctx context.Context, id, operation, code, event string, resolve identityport.SessionPrincipalResolver, part communityport.ThreadAuthorParticipant) error {
	if g == nil {
		return communityport.ErrAuthorCommandUnavailable
	}
	return executeFeatureAuthorCommand(ctx, g.threadSvc, id, operation, code, event, resolve, part)
}
func executeFeatureAuthorCommand(ctx context.Context, svc *service.ThreadService, id, operation, code, event string, resolve identityport.SessionPrincipalResolver, part communityport.ThreadAuthorParticipant) error {
	if svc == nil {
		return communityport.ErrAuthorCommandUnavailable
	}
	err := svc.ExecuteThreadAuthorCommand(ctx, id, operation, code, event, resolve, part)
	err = normalizePublicContentError(err)
	if errors.Is(err, service.ErrThreadOwnerPrincipalUnavailable) {
		return identityport.ErrUserSessionPrincipalUnavailable
	}
	return err
}

func (g *moduleContentGateway) LoadThreadAuthorEditProof(ctx context.Context, id string, resolve identityport.SessionPrincipalResolver) (*domain.Thread, error) {
	delegate, err := g.gateway()
	if err != nil {
		return nil, communityport.ErrAuthorCommandUnavailable
	}
	return loadFeatureAuthorEditProof(ctx, delegate.threadSvc, id, resolve)
}
func (g *contentGateway) LoadThreadAuthorEditProof(ctx context.Context, id string, resolve identityport.SessionPrincipalResolver) (*domain.Thread, error) {
	if g == nil {
		return nil, communityport.ErrAuthorCommandUnavailable
	}
	return loadFeatureAuthorEditProof(ctx, g.threadSvc, id, resolve)
}
func loadFeatureAuthorEditProof(ctx context.Context, svc *service.ThreadService, id string, resolve identityport.SessionPrincipalResolver) (*domain.Thread, error) {
	if svc == nil {
		return nil, communityport.ErrAuthorCommandUnavailable
	}
	thread, err := svc.LoadThreadAuthorEditProof(ctx, id, resolve)
	if errors.Is(err, service.ErrThreadOwnerPrincipalUnavailable) {
		return nil, identityport.ErrUserSessionPrincipalUnavailable
	}
	return thread, normalizePublicContentError(err)
}
