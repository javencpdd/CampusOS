// Package httpapi builds CampusOS's versioned HTTP surface from module ports.
// It deliberately owns route registration so the process Server only starts the
// HTTP listener after module bootstrap has completed.
package httpapi

import (
	"context"

	"github.com/campusos/CampusOS/internal/ai"
	communitycore "github.com/campusos/CampusOS/internal/community"
	identitycore "github.com/campusos/CampusOS/internal/core/identity"
	"github.com/campusos/CampusOS/internal/homepage"
	"github.com/campusos/CampusOS/internal/integration"
	"github.com/campusos/CampusOS/internal/mcp"
	"github.com/campusos/CampusOS/internal/message"
	"github.com/campusos/CampusOS/internal/moderation"
	platformfeature "github.com/campusos/CampusOS/internal/platform/feature"
	platformroute "github.com/campusos/CampusOS/internal/platform/route"
	"github.com/campusos/CampusOS/internal/platformlog"
	"github.com/campusos/CampusOS/internal/plugin"
	"github.com/campusos/CampusOS/internal/richtext"
	"github.com/campusos/CampusOS/internal/schedule"
	"github.com/campusos/CampusOS/internal/space"
	"github.com/campusos/CampusOS/internal/webhook"
	"github.com/campusos/CampusOS/internal/webtheme"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/campusos/CampusOS/pkg/middleware"
	"github.com/campusos/CampusOS/pkg/observability"
	"github.com/gin-gonic/gin"
)

// Dependencies are module-owned handlers and the small platform ports needed
// to expose them through HTTP. No concrete repository is accepted here.
type Dependencies struct {
	JWT           *auth.JWTManager
	Permissions   middleware.PermissionChecker
	Features      *platformfeature.Registry
	PluginManager *plugin.Manager
	Identity      identitycore.HTTPHandlers
	Community     communitycore.HTTPHandlers
	Space         *space.Handler
	Plugin        *plugin.Handler
	AI            *ai.Handler
	Integration   *integration.Handler
	Webhook       *webhook.Handler
	MCP           *mcp.Handler
	Message       *message.Handler
	Homepage      *homepage.Handler
	WebTheme      *webtheme.Handler
	RichText      *richtext.Handler
	Schedule      *schedule.Handler
	PlatformLog   *platformlog.Handler
	Moderation    *moderation.Handler
	Metrics       *observability.Collector
}

// Build preserves the existing /api/v1 route and authorization contract.
func Build(d Dependencies) *Router {
	r := gin.Default()
	routeRegistry := platformroute.NewRegistry()
	userHandler := d.Identity.User
	roleHandler := d.Identity.Role
	threadHandler := d.Community.Thread
	categoryHandler := d.Community.Category
	postHandler := d.Community.Post
	eventHandler := d.Community.Event
	runtimeHTTPHandler := plugin.NewRuntimeHTTPHandler(d.PluginManager, func(ctx context.Context, userID, resource, action string) (bool, error) {
		return d.Permissions.Check(ctx, userID, resource, action)
	}, d.Features)

	r.Use(middleware.Recovery())
	r.Use(middleware.CORS())
	r.Use(middleware.TraceID())
	r.Use(middleware.Logger())
	r.Use(observability.Middleware(d.Metrics))
	r.Use(platformfeature.PathGate(d.Features,
		platformfeature.PathRule{Prefix: "/api/v1/spaces", FeatureID: "personal-space"},
		platformfeature.PathRule{Prefix: "/api/v1/space", FeatureID: "personal-space"},
		platformfeature.PathRule{Prefix: "/api/v1/u/", FeatureID: "personal-space"},
		platformfeature.PathRule{Prefix: "/api/v1/richtext", FeatureID: "controlled-richtext-article"},
		platformfeature.PathRule{Prefix: "/api/v1/schedule", FeatureID: "personal-schedule"},
		platformfeature.PathRule{Prefix: "/api/v1/home", FeatureID: "appearance"},
		platformfeature.PathRule{Prefix: "/api/v1/web-themes", FeatureID: "appearance"},
	))

	v1 := r.Group("/api/v1")
	public := newOwnedGroup(v1.Group(""), routeRegistry, platformroute.AudiencePublic, d.Permissions)
	{
		public.GET("/health", userHandler.HealthCheck)
		public.GET("/home/config", d.Homepage.GetConfig)
		public.GET("/web-themes", d.WebTheme.Catalog)
		public.GET("/web-themes/:name", d.WebTheme.Package)
		public.GET("/web-themes/:name/assets/*path", d.WebTheme.Asset)
		public.GET("/ui/runtime-manifest", middleware.OptionalJWT(d.JWT), runtimeHTTPHandler.RuntimeManifest)
		public.GET("/ui/events", runtimeHTTPHandler.Events)
		public.GET("/richtext/status", d.RichText.Status)
		public.GET("/schedule/status", d.Schedule.Status)
		public.POST("/auth/register", userHandler.Register)
		public.POST("/auth/login", userHandler.Login)
		public.GET("/threads", threadHandler.ListThreads)
		public.GET("/threads/:id", threadHandler.GetThread)
		public.GET("/richtext/articles/:id", d.RichText.GetPublished)
		public.GET("/richtext/assets/:user_id/:filename", d.RichText.ServeAsset)
		public.GET("/users", userHandler.ListUsers)
		public.GET("/users/:id", userHandler.GetUser)
		public.GET("/space/:user_id/contents", d.Space.ListContentsByUserID)
		public.GET("/space/:user_id", d.Space.GetByUserID)
		public.GET("/spaces/files/:user_id/avatars/:filename", d.Space.ServeAvatarFile)
		public.GET("/u/:username/contents", d.Space.ListContentsByUsername)
		public.GET("/u/:username", d.Space.GetByUsername)
		public.GET("/categories", categoryHandler.List)
		public.GET("/categories/:id", categoryHandler.Get)
		public.GET("/threads/:id/posts", postHandler.ListPosts)
		public.GET("/events", eventHandler.ListEvents)
	}

	authenticated := newOwnedGroup(v1.Group(""), routeRegistry, platformroute.AudienceAuthenticated, d.Permissions)
	authenticated.Use(middleware.JWTAuth(d.JWT))
	{
		authenticated.GET("/auth/me", userHandler.GetMe)
		authenticated.PUT("/users/:id", userHandler.UpdateUser)
		authenticated.GET("/spaces/me", d.Space.GetMe)
		authenticated.PUT("/spaces/me", d.Space.UpdateMe)
		authenticated.GET("/spaces/me/storage", d.Space.GetStorageStatus)
		authenticated.POST("/spaces/me/avatar", d.Space.UploadAvatar)
		authenticated.POST("/spaces/me/styles/validate", d.Space.ValidateStylePackage)
		authenticated.POST("/spaces/me/styles/preview", d.Space.PreviewStylePackage)
		authenticated.POST("/spaces/me/styles/export", d.Space.ExportStylePackage)
		authenticated.POST("/spaces/me/styles/apply", d.Space.ApplyStylePackage)
		authenticated.POST("/spaces/me/styles/html/validate", d.Space.ValidateCustomHTML)
		authenticated.GET("/spaces/me/styles/html-example", d.Space.CustomHTMLExample)
		authenticated.POST("/spaces/me/styles/html/apply", d.Space.ApplyCustomHTML)
		authenticated.POST("/spaces/me/styles/packs/validate", d.Space.ValidateStylePackZip)
		authenticated.GET("/spaces/me/styles/packs/example", d.Space.StylePackExample)
		authenticated.GET("/spaces/me/styles/packs/example.zip", d.Space.StylePackExampleZip)
		authenticated.GET("/spaces/me/styles/packs/sources", d.Space.ListSourceStylePacks)
		authenticated.POST("/spaces/me/styles/packs/apply", d.Space.ApplyStylePackZip)
		authenticated.POST("/spaces/me/styles/packs/apply-source", d.Space.ApplySourceStylePack)
		authenticated.POST("/spaces/me/styles/rollback", d.Space.RollbackStyle)
		authenticated.POST("/spaces/me/styles/default", d.Space.RestoreDefaultStyle)
		authenticated.GET("/spaces/me/sync-status", d.Space.GetSyncStatus)
		authenticated.GET("/schedule/me", d.Schedule.GetMe)
		authenticated.GET("/schedule/me/terms", d.Schedule.ListTerms)
		authenticated.POST("/schedule/me/terms/activate", d.Schedule.ActivateTerm)
		authenticated.PUT("/schedule/me", d.Schedule.SaveMe)
		authenticated.POST("/schedule/me/import", d.Schedule.ImportMe)
		authenticated.POST("/richtext/articles", d.RichText.CreateDraft)
		authenticated.GET("/richtext/articles/:id/me", d.RichText.GetMine)
		authenticated.PUT("/richtext/articles/:id", d.RichText.UpdateDraft)
		authenticated.POST("/richtext/preview", d.RichText.Preview)
		authenticated.POST("/richtext/assets", d.RichText.UploadAsset)
		authenticated.POST("/richtext/articles/:id/publish", d.RichText.Publish)
		authenticated.POST("/richtext/articles/:id/offline", d.RichText.Offline)
		authenticated.DELETE("/richtext/articles/:id", d.RichText.Delete)
		authenticated.POST("/threads", threadHandler.CreateThread)
		authenticated.GET("/threads/:id/me", threadHandler.GetThreadForCurrentUser)
		authenticated.PUT("/threads/:id", threadHandler.UpdateThread)
		authenticated.DELETE("/threads/:id", threadHandler.DeleteThread)
		authenticated.GET("/threads/:id/posts/me", postHandler.ListPostsForCurrentUser)
		authenticated.POST("/threads/:id/posts", postHandler.CreatePost)
		authenticated.PUT("/threads/:id/posts/:post_id", postHandler.UpdatePost)
		authenticated.DELETE("/threads/:id/posts/:post_id", postHandler.DeletePost)
		authenticated.GET("/moderation/status", d.Moderation.Status)
		authenticated.GET("/moderation/me", d.Moderation.MyAccess)
		authenticated.POST("/moderation/threads/:id/pin", d.Moderation.Pin)
		authenticated.POST("/moderation/threads/:id/unpin", d.Moderation.Unpin)
		authenticated.POST("/moderation/threads/:id/lock", d.Moderation.Lock)
		authenticated.POST("/moderation/threads/:id/unlock", d.Moderation.Unlock)
		authenticated.DELETE("/moderation/threads/:id/posts/:post_id", d.Moderation.DeletePost)
		authenticated.GET("/plugin-market", d.Plugin.MarketCatalog)
		authenticated.GET("/plugin-market/me", d.Plugin.MarketMyGrants)
		authenticated.GET("/plugin-market/me/usage", d.Plugin.MarketMyUsage)
		authenticated.POST("/plugin-market/:name/enable", d.Plugin.EnableMarketPlugin)
		authenticated.POST("/plugin-market/:name/revoke", d.Plugin.RevokeMarketPlugin)
		authenticated.POST("/plugin-market/:name/request", d.Plugin.RequestMarketInstall)
		authenticated.GET("/plugin-market/search", d.Plugin.SearchMarketRecords)
		authenticated.POST("/plugin-market/:name/records/:collection", d.Plugin.CreateMarketRecord)
		authenticated.GET("/plugin-market/:name/records/:collection", d.Plugin.ListMarketRecords)
		authenticated.GET("/plugin-market/:name/records/:collection/:key", d.Plugin.GetMarketRecord)
		authenticated.PUT("/plugin-market/:name/records/:collection/:key", d.Plugin.UpdateMarketRecord)
		authenticated.DELETE("/plugin-market/:name/records/:collection/:key", d.Plugin.DeleteMarketRecord)
		authenticated.POST("/plugin-market/:name/files", d.Plugin.UploadMarketFile)
		authenticated.GET("/plugin-market/:name/files", d.Plugin.ListMarketFiles)
		authenticated.GET("/plugin-market/:name/files/:file_id/download", d.Plugin.DownloadMarketFile)
		authenticated.DELETE("/plugin-market/:name/files/:file_id", d.Plugin.DeleteMarketFile)
		authenticated.GET("/plugin-market/:name/export", d.Plugin.ExportMarketData)
		authenticated.DELETE("/plugin-market/:name/data", d.Plugin.DeleteMarketData)
		authenticated.GET("/extensions/:plugin/*path", runtimeHTTPHandler.Extension)
		authenticated.POST("/extensions/:plugin/*path", runtimeHTTPHandler.Extension)
		authenticated.PUT("/extensions/:plugin/*path", runtimeHTTPHandler.Extension)
		authenticated.PATCH("/extensions/:plugin/*path", runtimeHTTPHandler.Extension)
		authenticated.DELETE("/extensions/:plugin/*path", runtimeHTTPHandler.Extension)
	}

	admin := newOwnedGroup(v1.Group(""), routeRegistry, platformroute.AudienceAdmin, d.Permissions)
	admin.Use(middleware.JWTAuth(d.JWT))
	{
		admin.Permission("user", "suspend").POST("/users/:id/suspend", userHandler.SuspendUser)
		admin.Permission("user", "suspend").POST("/users/:id/activate", userHandler.ActivateUser)
		admin.Permission("thread", "read").GET("/admin/threads", threadHandler.AdminListThreads)
		admin.Permission("thread", "pin").POST("/threads/:id/pin", threadHandler.PinThread)
		admin.Permission("thread", "pin").POST("/threads/:id/unpin", threadHandler.UnpinThread)
		admin.Permission("thread", "lock").POST("/threads/:id/lock", threadHandler.LockThread)
		admin.Permission("thread", "lock").POST("/threads/:id/unlock", threadHandler.UnlockThread)
		admin.Permission("thread", "delete").DELETE("/admin/threads/:id", threadHandler.AdminDeleteThread)
		admin.Permission("richtext", "moderate").POST("/richtext/articles/:id/admin/offline", d.RichText.AdminOffline)
		admin.Permission("richtext", "moderate").POST("/richtext/articles/:id/admin/restore", d.RichText.AdminRestore)
		admin.Permission("richtext", "moderate").DELETE("/richtext/articles/:id/admin", d.RichText.AdminDelete)
		admin.Permission("category", "write").POST("/categories", categoryHandler.Create)
		admin.Permission("category", "write").PUT("/categories/:id", categoryHandler.Update)
		admin.Permission("category", "delete").DELETE("/categories/:id", categoryHandler.Delete)
		admin.Permission("plugin", "read").GET("/plugins", d.Plugin.ListPlugins)
		admin.Permission("plugin", "read").GET("/plugins/:name", d.Plugin.GetPlugin)
		admin.Permission("plugin", "read").GET("/plugins/:name/logs", d.Plugin.ListPluginLogs)
		admin.Permission("plugin", "read").GET("/plugins/:name/export", d.Plugin.ExportPlugin)
		admin.Permission("plugin", "configure").PUT("/plugins/:name/config", d.Plugin.UpdatePluginConfig)
		admin.Permission("plugin", "lifecycle").POST("/plugins/:name/enable", d.Plugin.EnablePlugin)
		admin.Permission("plugin", "lifecycle").POST("/plugins/:name/disable", d.Plugin.DisablePlugin)
		admin.Permission("plugin", "lifecycle").POST("/plugins/:name/reload", d.Plugin.ReloadUserPlugin)
		admin.Permission("plugin", "uninstall").DELETE("/plugins/:name", d.Plugin.UninstallPlugin)
		admin.Permission("plugin", "install").POST("/plugin-packages/import", d.Plugin.ImportPluginPackage)
		admin.Permission("plugin", "install").POST("/plugin-packages/precheck", d.Plugin.PrecheckPluginPackage)
		admin.Permission("plugin", "read").GET("/plugins/:name/snapshots", d.Plugin.ListVersionSnapshots)
		admin.Permission("plugin", "install").POST("/plugins/:name/rollback", d.Plugin.RollbackVersionSnapshot)
		admin.Permission("plugin", "read").GET("/plugin-market/admin/overview", d.Plugin.AdminMarketOverview)
		admin.Permission("plugin", "configure").PUT("/plugin-market/admin/catalog/:name", d.Plugin.AdminSetMarketVisibility)
		admin.Permission("plugin", "read").GET("/plugin-market/admin/requests", d.Plugin.AdminMarketRequests)
		admin.Permission("plugin", "configure").POST("/plugin-market/admin/requests/:id/review", d.Plugin.AdminReviewMarketRequest)
		admin.Permission("plugin", "read").GET("/plugin-market/admin/releases/:name", d.Plugin.AdminMarketReleases)
		admin.Permission("plugin", "read").GET("/plugin-market/admin/audits", d.Plugin.AdminMarketAudits)
		admin.Permission("plugin", "install").POST("/plugin-market/admin/releases/:name", d.Plugin.AdminSaveMarketRelease)
		admin.Permission("ai", "read").GET("/ai/status", d.AI.GetStatus)
		admin.Permission("ai", "read").GET("/ai/logs", d.AI.ListLogs)
		admin.Permission("integration", "read").GET("/integrations/overview", d.Integration.Overview)
		admin.Permission("metrics", "read").GET("/metrics/summary", d.Integration.Metrics)
		admin.Permission("space", "manage").GET("/spaces/admin/summary", d.Space.AdminSummary)
		admin.Permission("space", "manage").POST("/spaces/:user_id/disable", d.Space.DisableSpace)
		admin.Permission("space", "manage").POST("/spaces/:user_id/enable", d.Space.EnableSpace)
		admin.Permission("webhook", "read").GET("/webhooks", d.Webhook.ListEndpoints)
		admin.Permission("webhook", "write").POST("/webhooks", d.Webhook.CreateEndpoint)
		admin.Permission("webhook", "read").GET("/webhooks/summary", d.Webhook.Summary)
		admin.Permission("webhook", "execute").POST("/webhooks/:id/test", d.Webhook.TestEndpoint)
		admin.Permission("webhook", "write").POST("/webhooks/:id/enable", d.Webhook.EnableEndpoint)
		admin.Permission("webhook", "write").POST("/webhooks/:id/disable", d.Webhook.DisableEndpoint)
		admin.Permission("webhook", "read").GET("/webhooks/:id/deliveries", d.Webhook.ListDeliveries)
		admin.Permission("mcp", "read").GET("/mcp/tools", d.MCP.ListTools)
		admin.Permission("mcp", "call").POST("/mcp/tools/:name/call", d.MCP.CallTool)
		admin.Permission("mcp", "read").GET("/mcp/audit", d.MCP.ListAudit)
		admin.Permission("mcp", "read").GET("/mcp/settings", d.MCP.GetSettings)
		admin.Permission("mcp", "configure").PUT("/mcp/settings", d.MCP.UpdateSettings)
		admin.Permission("message", "read").GET("/messages/adapters", d.Message.ListAdapters)
		admin.Permission("message", "write").POST("/messages/local/inbound", d.Message.ReceiveLocal)
		admin.Permission("message", "read").GET("/messages/logs", d.Message.ListMessages)
		admin.Permission("message", "write").POST("/messages/bindings", d.Message.CreateBinding)
		admin.Permission("message", "read").GET("/messages/summary", d.Message.Summary)
		admin.Permission("platform_log", "read").GET("/platform/logs/sources", d.PlatformLog.Sources)
		admin.Permission("platform_log", "read").GET("/platform/logs/stream", d.PlatformLog.Stream)
		admin.Permission("homepage", "configure").POST("/home/style-packs/validate", d.Homepage.ValidateStylePack)
		admin.Permission("homepage", "configure").GET("/home/style-packs/example", d.Homepage.StylePackExample)
		admin.Permission("homepage", "configure").GET("/home/style-packs/example.zip", d.Homepage.StylePackExampleZip)
		admin.Permission("homepage", "configure").GET("/home/style-packs/sources", d.Homepage.ListSourceStylePacks)
		admin.Permission("homepage", "configure").POST("/home/style-packs/apply", d.Homepage.ApplyStylePack)
		admin.Permission("homepage", "configure").POST("/home/style-packs/apply-source", d.Homepage.ApplySourceStylePack)
		admin.Permission("homepage", "configure").POST("/home/style-packs/rollback", d.Homepage.RollbackStylePack)
		admin.Permission("role", "read").GET("/roles", roleHandler.ListRoles)
		admin.Permission("role", "read").GET("/users/:id/roles", roleHandler.GetUserRoles)
		admin.Permission("role", "assign").POST("/users/:id/roles", roleHandler.AssignRole)
		admin.Permission("role", "revoke").DELETE("/users/:id/roles", roleHandler.RevokeRole)
		admin.Permission("role", "read").GET("/moderation/admin/moderators", d.Moderation.ListModerators)
		admin.Permission("role", "read").GET("/moderation/admin/moderators/:id", d.Moderation.GetModerator)
		admin.Permission("role", "assign").PUT("/moderation/admin/moderators/:id", d.Moderation.SetModerator)
	}

	return newRouter(r, routeRegistry)
}
