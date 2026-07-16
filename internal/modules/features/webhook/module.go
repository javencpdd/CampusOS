package webhook

import (
	"context"
	"errors"
	"fmt"

	platformmodule "github.com/campusos/CampusOS/internal/platform/module"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/pkg/eventbus"
	"github.com/campusos/CampusOS/pkg/observability"
)

const ModuleID = "integration.webhook"
const portStore = "integration.webhook.adapter.store"

type Config struct {
	EgressPolicy EgressPolicy
}

type Module struct {
	metrics *observability.Collector
	config  Config
	app     *platformmodule.AppContext
	service *Service
	handler *Handler
}

func NewModule(metrics *observability.Collector, config ...Config) *Module {
	module := &Module{metrics: metrics}
	if len(config) > 0 {
		module.config = config[0]
	}
	return module
}
func (m *Module) ID() string             { return ModuleID }
func (m *Module) Dependencies() []string { return []string{"core.event-bus", reliability.ModuleID} }
func (m *Module) Register(app *platformmodule.AppContext) error {
	if app == nil {
		return errors.New("webhook module app context is required")
	}
	m.app = app
	return nil
}
func (m *Module) Start(context.Context) error {
	storeValue, ok := m.app.Lookup(portStore)
	if !ok {
		return errors.New("webhook store adapter is unavailable")
	}
	store, ok := storeValue.(Store)
	if !ok {
		return fmt.Errorf("webhook store adapter has incompatible type %T", storeValue)
	}
	busValue, ok := m.app.Lookup("platform.event-bus")
	if !ok {
		return errors.New("webhook event bus port is unavailable")
	}
	bus, ok := busValue.(eventbus.EventBus)
	if !ok || bus == nil {
		return fmt.Errorf("webhook event bus port has incompatible type %T", busValue)
	}
	reliabilityValue, ok := m.app.Lookup("platform.reliability.service")
	if !ok {
		return errors.New("webhook reliability port is unavailable")
	}
	reliable, ok := reliabilityValue.(*reliability.Service)
	if !ok || reliable == nil {
		return fmt.Errorf("webhook reliability port has incompatible type %T", reliabilityValue)
	}
	m.service = NewService(store, m.metrics, reliable)
	m.service.SetEgressPolicy(m.config.EgressPolicy)
	if err := m.service.Register(bus); err != nil {
		return err
	}
	m.handler = NewHandler(m.service)
	return m.app.Provide("integration.webhook.service", m.service)
}
func (m *Module) Stop(context.Context) error { return nil }
func (m *Module) Health(context.Context) platformmodule.Health {
	if m.service == nil || m.handler == nil {
		return platformmodule.Health{Status: platformmodule.HealthUnhealthy, Message: "webhook module is not started"}
	}
	return platformmodule.Health{Status: platformmodule.HealthHealthy}
}
func (m *Module) Service() *Service { return m.service }
func (m *Module) Handler() *Handler { return m.handler }
