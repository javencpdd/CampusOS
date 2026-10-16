package community

import (
	"context"
	"errors"

	communityport "github.com/campusos/CampusOS/internal/modules/core/community/port"
	"github.com/campusos/CampusOS/internal/modules/core/community/repository"
	"github.com/campusos/CampusOS/internal/modules/core/community/service"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
)

func (g *moduleModerationGateway) UserSetThreadFlag(ctx context.Context, id, operation string, resolve identityport.SessionPrincipalResolver, participant communityport.ThreadFlagGovernanceParticipant) (*communityport.ThreadFlagResult, error) {
	delegate, err := g.gateway()
	if err != nil {
		return nil, errors.Join(communityport.ErrGovernanceCommandUnavailable, err)
	}
	return delegate.UserSetThreadFlag(ctx, id, operation, resolve, participant)
}

func (g *moderationGateway) UserSetThreadFlag(ctx context.Context, id, operation string, resolve identityport.SessionPrincipalResolver, participant communityport.ThreadFlagGovernanceParticipant) (*communityport.ThreadFlagResult, error) {
	if g == nil || g.threadSvc == nil {
		return nil, communityport.ErrGovernanceCommandUnavailable
	}
	result, err := g.threadSvc.UserSetThreadFlag(ctx, id, operation, resolve, participant)
	switch {
	case errors.Is(err, repository.ErrThreadNotFound):
		return nil, communityport.ErrThreadNotFound
	case errors.Is(err, service.ErrThreadGovernanceDenied):
		return nil, errors.Join(communityport.ErrGovernanceCommandDenied, err)
	case errors.Is(err, service.ErrThreadGovernanceUnavailable):
		return nil, errors.Join(communityport.ErrGovernanceCommandUnavailable, err)
	case errors.Is(err, repository.ErrThreadRevisionConflict), errors.Is(err, service.ErrThreadStateConflict):
		return nil, errors.Join(communityport.ErrGovernanceCommandConflict, err)
	default:
		return result, err
	}
}
