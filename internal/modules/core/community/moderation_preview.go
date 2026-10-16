package community

import (
	"context"
	"errors"

	communityport "github.com/campusos/CampusOS/internal/modules/core/community/port"
	"github.com/campusos/CampusOS/internal/modules/core/community/repository"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
)

func (g *moduleModerationGateway) PreviewUserModeration(ctx context.Context, id string, posts []string, resolve identityport.SessionPrincipalResolver, config func(context.Context) (communityport.ModerationPreviewConfig, error)) (*communityport.UserModerationPreview, error) {
	delegate, err := g.gateway()
	if err != nil {
		return nil, errors.Join(communityport.ErrGovernanceCommandUnavailable, err)
	}
	return delegate.PreviewUserModeration(ctx, id, posts, resolve, config)
}

func (g *moderationGateway) PreviewUserModeration(ctx context.Context, id string, posts []string, resolve identityport.SessionPrincipalResolver, config func(context.Context) (communityport.ModerationPreviewConfig, error)) (*communityport.UserModerationPreview, error) {
	if g == nil || g.postSvc == nil {
		return nil, communityport.ErrGovernanceCommandUnavailable
	}
	result, err := g.postSvc.PreviewUserModeration(ctx, id, posts, resolve, config)
	if errors.Is(err, repository.ErrThreadNotFound) {
		return nil, communityport.ErrThreadNotFound
	}
	if errors.Is(err, repository.ErrPostNotFound) {
		return nil, communityport.ErrPostNotFound
	}
	return result, err
}
