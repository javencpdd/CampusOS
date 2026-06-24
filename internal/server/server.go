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
	natsBus, err := eventbus.NewNATSEventBus(s.cfg.NATS.URL)
	if err != nil {
		log.Printf("⚠️  NATS 连接失败，回退到内存事件总线: %v", err)
		bus = eventbus.NewMemoryEventBus()
	} else {
		bus = natsBus
	}
	s.bus = bus
	defer bus.Close()

	// ─── 注册默认事件订阅（事件日志）───
	s.registerDefaultSubscriptions(bus)

	// ─── 初始化 PostgreSQL ───
	pool, err := database.New(s.cfg.Database.DSN)
	if err != nil {
		log.Printf("⚠️  PostgreSQL 连接失败，回退到内存模式: %v", err)
		return s.runMemoryMode(bus)
	}
	defer pool.Close()
	log.Printf("✅ PostgreSQL 连接成功")

	// ─── 初始化 JWT ───
	jwtMgr := s.newJWTManager()

	// ─── 初始化仓储层（PostgreSQL）───
	userRepo := identityrepo.NewPgUserRepository(pool)
	threadRepo := repository.NewPgThreadRepository(pool)

	// ─── 初始化服务层 ───
	userSvc := identitysvc.NewUserService(userRepo, jwtMgr, userRepo, bus)
	threadSvc := service.NewThreadService(threadRepo, bus)

	// ─── 初始化处理器层 ───
	userHandler := identityhandler.NewUserHandler(userSvc)
	threadHandler := handler.NewThreadHandler(threadSvc)
	categoryHandler := handler.NewCategoryHandler(nil)
	postHandler := handler.NewPostHandler(nil)

	// 创建事件历史处理器（仅内存模式可用）
	var memBus *eventbus.MemoryEventBus
	if mb, ok := bus.(*eventbus.MemoryEventBus); ok {
		memBus = mb
	}
	eventHandler := handler.NewEventHandler(memBus)

	return s.setupRoutes(jwtMgr, userHandler, threadHandler, categoryHandler, postHandler, eventHandler)
}

func (s *Server) runMemoryMode(bus eventbus.EventBus) error {
	jwtMgr := s.newJWTManager()

	userRepo := identityrepo.NewMemoryUserRepository()
	threadRepo := repository.NewMemoryThreadRepository()
	categoryRepo := repository.NewMemoryCategoryRepository()
	postRepo := repository.NewMemoryPostRepository()

	userSvc := identitysvc.NewUserService(userRepo, jwtMgr, nil, bus)
	threadSvc := service.NewThreadService(threadRepo, bus)
	categorySvc := service.NewCategoryService(categoryRepo, bus)
	postSvc := service.NewPostService(postRepo, bus)

	userHandler := identityhandler.NewUserHandler(userSvc)
	threadHandler := handler.NewThreadHandler(threadSvc)
	categoryHandler := handler.NewCategoryHandler(categorySvc)
	postHandler := handler.NewPostHandler(postSvc)

	var memBus *eventbus.MemoryEventBus
	if mb, ok := bus.(*eventbus.MemoryEventBus); ok {
		memBus = mb
	}
	eventHandler := handler.NewEventHandler(memBus)

	return s.setupRoutes(jwtMgr, userHandler, threadHandler, categoryHandler, postHandler, eventHandler)
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
	// 事件日志订阅器
	bus.Subscribe("user.created", func(ctx context.Context, event eventbus.Event) error {
		log.Printf("📢 Event: %s | Subject: %s | Source: %s", event.Type, event.Subject, event.Source)
		return nil
	})
	bus.Subscribe("thread.created", func(ctx context.Context, event eventbus.Event) error {
		log.Printf("📢 Event: %s | Subject: %s | Source: %s", event.Type, event.Subject, event.Source)
		return nil
	})
	bus.Subscribe("thread.updated", func(ctx context.Context, event eventbus.Event) error {
		log.Printf("📢 Event: %s | Subject: %s | Source: %s", event.Type, event.Subject, event.Source)
		return nil
	})
	bus.Subscribe("thread.deleted", func(ctx context.Context, event eventbus.Event) error {
		log.Printf("📢 Event: %s | Subject: %s | Source: %s", event.Type, event.Subject, event.Source)
		return nil
	})
	bus.Subscribe("post.created", func(ctx context.Context, event eventbus.Event) error {
		log.Printf("📢 Event: %s | Subject: %s | Source: %s", event.Type, event.Subject, event.Source)
		return nil
	})
	log.Printf("✅ 已注册 5 个默认事件订阅")
}

func (s *Server) setupRoutes(jwtMgr *auth.JWTManager,
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

	// 公开接口
	public := v1.Group("")
	{
		public.GET("/health", userHandler.HealthCheck)
		public.POST("/auth/register", userHandler.Register)
		public.POST("/auth/login", userHandler.Login)
		public.GET("/threads", threadHandler.ListThreads)
		public.GET("/threads/:id", threadHandler.GetThread)
		public.GET("/users", userHandler.ListUsers)
		public.GET("/users/:id", userHandler.GetUser)
	}

	// 版块接口
	public.GET("/categories", categoryHandler.List)
	public.GET("/categories/:id", categoryHandler.Get)
	// 帖子回复接口
	public.GET("/threads/:id/posts", postHandler.ListPosts)
	// 事件历史接口
	public.GET("/events", eventHandler.ListEvents)

	// 需要 JWT 认证的接口
	authenticated := v1.Group("")
	authenticated.Use(middleware.JWTAuth(jwtMgr))
	{
		authenticated.GET("/auth/me", userHandler.GetMe)
		authenticated.PUT("/users/:id", userHandler.UpdateUser)
		authenticated.POST("/threads", threadHandler.CreateThread)
		authenticated.PUT("/threads/:id", threadHandler.UpdateThread)
		authenticated.DELETE("/threads/:id", threadHandler.DeleteThread)
		authenticated.POST("/categories", categoryHandler.Create)
		authenticated.PUT("/categories/:id", categoryHandler.Update)
		authenticated.POST("/threads/:id/posts", postHandler.CreatePost)
	}

	addr := s.cfg.Server.Addr()
	log.Printf("🚀 CampusOS API 监听 %s", addr)
	return r.Run(addr)
}
