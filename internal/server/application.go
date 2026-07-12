package server

import (
	"context"
	"log"
	"time"

	"github.com/campusos/CampusOS/internal/ai"
	"github.com/campusos/CampusOS/internal/community/handler"
	"github.com/campusos/CampusOS/internal/community/repository"
	identityhandler "github.com/campusos/CampusOS/internal/core/identity/handler"
	identityrepo "github.com/campusos/CampusOS/internal/core/identity/repository"
	identitysvc "github.com/campusos/CampusOS/internal/core/identity/service"
	"github.com/campusos/CampusOS/internal/homepage"
	"github.com/campusos/CampusOS/internal/integration"
	"github.com/campusos/CampusOS/internal/mcp"
	"github.com/campusos/CampusOS/internal/message"
	"github.com/campusos/CampusOS/internal/moderation"
	platformfeature "github.com/campusos/CampusOS/internal/platform/feature"
	"github.com/campusos/CampusOS/internal/platformlog"
	"github.com/campusos/CampusOS/internal/plugin"
	"github.com/campusos/CampusOS/internal/plugin/hostapi"
	"github.com/campusos/CampusOS/internal/richtext"
	"github.com/campusos/CampusOS/internal/schedule"
	"github.com/campusos/CampusOS/internal/space"
	"github.com/campusos/CampusOS/internal/webhook"
	"github.com/campusos/CampusOS/internal/webtheme"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/campusos/CampusOS/pkg/eventbus"
	"github.com/campusos/CampusOS/pkg/middleware"
	"github.com/campusos/CampusOS/pkg/observability"
	"github.com/gin-gonic/gin"
)

func (s *Server) runApplication(infra *infrastructureBootstrap) error {
	metricsCollector := infra.metrics
	bus := infra.bus
	memBus := infra.memoryBus
	appCache := infra.cache

	// ─── 初始化 AI Gateway ───
	aiService := s.initAIService()

	pool := infra.database
	if pool == nil {
		log.Printf("⚠️  PostgreSQL 基础设施不可用，回退到内存模式: %v", infra.databaseErr)
		return s.runMemoryMode(bus, memBus, aiService, metricsCollector)
	}

	// ─── 设置 PG 插件仓储 ───
	pluginRepo := plugin.NewPgPluginRepository(pool)
	s.manager.SetPluginRepository(pluginRepo)
	s.startConfiguredPlugins()
	s.features.SyncLegacy()
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
	if err := s.registerBusinessPorts(userRepo, threadRepo, categoryRepo, postRepo); err != nil {
		return err
	}
	identityModule := assembleIdentity(userRepo, roleRepo, userRepo, jwtMgr, bus, s.cfg.Auth.PasswordHashEnabled)
	communityModule := assembleCommunity(threadRepo, categoryRepo, postRepo, bus, appCache)
	permSvc := identityModule.Permissions

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

	threadSvc := communityModule.Threads
	postSvc := communityModule.Posts
	moderationSettings := moderation.NewLegacySettings(func() map[string]interface{} { return pluginConfig(s.manager, moderation.PluginName) })
	moderationSvc := moderation.NewService(
		permSvc,
		categoryRepo,
		threadRepo,
		postRepo,
		threadSvc,
		postSvc,
		moderation.NewPgAuditStore(pool),
		moderationSettings.Current(),
	)
	moderationSvc.SetEnabledChecker(func() bool { return true })
	moderationSvc.SetConfigProvider(func() moderation.Config {
		return moderationSettings.Current()
	})
	spaceSvc := space.NewService(spaceRepo, userRepo)
	spaceSvc.SetThreadRepository(threadRepo)
	spaceSvc.SetPluginEnabledChecker(func() bool {
		return s.features.Enabled("personal-space")
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
		return s.features.Enabled("controlled-richtext-article")
	})
	s.configureRichTextAssetStore(richTextSvc)
	scheduleSvc := s.initScheduleService()
	platformLogSvc := platformlog.NewServiceFromEnv()

	// ─── 初始化处理器层 ───
	userHandler := identityModule.UserHandler
	threadHandler := communityModule.ThreadHandler
	categoryHandler := communityModule.CategoryHandler
	postHandler := communityModule.PostHandler
	spaceHandler := space.NewHandler(spaceSvc)
	eventHandler := handler.NewEventHandler(memBus)
	pluginHandler := plugin.NewHandler(s.manager, plugin.WithPluginsDir(plugin.PluginsDirFromEnv()))
	roleHandler := identityModule.RoleHandler
	aiHandler := ai.NewHandler(aiService)
	webhookHandler := webhook.NewHandler(webhookSvc)
	mcpHandler := mcp.NewHandler(mcpSvc)
	messageHandler := message.NewHandler(messageSvc)
	homepageHandler := homepage.NewHandler(homepageSvc)
	richTextHandler := richtext.NewHandler(richTextSvc)
	scheduleHandler := schedule.NewHandler(scheduleSvc)
	platformLogHandler := platformlog.NewHandler(platformLogSvc)
	moderationHandler := moderation.NewHandler(moderationSvc)
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

	return s.setupRoutes(jwtMgr, permSvc, userHandler, threadHandler, categoryHandler, postHandler, spaceHandler, eventHandler, pluginHandler, roleHandler, aiHandler, integrationHandler, webhookHandler, mcpHandler, messageHandler, homepageHandler, richTextHandler, scheduleHandler, platformLogHandler, moderationHandler, metricsCollector)
}

func (s *Server) runMemoryMode(bus eventbus.EventBus, memBus *eventbus.MemoryEventBus, aiService *ai.Service, metricsCollector *observability.Collector) error {
	jwtMgr := s.newJWTManager()

	userRepo := identityrepo.NewMemoryUserRepository()
	threadRepo := repository.NewMemoryThreadRepository()
	categoryRepo := repository.NewMemoryCategoryRepository()
	postRepo := repository.NewMemoryPostRepository()
	spaceRepo := space.NewMemoryRepository()
	roleRepo := identityrepo.NewMemoryRoleRepository()
	if err := s.registerBusinessPorts(userRepo, threadRepo, categoryRepo, postRepo); err != nil {
		return err
	}
	pluginRepo := plugin.NewMemoryPluginRepository()
	s.manager.SetPluginRepository(pluginRepo)
	s.startConfiguredPlugins()
	s.features.SyncLegacy()
	s.normalizePersonalStoragePluginConfigs()
	identityModule := assembleIdentity(userRepo, roleRepo, nil, jwtMgr, bus, s.cfg.Auth.PasswordHashEnabled)
	communityModule := assembleCommunity(threadRepo, categoryRepo, postRepo, bus, nil)
	permSvc := identityModule.Permissions

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

	threadSvc := communityModule.Threads
	postSvc := communityModule.Posts
	moderationSettings := moderation.NewLegacySettings(func() map[string]interface{} { return pluginConfig(s.manager, moderation.PluginName) })
	moderationSvc := moderation.NewService(
		permSvc,
		categoryRepo,
		threadRepo,
		postRepo,
		threadSvc,
		postSvc,
		moderation.NewMemoryAuditStore(),
		moderationSettings.Current(),
	)
	moderationSvc.SetEnabledChecker(func() bool { return true })
	moderationSvc.SetConfigProvider(func() moderation.Config {
		return moderationSettings.Current()
	})
	spaceSvc := space.NewService(spaceRepo, userRepo)
	spaceSvc.SetThreadRepository(threadRepo)
	spaceSvc.SetPluginEnabledChecker(func() bool {
		return s.features.Enabled("personal-space")
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
		return s.features.Enabled("controlled-richtext-article")
	})
	s.configureRichTextAssetStore(richTextSvc)
	scheduleSvc := s.initScheduleService()
	platformLogSvc := platformlog.NewServiceFromEnv()

	userHandler := identityModule.UserHandler
	threadHandler := communityModule.ThreadHandler
	categoryHandler := communityModule.CategoryHandler
	postHandler := communityModule.PostHandler
	spaceHandler := space.NewHandler(spaceSvc)
	eventHandler := handler.NewEventHandler(memBus)
	pluginHandler := plugin.NewHandler(s.manager, plugin.WithPluginsDir(plugin.PluginsDirFromEnv()))
	roleHandler := identityModule.RoleHandler
	aiHandler := ai.NewHandler(aiService)
	webhookHandler := webhook.NewHandler(webhookSvc)
	mcpHandler := mcp.NewHandler(mcpSvc)
	messageHandler := message.NewHandler(messageSvc)
	homepageHandler := homepage.NewHandler(homepageSvc)
	richTextHandler := richtext.NewHandler(richTextSvc)
	scheduleHandler := schedule.NewHandler(scheduleSvc)
	platformLogHandler := platformlog.NewHandler(platformLogSvc)
	moderationHandler := moderation.NewHandler(moderationSvc)
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

	return s.setupRoutes(jwtMgr, permSvc, userHandler, threadHandler, categoryHandler, postHandler, spaceHandler, eventHandler, pluginHandler, roleHandler, aiHandler, integrationHandler, webhookHandler, mcpHandler, messageHandler, homepageHandler, richTextHandler, scheduleHandler, platformLogHandler, moderationHandler, metricsCollector)
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
	server.SetPluginAuthenticator(s.manager.AuthorizeHostAPI)
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
		return s.features.Enabled("personal-schedule")
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
	config, ok := manager.GetPluginConfig(name)
	if !ok {
		return nil
	}
	return config
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
	moderationHandler *moderation.Handler,
	metricsCollector *observability.Collector) error {

	r := gin.Default()
	webThemeHandler := webtheme.NewHandler(webtheme.NewService(s.manager.GetPlugin))
	runtimeHTTPHandler := plugin.NewRuntimeHTTPHandler(s.manager, func(ctx context.Context, userID, resource, action string) (bool, error) {
		return permSvc.Check(ctx, userID, resource, action)
	})

	// 全局中间件
	r.Use(middleware.Recovery())
	r.Use(middleware.CORS())
	r.Use(middleware.TraceID())
	r.Use(middleware.Logger())
	r.Use(observability.Middleware(metricsCollector))
	r.Use(platformfeature.PathGate(s.features,
		platformfeature.PathRule{Prefix: "/api/v1/spaces", FeatureID: "personal-space"},
		platformfeature.PathRule{Prefix: "/api/v1/space", FeatureID: "personal-space"},
		platformfeature.PathRule{Prefix: "/api/v1/u/", FeatureID: "personal-space"},
		platformfeature.PathRule{Prefix: "/api/v1/richtext", FeatureID: "controlled-richtext-article"},
		platformfeature.PathRule{Prefix: "/api/v1/schedule", FeatureID: "personal-schedule"},
		platformfeature.PathRule{Prefix: "/api/v1/home", FeatureID: "appearance"},
		platformfeature.PathRule{Prefix: "/api/v1/web-themes", FeatureID: "appearance"},
	))

	v1 := r.Group("/api/v1")

	// ─── 公开接口（无需认证）───
	public := v1.Group("")
	{
		public.GET("/health", userHandler.HealthCheck)
		public.GET("/home/config", homepageHandler.GetConfig)
		public.GET("/web-themes", webThemeHandler.Catalog)
		public.GET("/web-themes/:name", webThemeHandler.Package)
		public.GET("/web-themes/:name/assets/*path", webThemeHandler.Asset)
		public.GET("/ui/runtime-manifest", middleware.OptionalJWT(jwtMgr), runtimeHTTPHandler.RuntimeManifest)
		public.GET("/ui/events", runtimeHTTPHandler.Events)
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
		authenticated.GET("/moderation/status", moderationHandler.Status)
		authenticated.GET("/moderation/me", moderationHandler.MyAccess)
		authenticated.POST("/moderation/threads/:id/pin", moderationHandler.Pin)
		authenticated.POST("/moderation/threads/:id/unpin", moderationHandler.Unpin)
		authenticated.POST("/moderation/threads/:id/lock", moderationHandler.Lock)
		authenticated.POST("/moderation/threads/:id/unlock", moderationHandler.Unlock)
		authenticated.DELETE("/moderation/threads/:id/posts/:post_id", moderationHandler.DeletePost)
		authenticated.GET("/extensions/:plugin/*path", runtimeHTTPHandler.Extension)
		authenticated.POST("/extensions/:plugin/*path", runtimeHTTPHandler.Extension)
		authenticated.PUT("/extensions/:plugin/*path", runtimeHTTPHandler.Extension)
		authenticated.PATCH("/extensions/:plugin/*path", runtimeHTTPHandler.Extension)
		authenticated.DELETE("/extensions/:plugin/*path", runtimeHTTPHandler.Extension)
	}

	// ─── 管理员接口（需要权限）───
	admin := v1.Group("")
	admin.Use(middleware.JWTAuth(jwtMgr))
	{
		// 用户管理
		admin.POST("/users/:id/suspend", middleware.RequirePermission(permSvc, "user", "suspend"), userHandler.SuspendUser)
		admin.POST("/users/:id/activate", middleware.RequirePermission(permSvc, "user", "suspend"), userHandler.ActivateUser)

		// 帖子管理
		admin.GET("/admin/threads", middleware.RequirePermission(permSvc, "thread", "read"), threadHandler.AdminListThreads)
		admin.POST("/threads/:id/pin", middleware.RequirePermission(permSvc, "thread", "pin"), threadHandler.PinThread)
		admin.POST("/threads/:id/unpin", middleware.RequirePermission(permSvc, "thread", "pin"), threadHandler.UnpinThread)
		admin.POST("/threads/:id/lock", middleware.RequirePermission(permSvc, "thread", "lock"), threadHandler.LockThread)
		admin.POST("/threads/:id/unlock", middleware.RequirePermission(permSvc, "thread", "lock"), threadHandler.UnlockThread)
		admin.DELETE("/admin/threads/:id", middleware.RequirePermission(permSvc, "thread", "delete"), threadHandler.AdminDeleteThread)
		admin.POST("/richtext/articles/:id/admin/offline", middleware.RequirePermission(permSvc, "richtext", "moderate"), richTextHandler.AdminOffline)
		admin.POST("/richtext/articles/:id/admin/restore", middleware.RequirePermission(permSvc, "richtext", "moderate"), richTextHandler.AdminRestore)
		admin.DELETE("/richtext/articles/:id/admin", middleware.RequirePermission(permSvc, "richtext", "moderate"), richTextHandler.AdminDelete)

		// 版块管理
		admin.POST("/categories", middleware.RequirePermission(permSvc, "category", "write"), categoryHandler.Create)
		admin.PUT("/categories/:id", middleware.RequirePermission(permSvc, "category", "write"), categoryHandler.Update)
		admin.DELETE("/categories/:id", middleware.RequirePermission(permSvc, "category", "delete"), categoryHandler.Delete)

		// 插件管理
		admin.GET("/plugins", middleware.RequirePermission(permSvc, "plugin", "read"), pluginHandler.ListPlugins)
		admin.GET("/plugins/:name", middleware.RequirePermission(permSvc, "plugin", "read"), pluginHandler.GetPlugin)
		admin.GET("/plugins/:name/logs", middleware.RequirePermission(permSvc, "plugin", "read"), pluginHandler.ListPluginLogs)
		admin.GET("/plugins/:name/export", middleware.RequirePermission(permSvc, "plugin", "read"), pluginHandler.ExportPlugin)
		admin.PUT("/plugins/:name/config", middleware.RequirePermission(permSvc, "plugin", "configure"), pluginHandler.UpdatePluginConfig)
		admin.POST("/plugins/:name/enable", middleware.RequirePermission(permSvc, "plugin", "lifecycle"), pluginHandler.EnablePlugin)
		admin.POST("/plugins/:name/disable", middleware.RequirePermission(permSvc, "plugin", "lifecycle"), pluginHandler.DisablePlugin)
		admin.POST("/plugins/:name/reload", middleware.RequirePermission(permSvc, "plugin", "lifecycle"), pluginHandler.ReloadUserPlugin)
		admin.DELETE("/plugins/:name", middleware.RequirePermission(permSvc, "plugin", "uninstall"), pluginHandler.UninstallPlugin)
		admin.POST("/plugin-packages/import", middleware.RequirePermission(permSvc, "plugin", "install"), pluginHandler.ImportPluginPackage)
		admin.POST("/plugin-packages/precheck", middleware.RequirePermission(permSvc, "plugin", "install"), pluginHandler.PrecheckPluginPackage)
		admin.GET("/plugins/:name/snapshots", middleware.RequirePermission(permSvc, "plugin", "read"), pluginHandler.ListVersionSnapshots)
		admin.POST("/plugins/:name/rollback", middleware.RequirePermission(permSvc, "plugin", "install"), pluginHandler.RollbackVersionSnapshot)

		// AI Gateway 管理
		admin.GET("/ai/status", middleware.RequirePermission(permSvc, "ai", "read"), aiHandler.GetStatus)
		admin.GET("/ai/logs", middleware.RequirePermission(permSvc, "ai", "read"), aiHandler.ListLogs)

		// v0.5 集成中心与运营化能力
		admin.GET("/integrations/overview", middleware.RequirePermission(permSvc, "integration", "read"), integrationHandler.Overview)
		admin.GET("/metrics/summary", middleware.RequirePermission(permSvc, "metrics", "read"), integrationHandler.Metrics)
		admin.GET("/spaces/admin/summary", middleware.RequirePermission(permSvc, "space", "manage"), spaceHandler.AdminSummary)
		admin.POST("/spaces/:user_id/disable", middleware.RequirePermission(permSvc, "space", "manage"), spaceHandler.DisableSpace)
		admin.POST("/spaces/:user_id/enable", middleware.RequirePermission(permSvc, "space", "manage"), spaceHandler.EnableSpace)

		admin.GET("/webhooks", middleware.RequirePermission(permSvc, "webhook", "read"), webhookHandler.ListEndpoints)
		admin.POST("/webhooks", middleware.RequirePermission(permSvc, "webhook", "write"), webhookHandler.CreateEndpoint)
		admin.GET("/webhooks/summary", middleware.RequirePermission(permSvc, "webhook", "read"), webhookHandler.Summary)
		admin.POST("/webhooks/:id/test", middleware.RequirePermission(permSvc, "webhook", "execute"), webhookHandler.TestEndpoint)
		admin.POST("/webhooks/:id/enable", middleware.RequirePermission(permSvc, "webhook", "write"), webhookHandler.EnableEndpoint)
		admin.POST("/webhooks/:id/disable", middleware.RequirePermission(permSvc, "webhook", "write"), webhookHandler.DisableEndpoint)
		admin.GET("/webhooks/:id/deliveries", middleware.RequirePermission(permSvc, "webhook", "read"), webhookHandler.ListDeliveries)

		admin.GET("/mcp/tools", middleware.RequirePermission(permSvc, "mcp", "read"), mcpHandler.ListTools)
		admin.POST("/mcp/tools/:name/call", middleware.RequirePermission(permSvc, "mcp", "call"), mcpHandler.CallTool)
		admin.GET("/mcp/audit", middleware.RequirePermission(permSvc, "mcp", "read"), mcpHandler.ListAudit)
		admin.GET("/mcp/settings", middleware.RequirePermission(permSvc, "mcp", "read"), mcpHandler.GetSettings)
		admin.PUT("/mcp/settings", middleware.RequirePermission(permSvc, "mcp", "configure"), mcpHandler.UpdateSettings)

		admin.GET("/messages/adapters", middleware.RequirePermission(permSvc, "message", "read"), messageHandler.ListAdapters)
		admin.POST("/messages/local/inbound", middleware.RequirePermission(permSvc, "message", "write"), messageHandler.ReceiveLocal)
		admin.GET("/messages/logs", middleware.RequirePermission(permSvc, "message", "read"), messageHandler.ListMessages)
		admin.POST("/messages/bindings", middleware.RequirePermission(permSvc, "message", "write"), messageHandler.CreateBinding)
		admin.GET("/messages/summary", middleware.RequirePermission(permSvc, "message", "read"), messageHandler.Summary)

		admin.GET("/platform/logs/sources", middleware.RequirePermission(permSvc, "platform_log", "read"), platformLogHandler.Sources)
		admin.GET("/platform/logs/stream", middleware.RequirePermission(permSvc, "platform_log", "read"), platformLogHandler.Stream)

		admin.POST("/home/style-packs/validate", middleware.RequirePermission(permSvc, "homepage", "configure"), homepageHandler.ValidateStylePack)
		admin.GET("/home/style-packs/example", middleware.RequirePermission(permSvc, "homepage", "configure"), homepageHandler.StylePackExample)
		admin.GET("/home/style-packs/example.zip", middleware.RequirePermission(permSvc, "homepage", "configure"), homepageHandler.StylePackExampleZip)
		admin.GET("/home/style-packs/sources", middleware.RequirePermission(permSvc, "homepage", "configure"), homepageHandler.ListSourceStylePacks)
		admin.POST("/home/style-packs/apply", middleware.RequirePermission(permSvc, "homepage", "configure"), homepageHandler.ApplyStylePack)
		admin.POST("/home/style-packs/apply-source", middleware.RequirePermission(permSvc, "homepage", "configure"), homepageHandler.ApplySourceStylePack)
		admin.POST("/home/style-packs/rollback", middleware.RequirePermission(permSvc, "homepage", "configure"), homepageHandler.RollbackStylePack)

		// 角色管理
		admin.GET("/roles", middleware.RequirePermission(permSvc, "role", "read"), roleHandler.ListRoles)
		admin.GET("/users/:id/roles", middleware.RequirePermission(permSvc, "role", "read"), roleHandler.GetUserRoles)
		admin.POST("/users/:id/roles", middleware.RequirePermission(permSvc, "role", "assign"), roleHandler.AssignRole)
		admin.DELETE("/users/:id/roles", middleware.RequirePermission(permSvc, "role", "revoke"), roleHandler.RevokeRole)

		// 板块版主插件配置。角色范围保存在核心 RBAC，治理动作由插件运行状态控制。
		admin.GET("/moderation/admin/moderators", middleware.RequirePermission(permSvc, "role", "read"), moderationHandler.ListModerators)
		admin.GET("/moderation/admin/moderators/:id", middleware.RequirePermission(permSvc, "role", "read"), moderationHandler.GetModerator)
		admin.PUT("/moderation/admin/moderators/:id", middleware.RequirePermission(permSvc, "role", "assign"), moderationHandler.SetModerator)
	}

	addr := s.cfg.Server.Addr()
	log.Printf("🚀 CampusOS API 监听 %s", addr)
	log.Printf("📋 API 端点总数: %d", len(r.Routes()))
	log.Printf("🔌 已加载 %d 个插件", len(s.manager.ListPlugins()))
	return s.serveHTTP(r)
}
