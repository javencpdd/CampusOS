package academicterm

import (
	"context"
	"errors"
	"fmt"

	platformmodule "github.com/campusos/CampusOS/internal/platform/module"
)

const portRepository = "academic-term.adapter.repository"

type Module struct {
	repository Repository
	service    *Service
	handler    *Handler
}

func NewModule() *Module                 { return &Module{} }
func (m *Module) ID() string             { return ModuleID }
func (m *Module) Dependencies() []string { return []string{"core.identity"} }

func (m *Module) Register(app *platformmodule.AppContext) error {
	if app == nil {
		return errors.New("academic term app context is required")
	}
	repository := Repository(NewMemoryRepository())
	if value, ok := app.Lookup(portRepository); ok {
		var compatible bool
		repository, compatible = value.(Repository)
		if !compatible || repository == nil {
			return fmt.Errorf("academic term repository has incompatible type %T", value)
		}
	}
	service, err := NewService(repository)
	if err != nil {
		return err
	}
	m.repository, m.service, m.handler = repository, service, NewHandler(service)
	if err := app.Provide("academic-term.service", Port(service)); err != nil {
		return err
	}
	return nil
}

func (m *Module) Start(context.Context) error { return nil }
func (m *Module) Stop(context.Context) error  { return nil }
func (m *Module) Health(context.Context) platformmodule.Health {
	if m.repository == nil || m.service == nil || m.handler == nil {
		return platformmodule.Health{Status: platformmodule.HealthUnhealthy, Message: "academic term service is unavailable"}
	}
	return platformmodule.Health{Status: platformmodule.HealthHealthy}
}
func (m *Module) Service() *Service { return m.service }
func (m *Module) Handler() *Handler { return m.handler }
