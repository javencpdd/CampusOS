package space

import (
	"context"
	"io"

	"github.com/campusos/CampusOS/internal/modules/features/appearance/stylepack"
)

// Space-prefixed methods make the target contract unambiguous when it is
// combined with Homepage and WebTheme in Appearance.Application.
func (s *Service) PreviewSpaceStylePackage(ctx context.Context, userID string, pkg StylePackage) (*StylePreview, error) {
	return s.PreviewStylePackage(ctx, userID, pkg)
}
func (s *Service) ExportSpaceStylePackage(ctx context.Context, userID string, request StyleExportRequest) (*StyleExportResult, error) {
	return s.ExportStylePackage(ctx, userID, request)
}
func (s *Service) ApplySpaceStylePackage(ctx context.Context, userID string, pkg StylePackage) (*StyleApplyResult, error) {
	return s.ApplyStylePackage(ctx, userID, pkg)
}
func (s *Service) ValidateSpaceCustomHTML(ctx context.Context, userID, html string) (*StyleValidationResult, error) {
	return s.ValidateCustomHTML(ctx, userID, html)
}
func (s *Service) SpaceCustomHTMLExample(ctx context.Context, userID string) (*StyleHTMLExampleResult, error) {
	return s.CustomHTMLExample(ctx, userID)
}
func (s *Service) ApplySpaceCustomHTML(ctx context.Context, userID, html string) (*StyleApplyResult, error) {
	return s.ApplyCustomHTML(ctx, userID, html)
}
func (s *Service) ValidateSpaceStylePackZip(ctx context.Context, userID string, reader io.ReaderAt, size int64) (*StylePackResult, error) {
	return s.ValidateStylePackZip(ctx, userID, reader, size)
}
func (s *Service) SpaceStylePackExample(ctx context.Context, userID string) (*stylepack.FileBundle, error) {
	return s.StylePackExample(ctx, userID)
}
func (s *Service) ListSpaceStylePacks(ctx context.Context, userID string) (*stylepack.SourcePackList, error) {
	return s.ListSourceStylePacks(ctx, userID)
}
func (s *Service) ApplySpaceStylePackZip(ctx context.Context, userID string, reader io.ReaderAt, size int64) (*StyleApplyResult, error) {
	return s.ApplyStylePackZip(ctx, userID, reader, size)
}
func (s *Service) ApplySpaceSourceStylePack(ctx context.Context, userID, name string) (*StyleApplyResult, error) {
	return s.ApplySourceStylePack(ctx, userID, name)
}
func (s *Service) RollbackSpaceStyle(ctx context.Context, userID string) (*PublicSpace, error) {
	return s.RollbackStyle(ctx, userID)
}
func (s *Service) RestoreDefaultSpaceStyle(ctx context.Context, userID string) (*PublicSpace, error) {
	return s.RestoreDefaultStyle(ctx, userID)
}
