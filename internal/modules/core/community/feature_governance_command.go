package community

import (
	"context"
	"errors"

	communityport "github.com/campusos/CampusOS/internal/modules/core/community/port"
	"github.com/campusos/CampusOS/internal/modules/core/community/repository"
	"github.com/campusos/CampusOS/internal/modules/core/community/service"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
)

func (g *moduleContentGateway) ExecuteThreadGovernanceCommand(ctx context.Context, id, op, reason string, resolve identityport.SessionPrincipalResolver, part communityport.ThreadGovernanceParticipant) error {
	delegate, err := g.gateway()
	if err != nil {
		return communityport.ErrGovernanceCommandUnavailable
	}
	return normalizeGovernanceCommandError(delegate.threadSvc.ExecuteThreadGovernanceCommand(ctx, id, op, reason, resolve, part))
}
func (g *contentGateway) ExecuteThreadGovernanceCommand(ctx context.Context, id, op, reason string, resolve identityport.SessionPrincipalResolver, part communityport.ThreadGovernanceParticipant) error {
	if g == nil || g.threadSvc == nil {
		return communityport.ErrGovernanceCommandUnavailable
	}
	return normalizeGovernanceCommandError(g.threadSvc.ExecuteThreadGovernanceCommand(ctx, id, op, reason, resolve, part))
}

func normalizeGovernanceCommandError(err error) error {
	err = normalizePublicContentError(err)
	switch {
	case errors.Is(err, service.ErrThreadGovernanceDenied):
		return errors.Join(communityport.ErrGovernanceCommandDenied, err)
	case errors.Is(err, service.ErrThreadStateConflict), errors.Is(err, repository.ErrThreadRevisionConflict), errors.Is(err, repository.ErrModerationCaseChanged):
		return errors.Join(communityport.ErrGovernanceCommandConflict, err)
	case errors.Is(err, service.ErrThreadGovernanceUnavailable):
		return errors.Join(communityport.ErrGovernanceCommandUnavailable, err)
	}
	return err
}
