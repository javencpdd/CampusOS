package server

import (
	"context"
	"log"
	"time"

	"github.com/campusos/CampusOS/internal/community/handler"
	"github.com/campusos/CampusOS/internal/community/repository"
	"github.com/campusos/CampusOS/internal/community/service"
	identityhandler "github.com/campusos/CampusOS/internal/core/identity/handler"
	identityrepo "github.com/campusos/CampusOS/internal/core/identity/repository"
	identitysvc "github.com/campusos/CampusOS/internal/core/identity/service"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/campusos/CampusOS/pkg/config"
	"github.com/campusos/CampusOS/pkg/database"
	"github.com/campusos/CampusOS/pkg/eventbus"
	"github.com/campusos/CampusOS/pkg/middleware"
	"github.com/gin-gonic/gin"
)

type Server struct {
	cfg *config.Config
	bus eventbus.EventBus
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
		// NATS 模式下也创建一个 MemoryEventBus 用于事件历史
		memBus = eventbus.NewMemoryEventBus()
	}
	s.bus = bus
	defer bus.Close()

	// ─── 注册默认事件订阅（事件日志）───
	s.registerDefaultSubscriptions(bus)

	// ─── 初始化 PostgreSQL ───
	pool, err := database.New(s.cfg.Database.DSN)
	if err != nil {
		log.Printf("⚠️  PostgreSQL 连接失败，回退到内存模式: %v", err)
		return s.runMemoryMode(bus, memBus)
	}
	defer pool.Close()
	log.Printf("✅ PostgreSQL 连接成功")

	// ─── 初始化 JWT ───
	jwtMgr := s.newJWTManager()

	// ─── 初始化仓储层（PostgreSQL）───
	userRepo := identityrepo.NewPgUserRepository(pool)
	threadRepo := repository.NewPgThreadRepository(pool)
	categoryRepo := repository.NewPgCategoryRepository(pool)
	postRepo := repository.NewPgPostRepository(pool)
	roleRepo := identityrepo.NewPgRoleRepository(pool)

	// ─── 初始化服务层 ───
	userSvc := identitysvc.NewUserService(userRepo, jwtMgr, userRepo, bus)
	threadSvc := service.NewThreadService(threadRepo, bus)
	categorySvc := service.NewCategoryService(categoryRepo, bus)
	postSvc := service.NewPostService(postRepo, bus)
	permSvc := identitysvc.NewPermissionService(roleRepo)

	// ─── 初始化处理器层 ───
	userHandler := identityhandler.NewUserHandler(userSvc)
	threadHandler := handler.NewThreadHandler(threadSvc)
	categoryHandler := handler.NewCategoryHandler(categorySvc)
	postHandler := handler.NewPostHandler(postSvc)
	eventHandler := handler.NewEventHandler(memBus)

	return s.setupRoutes(jwtMgr, permSvc, userHandler, threadHandler, categoryHandler, postHandler, eventHandler)
}

func (s *Server) runMemoryMode(bus eventbus.EventBus, memBus *eventbus.MemoryEventBus) error {
	jwtMgr := s.newJWTManager()

	userRepo := identityrepo.NewMemoryUserRepository()
	threadRepo := repository.NewMemoryThreadRepository()
	categoryRepo := repository.NewMemoryCategoryRepository()
	postRepo := repository.NewMemoryPostRepository()
	roleRepo := identityrepo.NewMemoryRoleRepository()

	userSvc := identitysvc.NewUserService(userRepo, jwtMgr, nil, bus)
	threadSvc := service.NewThreadService(threadRepo, bus)
	categorySvc := service.NewCategoryService(categoryRepo, bus)
	postSvc := service.NewPostService(postRepo, bus)
	permSvc := identitysvc.NewPermissionService(roleRepo)

	userHandler := identityhandler.NewUserHandler(userSvc)
	threadHandler := handler.NewThreadHandler(threadSvc)
	categoryHandler := handler.NewCategoryHandler(categorySvc)
	postHandler := handler.NewPostHandler(postSvc)
	eventHandler := handler.NewEventHandler(memBus)

	return s.setupRoutes(jwtMgr, permSvc, userHandler, threadHandler, categoryHandler, postHandler, eventHandler)
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
			return nil
		})
	}
	log.Printf("✅ 已注册 %d 个默认事件订阅", len(eventTypes))
}

func (s *Server) setupRoutes(jwtMgr *auth.JWTManager,
	permSvc *identitysvc.PermissionService,
	userHandler *identityhandler.UserHandler,
	threadHandler *handler.ThreadHandler,
	categoryHandler *handler.CategoryHandler,
	postHandler *handler.PostHandler,
	eventHandler *handler.EventHandler) error {

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
		authenticated.POST("/threads", threadHandler.CreateThread)
		authenticated.PUT("/threads/:id", threadHandler.UpdateThread)
		authenticated.DELETE("/threads/:id", threadHandler.DeleteThread)
		authenticated.POST("/threads/:id/posts", postHandler.CreatePost)
		authenticated.PUT("/threads/:thread_id/posts/:post_id", postHandler.UpdatePost)
		authenticated.DELETE("/threads/:thread_id/posts/:post_id", postHandler.DeletePost)
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
	}

	addr := s.cfg.Server.Addr()
	log.Printf("🚀 CampusOS API 监听 %s", addr)
	log.Printf("📋 API 端点总数: 30")
	return r.Run(addr)
}
