package appearance

import (
	"context"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	space "github.com/campusos/CampusOS/internal/modules/features/personalspace"
)

func (a *Application) ListOwnSpaceSourceStylePacks(ctx context.Context, resolve identityport.SessionPrincipalResolver) (*space.SpaceSourceStylePackList, error) {
	target, err := a.spaceStyles()
	if err != nil {
		return nil, space.ErrSpaceFactsUnavailable
	}
	catalog, ok := target.(space.AuthenticatedSpaceStyleCatalog)
	if !ok {
		return nil, space.ErrSpaceFactsUnavailable
	}
	return catalog.ListOwnSpaceSourceStylePacks(ctx, resolve)
}
