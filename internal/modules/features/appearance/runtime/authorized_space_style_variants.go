package appearance

import (
	"context"
	"errors"
	"io"

	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	space "github.com/campusos/CampusOS/internal/modules/features/personalspace"
)

func (a *Application) checkedSpaceStyleVariants() (space.AuthenticatedSpaceStyleVariants, error) {
	target, err := a.spaceStyles()
	if err != nil {
		return nil, errors.Join(space.ErrSpaceFactsUnavailable, err)
	}
	commands, ok := target.(space.AuthenticatedSpaceStyleVariants)
	if !ok {
		return nil, space.ErrSpaceFactsUnavailable
	}
	return commands, nil
}
func (a *Application) ApplyOwnSpaceCustomHTML(ctx context.Context, html string, resolve identityport.SessionPrincipalResolver) (*space.SpaceMutationAck, error) {
	commands, err := a.checkedSpaceStyleVariants()
	if err != nil {
		return nil, err
	}
	return commands.ApplyOwnSpaceCustomHTML(ctx, html, resolve)
}
func (a *Application) ApplyOwnSpaceStylePackZip(ctx context.Context, reader io.ReaderAt, size int64, resolve identityport.SessionPrincipalResolver) (*space.SpaceMutationAck, error) {
	commands, err := a.checkedSpaceStyleVariants()
	if err != nil {
		return nil, err
	}
	return commands.ApplyOwnSpaceStylePackZip(ctx, reader, size, resolve)
}
func (a *Application) ApplyOwnSpaceSourceStylePack(ctx context.Context, name string, resolve identityport.SessionPrincipalResolver) (*space.SpaceMutationAck, error) {
	commands, err := a.checkedSpaceStyleVariants()
	if err != nil {
		return nil, err
	}
	return commands.ApplyOwnSpaceSourceStylePack(ctx, name, resolve)
}
