package server

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/campusos/CampusOS/internal/ai"
	communitycore "github.com/campusos/CampusOS/internal/community"
	communityport "github.com/campusos/CampusOS/internal/community/port"
	identitycore "github.com/campusos/CampusOS/internal/core/identity"
	identityport "github.com/campusos/CampusOS/internal/core/identity/port"
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

	// ─── 初始化 AI Gateway ───
	aiService := s.initAIService()

	pool := infra.database
	if pool == nil {
		log.Printf("⚠️  PostgreSQL 基础设施不可用，回退到内存模式: %v", infra.databaseErr)
		return s.runMemoryMode(bus, memBus, aiService, metricsCollector, infra.pluginRepo)
	}

	// ─── 设置 PG 插件仓储 ───
	pluginRepo := infra.pluginRepo
	aiService.SetCallLogStore(ai.NewPgCallLogger(pool))

	// ─── 种子数据（默认管理员）───
	if err := SeedAdmin(pool, s.cfg.Auth.PasswordHashEnabled); err != nil {
		log.Printf("⚠️  种子数据初始化失败: %v", err)
	}

	identityModule := s.identity
	if identityModule == nil || identityModule.Permissions() == nil {
		return fmt.Errorf("identity core module is unavailable")
	}
	jwtMgr := identityModule.JWTManager()
	communityModule := s.community
	if communityModule == nil || communityModule.ThreadService() == nil {
		return fmt.Errorf("community core module is unavailable")
	}
	categoryCatalog, contentGateway, err := s.communityIntegrationPorts()
	if err != nil {
		return err
	}
	communityHandlers := communityModule.Handlers()
	permSvc := identityModule.Permissions()
	identityHandlers := identityModule.Handlers()

	// ─── 初始化 Host API ───
	userReader, postReader, err := s.hostAPIPorts()
	if err != nil {
		return err
	}
	hostAPI := hostapi.NewHostAPIv2FromHostAPI(hostapi.NewHostAPI(userReader, contentGateway, postReader, bus))
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

	moderationModule := s.moderation
	if moderationModule == nil || moderationModule.Handler() == nil {
		return fmt.Errorf("moderation core module is unavailable")
	}
	if s.space == nil || s.space.Handler() == nil || s.richtext == nil || s.richtext.Handler() == nil || s.schedule == nil || s.schedule.Handler() == nil || s.appearance == nil || s.appearance.HomepageHandler() == nil || s.appearance.WebThemeHandler() == nil {
		return fmt.Errorf("built-in feature modules are unavailable")
	}
	spaceSvc := s.space.Service()
	spaceHandler := s.space.Handler()
	richTextHandler := s.richtext.Handler()
	scheduleHandler := s.schedule.Handler()
	homepageHandler := s.appearance.HomepageHandler()
	webThemeHandler := s.appearance.WebThemeHandler()
	webhookSvc := webhook.NewService(webhook.NewPgStore(pool), metricsCollector)
	if err := webhookSvc.Register(bus); err != nil {
		log.Printf("⚠️  Webhook 事件订阅失败: %v", err)
	}
	mcpSvc := mcp.NewService(categoryCatalog, contentGateway, mcp.NewPgAuditStore(pool), metricsCollector)
	messageSvc := message.NewService(message.NewPgStore(pool), metricsCollector)
	platformLogSvc := platformlog.NewServiceFromEnv()

	// ─── 初始化处理器层 ───
	pluginHandler := plugin.NewHandler(s.manager, plugin.WithPluginsDir(plugin.PluginsDirFromEnv()), plugin.WithFeatureRegistry(s.features))
	aiHandler := ai.NewHandler(aiService)
	webhookHandler := webhook.NewHandler(webhookSvc)
	mcpHandler := mcp.NewHandler(mcpSvc)
	messageHandler := message.NewHandler(messageSvc)
	platformLogHandler := platformlog.NewHandler(platformLogSvc)
	moderationHandler := moderationModule.Handler()
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

	return s.setupRoutes(jwtMgr, permSvc, identityHandlers, communityHandlers, spaceHandler, pluginHandler, aiHandler, integrationHandler, webhookHandler, mcpHandler, messageHandler, homepageHandler, webThemeHandler, richTextHandler, scheduleHandler, platformLogHandler, moderationHandler, metricsCollector)
}

func (s *Server) runMemoryMode(bus eventbus.EventBus, memBus *eventbus.MemoryEventBus, aiService *ai.Service, metricsCollector *observability.Collector, pluginRepo plugin.PluginRepository) error {
	identityModule := s.identity
	if identityModule == nil || identityModule.Permissions() == nil {
		return fmt.Errorf("identity core module is unavailable")
	}
	jwtMgr := identityModule.JWTManager()
	communityModule := s.community
	if communityModule == nil || communityModule.ThreadService() == nil {
		return fmt.Errorf("community core module is unavailable")
	}
	categoryCatalog, contentGateway, err := s.communityIntegrationPorts()
	if err != nil {
		return err
	}
	if pluginRepo == nil {
		pluginRepo = plugin.NewMemoryPluginRepository()
	}
	communityHandlers := communityModule.Handlers()
	permSvc := identityModule.Permissions()
	identityHandlers := identityModule.Handlers()

	// ─── 初始化 Host API ───
	userReader, postReader, err := s.hostAPIPorts()
	if err != nil {
		return err
	}
	hostAPI := hostapi.NewHostAPIv2FromHostAPI(hostapi.NewHostAPI(userReader, contentGateway, postReader, bus))
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

	moderationModule := s.moderation
	if moderationModule == nil || moderationModule.Handler() == nil {
		return fmt.Errorf("moderation core module is unavailable")
	}
	if s.space == nil || s.space.Handler() == nil || s.richtext == nil || s.richtext.Handler() == nil || s.schedule == nil || s.schedule.Handler() == nil || s.appearance == nil || s.appearance.HomepageHandler() == nil || s.appearance.WebThemeHandler() == nil {
		return fmt.Errorf("built-in feature modules are unavailable")
	}
	spaceSvc := s.space.Service()
	spaceHandler := s.space.Handler()
	richTextHandler := s.richtext.Handler()
	scheduleHandler := s.schedule.Handler()
	homepageHandler := s.appearance.HomepageHandler()
	webThemeHandler := s.appearance.WebThemeHandler()
	webhookSvc := webhook.NewService(webhook.NewMemoryStore(), metricsCollector)
	if err := webhookSvc.Register(bus); err != nil {
		log.Printf("⚠️  Webhook 事件订阅失败: %v", err)
	}
	mcpSvc := mcp.NewService(categoryCatalog, contentGateway, mcp.NewMemoryAuditStore(), metricsCollector)
	messageSvc := message.NewService(message.NewMemoryStore(), metricsCollector)
	platformLogSvc := platformlog.NewServiceFromEnv()

	pluginHandler := plugin.NewHandler(s.manager, plugin.WithPluginsDir(plugin.PluginsDirFromEnv()), plugin.WithFeatureRegistry(s.features))
	aiHandler := ai.NewHandler(aiService)
	webhookHandler := webhook.NewHandler(webhookSvc)
	mcpHandler := mcp.NewHandler(mcpSvc)
	messageHandler := message.NewHandler(messageSvc)
	platformLogHandler := platformlog.NewHandler(platformLogSvc)
	moderationHandler := moderationModule.Handler()
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

	return s.setupRoutes(jwtMgr, permSvc, identityHandlers, communityHandlers, spaceHandler, pluginHandler, aiHandler, integrationHandler, webhookHandler, mcpHandler, messageHandler, homepageHandler, webThemeHandler, richTextHandler, scheduleHandler, platformLogHandler, moderationHandler, metricsCollector)
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

func (s *Server) communityIntegrationPorts() (communityport.CategoryCatalog, communityport.ContentGateway, error) {
	if s.appContext == nil {
		return nil, nil, fmt.Errorf("module app context is unavailable")
	}
	categoriesValue, ok := s.appContext.Lookup("community.category-catalog")
	if !ok {
		return nil, nil, fmt.Errorf("community category catalog port is unavailable")
	}
	categories, ok := categoriesValue.(communityport.CategoryCatalog)
	if !ok {
		return nil, nil, fmt.Errorf("community category catalog port has incompatible type %T", categoriesValue)
	}
	contentValue, ok := s.appContext.Lookup("community.content-gateway")
	if !ok {
		return nil, nil, fmt.Errorf("community content gateway port is unavailable")
	}
	content, ok := contentValue.(communityport.ContentGateway)
	if !ok {
		return nil, nil, fmt.Errorf("community content gateway port has incompatible type %T", contentValue)
	}
	return categories, content, nil
}

func (s *Server) hostAPIPorts() (identityport.UserReader, hostapi.PostReader, error) {
	if s.appContext == nil {
		return nil, nil, fmt.Errorf("module app context is unavailable")
	}
	usersValue, ok := s.appContext.Lookup("identity.user-reader")
	if !ok {
		return nil, nil, fmt.Errorf("identity user reader port is unavailable")
	}
	users, ok := usersValue.(identityport.UserReader)
	if !ok {
		return nil, nil, fmt.Errorf("identity user reader port has incompatible type %T", usersValue)
	}
	postsValue, ok := s.appContext.Lookup("community.moderation-gateway")
	if !ok {
		return nil, nil, fmt.Errorf("community post reader port is unavailable")
	}
	posts, ok := postsValue.(hostapi.PostReader)
	if !ok {
		return nil, nil, fmt.Errorf("community post reader port has incompatible type %T", postsValue)
	}
	return users, posts, nil
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
	permSvc middleware.PermissionChecker,
	identityHandlers identitycore.HTTPHandlers,
	communityHandlers communitycore.HTTPHandlers,
	spaceHandler *space.Handler,
	pluginHandler *plugin.Handler,
	aiHandler *ai.Handler,
	integrationHandler *integration.Handler,
	webhookHandler *webhook.Handler,
	mcpHandler *mcp.Handler,
	messageHandler *message.Handler,
	homepageHandler *homepage.Handler,
	webThemeHandler *webtheme.Handler,
	richTextHandler *richtext.Handler,
	scheduleHandler *schedule.Handler,
	platformLogHandler *platformlog.Handler,
	moderationHandler *moderation.Handler,
	metricsCollector *observability.Collector) error {

	r := gin.Default()
	userHandler := identityHandlers.User
	roleHandler := identityHandlers.Role
	threadHandler := communityHandlers.Thread
	categoryHandler := communityHandlers.Category
	postHandler := communityHandlers.Post
	eventHandler := communityHandlers.Event
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
