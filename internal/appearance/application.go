package appearance

import (
	"context"
	"errors"
	"io"
	"sync"

	"github.com/campusos/CampusOS/internal/homepage"
	"github.com/campusos/CampusOS/internal/space"
	"github.com/campusos/CampusOS/internal/stylepack"
	"github.com/campusos/CampusOS/internal/webtheme"
)

// Application is the single runtime entry for every system Appearance target.
// Homepage and WebTheme remain target-specific policy adapters, but transports
// cannot call them directly.
type Application struct {
	Facade   *Facade
	homepage *homepage.Service
	webTheme *webtheme.Service
	mu       sync.RWMutex
	space    space.StyleApplication
}

func NewApplication(facade *Facade, homepageService *homepage.Service, webThemeService *webtheme.Service) *Application {
	return &Application{Facade: facade, homepage: homepageService, webTheme: webThemeService}
}

func (a *Application) AttachSpaceStyleTarget(target space.StyleApplication) {
	a.mu.Lock()
	a.space = target
	a.mu.Unlock()
}

func (a *Application) spaceStyles() (space.StyleApplication, error) {
	a.mu.RLock()
	target := a.space
	a.mu.RUnlock()
	if target == nil {
		return nil, errors.New("personal-space appearance target is unavailable")
	}
	return target, nil
}

func (a *Application) PublicConfig(ctx context.Context) (*homepage.Config, error) {
	return a.homepage.PublicConfig(ctx)
}
func (a *Application) ValidateStylePackZip(reader io.ReaderAt, size int64) (*homepage.StylePackResult, error) {
	return a.homepage.ValidateStylePackZip(reader, size)
}
func (a *Application) StylePackExample(ctx context.Context) (*stylepack.FileBundle, error) {
	return a.homepage.StylePackExample(ctx)
}
func (a *Application) ListSourceStylePacks(ctx context.Context) (*stylepack.SourcePackList, error) {
	return a.homepage.ListSourceStylePacks(ctx)
}
func (a *Application) ApplyStylePackZip(ctx context.Context, reader io.ReaderAt, size int64) (*homepage.Config, error) {
	return a.homepage.ApplyStylePackZip(ctx, reader, size)
}
func (a *Application) ApplySourceStylePack(ctx context.Context, name string) (*homepage.Config, error) {
	return a.homepage.ApplySourceStylePack(ctx, name)
}
func (a *Application) RollbackStylePack(ctx context.Context) (*homepage.Config, error) {
	return a.homepage.RollbackStylePack(ctx)
}
func (a *Application) ThemeCatalog() (*webtheme.Catalog, error) {
	return a.webTheme.Catalog()
}
func (a *Application) ThemePackage(name string) (*webtheme.RuntimePackage, error) {
	return a.webTheme.Package(name)
}
func (a *Application) ThemeAsset(name, path string) ([]byte, string, error) {
	return a.webTheme.Asset(name, path)
}

func (a *Application) ValidateSpaceStylePackage(ctx context.Context, userID string, pkg space.StylePackage) (space.StyleValidationResult, error) {
	target, err := a.spaceStyles()
	if err != nil {
		return space.StyleValidationResult{}, err
	}
	return target.ValidateSpaceStylePackage(ctx, userID, pkg)
}
func (a *Application) PreviewSpaceStylePackage(ctx context.Context, userID string, pkg space.StylePackage) (*space.StylePreview, error) {
	target, err := a.spaceStyles()
	if err != nil {
		return nil, err
	}
	return target.PreviewSpaceStylePackage(ctx, userID, pkg)
}
func (a *Application) ExportSpaceStylePackage(ctx context.Context, userID string, request space.StyleExportRequest) (*space.StyleExportResult, error) {
	target, err := a.spaceStyles()
	if err != nil {
		return nil, err
	}
	return target.ExportSpaceStylePackage(ctx, userID, request)
}
func (a *Application) ApplySpaceStylePackage(ctx context.Context, userID string, pkg space.StylePackage) (*space.StyleApplyResult, error) {
	target, err := a.spaceStyles()
	if err != nil {
		return nil, err
	}
	return target.ApplySpaceStylePackage(ctx, userID, pkg)
}
func (a *Application) ValidateSpaceCustomHTML(ctx context.Context, userID, html string) (*space.StyleValidationResult, error) {
	target, err := a.spaceStyles()
	if err != nil {
		return nil, err
	}
	return target.ValidateSpaceCustomHTML(ctx, userID, html)
}
func (a *Application) SpaceCustomHTMLExample(ctx context.Context, userID string) (*space.StyleHTMLExampleResult, error) {
	target, err := a.spaceStyles()
	if err != nil {
		return nil, err
	}
	return target.SpaceCustomHTMLExample(ctx, userID)
}
func (a *Application) ApplySpaceCustomHTML(ctx context.Context, userID, html string) (*space.StyleApplyResult, error) {
	target, err := a.spaceStyles()
	if err != nil {
		return nil, err
	}
	return target.ApplySpaceCustomHTML(ctx, userID, html)
}
func (a *Application) ValidateSpaceStylePackZip(ctx context.Context, userID string, reader io.ReaderAt, size int64) (*space.StylePackResult, error) {
	target, err := a.spaceStyles()
	if err != nil {
		return nil, err
	}
	return target.ValidateSpaceStylePackZip(ctx, userID, reader, size)
}
func (a *Application) SpaceStylePackExample(ctx context.Context, userID string) (*stylepack.FileBundle, error) {
	target, err := a.spaceStyles()
	if err != nil {
		return nil, err
	}
	return target.SpaceStylePackExample(ctx, userID)
}
func (a *Application) ListSpaceStylePacks(ctx context.Context, userID string) (*stylepack.SourcePackList, error) {
	target, err := a.spaceStyles()
	if err != nil {
		return nil, err
	}
	return target.ListSpaceStylePacks(ctx, userID)
}
func (a *Application) ApplySpaceStylePackZip(ctx context.Context, userID string, reader io.ReaderAt, size int64) (*space.StyleApplyResult, error) {
	target, err := a.spaceStyles()
	if err != nil {
		return nil, err
	}
	return target.ApplySpaceStylePackZip(ctx, userID, reader, size)
}
func (a *Application) ApplySpaceSourceStylePack(ctx context.Context, userID, name string) (*space.StyleApplyResult, error) {
	target, err := a.spaceStyles()
	if err != nil {
		return nil, err
	}
	return target.ApplySpaceSourceStylePack(ctx, userID, name)
}
func (a *Application) RollbackSpaceStyle(ctx context.Context, userID string) (*space.PublicSpace, error) {
	target, err := a.spaceStyles()
	if err != nil {
		return nil, err
	}
	return target.RollbackSpaceStyle(ctx, userID)
}
func (a *Application) RestoreDefaultSpaceStyle(ctx context.Context, userID string) (*space.PublicSpace, error) {
	target, err := a.spaceStyles()
	if err != nil {
		return nil, err
	}
	return target.RestoreDefaultSpaceStyle(ctx, userID)
}
