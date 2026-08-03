package community

import (
	"context"
	"errors"

	communityport "github.com/campusos/CampusOS/internal/modules/core/community/port"
	"github.com/campusos/CampusOS/internal/modules/core/community/service"
)

// moduleNotificationWriter exposes Community's NotificationService through the
// community.notification-writer Port so sibling Core modules (for example
// Moderation) can emit user-facing notifications without touching repositories.
type moduleNotificationWriter struct{ module *Module }

func (w *moduleNotificationWriter) notifier() (*service.NotificationService, error) {
	if w == nil || w.module == nil || w.module.notificationService == nil {
		return nil, errors.New("community notification writer is not started")
	}
	return w.module.notificationService, nil
}

func (w *moduleNotificationWriter) NotifyModeratorScopeGranted(ctx context.Context, userID string, categories []communityport.NamedCategory) error {
	notifier, err := w.notifier()
	if err != nil {
		return err
	}
	return notifier.NotifyModeratorScopeGranted(ctx, userID, toScopeCategories(categories))
}

func (w *moduleNotificationWriter) NotifyModeratorScopeRevoked(ctx context.Context, userID string, categories []communityport.NamedCategory) error {
	notifier, err := w.notifier()
	if err != nil {
		return err
	}
	return notifier.NotifyModeratorScopeRevoked(ctx, userID, toScopeCategories(categories))
}

func toScopeCategories(categories []communityport.NamedCategory) []service.ModeratorScopeCategory {
	result := make([]service.ModeratorScopeCategory, 0, len(categories))
	for _, category := range categories {
		result = append(result, service.ModeratorScopeCategory{ID: category.ID, Name: category.Name})
	}
	return result
}
