package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/campusos/CampusOS/internal/appearance"

	platformfeature "github.com/campusos/CampusOS/internal/platform/feature"
	platformmodule "github.com/campusos/CampusOS/internal/platform/module"
	"github.com/campusos/CampusOS/internal/plugin"
	pluginbuiltin "github.com/campusos/CampusOS/internal/plugin/builtin"
	plugingrpc "github.com/campusos/CampusOS/internal/plugin/grpc"
	pluginwasm "github.com/campusos/CampusOS/internal/plugin/wasm"
	"github.com/campusos/CampusOS/pkg/config"
	"github.com/campusos/CampusOS/pkg/eventbus"
)

const (
	moduleEventBus       = "core.event-bus"
	modulePluginPlatform = "core.plugin-platform"
	portPluginManager    = "plugin.manager"
)

type coreBoundaryModule struct {
	id           string
	dependencies []string
}

func (m coreBoundaryModule) ID() string             { return m.id }
func (m coreBoundaryModule) Dependencies() []string { return append([]string(nil), m.dependencies...) }
func (m coreBoundaryModule) Register(app *platformmodule.AppContext) error {
	return app.Provide("module.boundary."+m.id, m)
}

type eventBusModule struct {
	cfg      *config.Config
	bus      eventbus.EventBus
	memory   *eventbus.MemoryEventBus
	fallback bool
}

func newEventBusModule(cfg *config.Config) *eventBusModule {
	return &eventBusModule{cfg: cfg}
}

func (m *eventBusModule) ID() string             { return moduleEventBus }
func (m *eventBusModule) Dependencies() []string { return nil }
func (m *eventBusModule) Register(*platformmodule.AppContext) error {
	return nil
}

func (m *eventBusModule) Start(context.Context) error {
	natsBus, err := eventbus.NewNATSEventBus(m.cfg.NATS.URL)
	if err != nil {
		log.Printf("⚠️  NATS 连接失败，回退到内存事件总线: %v", err)
		m.memory = eventbus.NewMemoryEventBus()
		m.bus = m.memory
		m.fallback = true
		return nil
	}
	m.bus = natsBus
	// EventLog keeps its existing process-local read model while domain events
	// continue through NATS.
	m.memory = eventbus.NewMemoryEventBus()
	m.fallback = false
	return nil
}

func (m *eventBusModule) Stop(context.Context) error {
	if m.bus == nil {
		return nil
	}
	err := m.bus.Close()
	m.bus = nil
	m.memory = nil
	return err
}

func (m *eventBusModule) Health(context.Context) platformmodule.Health {
	if m.bus == nil {
		return platformmodule.Health{Status: platformmodule.HealthUnhealthy, Message: "event bus is not started"}
	}
	if m.fallback {
		return platformmodule.Health{Status: platformmodule.HealthDegraded, Message: "using in-memory fallback"}
	}
	return platformmodule.Health{Status: platformmodule.HealthHealthy}
}

func (m *eventBusModule) EventBus() eventbus.EventBus         { return m.bus }
func (m *eventBusModule) MemoryBus() *eventbus.MemoryEventBus { return m.memory }

type pluginPlatformModule struct {
	owner       *Server
	events      *eventBusModule
	manager     *plugin.Manager
	grpcRuntime *plugingrpc.GRPCRuntime
	cancel      context.CancelFunc
}

func newPluginPlatformModule(owner *Server, events *eventBusModule) *pluginPlatformModule {
	return &pluginPlatformModule{owner: owner, events: events}
}

func (m *pluginPlatformModule) ID() string { return modulePluginPlatform }
func (m *pluginPlatformModule) Dependencies() []string {
	return []string{moduleEventBus}
}

func (m *pluginPlatformModule) Register(app *platformmodule.AppContext) error {
	m.manager = plugin.NewManager()
	m.grpcRuntime = plugingrpc.NewGRPCRuntime()
	m.manager.RegisterRuntime("grpc", m.grpcRuntime)
	m.manager.RegisterRuntime("wasm", pluginwasm.NewRuntime())
	builtinRuntime := pluginbuiltin.NewRuntime()
	builtinRuntime.RegisterExtension("campus-welcome", campusWelcomeExtension)
	m.manager.RegisterRuntime("builtin", builtinRuntime)
	m.owner.manager = m.manager
	m.owner.features = platformfeature.NewRegistry(m.manager.IsPluginRunning)
	m.owner.appearance = appearance.NewCompatibilityFacade()
	for _, def := range []platformfeature.Definition{
		{ID: "personal-space", Mode: platformfeature.Restart, Dependencies: []string{"core.identity", "core.user-storage"}, LegacyPlugin: "personal-space"},
		{ID: "controlled-richtext-article", Mode: platformfeature.Restart, Dependencies: []string{"core.community", "core.user-storage"}, LegacyPlugin: "controlled-richtext-article"},
		{ID: "personal-schedule", Mode: platformfeature.Restart, Dependencies: []string{"core.identity", "core.user-storage"}, LegacyPlugin: "personal-schedule"},
		{ID: "appearance", Mode: platformfeature.HotGated, LegacyPlugin: "web-theme"},
	} {
		if err := m.owner.features.Register(def); err != nil {
			return err
		}
	}
	return app.Provide(portPluginManager, m.manager)
}

func (m *pluginPlatformModule) Start(ctx context.Context) error {
	if m.manager == nil || m.events.EventBus() == nil {
		return fmt.Errorf("plugin platform dependencies are not registered")
	}
	m.owner.registerDefaultSubscriptions(m.events.EventBus())
	if err := m.manager.InstallFromPluginsDir(plugin.PluginsDirFromEnv()); err != nil {
		log.Printf("⚠️  加载插件失败: %v", err)
	}
	healthContext, cancel := context.WithCancel(ctx)
	m.cancel = cancel
	m.grpcRuntime.StartHealthChecker(healthContext, 10*time.Second, m.manager)
	return nil
}

func (m *pluginPlatformModule) Stop(context.Context) error {
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	if m.manager != nil {
		m.manager.StopAll()
	}
	return nil
}

func (m *pluginPlatformModule) Health(context.Context) platformmodule.Health {
	if m.manager == nil {
		return platformmodule.Health{Status: platformmodule.HealthUnhealthy, Message: "plugin manager is unavailable"}
	}
	return platformmodule.Health{Status: platformmodule.HealthHealthy}
}

func campusWelcomeExtension(_ context.Context, request *plugin.ExtensionRequest) (*plugin.ExtensionResponse, error) {
	body, _ := json.Marshal(map[string]interface{}{
		"message":  "CampusOS extension gateway is ready",
		"path":     request.Path,
		"caller":   map[string]string{"user_id": request.Caller.UserID, "username": request.Caller.Username},
		"trace_id": request.Caller.TraceID,
	})
	return &plugin.ExtensionResponse{
		Status:  200,
		Headers: map[string]string{"Content-Type": "application/json"},
		Body:    body,
	}, nil
}
