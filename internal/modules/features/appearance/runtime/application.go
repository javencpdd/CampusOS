package appearance

import (
	"context"
	"errors"
	"io"
	"sync"

	"github.com/campusos/CampusOS/internal/modules/features/appearance/homepage"
	"github.com/campusos/CampusOS/internal/modules/features/appearance/stylepack"
	"github.com/campusos/CampusOS/internal/modules/features/appearance/webtheme"
	"github.com/campusos/CampusOS/internal/modules/features/personalspace"
	"github.com/campusos/CampusOS/internal/platform/reliability"
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
	reliable *reliability.Service
}

func NewApplication(facade *Facade, homepageService *homepage.Service, webThemeService *webtheme.Service) *Application {
	return &Application{Facade: facade, homepage: homepageService, webTheme: webThemeService}
}

func (a *Application) AttachSpaceStyleTarget(target space.StyleApplication) {
	a.mu.Lock()
	a.space = target
	a.mu.Unlock()
}

func (a *Application) SetReliability(reliable *reliability.Service) {
	a.mu.Lock()
	a.reliable = reliable
	a.mu.Unlock()
	if a.Facade != nil {
		a.Facade.SetReliability(reliable)
	}
}

func (a *Application) track(ctx context.Context, kind, subjectType, subjectID, actorID string, action func(context.Context) error) error {
	a.mu.RLock()
	reliable := a.reliable
	a.mu.RUnlock()
	if reliable == nil {
		return action(ctx)
	}
	return reliable.TrackOperation(ctx, reliability.Operation{
		Kind: kind, SubjectType: subjectType, SubjectID: subjectID, ActorID: actorID,
	}, action)
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
	var result *homepage.Config
	err := a.track(ctx, "appearance.homepage.apply", "homepage", "system", "system", func(operationCtx context.Context) error {
		var err error
		result, err = a.homepage.ApplyStylePackZip(operationCtx, reader, size)
		return err
	})
	return result, err
}
func (a *Application) ApplySourceStylePack(ctx context.Context, name string) (*homepage.Config, error) {
	var result *homepage.Config
	err := a.track(ctx, "appearance.homepage.apply", "homepage-pack", name, "system", func(operationCtx context.Context) error {
		var err error
		result, err = a.homepage.ApplySourceStylePack(operationCtx, name)
		return err
	})
	if err == nil {
		a.recordLegacySourceUsage(ctx, "homepage", name)
	}
	return result, err
}
func (a *Application) RollbackStylePack(ctx context.Context) (*homepage.Config, error) {
	var result *homepage.Config
	err := a.track(ctx, "appearance.homepage.rollback", "homepage", "system", "system", func(operationCtx context.Context) error {
		var err error
		result, err = a.homepage.RollbackStylePack(operationCtx)
		return err
	})
	return result, err
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
	var result *space.StyleApplyResult
	err = a.track(ctx, "appearance.space.apply", "space-style", pkg.Manifest.Name, userID, func(operationCtx context.Context) error {
		var applyErr error
		result, applyErr = target.ApplySpaceStylePackage(operationCtx, userID, pkg)
		return applyErr
	})
	return result, err
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
	var result *space.StyleApplyResult
	err = a.track(ctx, "appearance.space.apply", "space-style", "custom-html", userID, func(operationCtx context.Context) error {
		var applyErr error
		result, applyErr = target.ApplySpaceCustomHTML(operationCtx, userID, html)
		return applyErr
	})
	return result, err
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
	var result *space.StyleApplyResult
	err = a.track(ctx, "appearance.space.apply", "space-style-pack", "uploaded", userID, func(operationCtx context.Context) error {
		var applyErr error
		result, applyErr = target.ApplySpaceStylePackZip(operationCtx, userID, reader, size)
		return applyErr
	})
	return result, err
}
func (a *Application) ApplySpaceSourceStylePack(ctx context.Context, userID, name string) (*space.StyleApplyResult, error) {
	target, err := a.spaceStyles()
	if err != nil {
		return nil, err
	}
	var result *space.StyleApplyResult
	err = a.track(ctx, "appearance.space.apply", "space-style-pack", name, userID, func(operationCtx context.Context) error {
		var applyErr error
		result, applyErr = target.ApplySpaceSourceStylePack(operationCtx, userID, name)
		return applyErr
	})
	if err == nil {
		a.recordLegacySourceUsage(ctx, "personal-space", name)
	}
	return result, err
}
func (a *Application) RollbackSpaceStyle(ctx context.Context, userID string) (*space.PublicSpace, error) {
	target, err := a.spaceStyles()
	if err != nil {
		return nil, err
	}
	var result *space.PublicSpace
	err = a.track(ctx, "appearance.space.rollback", "space", userID, userID, func(operationCtx context.Context) error {
		var applyErr error
		result, applyErr = target.RollbackSpaceStyle(operationCtx, userID)
		return applyErr
	})
	return result, err
}
func (a *Application) RestoreDefaultSpaceStyle(ctx context.Context, userID string) (*space.PublicSpace, error) {
	target, err := a.spaceStyles()
	if err != nil {
		return nil, err
	}
	var result *space.PublicSpace
	err = a.track(ctx, "appearance.space.restore_default", "space", userID, userID, func(operationCtx context.Context) error {
		var applyErr error
		result, applyErr = target.RestoreDefaultSpaceStyle(operationCtx, userID)
		return applyErr
	})
	return result, err
}

func (a *Application) recordLegacySourceUsage(ctx context.Context, target, name string) {
	a.mu.RLock()
	reliable := a.reliable
	a.mu.RUnlock()
	if reliable == nil {
		return
	}
	_ = reliable.RecordCompatibility(ctx, "legacy-style-source:"+target+":"+name, "legacy-plugin-data-style-pack", map[string]string{
		"target": target,
		"name":   name,
	})
}
