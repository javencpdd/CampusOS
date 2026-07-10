package server

import (
	"context"
	"log"
	"strings"
	"time"

	"github.com/campusos/CampusOS/internal/ai"
	"github.com/campusos/CampusOS/internal/community/handler"
	"github.com/campusos/CampusOS/internal/community/repository"
	"github.com/campusos/CampusOS/internal/community/service"
	identityhandler "github.com/campusos/CampusOS/internal/core/identity/handler"
	identityrepo "github.com/campusos/CampusOS/internal/core/identity/repository"
	identitysvc "github.com/campusos/CampusOS/internal/core/identity/service"
	"github.com/campusos/CampusOS/internal/homepage"
	"github.com/campusos/CampusOS/internal/integration"
	"github.com/campusos/CampusOS/internal/mcp"
	"github.com/campusos/CampusOS/internal/message"
	"github.com/campusos/CampusOS/internal/platformlog"
	"github.com/campusos/CampusOS/internal/plugin"
	pluginbuiltin "github.com/campusos/CampusOS/internal/plugin/builtin"
	plugingrpc "github.com/campusos/CampusOS/internal/plugin/grpc"
	"github.com/campusos/CampusOS/internal/plugin/hostapi"
	pluginwasm "github.com/campusos/CampusOS/internal/plugin/wasm"
	"github.com/campusos/CampusOS/internal/richtext"
	"github.com/campusos/CampusOS/internal/schedule"
	"github.com/campusos/CampusOS/internal/space"
	"github.com/campusos/CampusOS/internal/webhook"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/campusos/CampusOS/pkg/cache"
	"github.com/campusos/CampusOS/pkg/config"
	"github.com/campusos/CampusOS/pkg/database"
	"github.com/campusos/CampusOS/pkg/eventbus"
	"github.com/campusos/CampusOS/pkg/middleware"
	"github.com/campusos/CampusOS/pkg/observability"
	"github.com/gin-gonic/gin"
)

type Server struct {
	cfg     *config.Config
	bus     eventbus.EventBus
	manager *plugin.Manager
}

func New(cfg *config.Config) *Server {
	return &Server{cfg: cfg}
}

func (s *Server) Run() error {
	metricsCollector := observability.NewCollector()

	// ─── 初始化 EventBus ───
	var bus eventbus.EventBus
	var memBus *eventbus.MemoryEventBus
	natsBus, err := eventbus.NewNATSEventBus(s.cfg.NATS.URL)
	if err != nil {
		log.Printf("⚠️  NATS 连接失败，回退到内存事件总线: %v", err)
		mb := eventbus.NewMemoryEventBus()
		bus = mb
		memBus = mb
	} else {
		bus = natsBus
		memBus = eventbus.NewMemoryEventBus()
	}
	s.bus = bus
	defer bus.Close()

	// ─── 初始化 Plugin Manager ───
	s.manager = plugin.NewManager()
	grpcRuntime := plugingrpc.NewGRPCRuntime()
	s.manager.RegisterRuntime("grpc", grpcRuntime)
	s.manager.RegisterRuntime("wasm", pluginwasm.NewRuntime())
	s.manager.RegisterRuntime("builtin", pluginbuiltin.NewRuntime())

	// ─── 初始化插件仓储（PG 模式在 PostgreSQL 连接后设置）───
	var pluginRepo plugin.PluginRepository
	var apiKeyRepo plugin.APIKeyRepository
	_ = pluginRepo // 延迟赋值
	_ = apiKeyRepo // 延迟赋值

	// ─── 注册默认事件订阅（事件日志 + 插件分发）───
	s.registerDefaultSubscriptions(bus)

	// ─── 加载插件 ───
	pluginsDir := plugin.PluginsDirFromEnv()
	if err := s.manager.InstallFromPluginsDir(pluginsDir); err != nil {
		log.Printf("⚠️  加载插件失败: %v", err)
	}

	// ─── 启动健康检查 ───
	grpcRuntime.StartHealthChecker(context.Background(), 10*time.Second, s.manager)

	// ─── 初始化缓存 ───
	redisAddr := s.cfg.Redis.Addr
	redisPassword := s.cfg.Redis.Password
	redisDB := s.cfg.Redis.DB
	redisEnabled := s.cfg.Redis.Enabled && redisAddr != ""
	// 解析 host:port（redisAddr 可能是 "localhost:6379" 格式）
	redisHost := redisAddr
	redisPort := "6379"
	if idx := strings.LastIndex(redisAddr, ":"); idx > 0 {
		redisHost = redisAddr[:idx]
		redisPort = redisAddr[idx+1:]
	}
	appCache := cache.NewCache(cache.CacheConfig{
		Enabled:  redisEnabled,
		Host:     redisHost,
		Port:     redisPort,
		Password: redisPassword,
		DB:       redisDB,
	})

	// ─── 初始化 AI Gateway ───
	aiService := s.initAIService()

	// ─── 初始化 PostgreSQL ───
	pool, err := database.New(s.cfg.Database.DSN)
	if err != nil {
		log.Printf("⚠️  PostgreSQL 连接失败，回退到内存模式: %v", err)
		return s.runMemoryMode(bus, memBus, aiService, metricsCollector)
	}
	defer pool.Close()
	log.Printf("✅ PostgreSQL 连接成功")

	// ─── 设置 PG 插件仓储 ───
	pluginRepo = plugin.NewPgPluginRepository(pool)
	apiKeyRepo = plugin.NewPgAPIKeyRepository(pool)
	s.manager.SetPluginRepository(pluginRepo)
	s.startConfiguredPlugins()
	s.normalizePersonalStoragePluginConfigs()
	aiService.SetCallLogStore(ai.NewPgCallLogger(pool))

	// ─── 种子数据（默认管理员）───
	if err := SeedAdmin(pool, s.cfg.Auth.PasswordHashEnabled); err != nil {
		log.Printf("⚠️  种子数据初始化失败: %v", err)
	}

	// ─── 初始化 JWT ───
	jwtMgr := s.newJWTManager()

	// ─── 初始化仓储层（PostgreSQL）───
	userRepo := identityrepo.NewPgUserRepository(pool)
	threadRepo := repository.NewPgThreadRepository(pool)
	categoryRepo := repository.NewPgCategoryRepository(pool)
	postRepo := repository.NewPgPostRepository(pool)
	spaceRepo := space.NewPgRepository(pool)
	roleRepo := identityrepo.NewPgRoleRepository(pool)
	permSvc := identitysvc.NewPermissionService(roleRepo)

	// ─── 初始化 Host API ───
	hostAPI := hostapi.NewHostAPIv2FromHostAPI(hostapi.NewHostAPI(userRepo, threadRepo, categoryRepo, postRepo, bus))
	hostAPI.SetPluginRepository(pluginRepo)
	if store, err := hostapi.NewSQLiteKVStore(s.cfg.Plugin.DataDir); err != nil {
		log.Printf("⚠️  SQLite 插件 KV 初始化失败，回退到内存存储: %v", err)
	} else {
		hostAPI.SetStorageStore(store)
	}
	hostAPI.SetPermissionChecker(permSvc)
	hostAPIServer, err := s.startHostAPIServer(hostAPI)
	if err != nil {
		return err
	}
	if hostAPIServer != nil {
		defer hostAPIServer.Stop()
	}

	// ─── 初始化服务层 ───
	userSvc := identitysvc.NewUserService(userRepo, jwtMgr, userRepo, bus)
	userSvc.SetPasswordHashEnabled(s.cfg.Auth.PasswordHashEnabled)
	userSvc.SetRoleRepository(roleRepo)
	threadSvc := service.NewThreadService(threadRepo, bus)
	threadSvc.SetCategoryRepository(categoryRepo)
	threadSvc.SetCache(appCache)
	categorySvc := service.NewCategoryService(categoryRepo, bus)
	postSvc := service.NewPostService(postRepo, bus)
	postSvc.SetThreadRepository(threadRepo)
	postSvc.SetCache(appCache)
	spaceSvc := space.NewService(spaceRepo, userRepo)
	spaceSvc.SetThreadRepository(threadRepo)
	spaceSvc.SetPluginEnabledChecker(func() bool {
		return s.manager.IsPluginRunning("personal-space")
	})
	s.configureSpaceFileStore(spaceSvc)
	if err := spaceSvc.RegisterEventHandlers(bus); err != nil {
		log.Printf("⚠️  个人主页内容同步订阅失败: %v", err)
	}
	webhookSvc := webhook.NewService(webhook.NewPgStore(pool), metricsCollector)
	if err := webhookSvc.Register(bus); err != nil {
		log.Printf("⚠️  Webhook 事件订阅失败: %v", err)
	}
	mcpSvc := mcp.NewService(categoryRepo, threadRepo, mcp.NewPgAuditStore(pool), metricsCollector)
	messageSvc := message.NewService(message.NewPgStore(pool), metricsCollector)
	homepageSvc := homepage.NewService(s.manager.GetPlugin, categoryRepo)
	homepageSvc.SetConfigUpdater(s.manager.UpdateConfig)
	richTextSvc := richtext.NewService(richtext.NewPgStore(pool), threadRepo, threadSvc)
	richTextSvc.SetEnabledChecker(func() bool {
		return s.manager.IsPluginRunning("controlled-richtext-article")
	})
	s.configureRichTextAssetStore(richTextSvc)
	scheduleSvc := s.initScheduleService()
	platformLogSvc := platformlog.NewServiceFromEnv()

	// ─── 初始化处理器层 ───
	userHandler := identityhandler.NewUserHandler(userSvc)
	threadHandler := handler.NewThreadHandler(threadSvc)
	categoryHandler := handler.NewCategoryHandler(categorySvc)
	postHandler := handler.NewPostHandler(postSvc)
	spaceHandler := space.NewHandler(spaceSvc)
	eventHandler := handler.NewEventHandler(memBus)
	pluginHandler := plugin.NewHandler(s.manager, plugin.WithPluginsDir(plugin.PluginsDirFromEnv()))
	roleHandler := identityhandler.NewRoleHandler(permSvc)
	aiHandler := ai.NewHandler(aiService)
	webhookHandler := webhook.NewHandler(webhookSvc)
	mcpHandler := mcp.NewHandler(mcpSvc)
	messageHandler := message.NewHandler(messageSvc)
	homepageHandler := homepage.NewHandler(homepageSvc)
	richTextHandler := richtext.NewHandler(richTextSvc)
	scheduleHandler := schedule.NewHandler(scheduleSvc)
	platformLogHandler := platformlog.NewHandler(platformLogSvc)
	integrationHandler := integration.NewHandler(
		integration.WithPool(pool),
		integration.WithConfig(s.cfg),
		integration.WithPluginManager(s.manager),
		integration.WithAIService(aiService),
		integration.WithSpaceService(spaceSvc),
		integration.WithWebhookService(webhookSvc),
		integration.WithMCPService(mcpSvc),
		integration.WithMessageService(messageSvc),
		integration.WithMetrics(metricsCollector),
	)

	return s.setupRoutes(jwtMgr, permSvc, userHandler, threadHandler, categoryHandler, postHandler, spaceHandler, eventHandler, pluginHandler, roleHandler, aiHandler, integrationHandler, webhookHandler, mcpHandler, messageHandler, homepageHandler, richTextHandler, scheduleHandler, platformLogHandler, metricsCollector)
}

func (s *Server) runMemoryMode(bus eventbus.EventBus, memBus *eventbus.MemoryEventBus, aiService *ai.Service, metricsCollector *observability.Collector) error {
	jwtMgr := s.newJWTManager()

	userRepo := identityrepo.NewMemoryUserRepository()
	threadRepo := repository.NewMemoryThreadRepository()
	categoryRepo := repository.NewMemoryCategoryRepository()
	postRepo := repository.NewMemoryPostRepository()
	spaceRepo := space.NewMemoryRepository()
	roleRepo := identityrepo.NewMemoryRoleRepository()
	pluginRepo := plugin.NewMemoryPluginRepository()
	s.manager.SetPluginRepository(pluginRepo)
	s.startConfiguredPlugins()
	s.normalizePersonalStoragePluginConfigs()
	permSvc := identitysvc.NewPermissionService(roleRepo)

	// ─── 初始化 Host API ───
	hostAPI := hostapi.NewHostAPIv2FromHostAPI(hostapi.NewHostAPI(userRepo, threadRepo, categoryRepo, postRepo, bus))
	hostAPI.SetPluginRepository(pluginRepo)
	if store, err := hostapi.NewSQLiteKVStore(s.cfg.Plugin.DataDir); err != nil {
		log.Printf("⚠️  SQLite 插件 KV 初始化失败，回退到内存存储: %v", err)
	} else {
		hostAPI.SetStorageStore(store)
	}
	hostAPI.SetPermissionChecker(permSvc)
	hostAPIServer, err := s.startHostAPIServer(hostAPI)
	if err != nil {
		return err
	}
	if hostAPIServer != nil {
		defer hostAPIServer.Stop()
	}

	userSvc := identitysvc.NewUserService(userRepo, jwtMgr, nil, bus)
	userSvc.SetPasswordHashEnabled(s.cfg.Auth.PasswordHashEnabled)
	userSvc.SetRoleRepository(roleRepo)
	threadSvc := service.NewThreadService(threadRepo, bus)
	threadSvc.SetCategoryRepository(categoryRepo)
	categorySvc := service.NewCategoryService(categoryRepo, bus)
	postSvc := service.NewPostService(postRepo, bus)
	postSvc.SetThreadRepository(threadRepo)
	spaceSvc := space.NewService(spaceRepo, userRepo)
	spaceSvc.SetThreadRepository(threadRepo)
	spaceSvc.SetPluginEnabledChecker(func() bool {
		return s.manager.IsPluginRunning("personal-space")
	})
	s.configureSpaceFileStore(spaceSvc)
	if err := spaceSvc.RegisterEventHandlers(bus); err != nil {
		log.Printf("⚠️  个人主页内容同步订阅失败: %v", err)
	}
	webhookSvc := webhook.NewService(webhook.NewMemoryStore(), metricsCollector)
	if err := webhookSvc.Register(bus); err != nil {
		log.Printf("⚠️  Webhook 事件订阅失败: %v", err)
	}
	mcpSvc := mcp.NewService(categoryRepo, threadRepo, mcp.NewMemoryAuditStore(), metricsCollector)
	messageSvc := message.NewService(message.NewMemoryStore(), metricsCollector)
	homepageSvc := homepage.NewService(s.manager.GetPlugin, categoryRepo)
	homepageSvc.SetConfigUpdater(s.manager.UpdateConfig)
	richTextSvc := richtext.NewService(richtext.NewMemoryStore(), threadRepo, threadSvc)
	richTextSvc.SetEnabledChecker(func() bool {
		return s.manager.IsPluginRunning("controlled-richtext-article")
	})
	s.configureRichTextAssetStore(richTextSvc)
	scheduleSvc := s.initScheduleService()
	platformLogSvc := platformlog.NewServiceFromEnv()

	userHandler := identityhandler.NewUserHandler(userSvc)
	threadHandler := handler.NewThreadHandler(threadSvc)
	categoryHandler := handler.NewCategoryHandler(categorySvc)
	postHandler := handler.NewPostHandler(postSvc)
	spaceHandler := space.NewHandler(spaceSvc)
	eventHandler := handler.NewEventHandler(memBus)
	pluginHandler := plugin.NewHandler(s.manager, plugin.WithPluginsDir(plugin.PluginsDirFromEnv()))
	roleHandler := identityhandler.NewRoleHandler(permSvc)
	aiHandler := ai.NewHandler(aiService)
	webhookHandler := webhook.NewHandler(webhookSvc)
	mcpHandler := mcp.NewHandler(mcpSvc)
	messageHandler := message.NewHandler(messageSvc)
	homepageHandler := homepage.NewHandler(homepageSvc)
	richTextHandler := richtext.NewHandler(richTextSvc)
	scheduleHandler := schedule.NewHandler(scheduleSvc)
	platformLogHandler := platformlog.NewHandler(platformLogSvc)
	integrationHandler := integration.NewHandler(
		integration.WithConfig(s.cfg),
		integration.WithPluginManager(s.manager),
		integration.WithAIService(aiService),
		integration.WithSpaceService(spaceSvc),
		integration.WithWebhookService(webhookSvc),
		integration.WithMCPService(mcpSvc),
		integration.WithMessageService(messageSvc),
		integration.WithMetrics(metricsCollector),
	)

	return s.setupRoutes(jwtMgr, permSvc, userHandler, threadHandler, categoryHandler, postHandler, spaceHandler, eventHandler, pluginHandler, roleHandler, aiHandler, integrationHandler, webhookHandler, mcpHandler, messageHandler, homepageHandler, richTextHandler, scheduleHandler, platformLogHandler, metricsCollector)
}

func (s *Server) initAIService() *ai.Service {
	service, err := ai.NewServiceFromConfig(s.cfg.AI)
	status := service.Status()
	if err != nil {
		log.Printf("⚠️  AI Gateway 初始化失败: %v", err)
		return service
	}
	if status.Enabled && status.Ready {
		log.Printf("✅ AI Gateway 已启用: provider=%s", status.Provider)
		return service
	}
	log.Printf("🔌 AI Gateway 已禁用")
	return service
}

func (s *Server) startHostAPIServer(hostAPI *hostapi.HostAPIv2) (*hostapi.HostAPIServer, error) {
	if !s.cfg.HostAPI.Enabled {
		log.Printf("🔌 Host API 已禁用")
		return nil, nil
	}
	server := hostapi.NewHostAPIServer(hostAPI, s.cfg.HostAPI.Addr, s.manager.GetPlugin)
	if err := server.Start(); err != nil {
		return nil, err
	}
	return server, nil
}

func (s *Server) newJWTManager() *auth.JWTManager {
	accessTTL, _ := time.ParseDuration(s.cfg.JWT.AccessTTL)
	refreshTTL, _ := time.ParseDuration(s.cfg.JWT.RefreshTTL)
	return auth.NewJWTManager(auth.JWTConfig{
		Secret:     s.cfg.JWT.Secret,
		AccessTTL:  accessTTL,
		RefreshTTL: refreshTTL,
		Issuer:     s.cfg.JWT.Issuer,
	})
}

func (s *Server) configureSpaceFileStore(spaceSvc *space.Service) {
	if spaceSvc == nil {
		return
	}
	cfg := space.FileStorageConfigFromPluginConfig(personalSpacePluginConfig(s.manager))
	store, err := space.NewLocalFileStore(cfg)
	if err != nil {
		log.Printf("⚠️  个人空间文件存储初始化失败: %v", err)
		return
	}
	spaceSvc.SetFileStore(store)
}

func (s *Server) configureRichTextAssetStore(richTextSvc *richtext.Service) {
	if richTextSvc == nil {
		return
	}
	cfg := richtext.AssetStoreConfigFromPluginConfig(pluginConfig(s.manager, "controlled-richtext-article"), personalSpacePluginConfig(s.manager))
	store, err := richtext.NewLocalAssetStore(cfg)
	if err != nil {
		log.Printf("⚠️  富文本图片存储初始化失败: %v", err)
		return
	}
	richTextSvc.SetAssetStore(store)
}

func (s *Server) initScheduleService() *schedule.Service {
	cfg := schedule.ConfigFromPluginConfig(pluginConfig(s.manager, "personal-schedule"), personalSpacePluginConfig(s.manager))
	svc, err := schedule.NewService(cfg)
	if err != nil {
		log.Printf("⚠️  个人课表存储初始化失败，回退到默认 personal-space 目录: %v", err)
		svc, err = schedule.NewService(schedule.Config{})
		if err != nil {
			log.Printf("⚠️  个人课表默认存储仍初始化失败: %v", err)
			svc = schedule.NewDisabledService()
		}
	}
	svc.SetEnabledChecker(func() bool {
		return s.manager.IsPluginRunning("personal-schedule")
	})
	return svc
}

func (s *Server) normalizePersonalStoragePluginConfigs() {
	if s.manager == nil {
		return
	}
	if personalSpace, ok := s.manager.GetPlugin("personal-space"); ok && personalSpace != nil && personalSpace.Manifest != nil {
		config := copyPluginConfig(personalSpace.Manifest.Config)
		if root, ok := config["file_root"].(string); ok {
			if normalized := space.NormalizePersonalSpaceFileRoot(root); normalized != root {
				config["file_root"] = normalized
				if _, err := s.manager.UpdateConfig("personal-space", config); err != nil {
					log.Printf("⚠️  个人空间存储配置迁移失败: %v", err)
				}
			}
		}
	}
	if personalSchedule, ok := s.manager.GetPlugin("personal-schedule"); ok && personalSchedule != nil && personalSchedule.Manifest != nil {
		config := copyPluginConfig(personalSchedule.Manifest.Config)
		if _, exists := config["data_root"]; exists {
			delete(config, "data_root")
			if _, err := s.manager.UpdateConfig("personal-schedule", config); err != nil {
				log.Printf("⚠️  个人课表存储配置迁移失败: %v", err)
			}
		}
	}
	if richText, ok := s.manager.GetPlugin("controlled-richtext-article"); ok && richText != nil && richText.Manifest != nil {
		config := copyPluginConfig(richText.Manifest.Config)
		if _, exists := config["file_root"]; exists {
			delete(config, "file_root")
			if _, err := s.manager.UpdateConfig("controlled-richtext-article", config); err != nil {
				log.Printf("⚠️  富文本图片存储配置迁移失败: %v", err)
			}
		}
	}
}

func copyPluginConfig(config map[string]interface{}) map[string]interface{} {
	copy := make(map[string]interface{}, len(config))
	for key, value := range config {
		copy[key] = value
	}
	return copy
}

func personalSpacePluginConfig(manager *plugin.Manager) map[string]interface{} {
	return pluginConfig(manager, "personal-space")
}

func pluginConfig(manager *plugin.Manager, name string) map[string]interface{} {
	if manager == nil {
		return nil
	}
	p, ok := manager.GetPlugin(name)
	if !ok || p == nil || p.Manifest == nil {
		return nil
	}
	return p.Manifest.Config
}

func (s *Server) startConfiguredPlugins() {
	if s.manager == nil {
		return
	}
	s.manager.StartDesiredPlugins(plugin.ScopeSystem)
	s.manager.StartDesiredPlugins(plugin.ScopeUser)
}

func (s *Server) registerDefaultSubscriptions(bus eventbus.EventBus) {
	eventTypes := []string{
		"user.created", "thread.created", "thread.updated", "thread.deleted",
		"post.created", "category.created",
	}
	for _, et := range eventTypes {
		bus.Subscribe(et, func(ctx context.Context, event eventbus.Event) error {
			log.Printf("📢 Event: %s | Subject: %s | Source: %s", event.Type, event.Subject, event.Source)
			// 分发事件到插件
			if s.manager != nil {
				s.manager.DispatchEvent(ctx, &plugin.EventMessage{
					Type:    event.Type,
					Source:  event.Source,
					Subject: event.Subject,
					Data:    event.Data,
				})
			}
			return nil
		})
	}
	log.Printf("✅ 已注册 %d 个默认事件订阅（含插件分发）", len(eventTypes))
}

func (s *Server) setupRoutes(jwtMgr *auth.JWTManager,
	permSvc *identitysvc.PermissionService,
	userHandler *identityhandler.UserHandler,
	threadHandler *handler.ThreadHandler,
	categoryHandler *handler.CategoryHandler,
	postHandler *handler.PostHandler,
	spaceHandler *space.Handler,
	eventHandler *handler.EventHandler,
	pluginHandler *plugin.Handler,
	roleHandler *identityhandler.RoleHandler,
	aiHandler *ai.Handler,
	integrationHandler *integration.Handler,
	webhookHandler *webhook.Handler,
	mcpHandler *mcp.Handler,
	messageHandler *message.Handler,
	homepageHandler *homepage.Handler,
	richTextHandler *richtext.Handler,
	scheduleHandler *schedule.Handler,
	platformLogHandler *platformlog.Handler,
	metricsCollector *observability.Collector) error {

	r := gin.Default()

	// 全局中间件
	r.Use(middleware.Recovery())
	r.Use(middleware.CORS())
	r.Use(middleware.TraceID())
	r.Use(middleware.Logger())
	r.Use(observability.Middleware(metricsCollector))

	v1 := r.Group("/api/v1")

	// ─── 公开接口（无需认证）───
	public := v1.Group("")
	{
		public.GET("/health", userHandler.HealthCheck)
		public.GET("/home/config", homepageHandler.GetConfig)
		public.GET("/richtext/status", richTextHandler.Status)
		public.GET("/schedule/status", scheduleHandler.Status)
		public.POST("/auth/register", userHandler.Register)
		public.POST("/auth/login", userHandler.Login)
		public.GET("/threads", threadHandler.ListThreads)
		public.GET("/threads/:id", threadHandler.GetThread)
		public.GET("/richtext/articles/:id", richTextHandler.GetPublished)
		public.GET("/richtext/assets/:user_id/:filename", richTextHandler.ServeAsset)
		public.GET("/users", userHandler.ListUsers)
		public.GET("/users/:id", userHandler.GetUser)
		public.GET("/space/:user_id/contents", spaceHandler.ListContentsByUserID)
		public.GET("/space/:user_id", spaceHandler.GetByUserID)
		public.GET("/spaces/files/:user_id/avatars/:filename", spaceHandler.ServeAvatarFile)
		public.GET("/u/:username/contents", spaceHandler.ListContentsByUsername)
		public.GET("/u/:username", spaceHandler.GetByUsername)
		public.GET("/categories", categoryHandler.List)
		public.GET("/categories/:id", categoryHandler.Get)
		public.GET("/threads/:id/posts", postHandler.ListPosts)
		public.GET("/events", eventHandler.ListEvents)
	}

	// ─── 需要 JWT 认证的接口（普通用户）───
	authenticated := v1.Group("")
	authenticated.Use(middleware.JWTAuth(jwtMgr))
	{
		authenticated.GET("/auth/me", userHandler.GetMe)
		authenticated.PUT("/users/:id", userHandler.UpdateUser)
		authenticated.GET("/spaces/me", spaceHandler.GetMe)
		authenticated.PUT("/spaces/me", spaceHandler.UpdateMe)
		authenticated.GET("/spaces/me/storage", spaceHandler.GetStorageStatus)
		authenticated.POST("/spaces/me/avatar", spaceHandler.UploadAvatar)
		authenticated.POST("/spaces/me/styles/validate", spaceHandler.ValidateStylePackage)
		authenticated.POST("/spaces/me/styles/preview", spaceHandler.PreviewStylePackage)
		authenticated.POST("/spaces/me/styles/export", spaceHandler.ExportStylePackage)
		authenticated.POST("/spaces/me/styles/apply", spaceHandler.ApplyStylePackage)
		authenticated.POST("/spaces/me/styles/html/validate", spaceHandler.ValidateCustomHTML)
		authenticated.GET("/spaces/me/styles/html-example", spaceHandler.CustomHTMLExample)
		authenticated.POST("/spaces/me/styles/html/apply", spaceHandler.ApplyCustomHTML)
		authenticated.POST("/spaces/me/styles/packs/validate", spaceHandler.ValidateStylePackZip)
		authenticated.GET("/spaces/me/styles/packs/example", spaceHandler.StylePackExample)
		authenticated.GET("/spaces/me/styles/packs/example.zip", spaceHandler.StylePackExampleZip)
		authenticated.GET("/spaces/me/styles/packs/sources", spaceHandler.ListSourceStylePacks)
		authenticated.POST("/spaces/me/styles/packs/apply", spaceHandler.ApplyStylePackZip)
		authenticated.POST("/spaces/me/styles/packs/apply-source", spaceHandler.ApplySourceStylePack)
		authenticated.POST("/spaces/me/styles/rollback", spaceHandler.RollbackStyle)
		authenticated.POST("/spaces/me/styles/default", spaceHandler.RestoreDefaultStyle)
		authenticated.GET("/spaces/me/sync-status", spaceHandler.GetSyncStatus)
		authenticated.GET("/schedule/me", scheduleHandler.GetMe)
		authenticated.GET("/schedule/me/terms", scheduleHandler.ListTerms)
		authenticated.POST("/schedule/me/terms/activate", scheduleHandler.ActivateTerm)
		authenticated.PUT("/schedule/me", scheduleHandler.SaveMe)
		authenticated.POST("/schedule/me/import", scheduleHandler.ImportMe)
		authenticated.POST("/richtext/articles", richTextHandler.CreateDraft)
		authenticated.GET("/richtext/articles/:id/me", richTextHandler.GetMine)
		authenticated.PUT("/richtext/articles/:id", richTextHandler.UpdateDraft)
		authenticated.POST("/richtext/preview", richTextHandler.Preview)
		authenticated.POST("/richtext/assets", richTextHandler.UploadAsset)
		authenticated.POST("/richtext/articles/:id/publish", richTextHandler.Publish)
		authenticated.POST("/richtext/articles/:id/offline", richTextHandler.Offline)
		authenticated.DELETE("/richtext/articles/:id", richTextHandler.Delete)
		authenticated.POST("/threads", threadHandler.CreateThread)
		authenticated.GET("/threads/:id/me", threadHandler.GetThreadForCurrentUser)
		authenticated.PUT("/threads/:id", threadHandler.UpdateThread)
		authenticated.DELETE("/threads/:id", threadHandler.DeleteThread)
		authenticated.GET("/threads/:id/posts/me", postHandler.ListPostsForCurrentUser)
		authenticated.POST("/threads/:id/posts", postHandler.CreatePost)
		authenticated.PUT("/threads/:id/posts/:post_id", postHandler.UpdatePost)
		authenticated.DELETE("/threads/:id/posts/:post_id", postHandler.DeletePost)
		authenticated.POST("/categories", categoryHandler.Create)
		authenticated.PUT("/categories/:id", categoryHandler.Update)
	}

	// ─── 管理员接口（需要权限）───
	admin := v1.Group("")
	admin.Use(middleware.JWTAuth(jwtMgr))
	{
		// 用户管理
		admin.POST("/users/:id/suspend", middleware.RequirePermission(permSvc, "user", "suspend"), userHandler.SuspendUser)
		admin.POST("/users/:id/activate", middleware.RequirePermission(permSvc, "user", "suspend"), userHandler.ActivateUser)

		// 帖子管理
		admin.GET("/admin/threads", middleware.RequirePermission(permSvc, "role", "manage"), threadHandler.AdminListThreads)
		admin.POST("/threads/:id/pin", middleware.RequirePermission(permSvc, "thread", "pin"), threadHandler.PinThread)
		admin.POST("/threads/:id/unpin", middleware.RequirePermission(permSvc, "thread", "pin"), threadHandler.UnpinThread)
		admin.POST("/threads/:id/lock", middleware.RequirePermission(permSvc, "thread", "pin"), threadHandler.LockThread)
		admin.POST("/threads/:id/unlock", middleware.RequirePermission(permSvc, "thread", "pin"), threadHandler.UnlockThread)
		admin.DELETE("/admin/threads/:id", middleware.RequirePermission(permSvc, "thread", "delete"), threadHandler.AdminDeleteThread)
		admin.POST("/richtext/articles/:id/admin/offline", middleware.RequirePermission(permSvc, "role", "manage"), richTextHandler.AdminOffline)
		admin.POST("/richtext/articles/:id/admin/restore", middleware.RequirePermission(permSvc, "role", "manage"), richTextHandler.AdminRestore)
		admin.DELETE("/richtext/articles/:id/admin", middleware.RequirePermission(permSvc, "role", "manage"), richTextHandler.AdminDelete)

		// 版块管理
		admin.DELETE("/categories/:id", middleware.RequirePermission(permSvc, "category", "delete"), categoryHandler.Delete)

		// 插件管理
		admin.GET("/plugins", middleware.RequirePermission(permSvc, "role", "manage"), pluginHandler.ListPlugins)
		admin.GET("/plugins/:name", middleware.RequirePermission(permSvc, "role", "manage"), pluginHandler.GetPlugin)
		admin.GET("/plugins/:name/logs", middleware.RequirePermission(permSvc, "role", "manage"), pluginHandler.ListPluginLogs)
		admin.GET("/plugins/:name/export", middleware.RequirePermission(permSvc, "role", "manage"), pluginHandler.ExportPlugin)
		admin.PUT("/plugins/:name/config", middleware.RequirePermission(permSvc, "role", "manage"), pluginHandler.UpdatePluginConfig)
		admin.POST("/plugins/:name/enable", middleware.RequirePermission(permSvc, "role", "manage"), pluginHandler.EnablePlugin)
		admin.POST("/plugins/:name/disable", middleware.RequirePermission(permSvc, "role", "manage"), pluginHandler.DisablePlugin)
		admin.POST("/plugins/:name/reload", middleware.RequirePermission(permSvc, "role", "manage"), pluginHandler.ReloadUserPlugin)
		admin.DELETE("/plugins/:name", middleware.RequirePermission(permSvc, "role", "manage"), pluginHandler.UninstallPlugin)
		admin.POST("/plugin-packages/import", middleware.RequirePermission(permSvc, "role", "manage"), pluginHandler.ImportPluginPackage)
		admin.POST("/plugin-packages/precheck", middleware.RequirePermission(permSvc, "role", "manage"), pluginHandler.PrecheckPluginPackage)

		// AI Gateway 管理
		admin.GET("/ai/status", middleware.RequirePermission(permSvc, "role", "manage"), aiHandler.GetStatus)
		admin.GET("/ai/logs", middleware.RequirePermission(permSvc, "role", "manage"), aiHandler.ListLogs)

		// v0.5 集成中心与运营化能力
		admin.GET("/integrations/overview", middleware.RequirePermission(permSvc, "role", "manage"), integrationHandler.Overview)
		admin.GET("/metrics/summary", middleware.RequirePermission(permSvc, "role", "manage"), integrationHandler.Metrics)
		admin.GET("/spaces/admin/summary", middleware.RequirePermission(permSvc, "role", "manage"), spaceHandler.AdminSummary)
		admin.POST("/spaces/:user_id/disable", middleware.RequirePermission(permSvc, "role", "manage"), spaceHandler.DisableSpace)
		admin.POST("/spaces/:user_id/enable", middleware.RequirePermission(permSvc, "role", "manage"), spaceHandler.EnableSpace)

		admin.GET("/webhooks", middleware.RequirePermission(permSvc, "role", "manage"), webhookHandler.ListEndpoints)
		admin.POST("/webhooks", middleware.RequirePermission(permSvc, "role", "manage"), webhookHandler.CreateEndpoint)
		admin.GET("/webhooks/summary", middleware.RequirePermission(permSvc, "role", "manage"), webhookHandler.Summary)
		admin.POST("/webhooks/:id/test", middleware.RequirePermission(permSvc, "role", "manage"), webhookHandler.TestEndpoint)
		admin.POST("/webhooks/:id/enable", middleware.RequirePermission(permSvc, "role", "manage"), webhookHandler.EnableEndpoint)
		admin.POST("/webhooks/:id/disable", middleware.RequirePermission(permSvc, "role", "manage"), webhookHandler.DisableEndpoint)
		admin.GET("/webhooks/:id/deliveries", middleware.RequirePermission(permSvc, "role", "manage"), webhookHandler.ListDeliveries)

		admin.GET("/mcp/tools", middleware.RequirePermission(permSvc, "role", "manage"), mcpHandler.ListTools)
		admin.POST("/mcp/tools/:name/call", middleware.RequirePermission(permSvc, "role", "manage"), mcpHandler.CallTool)
		admin.GET("/mcp/audit", middleware.RequirePermission(permSvc, "role", "manage"), mcpHandler.ListAudit)
		admin.GET("/mcp/settings", middleware.RequirePermission(permSvc, "role", "manage"), mcpHandler.GetSettings)
		admin.PUT("/mcp/settings", middleware.RequirePermission(permSvc, "role", "manage"), mcpHandler.UpdateSettings)

		admin.GET("/messages/adapters", middleware.RequirePermission(permSvc, "role", "manage"), messageHandler.ListAdapters)
		admin.POST("/messages/local/inbound", middleware.RequirePermission(permSvc, "role", "manage"), messageHandler.ReceiveLocal)
		admin.GET("/messages/logs", middleware.RequirePermission(permSvc, "role", "manage"), messageHandler.ListMessages)
		admin.POST("/messages/bindings", middleware.RequirePermission(permSvc, "role", "manage"), messageHandler.CreateBinding)
		admin.GET("/messages/summary", middleware.RequirePermission(permSvc, "role", "manage"), messageHandler.Summary)

		admin.GET("/platform/logs/sources", middleware.RequirePermission(permSvc, "role", "manage"), platformLogHandler.Sources)
		admin.GET("/platform/logs/stream", middleware.RequirePermission(permSvc, "role", "manage"), platformLogHandler.Stream)

		admin.POST("/home/style-packs/validate", middleware.RequirePermission(permSvc, "role", "manage"), homepageHandler.ValidateStylePack)
		admin.GET("/home/style-packs/example", middleware.RequirePermission(permSvc, "role", "manage"), homepageHandler.StylePackExample)
		admin.GET("/home/style-packs/example.zip", middleware.RequirePermission(permSvc, "role", "manage"), homepageHandler.StylePackExampleZip)
		admin.GET("/home/style-packs/sources", middleware.RequirePermission(permSvc, "role", "manage"), homepageHandler.ListSourceStylePacks)
		admin.POST("/home/style-packs/apply", middleware.RequirePermission(permSvc, "role", "manage"), homepageHandler.ApplyStylePack)
		admin.POST("/home/style-packs/apply-source", middleware.RequirePermission(permSvc, "role", "manage"), homepageHandler.ApplySourceStylePack)
		admin.POST("/home/style-packs/rollback", middleware.RequirePermission(permSvc, "role", "manage"), homepageHandler.RollbackStylePack)

		// 角色管理
		admin.GET("/roles", middleware.RequirePermission(permSvc, "role", "manage"), roleHandler.ListRoles)
		admin.GET("/users/:id/roles", middleware.RequirePermission(permSvc, "role", "manage"), roleHandler.GetUserRoles)
		admin.POST("/users/:id/roles", middleware.RequirePermission(permSvc, "role", "manage"), roleHandler.AssignRole)
		admin.DELETE("/users/:id/roles", middleware.RequirePermission(permSvc, "role", "manage"), roleHandler.RevokeRole)
	}

	// 服务关闭时停止所有插件
	defer s.manager.StopAll()

	addr := s.cfg.Server.Addr()
	log.Printf("🚀 CampusOS API 监听 %s", addr)
	log.Printf("📋 API 端点总数: 75+")
	log.Printf("🔌 已加载 %d 个插件", len(s.manager.ListPlugins()))
	return r.Run(addr)
}
