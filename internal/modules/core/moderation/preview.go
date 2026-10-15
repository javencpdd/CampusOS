package moderation

import (
	"context"

	communityport "github.com/campusos/CampusOS/internal/modules/core/community/port"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
)

func (s *Service) PreviewForPrincipal(ctx context.Context, id string, posts []string, resolve identityport.SessionPrincipalResolver) (*communityport.UserModerationPreview, error) {
	if s == nil {
		return nil, communityport.ErrGovernanceCommandUnavailable
	}
	checked, ok := s.community.(communityport.CheckedModerationPreview)
	if !ok || checked == nil {
		return nil, communityport.ErrGovernanceCommandUnavailable
	}
	return checked.PreviewUserModeration(ctx, id, posts, resolve, func(ctx context.Context) (communityport.ModerationPreviewConfig, error) {
		if err := ctx.Err(); err != nil {
			return communityport.ModerationPreviewConfig{}, err
		}
		if s.enabled == nil {
			return communityport.ModerationPreviewConfig{}, communityport.ErrGovernanceCommandUnavailable
		}
		if !s.enabled() {
			return communityport.ModerationPreviewConfig{}, nil
		}
		config := s.currentConfig()
		return communityport.ModerationPreviewConfig{Pin: config.AllowPin, Lock: config.AllowLock, DeletePost: config.AllowDeletePost}, nil
	})
}
