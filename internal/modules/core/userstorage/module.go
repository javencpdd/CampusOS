package storage

import (
	"context"
	"fmt"

	platformmodule "github.com/campusos/CampusOS/internal/platform/module"
)

const ModuleID = "core.user-storage"

type ModuleConfig struct {
	Root                 string
	QuotaBytes           int64
	MaxContentImageBytes int64
}

// Module owns the local default Provider and publishes the stable User
// Storage, Quota, and SafePath ports. Feature-specific compatibility adapters
// may still translate old config shapes while A7 migrates them.
type Module struct {
	config        ModuleConfig
	adapter       *LocalAdapter
	objects       *ObjectService
	contentImages *ContentImageStore
	handler       *Handler
}

func NewModule(config ModuleConfig) *Module { return &Module{config: config} }

func (m *Module) ID() string { return ModuleID }

func (m *Module) Dependencies() []string { return []string{"core.identity"} }

func (m *Module) Register(app *platformmodule.AppContext) error {
	quotaRepository := QuotaRepository(NewMemoryQuotaRepository())
	objectRepository := ObjectRepository(NewMemoryObjectRepository())
	if value, ok := app.Lookup(portQuotaRepository); ok {
		var compatible bool
		quotaRepository, compatible = value.(QuotaRepository)
		if !compatible || quotaRepository == nil {
			return fmt.Errorf("user storage quota repository has incompatible type %T", value)
		}
	}
	if value, ok := app.Lookup(portObjectRepository); ok {
		var compatible bool
		objectRepository, compatible = value.(ObjectRepository)
		if !compatible || objectRepository == nil {
			return fmt.Errorf("user storage object repository has incompatible type %T", value)
		}
	}
	adapter, err := NewLocalAdapterWithQuotaRepository(m.config.Root, m.config.QuotaBytes, quotaRepository)
	if err != nil {
		return fmt.Errorf("initialize local user storage provider: %w", err)
	}
	m.adapter = adapter
	objects, err := NewObjectService(adapter, adapter, objectRepository, adapter.DefaultQuotaBytes())
	if err != nil {
		return fmt.Errorf("initialize storage object service: %w", err)
	}
	m.objects = objects
	contentImages, err := NewContentImageStore(adapter, adapter, m.config.MaxContentImageBytes)
	if err != nil {
		return fmt.Errorf("initialize content image store: %w", err)
	}
	m.contentImages = contentImages
	m.handler = NewHandler(contentImages)
	for _, binding := range []struct {
		name  string
		value interface{}
	}{
		{"storage.user", Port(adapter)},
		{"storage.quota", Quota(adapter)},
		{"storage.quota-manager", QuotaManager(adapter)},
		{"storage.safe-path", SafePath(adapter)},
		{"storage.provider", Provider(LocalProvider{})},
		{"storage.objects", ObjectPort(objects)},
		{"storage.content-images", contentImages},
	} {
		if err := app.Provide(binding.name, binding.value); err != nil {
			return err
		}
	}
	return nil
}

func (m *Module) Start(context.Context) error { return nil }

func (m *Module) Stop(context.Context) error { return nil }

func (m *Module) Health(context.Context) platformmodule.Health {
	if m.adapter == nil || m.objects == nil || m.contentImages == nil || m.handler == nil {
		return platformmodule.Health{Status: platformmodule.HealthUnhealthy, Message: "local user storage provider is unavailable"}
	}
	return platformmodule.Health{Status: platformmodule.HealthHealthy}
}

func (m *Module) Adapter() *LocalAdapter            { return m.adapter }
func (m *Module) Objects() *ObjectService           { return m.objects }
func (m *Module) Handler() *Handler                 { return m.handler }
func (m *Module) ContentImages() *ContentImageStore { return m.contentImages }

type LocalProvider struct{}

func (LocalProvider) Name() string    { return "local" }
func (LocalProvider) Available() bool { return true }
