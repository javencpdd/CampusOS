package emaildelivery

import (
	"context"
	"errors"
	"fmt"

	identitycore "github.com/campusos/CampusOS/internal/modules/core/identity"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	platformmodule "github.com/campusos/CampusOS/internal/platform/module"
	"github.com/campusos/CampusOS/internal/platform/reliability"
)

const ModuleID = "core.email-delivery"

type Module struct {
	config   Config
	app      *platformmodule.AppContext
	reliable *reliability.Service
	service  *Service
	handler  *Handler
}

func NewModule(config Config) *Module { return &Module{config: config} }

func (m *Module) ID() string { return ModuleID }

func (m *Module) Dependencies() []string {
	return []string{identitycore.ModuleID, reliability.ModuleID}
}

func (m *Module) Register(app *platformmodule.AppContext) error {
	if app == nil {
		return errors.New("email delivery module app context is required")
	}
	m.app = app
	return nil
}

func (m *Module) Start(context.Context) error {
	if m.app == nil {
		return errors.New("email delivery module is not registered")
	}
	dispatchValue, ok := m.app.Lookup("identity.challenge-dispatch-reader")
	if !ok {
		return errors.New("identity challenge dispatch reader port is unavailable")
	}
	dispatch, ok := dispatchValue.(identityport.ChallengeDispatchReader)
	if !ok {
		return fmt.Errorf("identity challenge dispatch reader port has incompatible type %T", dispatchValue)
	}
	reliabilityValue, ok := m.app.Lookup("platform.reliability.service")
	if !ok {
		return errors.New("reliability service port is unavailable")
	}
	reliable, ok := reliabilityValue.(*reliability.Service)
	if !ok || reliable == nil {
		return fmt.Errorf("reliability service port has incompatible type %T", reliabilityValue)
	}
	sender, err := NewSender(m.config)
	if err != nil {
		return fmt.Errorf("initialize email provider: %w", err)
	}
	service, err := NewService(dispatch, reliable, sender)
	if err != nil {
		return err
	}
	reliable.RegisterConsumer(ChallengeRequestedEvent, ChallengeConsumer, service.DeliverChallenge)
	m.reliable = reliable
	m.service = service
	m.handler = NewHandler(service)
	return nil
}

func (m *Module) Stop(context.Context) error {
	if m.reliable != nil {
		m.reliable.RegisterConsumer(ChallengeRequestedEvent, ChallengeConsumer, nil)
	}
	return nil
}

func (m *Module) Health(context.Context) platformmodule.Health {
	if m.service == nil || m.handler == nil {
		return platformmodule.Health{Status: platformmodule.HealthUnhealthy, Message: "email delivery services are not started"}
	}
	status := m.service.Status()
	if status.State == "degraded" {
		return platformmodule.Health{Status: platformmodule.HealthDegraded, Message: "email provider delivery is degraded"}
	}
	return platformmodule.Health{Status: platformmodule.HealthHealthy}
}

func (m *Module) Service() *Service { return m.service }

func (m *Module) Handler() *Handler { return m.handler }
