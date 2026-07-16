package reliability

import (
	"context"
	"errors"
	"fmt"
	"log"

	platformmodule "github.com/campusos/CampusOS/internal/platform/module"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/campusos/CampusOS/pkg/eventbus"
)

const ModuleID = "core.reliability"

const (
	portStore        = "platform.reliability.adapter.store"
	portTransactions = "platform.reliability.adapter.transactions"
	portService      = "platform.reliability.service"
	portHandler      = "platform.reliability.http-handler"
)

// Module owns the platform-level durable command and outbox services. It is a
// Core Module and is not visible as an installable external plugin.
type Module struct {
	app     *platformmodule.AppContext
	service *Service
	handler *Handler
}

func NewModule() *Module                 { return &Module{} }
func (m *Module) ID() string             { return ModuleID }
func (m *Module) Dependencies() []string { return []string{"core.event-bus"} }

func (m *Module) Register(app *platformmodule.AppContext) error {
	if app == nil {
		return errors.New("reliability module app context is required")
	}
	storeValue, ok := app.Lookup(portStore)
	if !ok {
		return errors.New("reliability store adapter is unavailable")
	}
	store, ok := storeValue.(Store)
	if !ok {
		return fmt.Errorf("reliability store adapter has incompatible type %T", storeValue)
	}
	transactionValue, ok := app.Lookup(portTransactions)
	if !ok {
		return errors.New("reliability transaction adapter is unavailable")
	}
	transactions, ok := transactionValue.(transaction.Manager)
	if !ok {
		return fmt.Errorf("reliability transaction adapter has incompatible type %T", transactionValue)
	}
	m.app = app
	m.service = NewService(transactions, store)
	m.handler = NewHandler(m.service)
	if err := m.app.Provide(portService, m.service); err != nil {
		return err
	}
	return m.app.Provide(portHandler, m.handler)
}

func (m *Module) Start(ctx context.Context) error {
	if m.app == nil || m.service == nil || m.handler == nil {
		return errors.New("reliability module is not registered")
	}
	busValue, ok := m.app.Lookup("platform.event-bus")
	if !ok {
		return errors.New("reliability event bus port is unavailable")
	}
	bus, ok := busValue.(eventbus.EventBus)
	if !ok || bus == nil {
		return fmt.Errorf("reliability event bus port has incompatible type %T", busValue)
	}
	m.service.SetFallbackHandler(DefaultEventBusHandler(bus))
	if recovered, err := m.service.RecoverInterruptedOperations(ctx); err != nil {
		// Keep a pre-migration server readable during the additive rollout. The
		// reliability APIs will surface the missing migration, and production
		// deploys must apply 000027 before enabling v11 write paths.
		log.Printf("reliability interrupted-operation recovery skipped: %v", err)
	} else if len(recovered) > 0 {
		log.Printf("reliability marked %d interrupted operations as failed for explicit recovery", len(recovered))
	}
	m.service.Start(ctx)
	return nil
}

func (m *Module) Stop(ctx context.Context) error {
	if m.service == nil {
		return nil
	}
	return m.service.Stop(ctx)
}

func (m *Module) Health(ctx context.Context) platformmodule.Health {
	if m.service == nil {
		return platformmodule.Health{Status: platformmodule.HealthUnhealthy, Message: "reliability service is not started"}
	}
	summary, err := m.service.Summary(ctx)
	if err != nil {
		return platformmodule.Health{Status: platformmodule.HealthUnhealthy, Message: err.Error()}
	}
	if summary.Dead > 0 {
		return platformmodule.Health{Status: platformmodule.HealthDegraded, Message: fmt.Sprintf("%d dead-letter events", summary.Dead)}
	}
	return platformmodule.Health{Status: platformmodule.HealthHealthy}
}

func (m *Module) Service() *Service { return m.service }
func (m *Module) Handler() *Handler { return m.handler }
