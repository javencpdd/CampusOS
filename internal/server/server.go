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
	"github.com/campusos/CampusOS/internal/plugin"
	plugingrpc "github.com/campusos/CampusOS/internal/plugin/grpc"
	"github.com/campusos/CampusOS/internal/plugin/hostapi"
	pluginwasm "github.com/campusos/CampusOS/internal/plugin/wasm"
	"github.com/campusos/CampusOS/internal/space"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/campusos/CampusOS/pkg/cache"
	"github.com/campusos/CampusOS/pkg/config"
	"github.com/campusos/CampusOS/pkg/database"
	"github.com/campusos/CampusOS/pkg/eventbus"
	"github.com/campusos/CampusOS/pkg/middleware"
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
		return s.runMemoryMode(bus, memBus, aiService)
	}
	defer pool.Close()
	log.Printf("✅ PostgreSQL 连接成功")

	// ─── 设置 PG 插件仓储 ───
	pluginRepo = plugin.NewPgPluginRepository(pool)
	apiKeyRepo = plugin.NewPgAPIKeyRepository(pool)
	s.manager.SetPluginRepository(pluginRepo)
	aiService.SetCallLogStore(ai.NewPgCallLogger(pool))

	// ─── 种子数据（默认管理员）───
	if err := SeedAdmin(pool); err != nil {
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
	userSvc.SetRoleRepository(roleRepo)
	threadSvc := service.NewThreadService(threadRepo, bus)
	threadSvc.SetCache(appCache)
	categorySvc := service.NewCategoryService(categoryRepo, bus)
	postSvc := service.NewPostService(postRepo, bus)
	spaceSvc := space.NewService(spaceRepo, userRepo)
	if err := spaceSvc.RegisterEventHandlers(bus); err != nil {
		log.Printf("⚠️  个人主页内容同步订阅失败: %v", err)
	}

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

	return s.setupRoutes(jwtMgr, permSvc, userHandler, threadHandler, categoryHandler, postHandler, spaceHandler, eventHandler, pluginHandler, roleHandler, aiHandler)
}

func (s *Server) runMemoryMode(bus eventbus.EventBus, memBus *eventbus.MemoryEventBus, aiService *ai.Service) error {
	jwtMgr := s.newJWTManager()

	userRepo := identityrepo.NewMemoryUserRepository()
	threadRepo := repository.NewMemoryThreadRepository()
	categoryRepo := repository.NewMemoryCategoryRepository()
	postRepo := repository.NewMemoryPostRepository()
	spaceRepo := space.NewMemoryRepository()
	roleRepo := identityrepo.NewMemoryRoleRepository()
	pluginRepo := plugin.NewMemoryPluginRepository()
	s.manager.SetPluginRepository(pluginRepo)
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
	userSvc.SetRoleRepository(roleRepo)
	threadSvc := service.NewThreadService(threadRepo, bus)
	categorySvc := service.NewCategoryService(categoryRepo, bus)
	postSvc := service.NewPostService(postRepo, bus)
	spaceSvc := space.NewService(spaceRepo, userRepo)
	if err := spaceSvc.RegisterEventHandlers(bus); err != nil {
		log.Printf("⚠️  个人主页内容同步订阅失败: %v", err)
	}

	userHandler := identityhandler.NewUserHandler(userSvc)
	threadHandler := handler.NewThreadHandler(threadSvc)
	categoryHandler := handler.NewCategoryHandler(categorySvc)
	postHandler := handler.NewPostHandler(postSvc)
	spaceHandler := space.NewHandler(spaceSvc)
	eventHandler := handler.NewEventHandler(memBus)
	pluginHandler := plugin.NewHandler(s.manager, plugin.WithPluginsDir(plugin.PluginsDirFromEnv()))
	roleHandler := identityhandler.NewRoleHandler(permSvc)
	aiHandler := ai.NewHandler(aiService)

	return s.setupRoutes(jwtMgr, permSvc, userHandler, threadHandler, categoryHandler, postHandler, spaceHandler, eventHandler, pluginHandler, roleHandler, aiHandler)
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
	aiHandler *ai.Handler) error {

	r := gin.Default()

	// 全局中间件
	r.Use(middleware.Recovery())
	r.Use(middleware.CORS())
	r.Use(middleware.TraceID())
	r.Use(middleware.Logger())

	v1 := r.Group("/api/v1")

	// ─── 公开接口（无需认证）───
	public := v1.Group("")
	{
		public.GET("/health", userHandler.HealthCheck)
		public.POST("/auth/register", userHandler.Register)
		public.POST("/auth/login", userHandler.Login)
		public.GET("/threads", threadHandler.ListThreads)
		public.GET("/threads/:id", threadHandler.GetThread)
		public.GET("/users", userHandler.ListUsers)
		public.GET("/users/:id", userHandler.GetUser)
		public.GET("/space/:user_id/contents", spaceHandler.ListContentsByUserID)
		public.GET("/space/:user_id", spaceHandler.GetByUserID)
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
		authenticated.POST("/spaces/me/styles/validate", spaceHandler.ValidateStylePackage)
		authenticated.POST("/spaces/me/styles/preview", spaceHandler.PreviewStylePackage)
		authenticated.POST("/spaces/me/styles/export", spaceHandler.ExportStylePackage)
		authenticated.POST("/threads", threadHandler.CreateThread)
		authenticated.PUT("/threads/:id", threadHandler.UpdateThread)
		authenticated.DELETE("/threads/:id", threadHandler.DeleteThread)
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
		admin.POST("/threads/:id/pin", middleware.RequirePermission(permSvc, "thread", "pin"), threadHandler.PinThread)
		admin.POST("/threads/:id/unpin", middleware.RequirePermission(permSvc, "thread", "pin"), threadHandler.UnpinThread)
		admin.POST("/threads/:id/lock", middleware.RequirePermission(permSvc, "thread", "pin"), threadHandler.LockThread)
		admin.POST("/threads/:id/unlock", middleware.RequirePermission(permSvc, "thread", "pin"), threadHandler.UnlockThread)

		// 版块管理
		admin.DELETE("/categories/:id", middleware.RequirePermission(permSvc, "category", "delete"), categoryHandler.Delete)

		// 插件管理
		admin.GET("/plugins", middleware.RequirePermission(permSvc, "role", "manage"), pluginHandler.ListPlugins)
		admin.GET("/plugins/:name", middleware.RequirePermission(permSvc, "role", "manage"), pluginHandler.GetPlugin)
		admin.GET("/plugins/:name/logs", middleware.RequirePermission(permSvc, "role", "manage"), pluginHandler.ListPluginLogs)
		admin.GET("/plugins/:name/export", middleware.RequirePermission(permSvc, "role", "manage"), pluginHandler.ExportPlugin)
		admin.POST("/plugins/:name/enable", middleware.RequirePermission(permSvc, "role", "manage"), pluginHandler.EnablePlugin)
		admin.POST("/plugins/:name/disable", middleware.RequirePermission(permSvc, "role", "manage"), pluginHandler.DisablePlugin)
		admin.DELETE("/plugins/:name", middleware.RequirePermission(permSvc, "role", "manage"), pluginHandler.UninstallPlugin)
		admin.POST("/plugin-packages/import", middleware.RequirePermission(permSvc, "role", "manage"), pluginHandler.ImportPluginPackage)

		// AI Gateway 管理
		admin.GET("/ai/status", middleware.RequirePermission(permSvc, "role", "manage"), aiHandler.GetStatus)
		admin.GET("/ai/logs", middleware.RequirePermission(permSvc, "role", "manage"), aiHandler.ListLogs)

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
	log.Printf("📋 API 端点总数: 50")
	log.Printf("🔌 已加载 %d 个插件", len(s.manager.ListPlugins()))
	return r.Run(addr)
}
