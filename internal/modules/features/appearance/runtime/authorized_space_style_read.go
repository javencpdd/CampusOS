package appearance

import (
	"context"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	corestorage "github.com/campusos/CampusOS/internal/modules/core/userstorage"
	"github.com/campusos/CampusOS/internal/modules/features/appearance/stylepack"
	space "github.com/campusos/CampusOS/internal/modules/features/personalspace"
	"io"
)

func (a *Application) checkedSpaceStyleReads() (space.AuthenticatedSpaceStyleReads, error) {
	if a == nil {
		return nil, space.ErrSpaceFactsUnavailable
	}
	target, err := a.spaceStyles()
	if err != nil {
		return nil, space.ErrSpaceFactsUnavailable
	}
	reads, ok := target.(space.AuthenticatedSpaceStyleReads)
	if !ok || corestorage.NilImageAdapter(reads) {
		return nil, space.ErrSpaceFactsUnavailable
	}
	return reads, nil
}
func (a *Application) ValidateOwnSpaceStylePackage(ctx context.Context, pkg space.StylePackage, resolve identityport.SessionPrincipalResolver) (*space.StyleValidationResult, error) {
	reads, err := a.checkedSpaceStyleReads()
	if err != nil {
		return nil, err
	}
	return reads.ValidateOwnSpaceStylePackage(ctx, pkg, resolve)
}
func (a *Application) PreviewOwnSpaceStylePackage(ctx context.Context, pkg space.StylePackage, resolve identityport.SessionPrincipalResolver) (*space.SpaceStylePreview, error) {
	reads, err := a.checkedSpaceStyleReads()
	if err != nil {
		return nil, err
	}
	return reads.PreviewOwnSpaceStylePackage(ctx, pkg, resolve)
}
func (a *Application) ExportOwnSpaceStylePackage(ctx context.Context, req space.StyleExportRequest, resolve identityport.SessionPrincipalResolver) (*space.StyleExportResult, error) {
	reads, err := a.checkedSpaceStyleReads()
	if err != nil {
		return nil, err
	}
	return reads.ExportOwnSpaceStylePackage(ctx, req, resolve)
}
func (a *Application) ValidateOwnSpaceCustomHTML(ctx context.Context, html string, resolve identityport.SessionPrincipalResolver) (*space.StyleValidationResult, error) {
	reads, err := a.checkedSpaceStyleReads()
	if err != nil {
		return nil, err
	}
	return reads.ValidateOwnSpaceCustomHTML(ctx, html, resolve)
}
func (a *Application) OwnSpaceCustomHTMLExample(ctx context.Context, resolve identityport.SessionPrincipalResolver) (*space.StyleHTMLExampleResult, error) {
	reads, err := a.checkedSpaceStyleReads()
	if err != nil {
		return nil, err
	}
	return reads.OwnSpaceCustomHTMLExample(ctx, resolve)
}
func (a *Application) ValidateOwnSpaceStylePackZip(ctx context.Context, reader io.ReaderAt, size int64, resolve identityport.SessionPrincipalResolver) (*space.StylePackResult, error) {
	reads, err := a.checkedSpaceStyleReads()
	if err != nil {
		return nil, err
	}
	return reads.ValidateOwnSpaceStylePackZip(ctx, reader, size, resolve)
}
func (a *Application) OwnSpaceStylePackExample(ctx context.Context, resolve identityport.SessionPrincipalResolver) (*stylepack.FileBundle, error) {
	reads, err := a.checkedSpaceStyleReads()
	if err != nil {
		return nil, err
	}
	return reads.OwnSpaceStylePackExample(ctx, resolve)
}
func (a *Application) OwnSpaceStylePackExampleZip(ctx context.Context, resolve identityport.SessionPrincipalResolver) (*space.StylePackZipExample, error) {
	reads, err := a.checkedSpaceStyleReads()
	if err != nil {
		return nil, err
	}
	return reads.OwnSpaceStylePackExampleZip(ctx, resolve)
}
