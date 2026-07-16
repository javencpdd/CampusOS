package schedule

import (
	"context"
	"errors"
	"fmt"

	corestorage "github.com/campusos/CampusOS/internal/modules/core/userstorage"
	platformmodule "github.com/campusos/CampusOS/internal/platform/module"
)

const ModuleID = "feature.personal-schedule"

type ModuleConfig struct {
	Config  func() Config
	Enabled func() bool
}

type Module struct {
	config  ModuleConfig
	storage corestorage.Port
	service *Service
	handler *Handler
}

func NewModule(config ModuleConfig) *Module { return &Module{config: config} }
func (m *Module) ID() string                { return ModuleID }
func (m *Module) Dependencies() []string {
	return []string{"core.identity", "core.user-storage", "core.feature-registry"}
}

func (m *Module) Register(app *platformmodule.AppContext) error {
	if app == nil {
		return errors.New("schedule module app context is required")
	}
	value, ok := app.Lookup("storage.user")
	if !ok {
		return errors.New("user storage port is unavailable")
	}
	storage, ok := value.(corestorage.Port)
	if !ok {
		return fmt.Errorf("user storage port has incompatible type %T", value)
	}
	m.storage = storage
	return nil
}

func (m *Module) Start(context.Context) error {
	if m.storage == nil {
		return errors.New("schedule module is not registered")
	}
	config := Config{}
	if m.config.Config != nil {
		config = m.config.Config()
	}
	var svc *Service
	var err error
	if corestorage.NormalizeRoot(config.RootDir) == corestorage.DefaultRoot {
		svc, err = NewServiceWithStorage(config, m.storage)
	} else {
		// Preserve a historical custom schedule root until an explicit User
		// Storage migration moves its files.
		svc, err = NewService(config)
	}
	if err != nil {
		return fmt.Errorf("initialize schedule storage: %w", err)
	}
	svc.SetEnabledChecker(m.enabled)
	m.service = svc
	m.handler = NewHandler(svc)
	return nil
}

func (m *Module) Stop(context.Context) error { return nil }

func (m *Module) Health(context.Context) platformmodule.Health {
	if m.service == nil || m.handler == nil {
		return platformmodule.Health{Status: platformmodule.HealthUnhealthy, Message: "schedule service is not started"}
	}
	return platformmodule.Health{Status: platformmodule.HealthHealthy}
}

func (m *Module) Handler() *Handler { return m.handler }
func (m *Module) Service() *Service { return m.service }

func (m *Module) enabled() bool {
	return m.config.Enabled == nil || m.config.Enabled()
}
