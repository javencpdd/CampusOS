package server

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	communityport "github.com/campusos/CampusOS/internal/modules/core/community/port"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	corestorage "github.com/campusos/CampusOS/internal/modules/core/userstorage"
	platformfeature "github.com/campusos/CampusOS/internal/platform/feature"
	platformmodule "github.com/campusos/CampusOS/internal/platform/module"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/plugin"
	plugincontainer "github.com/campusos/CampusOS/internal/plugin/container"
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
	owner              *Server
	events             *eventBusModule
	features           *featureRegistryModule
	app                *platformmodule.AppContext
	repository         plugin.PluginRepository
	marketStore        plugin.MarketStore
	authorizationStore plugin.AuthorizationStore
	authorization      *plugin.AuthorizationService
	secretStore        plugin.SecretStore
	identityStore      plugin.PluginV5IdentityStore
	stateStore         plugin.V5StateStore
	catalog            *plugin.PluginV5PackageCatalog
	platform           *plugin.V5Platform
	files              *plugin.V5ManagedFiles
	v5Handler          *plugin.V5HTTPHandler
	secrets            *plugin.SecretService
	manager            *plugin.Manager
	market             *plugin.MarketService
	grpcRuntime        *plugingrpc.GRPCRuntime
	handler            *plugin.Handler
	hostAPI            *hostapi.HostAPIServer
	cancel             context.CancelFunc
	workerDone         chan struct{}
	managed            *plugin.V5ManagedRuntime
	wasmCoordinator    *plugin.V5RuntimeCoordinator
	containerFactory   *plugincontainer.V5Factory
	eventStore         plugin.V5EventStore
	metadata           *plugin.V5EventService
	eventWorkerDone    chan struct{}
}

func newPluginPlatformModule(owner *Server, events *eventBusModule, features *featureRegistryModule, repository plugin.PluginRepository, marketStore plugin.MarketStore, authorizationStore plugin.AuthorizationStore, secretStore plugin.SecretStore, identityStore plugin.PluginV5IdentityStore, states ...plugin.V5StateStore) *pluginPlatformModule {
	var state plugin.V5StateStore
	if len(states) > 0 {
		state = states[0]
	}
	return &pluginPlatformModule{owner: owner, events: events, features: features, repository: repository, marketStore: marketStore, authorizationStore: authorizationStore, secretStore: secretStore, identityStore: identityStore, stateStore: state}
}

func (m *pluginPlatformModule) ID() string { return modulePluginPlatform }
func (m *pluginPlatformModule) Dependencies() []string {
	return []string{moduleEventBus, moduleFeatureConfig, corestorage.ModuleID, reliability.ModuleID, "core.identity"}
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
	// V12-03b retires legacy runtime registration in production.
	m.owner.manager = m.manager
	if m.features == nil || m.features.Registry() == nil {
		return errors.New("authoritative feature registry is unavailable")
	}
	if err := app.Provide(portPluginManager, m.manager); err != nil {
		return err
	}
	if err := app.Provide("plugin.catalog", &pluginport.V5CatalogAdapter{Current: func() *plugin.V5Platform { return m.platform }}); err != nil {
		return err
	}
	trust := []byte(`{"contract":"campusos.plugin-trust/v1","keys":[]}`)
	if name := os.Getenv("CAMPUSOS_PLUGIN_V5_TRUST_STORE"); name != "" {
		var err error
		trust, err = plugin.ReadPluginV5DocumentFile(name)
		if err != nil || len(trust) > plugin.PluginV5ManifestMaxBytes {
			return plugin.ErrPluginV5PackageSignature
		}
	}
	catalog, err := plugin.NewPluginV5PackageCatalogWithIdentityStore(trust, m.identityStore)
	if err != nil {
		return err
	}
	m.catalog = catalog
	return app.Provide("plugin.v5.package-catalog", catalog)
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
	if m.manager == nil || m.app == nil || m.catalog == nil {
		return errors.New("plugin platform is not registered")
	}
	reliableValue, _ := m.app.Lookup("platform.reliability.service")
	reliable, ok := reliableValue.(*reliability.Service)
	if !ok || reliable == nil {
		return errors.New("plugin reliability is unavailable")
	}
	adminValue, _ := m.app.Lookup("identity.admin-execution-authority")
	admins, ok := adminValue.(identityport.AdminExecutionAuthorityReader)
	if !ok {
		return errors.New("plugin admin authority is unavailable")
	}
	if m.stateStore == nil {
		m.stateStore = plugin.NewPgV5StateStore(nil)
	}
	var err error
	m.files, err = plugin.NewV5ManagedFiles(filepath.Join(m.owner.cfg.Plugin.DataDir, "v5"))
	if err != nil {
		return err
	}
	m.platform = plugin.NewV5Platform(m.stateStore, m.files, m.catalog, reliable, admins)
	users, _ := m.app.Lookup("identity.user-reader")
	m.platform.Users, _ = users.(identityport.UserReader)
	m.v5Handler = &plugin.V5HTTPHandler{Platform: m.platform}
	if m.owner.cfg.Plugin.UIListen != "" || m.owner.cfg.Plugin.UIOrigin != "" {
		if m.owner.cfg.Plugin.UIListen == "" {
			return errors.New("plugin UI requires a dedicated listener")
		}
		m.owner.pluginUI, err = plugin.NewV5UIHost(m.platform, m.owner.cfg.Plugin.UIOrigin, m.owner.cfg.Plugin.UIParents)
		if err != nil {
			return errors.New("plugin UI origin/parents configuration is invalid")
		}
		m.v5Handler.UIHost = m.owner.pluginUI
	}
	adminSessions, _ := m.app.Lookup("identity.admin-session-authority")
	m.v5Handler.AdminSessions, _ = adminSessions.(identityport.AdminSessionAuthorityReader)
	userSessions, _ := m.app.Lookup("identity.session-verifier")
	m.v5Handler.UserSessions, _ = userSessions.(identityport.SessionVerifier)
	strength, _ := m.app.Lookup("identity.session-strength")
	m.v5Handler.UserStrength, _ = strength.(identityport.SessionStrengthReader)
	// Legacy handlers remain only for historical tests and closed endpoints.
	m.handler = plugin.NewHandler(m.manager, plugin.WithBuiltinFeatureCompatibility(m.features.Handler()))
	if err := m.app.Provide("plugin.http-handler", m.handler); err != nil {
		return err
	}
	if err := m.app.Provide("plugin.v5.platform", m.platform); err != nil {
		return err
	}
	if err := m.app.Provide("plugin.v5.http-handler", m.v5Handler); err != nil {
		return err
	}
	var wasmFactory plugin.V5RuntimeFactory
	var containerFactory plugin.V5RuntimeFactory
	var wasmLeader plugin.V5RuntimeLeader
	var runnerErr error
	if m.owner.cfg.Plugin.WasmImage != "" || m.owner.cfg.Plugin.ContainerEnabled {
		if store, ok := m.stateStore.(*plugin.PgV5StateStore); ok {
			if m.owner.cfg.Plugin.WasmImage != "" {
				wasmFactory, runnerErr = pluginwasm.NewV5IsolatedFactoryForDeployment(m.owner.cfg.Plugin.WasmImage, store.RuntimeNamespace())
			}
			if runnerErr == nil && m.owner.cfg.Plugin.ContainerEnabled {
				m.containerFactory, runnerErr = plugincontainer.NewV5Factory(store.RuntimeNamespace())
				if runnerErr == nil {
					containerFactory = m.containerFactory
				}
			}
			if runnerErr == nil {
				wasmLeader, runnerErr = store.AcquireRuntimeLeader(ctx)
				if runnerErr == nil {
					if wasmFactory != nil {
						runnerErr = wasmFactory.(interface{ FenceOrphans(context.Context) error }).FenceOrphans(ctx)
					}
					if runnerErr == nil && m.containerFactory != nil {
						runnerErr = m.containerFactory.FenceOrphans(ctx)
					}
					if runnerErr != nil {
						_ = wasmLeader.Release(ctx)
						wasmLeader = nil
					}
				}
			}
		} else {
			runnerErr = plugin.ErrV5RuntimeMissing
		}
	}
	if runnerErr != nil && m.containerFactory != nil {
		closeCtx, stop := context.WithTimeout(context.Background(), 3*time.Second)
		_ = m.containerFactory.Close(closeCtx)
		stop()
		m.containerFactory = nil
		containerFactory = nil
	}
	directory := plugin.NewV5ContributionDirectory(nil)
	if runnerErr != nil {
		log.Printf("插件隔离执行器不可用，入口将保持关闭")
	} else if err := m.platform.ReconcileStartup(ctx); err != nil {
		log.Printf("插件 v5 对账暂不可用，入口将保持关闭")
	} else {
		directory = plugin.NewV5ContributionDirectory(m.platform)
	}
	if err := m.app.Provide("plugin.v5.contributions", directory); err != nil {
		return err
	}
	// A consumer Feature and managed transport must be bound by a compiled
	// adapter. Do not invent a registry Feature or couple built-in AI to plugins.
	if err := m.app.Provide("plugin.v5.chat-dispatcher", plugin.NewV5ChatDispatcher(directory, nil, "", nil)); err != nil {
		return err
	}
	m.managed = plugin.NewV5ManagedRuntime(directory)
	if runnerErr == nil && (wasmFactory != nil || containerFactory != nil) && wasmLeader != nil {
		m.wasmCoordinator = plugin.NewV5RuntimeCoordinator(m.platform, m.managed, plugin.NewV5RuntimeRouter(wasmFactory, containerFactory), wasmLeader)
		if err := m.app.Provide("plugin.v5.runtime-coordinator", m.wasmCoordinator); err != nil {
			return err
		}
		if err := m.app.Provide("plugin.v5.wasm-coordinator", m.wasmCoordinator); err != nil {
			return err
		}
	}
	if err := m.app.Provide("plugin.v5.runtime", m.managed); err != nil {
		return err
	}
	if err := m.app.Provide("plugin.v5.dispatcher", m.managed); err != nil {
		return err
	}
	if err := m.bindKnowledgeHost(); err != nil {
		return err
	}
	if err := m.bindAIHost(); err != nil {
		return err
	}
	var publicMetadata plugin.V5MetadataAuthorizer
	if value, ok := m.app.Lookup("community.content-query"); ok {
		if reader, ok := value.(communityport.ContentQuery); ok && reader != nil {
			if err := m.managed.BindHost("community.thread.public.read", plugin.V5PublicThreadEndpoint(reader, pluginCommunityGate{}, "core.community")); err != nil {
				return err
			}
			publicMetadata = func(ctx context.Context, kind, ref string) (bool, error) {
				if kind != "community.thread.public.changed/v1" || !strings.HasPrefix(ref, "thread:") {
					return false, nil
				}
				thread, err := reader.GetPublicThread(ctx, strings.TrimPrefix(ref, "thread:"))
				return err == nil && thread != nil && thread.FactsVersion > 0, err
			}
		}
	}
	if m.eventStore == nil {
		// An absent durable store stays unavailable; never silently fall back
		// to a process-local delivery queue in production.
		m.eventStore = plugin.NewPgV5EventStore(nil)
	}
	m.metadata = plugin.NewV5EventService(m.managed, m.eventStore, publicMetadata)
	m.metadata.Register()
	if err := m.app.Provide("plugin.v5.metadata-events", m.metadata); err != nil {
		return err
	}
	workerCtx, cancel := context.WithCancel(ctx)
	m.cancel = cancel
	m.workerDone = make(chan struct{})
	go func() {
		defer close(m.workerDone)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-workerCtx.Done():
				return
			case <-ticker.C:
				m.managed.Sweep(workerCtx)
				if _, err := m.platform.ProcessOnce(workerCtx, 16); err != nil && workerCtx.Err() == nil {
					log.Printf("插件 v5 操作等待重试")
				}
				if m.wasmCoordinator != nil {
					callCtx, stop := context.WithTimeout(workerCtx, 8*time.Second)
					_, _ = m.wasmCoordinator.ProcessOnce(callCtx)
					stop()
				}
			}
		}
	}()
	m.eventWorkerDone = make(chan struct{})
	go func() {
		defer close(m.eventWorkerDone)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-workerCtx.Done():
				return
			case <-ticker.C:
				// Callback IO, leases and retries are independent of both the
				// Core transaction and its durable Outbox worker.
				_, _ = m.metadata.ProcessOnce(workerCtx, 16)
			}
		}
	}()
	return nil
}

// Community is an always-on Core module. Its protected public-read Port is
// the authority; no synthetic optional Feature is added to the registry.
type pluginCommunityGate struct{}

func (pluginCommunityGate) Enabled(id string) bool { return id == "core.community" }

// ensurePDFViewerDefaultGrants creates auditable, revocable administrator
// grants for the compiled first-party reader. It is not an authorization
// bypass: a later administrator denial/revocation immediately wins, and every
// user still supplies their own consent for restricted file reads.
func (m *pluginPlatformModule) ensurePDFViewerDefaultGrants(ctx context.Context) error {
	if m.authorization == nil || !m.authorization.Available() {
		return errors.New("plugin authorization service is unavailable")
	}
	if _, installed := m.manager.V4Release(plugin.PDFViewerV4PluginName); !installed {
		// The host remains available when an administrator has not installed a
		// release yet. Preview requests then safely fall back to the existing
		// authenticated download path instead of making API startup fail.
		return nil
	}
	version, err := m.authorization.ActiveVersion(ctx, plugin.PDFViewerV4PluginName)
	if err != nil {
		return err
	}
	for _, capability := range []string{"article_attachment.self.preview", "personal_space_file.self.read", "plugin_ui.surface.open", "plugin_record.self.read", "plugin_record.self.write"} {
		// Set only the initial grant. An existing deny/revoke is a deliberate
		// administrator decision and must never be overwritten at startup.
		overview, err := m.authorization.Overview(ctx, plugin.PDFViewerV4PluginName, "")
		if err != nil {
			return err
		}
		found := false
		for _, grant := range overview.AdminGrants {
			if grant.CapabilityCode == capability {
				found = true
				break
			}
		}
		if !found {
			if _, err := m.authorization.SetAdminGrant(ctx, version.ID, capability, "granted", "系统首次注册第一方 PDF Viewer 的可撤销默认能力", map[string]interface{}{"scope": "self"}, "", nil); err != nil {
				return err
			}
		}
	}
	return nil
}

func (m *pluginPlatformModule) Stop(ctx context.Context) error {
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	if m.wasmCoordinator != nil {
		if err := m.wasmCoordinator.Close(ctx); err != nil {
			return err
		}
	}
	if m.managed != nil {
		if err := m.managed.Close(ctx); err != nil {
			return err
		}
	}
	if m.workerDone != nil {
		select {
		case <-m.workerDone:
		case <-ctx.Done():
			return ctx.Err()
		}
		m.workerDone = nil
	}
	if m.eventWorkerDone != nil {
		select {
		case <-m.eventWorkerDone:
		case <-ctx.Done():
			return ctx.Err()
		}
		m.eventWorkerDone = nil
	}
	if m.manager != nil {
		m.manager.StopAll()
	}
	if m.containerFactory != nil {
		if err := m.containerFactory.Close(ctx); err != nil {
			return err
		}
		m.containerFactory = nil
	}
	if m.files != nil {
		m.files.Close()
		m.files = nil
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
	api.SetAuthorizationService(m.authorization)
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
