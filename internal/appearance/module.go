package appearance

import (
	"context"
	"errors"
	"fmt"

	communitydomain "github.com/campusos/CampusOS/internal/community/domain"
	communityport "github.com/campusos/CampusOS/internal/community/port"
	"github.com/campusos/CampusOS/internal/homepage"
	platformfeature "github.com/campusos/CampusOS/internal/platform/feature"
	platformmodule "github.com/campusos/CampusOS/internal/platform/module"
	"github.com/campusos/CampusOS/internal/webtheme"
)

const ModuleID = "feature.appearance"

type ModuleConfig struct {
	FeatureRegistry func() *platformfeature.Registry
}

// Module makes Appearance the only built-in feature composition point for
// homepage configuration, web themes and resource-package application.
type Module struct {
	config       ModuleConfig
	features     *platformfeature.Registry
	categories   communityport.CategoryCatalog
	facade       *Facade
	homepage     *homepage.Service
	webTheme     *webtheme.Service
	homeHandler  *homepage.Handler
	themeHandler *webtheme.Handler
}

func NewModule(config ModuleConfig) *Module { return &Module{config: config} }
func (m *Module) ID() string                { return ModuleID }
func (m *Module) Dependencies() []string {
	return []string{"core.community", "core.plugin-platform"}
}

func (m *Module) Register(app *platformmodule.AppContext) error {
	if app == nil {
		return errors.New("appearance module app context is required")
	}
	if m.config.FeatureRegistry == nil || m.config.FeatureRegistry() == nil {
		return errors.New("appearance feature registry is required")
	}
	value, ok := app.Lookup("community.category-catalog")
	if !ok {
		return errors.New("community category catalog port is unavailable")
	}
	categories, ok := value.(communityport.CategoryCatalog)
	if !ok {
		return fmt.Errorf("community category catalog port has incompatible type %T", value)
	}
	m.features, m.categories = m.config.FeatureRegistry(), categories
	return app.Provide("appearance.facade", NewCompatibilityFacade())
}

func (m *Module) Start(context.Context) error {
	if m.features == nil || m.categories == nil {
		return errors.New("appearance module is not registered")
	}
	m.facade = NewCompatibilityFacade()
	homepageConfig := featureConfigSection{registry: m.features, section: "homepage"}
	webThemeConfig := featureConfigSection{registry: m.features, section: "web_theme"}
	m.homepage = homepage.NewService(homepageConfig, categoryCatalogAdapter{catalog: m.categories})
	m.webTheme = webtheme.NewService(webThemeConfig)
	m.homeHandler = homepage.NewHandler(m.homepage)
	m.themeHandler = webtheme.NewHandler(m.webTheme)
	return nil
}

func (m *Module) Stop(context.Context) error { return nil }

func (m *Module) Health(context.Context) platformmodule.Health {
	if m.facade == nil || m.homeHandler == nil || m.themeHandler == nil {
		return platformmodule.Health{Status: platformmodule.HealthUnhealthy, Message: "appearance services are not started"}
	}
	return platformmodule.Health{Status: platformmodule.HealthHealthy}
}

func (m *Module) Facade() *Facade                    { return m.facade }
func (m *Module) HomepageHandler() *homepage.Handler { return m.homeHandler }
func (m *Module) WebThemeHandler() *webtheme.Handler { return m.themeHandler }

type featureConfigSection struct {
	registry *platformfeature.Registry
	section  string
}

func (s featureConfigSection) Enabled() bool {
	return s.registry != nil && s.registry.Enabled("appearance")
}

func (s featureConfigSection) Config() map[string]interface{} {
	if s.registry == nil {
		return map[string]interface{}{}
	}
	root := s.registry.Config("appearance")
	value, ok := root[s.section].(map[string]interface{})
	if !ok {
		return map[string]interface{}{}
	}
	return copyConfig(value)
}

func (s featureConfigSection) Update(config map[string]interface{}) (map[string]interface{}, error) {
	if s.registry == nil {
		return nil, errors.New("appearance feature registry is unavailable")
	}
	root := s.registry.Config("appearance")
	root[s.section] = copyConfig(config)
	if err := s.registry.UpdateConfig("appearance", root); err != nil {
		return nil, err
	}
	return copyConfig(config), nil
}

type categoryCatalogAdapter struct{ catalog communityport.CategoryCatalog }

func (a categoryCatalogAdapter) List(ctx context.Context) ([]*communitydomain.Category, error) {
	return a.catalog.ListCategories(ctx)
}

func copyConfig(input map[string]interface{}) map[string]interface{} {
	output := make(map[string]interface{}, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}
