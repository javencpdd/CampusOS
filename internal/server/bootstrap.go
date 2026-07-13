package server

import (
	"context"
	"log"
	"strings"
	"time"

	communitycore "github.com/campusos/CampusOS/internal/community"
	identitycore "github.com/campusos/CampusOS/internal/core/identity"
	corestorage "github.com/campusos/CampusOS/internal/core/storage"
	"github.com/campusos/CampusOS/internal/moderation"
	platformfeature "github.com/campusos/CampusOS/internal/platform/feature"
	platformmodule "github.com/campusos/CampusOS/internal/platform/module"
	platformruntime "github.com/campusos/CampusOS/internal/platform/runtime"
	"github.com/campusos/CampusOS/pkg/cache"
	"github.com/campusos/CampusOS/pkg/database"
	"github.com/campusos/CampusOS/pkg/eventbus"
	"github.com/campusos/CampusOS/pkg/observability"
	"github.com/jackc/pgx/v5/pgxpool"
)

type infrastructureBootstrap struct {
	runtime     *platformruntime.Runtime
	modules     *platformmodule.Registry
	bus         eventbus.EventBus
	memoryBus   *eventbus.MemoryEventBus
	cache       cache.Cache
	metrics     *observability.Collector
	database    *pgxpool.Pool
	databaseErr error
}

func (s *Server) startInfrastructure() (*infrastructureBootstrap, error) {
	pool, databaseErr := database.New(s.cfg.Database.DSN)
	profileName := platformruntime.ProfileMemory
	if databaseErr != nil {
		log.Printf("⚠️  PostgreSQL 连接失败，业务装配将使用内存 Adapter: %v", databaseErr)
	} else {
		profileName = platformruntime.ProfilePostgreSQL
		log.Printf("✅ PostgreSQL 基础设施连接成功")
	}
	addr := s.cfg.Redis.Addr
	host := addr
	port := "6379"
	if idx := strings.LastIndex(addr, ":"); idx > 0 {
		host = addr[:idx]
		port = addr[idx+1:]
	}
	appCache := cache.NewCache(cache.CacheConfig{Enabled: s.cfg.Redis.Enabled && addr != "", Host: host, Port: port, Password: s.cfg.Redis.Password, DB: s.cfg.Redis.DB})
	featureStore := platformfeature.Store(platformfeature.NewMemoryStore())
	if pool != nil {
		featureStore = platformfeature.NewPostgreSQLStore(pool)
	}
	events := newEventBusModule(s.cfg)
	plugins := newPluginPlatformModule(s, events, featureStore)
	identityModule := identitycore.NewModule(identitycore.Config{JWT: s.newJWTManager(), PasswordHashEnabled: s.cfg.Auth.PasswordHashEnabled})
	communityModule := communitycore.NewModule()
	storageModule := corestorage.NewModule(corestorage.ModuleConfig{Root: corestorage.DefaultRoot, QuotaBytes: 10 * 1024 * 1024})
	moderationSettings := moderation.NewLegacySettings(func() map[string]interface{} { return pluginConfig(s.manager, moderation.PluginName) })
	moderationModule := moderation.NewModule(moderation.ModuleConfig{ConfigProvider: moderationSettings.Current})
	s.identity = identityModule
	s.community = communityModule
	s.moderation = moderationModule
	s.storage = storageModule
	entries := []platformruntime.Registration{
		{Module: events, Kind: platformmodule.KindCore, Enabled: true},
		{Module: identityModule, Kind: platformmodule.KindCore, Enabled: true},
		{Module: communityModule, Kind: platformmodule.KindCore, Enabled: true},
		{Module: moderationModule, Kind: platformmodule.KindCore, Enabled: true},
		{Module: storageModule, Kind: platformmodule.KindCore, Enabled: true},
		{Module: plugins, Kind: platformmodule.KindCore, Enabled: true},
	}
	appRuntime, err := platformruntime.New(platformruntime.Config{
		Profile: platformruntime.NewStaticProfile(profileName, func(app *platformmodule.AppContext) error {
			if err := app.Provide("platform.infrastructure-profile", string(profileName)); err != nil {
				return err
			}
			if err := app.Provide("platform.cache", appCache); err != nil {
				return err
			}
			if pool != nil {
				if err := identitycore.BindPostgreSQLAdapters(app, pool); err != nil {
					return err
				}
				if err := communitycore.BindPostgreSQLAdapters(app, pool); err != nil {
					return err
				}
				return moderation.BindPostgreSQLAdapters(app, pool)
			}
			if err := identitycore.BindMemoryAdapters(app); err != nil {
				return err
			}
			if err := communitycore.BindMemoryAdapters(app); err != nil {
				return err
			}
			return moderation.BindMemoryAdapters(app)
		}, nil),
		Modules: entries,
	})
	if err != nil {
		if pool != nil {
			pool.Close()
		}
		return nil, err
	}
	if err := appRuntime.Start(context.Background()); err != nil {
		if pool != nil {
			pool.Close()
		}
		return nil, err
	}
	s.appContext = appRuntime.AppContext()
	s.modules = appRuntime.Registry()
	s.bus = events.EventBus()
	return &infrastructureBootstrap{runtime: appRuntime, modules: appRuntime.Registry(), bus: events.EventBus(), memoryBus: events.MemoryBus(), cache: appCache, metrics: observability.NewCollector(), database: pool, databaseErr: databaseErr}, nil
}
func (b *infrastructureBootstrap) Stop() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := b.runtime.Stop(ctx); err != nil {
		log.Printf("⚠️  模块停止失败: %v", err)
	}
	if b.database != nil {
		b.database.Close()
	}
}
