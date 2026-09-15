package richtext

import (
	"context"
	"errors"
	"fmt"

	communityport "github.com/campusos/CampusOS/internal/modules/core/community/port"
	corestorage "github.com/campusos/CampusOS/internal/modules/core/userstorage"
	platformmodule "github.com/campusos/CampusOS/internal/platform/module"
	platformobservability "github.com/campusos/CampusOS/internal/platform/observability"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/pkg/observability"
)

const ModuleID = "feature.controlled-richtext-article"

const portStore = "feature.controlled-richtext-article.adapter.store"

type ModuleConfig struct {
	AssetStoreConfig          func() AssetStoreConfig
	Enabled                   func() bool
	PDFViewerEnabled          func() bool
	PDFViewerAuthorizer       func(context.Context, PDFViewerAuthorizationInput) error
	PersonalDocumentPDFReader PersonalDocumentPDFReader
}

// Module composes controlled rich-text through the public Community and User
// Storage ports. Its legacy configuration is supplied by the composition
// boundary, never by a Plugin Manager dependency inside the feature.
type Module struct {
	config    ModuleConfig
	app       *platformmodule.AppContext
	store     Store
	community communityport.ContentGateway
	storage   corestorage.Port
	objects   corestorage.ObjectPort
	meter     observability.Meter
	service   *Service
	handler   *Handler
}

func NewModule(config ModuleConfig) *Module { return &Module{config: config} }

func (m *Module) ID() string { return ModuleID }

func (m *Module) Dependencies() []string {
	return []string{"core.community", "core.user-storage", "core.feature-registry", reliability.ModuleID, platformobservability.ModuleID}
}

func (m *Module) Register(app *platformmodule.AppContext) error {
	if app == nil {
		return errors.New("richtext module app context is required")
	}
	storeValue, ok := app.Lookup(portStore)
	if !ok {
		return errors.New("richtext store adapter is not bound by profile")
	}
	store, ok := storeValue.(Store)
	if !ok {
		return fmt.Errorf("richtext store adapter has incompatible type %T", storeValue)
	}
	communityValue, ok := app.Lookup("community.content-gateway")
	if !ok {
		return errors.New("community content gateway port is unavailable")
	}
	community, ok := communityValue.(communityport.ContentGateway)
	if !ok {
		return fmt.Errorf("community content gateway port has incompatible type %T", communityValue)
	}
	storageValue, ok := app.Lookup("storage.user")
	if !ok {
		return errors.New("user storage port is unavailable")
	}
	storage, ok := storageValue.(corestorage.Port)
	if !ok {
		return fmt.Errorf("user storage port has incompatible type %T", storageValue)
	}
	m.app, m.store, m.community, m.storage = app, store, community, storage
	objectsValue, ok := app.Lookup("storage.objects")
	if !ok {
		return errors.New("user storage object port is unavailable")
	}
	objects, ok := objectsValue.(corestorage.ObjectPort)
	if !ok || objects == nil {
		return fmt.Errorf("user storage object port has incompatible type %T", objectsValue)
	}
	m.objects = objects
	if value, exists := app.Lookup(platformobservability.PortMeter); exists {
		meter, compatible := value.(observability.Meter)
		if !compatible || meter == nil {
			return fmt.Errorf("richtext observability meter has incompatible type %T", value)
		}
		m.meter = meter
	}
	return nil
}

func (m *Module) Start(ctx context.Context) error {
	if m.app == nil || m.store == nil || m.community == nil || m.storage == nil || m.objects == nil {
		return errors.New("richtext module is not registered")
	}
	reliabilityValue, ok := m.app.Lookup("platform.reliability.service")
	if !ok {
		return errors.New("reliability service port is unavailable")
	}
	reliable, ok := reliabilityValue.(*reliability.Service)
	if !ok || reliable == nil {
		return fmt.Errorf("reliability service port has incompatible type %T", reliabilityValue)
	}
	svc := NewService(m.store, m.community)
	svc.SetReliability(reliable)
	svc.SetEnabledChecker(m.enabled)
	svc.SetPDFViewerEnabledChecker(m.pdfViewerEnabled)
	svc.SetPDFViewerAuthorizer(m.config.PDFViewerAuthorizer)
	svc.SetPersonalDocumentPDFReader(m.config.PersonalDocumentPDFReader)
	config := AssetStoreConfig{}
	if m.config.AssetStoreConfig != nil {
		config = m.config.AssetStoreConfig()
	}
	var assets *LocalAssetStore
	var err error
	if corestorage.NormalizeRoot(config.RootDir) == corestorage.DefaultRoot {
		assets, err = NewLocalAssetStoreWithStorage(config, m.storage)
	} else {
		// Preserve existing explicitly configured roots while they are migrated
		// through the legacy storage adapter.
		assets, err = NewLocalAssetStore(config)
	}
	if err != nil {
		return fmt.Errorf("initialize richtext asset store: %w", err)
	}
	svc.SetAssetStore(assets)
	svc.SetObjectPort(m.objects)
	svc.SetMeter(m.meter)
	svc.refreshAssetMetrics(ctx)
	m.service = svc
	m.handler = NewHandler(svc)
	return nil
}

func (m *Module) Stop(context.Context) error { return nil }

func (m *Module) Health(context.Context) platformmodule.Health {
	if m.service == nil || m.handler == nil {
		return platformmodule.Health{Status: platformmodule.HealthUnhealthy, Message: "richtext services are not started"}
	}
	return platformmodule.Health{Status: platformmodule.HealthHealthy}
}

func (m *Module) Handler() *Handler { return m.handler }
func (m *Module) Service() *Service { return m.service }

func (m *Module) enabled() bool {
	return m.config.Enabled == nil || m.config.Enabled()
}

func (m *Module) pdfViewerEnabled() bool {
	return m.config.PDFViewerEnabled == nil || m.config.PDFViewerEnabled()
}
