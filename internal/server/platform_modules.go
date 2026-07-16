package server

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	communityport "github.com/campusos/CampusOS/internal/modules/core/community/port"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	corestorage "github.com/campusos/CampusOS/internal/modules/core/userstorage"
	platformfeature "github.com/campusos/CampusOS/internal/platform/feature"
	platformmodule "github.com/campusos/CampusOS/internal/platform/module"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/plugin"
	plugingrpc "github.com/campusos/CampusOS/internal/plugin/grpc"
	"github.com/campusos/CampusOS/internal/plugin/hostapi"
	pluginport "github.com/campusos/CampusOS/internal/plugin/port"
	pluginwasm "github.com/campusos/CampusOS/internal/plugin/wasm"
	modulecatalog "github.com/campusos/CampusOS/modules"
	"github.com/campusos/CampusOS/pkg/config"
	"github.com/campusos/CampusOS/pkg/eventbus"
)

const (
	moduleEventBus       = "core.event-bus"
	moduleFeatureConfig  = "core.feature-registry"
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
	app      *platformmodule.AppContext
	bus      eventbus.EventBus
	memory   *eventbus.MemoryEventBus
	fallback bool
}

func newEventBusModule(cfg *config.Config) *eventBusModule {
	return &eventBusModule{cfg: cfg}
}

func (m *eventBusModule) ID() string             { return moduleEventBus }
func (m *eventBusModule) Dependencies() []string { return nil }
func (m *eventBusModule) Register(app *platformmodule.AppContext) error {
	m.app = app
	return nil
}

func (m *eventBusModule) Start(context.Context) error {
	natsBus, err := eventbus.NewNATSEventBus(m.cfg.NATS.URL)
	if err != nil {
		log.Printf("⚠️  NATS 连接失败，回退到内存事件总线: %v", err)
		m.memory = eventbus.NewMemoryEventBus()
		m.bus = m.memory
		m.fallback = true
		return m.publishPort()
	}
	m.bus = natsBus
	// EventLog keeps its existing process-local read model while domain events
	// continue through NATS.
	m.memory = eventbus.NewMemoryEventBus()
	m.fallback = false
	return m.publishPort()
}

func (m *eventBusModule) publishPort() error {
	if m.app == nil {
		return fmt.Errorf("event bus module app context is unavailable")
	}
	if err := m.app.Provide("platform.event-bus", m.bus); err != nil {
		return err
	}
	return m.app.Provide("platform.memory-event-bus", m.memory)
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

// featureRegistryModule owns Built-in Feature state independently from the
// external plugin platform. Legacy manifests are imported later through an
// explicit one-time compatibility seed.
type featureRegistryModule struct {
	owner    *Server
	store    platformfeature.Store
	registry *platformfeature.Registry
	catalog  *modulecatalog.Catalog
	handler  *platformfeature.Handler
}

func newFeatureRegistryModule(owner *Server, store platformfeature.Store) *featureRegistryModule {
	return &featureRegistryModule{owner: owner, store: store}
}

func (m *featureRegistryModule) ID() string             { return moduleFeatureConfig }
func (m *featureRegistryModule) Dependencies() []string { return nil }
func (m *featureRegistryModule) Register(app *platformmodule.AppContext) error {
	catalog, err := modulecatalog.Load()
	if err != nil {
		return fmt.Errorf("load built-in module catalog: %w", err)
	}
	m.registry = platformfeature.NewAuthoritativeRegistry(m.store)
	for _, descriptor := range catalog.FeatureDescriptors() {
		def := platformfeature.Definition{
			ID:             descriptor.FeatureID,
			Mode:           platformfeature.ActivationMode(descriptor.ActivationMode),
			Dependencies:   append([]string(nil), descriptor.Dependencies...),
			DefaultEnabled: descriptor.DefaultEnabled,
			DefaultConfig:  modulecatalog.DeepCopyConfig(descriptor.Config),
		}
		if err := m.registry.Register(def); err != nil {
			return err
		}
	}
	m.catalog = catalog
	m.handler = platformfeature.NewHandler(m.registry, catalog)
	m.owner.features = m.registry
	m.owner.featureHandler = m.handler
	m.owner.moduleCatalog = catalog
	if err := app.Provide("platform.feature-registry", m.registry); err != nil {
		return err
	}
	if err := app.Provide("platform.module-catalog", catalog); err != nil {
		return err
	}
	return app.Provide("feature.http-handler", m.handler)
}
func (m *featureRegistryModule) Registry() *platformfeature.Registry { return m.registry }
func (m *featureRegistryModule) Catalog() *modulecatalog.Catalog     { return m.catalog }
func (m *featureRegistryModule) Handler() *platformfeature.Handler   { return m.handler }

type pluginPlatformModule struct {
	owner       *Server
	events      *eventBusModule
	features    *featureRegistryModule
	app         *platformmodule.AppContext
	repository  plugin.PluginRepository
	marketStore plugin.MarketStore
	manager     *plugin.Manager
	market      *plugin.MarketService
	grpcRuntime *plugingrpc.GRPCRuntime
	handler     *plugin.Handler
	hostAPI     *hostapi.HostAPIServer
	cancel      context.CancelFunc
}

func newPluginPlatformModule(owner *Server, events *eventBusModule, features *featureRegistryModule, repository plugin.PluginRepository, marketStore plugin.MarketStore) *pluginPlatformModule {
	return &pluginPlatformModule{owner: owner, events: events, features: features, repository: repository, marketStore: marketStore}
}

func (m *pluginPlatformModule) ID() string { return modulePluginPlatform }
func (m *pluginPlatformModule) Dependencies() []string {
	return []string{moduleEventBus, moduleFeatureConfig, corestorage.ModuleID, reliability.ModuleID}
}

func (m *pluginPlatformModule) Register(app *platformmodule.AppContext) error {
	m.app = app
	m.manager = plugin.NewManager()
	if reliabilityValue, ok := app.Lookup("platform.reliability.service"); ok {
		reliable, compatible := reliabilityValue.(*reliability.Service)
		if !compatible || reliable == nil {
			return fmt.Errorf("plugin reliability port has incompatible type %T", reliabilityValue)
		}
		m.manager.SetOperationTracker(reliabilityPluginOperationTracker{service: reliable})
		m.manager.SetCompatibilityReporter(reliabilityPluginOperationTracker{service: reliable})
	}
	if m.repository != nil {
		m.manager.SetPluginRepository(m.repository)
	}
	m.grpcRuntime = plugingrpc.NewGRPCRuntime()
	m.manager.RegisterRuntime("grpc", m.grpcRuntime)
	m.manager.RegisterRuntime("wasm", pluginwasm.NewRuntime())
	m.owner.manager = m.manager
	if m.features == nil || m.features.Registry() == nil {
		return errors.New("authoritative feature registry is unavailable")
	}
	if err := app.Provide(portPluginManager, m.manager); err != nil {
		return err
	}
	return app.Provide("plugin.catalog", pluginport.NewCatalogAdapter(m.manager.Catalog()))
}

type reliabilityPluginOperationTracker struct {
	service *reliability.Service
}

func (t reliabilityPluginOperationTracker) Track(ctx context.Context, request plugin.OperationRequest, action func(context.Context) error) error {
	if t.service == nil {
		return action(ctx)
	}
	return t.service.TrackOperation(ctx, reliability.Operation{
		Kind: request.Kind, SubjectType: request.SubjectType, SubjectID: request.SubjectID,
		ActorID: request.ActorID, IdempotencyKey: request.IdempotencyKey,
	}, action)
}

func (t reliabilityPluginOperationTracker) RecordCompatibility(ctx context.Context, key, kind string, detail any) error {
	if t.service == nil {
		return nil
	}
	return t.service.RecordCompatibility(ctx, key, kind, detail)
}

func (m *pluginPlatformModule) Start(ctx context.Context) error {
	if m.manager == nil || m.events.EventBus() == nil {
		return fmt.Errorf("plugin platform dependencies are not registered")
	}
	m.owner.registerDefaultSubscriptions(m.events.EventBus())
	if err := m.manager.InstallFromPluginsDir(plugin.PluginsDirFromEnv()); err != nil {
		log.Printf("⚠️  加载插件失败: %v", err)
	}
	m.manager.StartDesiredPlugins(plugin.ScopeSystem)
	m.manager.StartDesiredPlugins(plugin.ScopeUser)
	storageValue, ok := m.app.Lookup("storage.user")
	if !ok {
		return errors.New("user storage port is unavailable for plugin market")
	}
	userStorage, ok := storageValue.(corestorage.Port)
	if !ok {
		return fmt.Errorf("user storage port has incompatible type %T", storageValue)
	}
	m.market = plugin.NewMarketService(m.marketStore, userStorage, func(name string) (*plugin.Manifest, bool) {
		installed, found := m.manager.GetPlugin(name)
		if !found || installed == nil || installed.Manifest == nil {
			return nil, false
		}
		return installed.Manifest, true
	})
	m.market.SetPluginActiveResolver(func(name string) bool {
		installed, found := m.manager.GetPlugin(name)
		return found && installed != nil && installed.Status == plugin.StatusRunning
	})
	if err := m.market.SyncCatalog(ctx, m.manager.ListPlugins()); err != nil {
		return fmt.Errorf("sync plugin market catalog: %w", err)
	}
	m.handler = plugin.NewHandler(m.manager, plugin.WithPluginsDir(plugin.PluginsDirFromEnv()), plugin.WithBuiltinFeatureCompatibility(m.features.Handler()), plugin.WithMarketService(m.market))
	if err := m.app.Provide("plugin.http-handler", m.handler); err != nil {
		return err
	}
	if err := m.startHostAPI(); err != nil {
		return err
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
	if m.hostAPI != nil {
		m.hostAPI.Stop()
		m.hostAPI = nil
	}
	return nil
}

func (m *pluginPlatformModule) startHostAPI() error {
	if !m.owner.cfg.HostAPI.Enabled {
		return nil
	}
	if m.app == nil {
		return errors.New("plugin platform app context is unavailable")
	}
	usersValue, ok := m.app.Lookup("identity.user-reader")
	if !ok {
		return errors.New("identity user reader port is unavailable for Host API")
	}
	users, ok := usersValue.(identityport.UserReader)
	if !ok {
		return fmt.Errorf("identity user reader port has incompatible type %T", usersValue)
	}
	threadsValue, ok := m.app.Lookup("community.content-query")
	if !ok {
		return errors.New("community content query port is unavailable for Host API")
	}
	threads, ok := threadsValue.(communityport.ContentQuery)
	if !ok {
		return fmt.Errorf("community content query port has incompatible type %T", threadsValue)
	}
	postsValue, ok := m.app.Lookup("community.moderation-gateway")
	if !ok {
		return errors.New("community post reader port is unavailable for Host API")
	}
	posts, ok := postsValue.(hostapi.PostReader)
	if !ok {
		return fmt.Errorf("community post reader port has incompatible type %T", postsValue)
	}
	permissionValue, ok := m.app.Lookup("identity.authorization")
	if !ok {
		return errors.New("identity authorization port is unavailable for Host API")
	}
	permission, ok := permissionValue.(identityport.Authorization)
	if !ok {
		return fmt.Errorf("identity authorization port has incompatible type %T", permissionValue)
	}
	api := hostapi.NewHostAPIv2FromHostAPI(hostapi.NewHostAPIWithContentQuery(users, threads, posts, m.events.EventBus()))
	api.SetPluginRepository(m.repository)
	if store, err := hostapi.NewSQLiteKVStore(m.owner.cfg.Plugin.DataDir); err != nil {
		log.Printf("⚠️ SQLite 插件 KV 初始化失败，回退到内存存储: %v", err)
	} else {
		api.SetStorageStore(store)
	}
	api.SetPermissionChecker(permission)
	api.SetMarketService(m.market)
	server := hostapi.NewHostAPIServer(api, m.owner.cfg.HostAPI.Addr, m.manager.GetPlugin)
	server.SetPluginAuthenticator(m.manager.AuthorizeHostAPI)
	if err := server.Start(); err != nil {
		return err
	}
	m.hostAPI = server
	return nil
}

func (m *pluginPlatformModule) Health(context.Context) platformmodule.Health {
	if m.manager == nil {
		return platformmodule.Health{Status: platformmodule.HealthUnhealthy, Message: "plugin manager is unavailable"}
	}
	return platformmodule.Health{Status: platformmodule.HealthHealthy}
}
