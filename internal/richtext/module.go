package richtext

import (
	"context"
	"errors"
	"fmt"

	communityport "github.com/campusos/CampusOS/internal/community/port"
	corestorage "github.com/campusos/CampusOS/internal/core/storage"
	platformmodule "github.com/campusos/CampusOS/internal/platform/module"
)

const ModuleID = "feature.controlled-richtext-article"

const portStore = "feature.controlled-richtext-article.adapter.store"

type ModuleConfig struct {
	AssetStoreConfig func() AssetStoreConfig
	Enabled          func() bool
}

// Module composes controlled rich-text through the public Community and User
// Storage ports. Its legacy configuration is supplied by the composition
// boundary, never by a Plugin Manager dependency inside the feature.
type Module struct {
	config    ModuleConfig
	store     Store
	community communityport.ContentGateway
	storage   corestorage.Port
	service   *Service
	handler   *Handler
}

func NewModule(config ModuleConfig) *Module { return &Module{config: config} }

func (m *Module) ID() string { return ModuleID }

func (m *Module) Dependencies() []string {
	return []string{"core.community", "core.user-storage", "core.plugin-platform"}
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
	m.store, m.community, m.storage = store, community, storage
	return nil
}

func (m *Module) Start(context.Context) error {
	if m.store == nil || m.community == nil || m.storage == nil {
		return errors.New("richtext module is not registered")
	}
	svc := NewService(m.store, m.community)
	svc.SetEnabledChecker(m.enabled)
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
