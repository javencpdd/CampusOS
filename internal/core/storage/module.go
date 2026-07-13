package storage

import (
	"context"
	"fmt"

	platformmodule "github.com/campusos/CampusOS/internal/platform/module"
)

const ModuleID = "core.user-storage"

type ModuleConfig struct {
	Root       string
	QuotaBytes int64
}

// Module owns the local default Provider and publishes the stable User
// Storage, Quota, and SafePath ports. Feature-specific compatibility adapters
// may still translate old config shapes while A7 migrates them.
type Module struct {
	config  ModuleConfig
	adapter *LocalAdapter
}

func NewModule(config ModuleConfig) *Module { return &Module{config: config} }

func (m *Module) ID() string { return ModuleID }

func (m *Module) Dependencies() []string { return []string{"core.identity"} }

func (m *Module) Register(app *platformmodule.AppContext) error {
	adapter, err := NewLocalAdapterWithQuota(m.config.Root, m.config.QuotaBytes)
	if err != nil {
		return fmt.Errorf("initialize local user storage provider: %w", err)
	}
	m.adapter = adapter
	for _, binding := range []struct {
		name  string
		value interface{}
	}{
		{"storage.user", Port(adapter)},
		{"storage.quota", Quota(adapter)},
		{"storage.safe-path", SafePath(adapter)},
		{"storage.provider", Provider(LocalProvider{})},
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
	if m.adapter == nil {
		return platformmodule.Health{Status: platformmodule.HealthUnhealthy, Message: "local user storage provider is unavailable"}
	}
	return platformmodule.Health{Status: platformmodule.HealthHealthy}
}

func (m *Module) Adapter() *LocalAdapter { return m.adapter }

type LocalProvider struct{}

func (LocalProvider) Name() string    { return "local" }
func (LocalProvider) Available() bool { return true }
