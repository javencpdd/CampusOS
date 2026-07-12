package server

import (
	communityhandler "github.com/campusos/CampusOS/internal/community/handler"
	communityport "github.com/campusos/CampusOS/internal/community/port"
	communityrepo "github.com/campusos/CampusOS/internal/community/repository"
	communitysvc "github.com/campusos/CampusOS/internal/community/service"
	identityhandler "github.com/campusos/CampusOS/internal/core/identity/handler"
	identityport "github.com/campusos/CampusOS/internal/core/identity/port"
	identityrepo "github.com/campusos/CampusOS/internal/core/identity/repository"
	identitysvc "github.com/campusos/CampusOS/internal/core/identity/service"
	corestorage "github.com/campusos/CampusOS/internal/core/storage"
	pluginport "github.com/campusos/CampusOS/internal/plugin/port"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/campusos/CampusOS/pkg/cache"
	"github.com/campusos/CampusOS/pkg/eventbus"
)

func (s *Server) registerBusinessPorts(users identityrepo.UserRepository, threads communityrepo.ThreadRepository, categories communityrepo.CategoryRepository, posts communityrepo.PostRepository) error {
	config := personalSpacePluginConfig(s.manager)
	root := corestorage.DefaultRoot
	if value, ok := config["file_root"].(string); ok && value != "" {
		root = value
	}
	quota := int64(10 * 1024 * 1024)
	if value := numericConfig(config["default_quota_bytes"]); value > 0 {
		quota = value
	}
	if value := numericConfig(config["default_quota_mb"]); value > 0 {
		quota = value * 1024 * 1024
	}
	userStorage, err := corestorage.NewLocalAdapterWithQuota(root, quota)
	if err != nil {
		return err
	}
	ports := []struct {
		name  string
		value interface{}
	}{
		{"identity.user-reader", identityport.NewRepositoryUserReader(users)},
		{"community.category-reader", communityport.NewRepositoryCategoryReader(categories)},
		{"community.thread-port", communityport.NewRepositoryThreadPort(threads)},
		{"community.post-port", communityport.NewRepositoryPostPort(posts)},
		{"plugin.catalog", pluginport.NewCatalogAdapter(s.manager.Catalog())},
		{"storage.user", userStorage},
		{"storage.quota", corestorage.Quota(userStorage)},
		{"storage.safe-path", corestorage.SafePath(userStorage)},
	}
	for _, port := range ports {
		if err := s.appContext.Provide(port.name, port.value); err != nil {
			return err
		}
	}
	return nil
}

func numericConfig(value interface{}) int64 {
	switch v := value.(type) {
	case int:
		return int64(v)
	case int64:
		return v
	case float64:
		return int64(v)
	case float32:
		return int64(v)
	default:
		return 0
	}
}

type identityAssembly struct {
	Users       *identitysvc.UserService
	Permissions *identitysvc.PermissionService
	UserHandler *identityhandler.UserHandler
	RoleHandler *identityhandler.RoleHandler
}

func assembleIdentity(users identityrepo.UserRepository, roles identityrepo.RoleRepository, credentials identitysvc.PgUserRepo, jwt *auth.JWTManager, bus eventbus.EventBus, passwordHash bool) identityAssembly {
	permissions := identitysvc.NewPermissionService(roles, users)
	userService := identitysvc.NewUserService(users, jwt, credentials, bus)
	userService.SetPasswordHashEnabled(passwordHash)
	userService.SetRoleRepository(roles)
	return identityAssembly{Users: userService, Permissions: permissions, UserHandler: identityhandler.NewUserHandler(userService), RoleHandler: identityhandler.NewRoleHandler(permissions)}
}

type communityAssembly struct {
	Threads         *communitysvc.ThreadService
	Categories      *communitysvc.CategoryService
	Posts           *communitysvc.PostService
	ThreadHandler   *communityhandler.ThreadHandler
	CategoryHandler *communityhandler.CategoryHandler
	PostHandler     *communityhandler.PostHandler
}

func assembleCommunity(threads communityrepo.ThreadRepository, categories communityrepo.CategoryRepository, posts communityrepo.PostRepository, bus eventbus.EventBus, appCache cache.Cache) communityAssembly {
	threadService := communitysvc.NewThreadService(threads, bus)
	threadService.SetCategoryRepository(categories)
	threadService.SetCache(appCache)
	categoryService := communitysvc.NewCategoryService(categories, bus)
	postService := communitysvc.NewPostService(posts, bus)
	postService.SetThreadRepository(threads)
	postService.SetCache(appCache)
	return communityAssembly{Threads: threadService, Categories: categoryService, Posts: postService, ThreadHandler: communityhandler.NewThreadHandler(threadService), CategoryHandler: communityhandler.NewCategoryHandler(categoryService), PostHandler: communityhandler.NewPostHandler(postService)}
}
