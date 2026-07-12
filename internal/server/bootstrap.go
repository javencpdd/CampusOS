package server

import (
	"context"
	"log"
	"strings"
	"time"

	platformmodule "github.com/campusos/CampusOS/internal/platform/module"
	"github.com/campusos/CampusOS/pkg/cache"
	"github.com/campusos/CampusOS/pkg/database"
	"github.com/campusos/CampusOS/pkg/eventbus"
	"github.com/campusos/CampusOS/pkg/observability"
	"github.com/jackc/pgx/v5/pgxpool"
)

type infrastructureBootstrap struct {
	modules     *platformmodule.Registry
	bus         eventbus.EventBus
	memoryBus   *eventbus.MemoryEventBus
	cache       cache.Cache
	metrics     *observability.Collector
	database    *pgxpool.Pool
	databaseErr error
}

func (s *Server) startInfrastructure() (*infrastructureBootstrap, error) {
	appContext := platformmodule.NewAppContext()
	registry := platformmodule.NewRegistry(appContext)
	s.appContext = appContext
	events := newEventBusModule(s.cfg)
	plugins := newPluginPlatformModule(s, events)
	modules := []platformmodule.Module{events,
		coreBoundaryModule{id: "core.identity"},
		coreBoundaryModule{id: "core.community", dependencies: []string{"core.identity"}},
		coreBoundaryModule{id: "core.moderation", dependencies: []string{"core.identity", "core.community"}},
		coreBoundaryModule{id: "core.user-storage", dependencies: []string{"core.identity"}},
		plugins,
	}
	for _, module := range modules {
		if err := registry.Add(module, platformmodule.KindCore, true); err != nil {
			return nil, err
		}
	}
	if err := registry.StartAll(context.Background()); err != nil {
		return nil, err
	}
	s.modules = registry
	s.bus = events.EventBus()
	addr := s.cfg.Redis.Addr
	host := addr
	port := "6379"
	if idx := strings.LastIndex(addr, ":"); idx > 0 {
		host = addr[:idx]
		port = addr[idx+1:]
	}
	pool, databaseErr := database.New(s.cfg.Database.DSN)
	if databaseErr != nil {
		log.Printf("⚠️  PostgreSQL 连接失败，业务装配将使用内存 Adapter: %v", databaseErr)
	} else {
		log.Printf("✅ PostgreSQL 基础设施连接成功")
	}
	return &infrastructureBootstrap{modules: registry, bus: events.EventBus(), memoryBus: events.MemoryBus(), cache: cache.NewCache(cache.CacheConfig{Enabled: s.cfg.Redis.Enabled && addr != "", Host: host, Port: port, Password: s.cfg.Redis.Password, DB: s.cfg.Redis.DB}), metrics: observability.NewCollector(), database: pool, databaseErr: databaseErr}, nil
}
func (b *infrastructureBootstrap) Stop() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := b.modules.StopAll(ctx); err != nil {
		log.Printf("⚠️  模块停止失败: %v", err)
	}
	if b.database != nil {
		b.database.Close()
	}
}
