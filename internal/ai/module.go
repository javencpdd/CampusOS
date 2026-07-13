package ai

import (
	"context"
	"errors"
	"fmt"

	platformmodule "github.com/campusos/CampusOS/internal/platform/module"
	"github.com/campusos/CampusOS/pkg/config"
)

const ModuleID = "integration.ai"

const portCallLogStore = "integration.ai.adapter.call-log-store"

type Module struct {
	config  config.AIConfig
	app     *platformmodule.AppContext
	service *Service
	handler *Handler
}

func NewModule(config config.AIConfig) *Module { return &Module{config: config} }
func (m *Module) ID() string                   { return ModuleID }
func (m *Module) Dependencies() []string       { return nil }

func (m *Module) Register(app *platformmodule.AppContext) error {
	if app == nil {
		return errors.New("AI module app context is required")
	}
	m.app = app
	return nil
}

func (m *Module) Start(context.Context) error {
	service, err := NewServiceFromConfig(m.config)
	if err != nil && service == nil {
		return fmt.Errorf("initialize AI service: %w", err)
	}
	if value, ok := m.app.Lookup(portCallLogStore); ok {
		if store, ok := value.(CallLogStore); ok && store != nil {
			service.SetCallLogStore(store)
		}
	}
	m.service = service
	m.handler = NewHandler(service)
	if provideErr := m.app.Provide("integration.ai.service", service); provideErr != nil {
		return provideErr
	}
	if err != nil {
		// An invalid optional provider is represented through Status and must
		// not prevent the rest of CampusOS from starting.
		return nil
	}
	return nil
}

func (m *Module) Stop(context.Context) error { return nil }
func (m *Module) Health(context.Context) platformmodule.Health {
	if m.service == nil || m.handler == nil {
		return platformmodule.Health{Status: platformmodule.HealthUnhealthy, Message: "AI module is not started"}
	}
	status := m.service.Status()
	if status.Enabled && !status.Ready {
		return platformmodule.Health{Status: platformmodule.HealthDegraded, Message: status.Error}
	}
	return platformmodule.Health{Status: platformmodule.HealthHealthy}
}
func (m *Module) Service() *Service { return m.service }
func (m *Module) Handler() *Handler { return m.handler }
