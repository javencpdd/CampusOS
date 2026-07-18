package server

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/campusos/CampusOS/internal/plugin"
	"github.com/campusos/CampusOS/internal/transport/httpapi"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/campusos/CampusOS/pkg/eventbus"
)

func (s *Server) runApplication(infra *infrastructureBootstrap) error {
	if s.reliability == nil || s.reliability.Service() == nil || s.reliability.Handler() == nil || s.emailDelivery == nil || s.emailDelivery.Service() == nil || s.emailDelivery.Handler() == nil || s.ai == nil || s.ai.Service() == nil || s.webhook == nil || s.webhook.Service() == nil || s.mcp == nil || s.mcp.Service() == nil || s.message == nil || s.message.Service() == nil || s.platformLog == nil || s.platformLog.Service() == nil || s.integration == nil || s.integration.Handler() == nil {
		return fmt.Errorf("integration modules are unavailable")
	}

	if infra.database == nil {
		log.Printf("⚠️  PostgreSQL 基础设施不可用，回退到内存模式: %v", infra.databaseErr)
	} else if err := SeedAdmin(infra.database, adminSeedOptions{
		Environment:                  s.cfg.Deployment.Environment,
		PasswordHashEnabled:          s.cfg.Auth.PasswordHashEnabled,
		BootstrapAdminSecret:         s.cfg.Auth.BootstrapAdminSecret,
		AllowDevelopmentDefaultAdmin: s.cfg.Auth.AllowDevelopmentDefaultAdmin,
	}); err != nil {
		return fmt.Errorf("initialize bootstrap administrator: %w", err)
	}

	if s.identity == nil || s.identity.Permissions() == nil {
		return fmt.Errorf("identity core module is unavailable")
	}
	if s.community == nil || s.community.ThreadService() == nil {
		return fmt.Errorf("community core module is unavailable")
	}
	if s.storage == nil || s.storage.Handler() == nil {
		return fmt.Errorf("user storage core module is unavailable")
	}
	if s.moderation == nil || s.moderation.Handler() == nil {
		return fmt.Errorf("moderation core module is unavailable")
	}
	if s.space == nil || s.space.Handler() == nil || s.richtext == nil || s.richtext.Handler() == nil || s.schedule == nil || s.schedule.Handler() == nil || s.mutualAid == nil || s.mutualAid.Handler() == nil || s.secondhand == nil || s.secondhand.Handler() == nil || s.appearance == nil || s.appearance.HomepageHandler() == nil || s.appearance.WebThemeHandler() == nil {
		return fmt.Errorf("built-in feature modules are unavailable")
	}
	pluginHandler, err := s.pluginHTTPHandler()
	if err != nil {
		return err
	}

	router := httpapi.Build(httpapi.Dependencies{
		JWT:             s.identity.JWTManager(),
		SessionVerifier: s.identity.Sessions(),
		Permissions:     s.identity.Permissions(),
		AdminAccess:     s.identity.AdminAccess(),
		Features:        s.features,
		Feature:         s.featureHandler,
		ModuleCatalog:   s.moduleCatalog,
		PluginManager:   s.manager,
		Identity:        s.identity.Handlers(),
		Community:       s.community.Handlers(),
		UserStorage:     s.storage.Handler(),
		Space:           s.space.Handler(),
		Plugin:          pluginHandler,
		AI:              s.ai.Handler(),
		Integration:     s.integration.Handler(),
		Webhook:         s.webhook.Handler(),
		MCP:             s.mcp.Handler(),
		Message:         s.message.Handler(),
		Homepage:        s.appearance.HomepageHandler(),
		WebTheme:        s.appearance.WebThemeHandler(),
		RichText:        s.richtext.Handler(),
		Schedule:        s.schedule.Handler(),
		MutualAid:       s.mutualAid.Handler(),
		Secondhand:      s.secondhand.Handler(),
		PlatformLog:     s.platformLog.Handler(),
		Moderation:      s.moderation.Handler(),
		Reliability:     s.reliability.Handler(),
		EmailDelivery:   s.emailDelivery.Handler(),
		Metrics:         infra.metrics,
	})
	if err := s.identity.SyncRouteDescriptors(context.Background(), router.RouteDescriptors()); err != nil {
		// The catalog is additive. Keep a pre-migration deployment readable, but
		// make the missing evidence impossible to overlook in server logs.
		log.Printf("⚠️ 路由权限目录同步失败: %v", err)
	}

	log.Printf("🚀 CampusOS API 监听 %s", s.cfg.Server.Addr())
	log.Printf("📋 API 端点总数: %d", len(router.Routes()))
	log.Printf("🔌 已加载 %d 个插件", len(s.manager.ListPlugins()))
	return s.serveHTTP(router)
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

func (s *Server) pluginHTTPHandler() (*plugin.Handler, error) {
	if s.appContext == nil {
		return nil, fmt.Errorf("module app context is unavailable")
	}
	value, ok := s.appContext.Lookup("plugin.http-handler")
	if !ok {
		return nil, fmt.Errorf("plugin HTTP handler port is unavailable")
	}
	handler, ok := value.(*plugin.Handler)
	if !ok {
		return nil, fmt.Errorf("plugin HTTP handler port has incompatible type %T", value)
	}
	return handler, nil
}

func (s *Server) registerDefaultSubscriptions(bus eventbus.EventBus) {
	eventTypes := []string{
		"user.created", "thread.created", "thread.updated", "thread.deleted",
		"post.created", "category.created", "category.updated", "category.moved",
		"category.archived", "category.restored",
	}
	for _, eventType := range eventTypes {
		bus.Subscribe(eventType, func(ctx context.Context, event eventbus.Event) error {
			log.Printf("📢 Event: %s | Subject: %s | Source: %s", event.Type, event.Subject, event.Source)
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
