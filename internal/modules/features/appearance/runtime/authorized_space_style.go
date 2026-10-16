package appearance

import (
	"context"
	"errors"

	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	space "github.com/campusos/CampusOS/internal/modules/features/personalspace"
)

func (a *Application) checkedSpaceStyleCommands() (space.AuthenticatedSpaceStyleCommands, error) {
	target, err := a.spaceStyles()
	if err != nil {
		return nil, errors.Join(space.ErrSpaceFactsUnavailable, err)
	}
	command, ok := target.(space.AuthenticatedSpaceStyleCommands)
	if !ok {
		return nil, space.ErrSpaceFactsUnavailable
	}
	return command, nil
}
func (a *Application) ApplyOwnSpaceStylePackage(ctx context.Context, pkg space.StylePackage, resolve identityport.SessionPrincipalResolver) (*space.SpaceMutationAck, error) {
	command, err := a.checkedSpaceStyleCommands()
	if err != nil {
		return nil, err
	}
	return command.ApplyOwnSpaceStylePackage(ctx, pkg, resolve)
}
func (a *Application) RestoreOwnSpaceDefaultStyle(ctx context.Context, resolve identityport.SessionPrincipalResolver) (*space.SpaceMutationAck, error) {
	command, err := a.checkedSpaceStyleCommands()
	if err != nil {
		return nil, err
	}
	return command.RestoreOwnSpaceDefaultStyle(ctx, resolve)
}
func (a *Application) RollbackOwnSpaceStyle(ctx context.Context, resolve identityport.SessionPrincipalResolver) (*space.SpaceMutationAck, error) {
	command, err := a.checkedSpaceStyleCommands()
	if err != nil {
		return nil, err
	}
	return command.RollbackOwnSpaceStyle(ctx, resolve)
}
