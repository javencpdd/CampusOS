package platformlog

import (
	"context"

	platformmodule "github.com/campusos/CampusOS/internal/platform/module"
)

const ModuleID = "integration.platform-log"

type Module struct {
	app     *platformmodule.AppContext
	service *Service
	handler *Handler
}

func NewModule() *Module                 { return &Module{} }
func (m *Module) ID() string             { return ModuleID }
func (m *Module) Dependencies() []string { return nil }
func (m *Module) Register(app *platformmodule.AppContext) error {
	m.app = app
	return nil
}
func (m *Module) Start(context.Context) error {
	m.service = NewServiceFromEnv()
	m.handler = NewHandler(m.service)
	return m.app.Provide("integration.platform-log.service", m.service)
}
func (m *Module) Stop(context.Context) error { return nil }
func (m *Module) Health(context.Context) platformmodule.Health {
	if m.service == nil || m.handler == nil {
		return platformmodule.Health{Status: platformmodule.HealthUnhealthy, Message: "platform log module is not started"}
	}
	return platformmodule.Health{Status: platformmodule.HealthHealthy}
}
func (m *Module) Service() *Service { return m.service }
func (m *Module) Handler() *Handler { return m.handler }
