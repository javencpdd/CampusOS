package server

import (
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
	"github.com/campusos/CampusOS/pkg/middleware"
	"github.com/gin-gonic/gin"
)

type Server struct {
	cfg *config.Config
}

func New(cfg *config.Config) *Server {
	return &Server{cfg: cfg}
}

func (s *Server) Run() error {
	// ─── 初始化 PostgreSQL ───
	pool, err := database.New(s.cfg.Database.DSN)
	if err != nil {
		log.Printf("⚠️  PostgreSQL 连接失败，回退到内存模式: %v", err)
		return s.runMemoryMode()
	}
	defer pool.Close()
	log.Printf("✅ PostgreSQL 连接成功")

	// ─── 初始化 JWT ───
	accessTTL, _ := time.ParseDuration(s.cfg.JWT.AccessTTL)
	refreshTTL, _ := time.ParseDuration(s.cfg.JWT.RefreshTTL)
	jwtMgr := auth.NewJWTManager(auth.JWTConfig{
		Secret:     s.cfg.JWT.Secret,
		AccessTTL:  accessTTL,
		RefreshTTL: refreshTTL,
		Issuer:     s.cfg.JWT.Issuer,
	})

	// ─── 初始化仓储层（PostgreSQL）───
	userRepo := identityrepo.NewPgUserRepository(pool)
	threadRepo := repository.NewPgThreadRepository(pool)

	// ─── 初始化服务层 ───
	userSvc := identitysvc.NewUserService(userRepo, jwtMgr, userRepo)
	threadSvc := service.NewThreadService(threadRepo)

	// ─── 初始化处理器层 ───
	userHandler := identityhandler.NewUserHandler(userSvc)
	threadHandler := handler.NewThreadHandler(threadSvc)
	categoryHandler := handler.NewCategoryHandler(nil) // PG mode 会在下面初始化
	postHandler := handler.NewPostHandler(nil)

	// ─── 配置路由 ───
	return s.setupRoutes(jwtMgr, userHandler, threadHandler, categoryHandler, postHandler)
}

func (s *Server) runMemoryMode() error {
	// 内存模式（兼容 v0.1.0-dev）
	jwtMgr := auth.NewJWTManager(auth.JWTConfig{
		Secret:     s.cfg.JWT.Secret,
		AccessTTL:  2 * time.Hour,
		RefreshTTL: 30 * 24 * time.Hour,
		Issuer:     s.cfg.JWT.Issuer,
	})

	userRepo := identityrepo.NewMemoryUserRepository()
	threadRepo := repository.NewMemoryThreadRepository()
	categoryRepo := repository.NewMemoryCategoryRepository()
	postRepo := repository.NewMemoryPostRepository()

	userSvc := identitysvc.NewUserService(userRepo, jwtMgr, nil)
	threadSvc := service.NewThreadService(threadRepo)
	categorySvc := service.NewCategoryService(categoryRepo)
	postSvc := service.NewPostService(postRepo)

	userHandler := identityhandler.NewUserHandler(userSvc)
	threadHandler := handler.NewThreadHandler(threadSvc)
	categoryHandler := handler.NewCategoryHandler(categorySvc)
	postHandler := handler.NewPostHandler(postSvc)

	return s.setupRoutes(jwtMgr, userHandler, threadHandler, categoryHandler, postHandler)
}

func (s *Server) setupRoutes(jwtMgr *auth.JWTManager,
	userHandler *identityhandler.UserHandler,
	threadHandler *handler.ThreadHandler,
	categoryHandler *handler.CategoryHandler,
	postHandler *handler.PostHandler) error {

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
