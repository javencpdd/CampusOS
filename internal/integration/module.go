package integration

import (
	"context"
	"errors"
	"fmt"

	"github.com/campusos/CampusOS/internal/ai"
	"github.com/campusos/CampusOS/internal/mcp"
	"github.com/campusos/CampusOS/internal/message"
	platformmodule "github.com/campusos/CampusOS/internal/platform/module"
	"github.com/campusos/CampusOS/internal/platformlog"
	"github.com/campusos/CampusOS/internal/plugin"
	"github.com/campusos/CampusOS/internal/space"
	"github.com/campusos/CampusOS/internal/webhook"
	"github.com/campusos/CampusOS/pkg/config"
	"github.com/campusos/CampusOS/pkg/observability"
	"github.com/jackc/pgx/v5/pgxpool"
)

const ModuleID = "integration.overview"

type ModuleConfig struct {
	Pool    *pgxpool.Pool
	Config  *config.Config
	Metrics *observability.Collector
}

// Module owns the legacy Integration Overview composition. It consumes
// explicitly published integration service ports, keeping Server out of
// summary aggregation while preserving the existing overview HTTP contract.
type Module struct {
	config  ModuleConfig
	app     *platformmodule.AppContext
	handler *Handler
}

func NewModule(config ModuleConfig) *Module { return &Module{config: config} }
func (m *Module) ID() string                { return ModuleID }
func (m *Module) Dependencies() []string {
	return []string{
		"core.plugin-platform",
		"feature.personal-space",
		ai.ModuleID,
		webhook.ModuleID,
		mcp.ModuleID,
		message.ModuleID,
		platformlog.ModuleID,
	}
}
func (m *Module) Register(app *platformmodule.AppContext) error {
	if app == nil {
		return errors.New("integration overview app context is required")
	}
	m.app = app
	return nil
}
func (m *Module) Start(context.Context) error {
	manager, err := lookup[*plugin.Manager](m.app, "plugin.manager")
	if err != nil {
		return err
	}
	spaceService, err := lookup[*space.Service](m.app, "feature.personal-space.service")
	if err != nil {
		return err
	}
	aiService, err := lookup[*ai.Service](m.app, "integration.ai.service")
	if err != nil {
		return err
	}
	webhookService, err := lookup[*webhook.Service](m.app, "integration.webhook.service")
	if err != nil {
		return err
	}
	mcpService, err := lookup[*mcp.Service](m.app, "integration.mcp.service")
	if err != nil {
		return err
	}
	messageService, err := lookup[*message.Service](m.app, "integration.message.service")
	if err != nil {
		return err
	}
	if _, err := lookup[*platformlog.Service](m.app, "integration.platform-log.service"); err != nil {
		return err
	}
	legacySummarySource := NewHandler(
		WithPool(m.config.Pool),
		WithConfig(m.config.Config),
		WithPluginManager(manager),
		WithAIService(aiService),
		WithSpaceService(spaceService),
		WithWebhookService(webhookService),
		WithMCPService(mcpService),
		WithMessageService(messageService),
		WithMetrics(m.config.Metrics),
	)
	m.handler = NewHandler(
		WithMetrics(m.config.Metrics),
		WithSummaryPorts(
			SummaryPortFunc(legacySummarySource.databaseCard),
			SummaryPortFunc(func(context.Context) Card { return legacySummarySource.pluginCard() }),
			SummaryPortFunc(func(context.Context) Card { return legacySummarySource.aiCard() }),
			SummaryPortFunc(legacySummarySource.spaceCard),
			SummaryPortFunc(legacySummarySource.webhookCard),
			SummaryPortFunc(legacySummarySource.mcpCard),
			SummaryPortFunc(legacySummarySource.messageCard),
			SummaryPortFunc(func(context.Context) Card { return legacySummarySource.metricsCard() }),
		),
	)
	return m.app.Provide("integration.overview.handler", m.handler)
}
func (m *Module) Stop(context.Context) error { return nil }
func (m *Module) Health(context.Context) platformmodule.Health {
	if m.handler == nil {
		return platformmodule.Health{Status: platformmodule.HealthUnhealthy, Message: "integration overview is not started"}
	}
	return platformmodule.Health{Status: platformmodule.HealthHealthy}
}
func (m *Module) Handler() *Handler { return m.handler }

func lookup[T any](app *platformmodule.AppContext, name string) (T, error) {
	var zero T
	value, ok := app.Lookup(name)
	if !ok {
		return zero, fmt.Errorf("integration port %q is unavailable", name)
	}
	result, ok := value.(T)
	if !ok {
		return zero, fmt.Errorf("integration port %q has incompatible type %T", name, value)
	}
	return result, nil
}
