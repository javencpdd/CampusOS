package mcp

import (
	"context"
	"errors"
	"fmt"

	communityport "github.com/campusos/CampusOS/internal/community/port"
	platformmodule "github.com/campusos/CampusOS/internal/platform/module"
	"github.com/campusos/CampusOS/pkg/observability"
)

const ModuleID = "integration.mcp"
const portAuditStore = "integration.mcp.adapter.audit-store"

type Module struct {
	metrics *observability.Collector
	app     *platformmodule.AppContext
	service *Service
	handler *Handler
}

func NewModule(metrics *observability.Collector) *Module { return &Module{metrics: metrics} }
func (m *Module) ID() string                             { return ModuleID }
func (m *Module) Dependencies() []string                 { return []string{"core.community"} }
func (m *Module) Register(app *platformmodule.AppContext) error {
	if app == nil {
		return errors.New("MCP module app context is required")
	}
	m.app = app
	return nil
}
func (m *Module) Start(context.Context) error {
	categoriesValue, ok := m.app.Lookup("community.category-catalog")
	if !ok {
		return errors.New("community category catalog port is unavailable")
	}
	categories, ok := categoriesValue.(communityport.CategoryCatalog)
	if !ok {
		return fmt.Errorf("community category catalog port has incompatible type %T", categoriesValue)
	}
	threadsValue, ok := m.app.Lookup("community.content-gateway")
	if !ok {
		return errors.New("community content gateway port is unavailable")
	}
	threads, ok := threadsValue.(communityport.ContentGateway)
	if !ok {
		return fmt.Errorf("community content gateway port has incompatible type %T", threadsValue)
	}
	auditValue, ok := m.app.Lookup(portAuditStore)
	if !ok {
		return errors.New("MCP audit store adapter is unavailable")
	}
	audit, ok := auditValue.(AuditStore)
	if !ok {
		return fmt.Errorf("MCP audit store adapter has incompatible type %T", auditValue)
	}
	m.service = NewService(categories, threads, audit, m.metrics)
	m.handler = NewHandler(m.service)
	return m.app.Provide("integration.mcp.service", m.service)
}
func (m *Module) Stop(context.Context) error { return nil }
func (m *Module) Health(context.Context) platformmodule.Health {
	if m.service == nil || m.handler == nil {
		return platformmodule.Health{Status: platformmodule.HealthUnhealthy, Message: "MCP module is not started"}
	}
	return platformmodule.Health{Status: platformmodule.HealthHealthy}
}
func (m *Module) Service() *Service { return m.service }
func (m *Module) Handler() *Handler { return m.handler }
