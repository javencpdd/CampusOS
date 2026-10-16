package appearance

import (
	"context"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	corestorage "github.com/campusos/CampusOS/internal/modules/core/userstorage"
	space "github.com/campusos/CampusOS/internal/modules/features/personalspace"
)

func (a *Application) ListOwnSpaceSourceStylePacks(ctx context.Context, resolve identityport.SessionPrincipalResolver) (*space.SpaceSourceStylePackList, error) {
	target, err := a.spaceStyles()
	if err != nil {
		return nil, space.ErrSpaceFactsUnavailable
	}
	catalog, ok := target.(space.AuthenticatedSpaceStyleCatalog)
	if !ok || corestorage.NilImageAdapter(catalog) {
		return nil, space.ErrSpaceFactsUnavailable
	}
	return catalog.ListOwnSpaceSourceStylePacks(ctx, resolve)
}
