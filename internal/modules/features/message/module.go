package message

import (
	"context"
	"errors"
	"fmt"

	platformmodule "github.com/campusos/CampusOS/internal/platform/module"
	"github.com/campusos/CampusOS/pkg/observability"
)

const ModuleID = "integration.message"
const portStore = "integration.message.adapter.store"

type Module struct {
	metrics *observability.Collector
	app     *platformmodule.AppContext
	service *Service
	handler *Handler
}

func NewModule(metrics *observability.Collector) *Module { return &Module{metrics: metrics} }
func (m *Module) ID() string                             { return ModuleID }
func (m *Module) Dependencies() []string                 { return nil }
func (m *Module) Register(app *platformmodule.AppContext) error {
	if app == nil {
		return errors.New("message module app context is required")
	}
	m.app = app
	return nil
}
func (m *Module) Start(context.Context) error {
	value, ok := m.app.Lookup(portStore)
	if !ok {
		return errors.New("message store adapter is unavailable")
	}
	store, ok := value.(Store)
	if !ok {
		return fmt.Errorf("message store adapter has incompatible type %T", value)
	}
	m.service = NewService(store, m.metrics)
	m.handler = NewHandler(m.service)
	return m.app.Provide("integration.message.service", m.service)
}
func (m *Module) Stop(context.Context) error { return nil }
func (m *Module) Health(context.Context) platformmodule.Health {
	if m.service == nil || m.handler == nil {
		return platformmodule.Health{Status: platformmodule.HealthUnhealthy, Message: "message module is not started"}
	}
	return platformmodule.Health{Status: platformmodule.HealthHealthy}
}
func (m *Module) Service() *Service { return m.service }
func (m *Module) Handler() *Handler { return m.handler }
