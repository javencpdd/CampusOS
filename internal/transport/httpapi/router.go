// Package httpapi builds CampusOS's versioned HTTP surface from module ports.
// It deliberately owns route registration so the process Server only starts the
// HTTP listener after module bootstrap has completed.
package httpapi

import (
	"context"
	"net/http"

	academicterm "github.com/campusos/CampusOS/internal/modules/core/academicterm"
	communitycore "github.com/campusos/CampusOS/internal/modules/core/community"
	"github.com/campusos/CampusOS/internal/modules/core/emaildelivery"
	identitycore "github.com/campusos/CampusOS/internal/modules/core/identity"
	"github.com/campusos/CampusOS/internal/modules/core/moderation"
	corestorage "github.com/campusos/CampusOS/internal/modules/core/userstorage"
	"github.com/campusos/CampusOS/internal/modules/features/ai"
	"github.com/campusos/CampusOS/internal/modules/features/appearance/homepage"
	"github.com/campusos/CampusOS/internal/modules/features/appearance/webtheme"
	"github.com/campusos/CampusOS/internal/modules/features/integration"
	"github.com/campusos/CampusOS/internal/modules/features/mcp"
	"github.com/campusos/CampusOS/internal/modules/features/message"
	"github.com/campusos/CampusOS/internal/modules/features/mutualaid"
	personaldocuments "github.com/campusos/CampusOS/internal/modules/features/personaldocuments"
	"github.com/campusos/CampusOS/internal/modules/features/personalspace"
	"github.com/campusos/CampusOS/internal/modules/features/platformlog"
	"github.com/campusos/CampusOS/internal/modules/features/richtext"
	"github.com/campusos/CampusOS/internal/modules/features/schedule"
	"github.com/campusos/CampusOS/internal/modules/features/secondhand"
	"github.com/campusos/CampusOS/internal/modules/features/webhook"
	platformfeature "github.com/campusos/CampusOS/internal/platform/feature"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	platformroute "github.com/campusos/CampusOS/internal/platform/route"
	"github.com/campusos/CampusOS/internal/plugin"
	modulecatalog "github.com/campusos/CampusOS/modules"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/campusos/CampusOS/pkg/middleware"
	"github.com/campusos/CampusOS/pkg/observability"
	"github.com/gin-gonic/gin"
)

// Dependencies are module-owned handlers and the small platform ports needed
// to expose them through HTTP. No concrete repository is accepted here.
type Dependencies struct {
	JWT               *auth.JWTManager
	SessionVerifier   middleware.AccessSessionVerifier
	Permissions       middleware.PermissionChecker
	AdminAccess       middleware.AdminAccessChecker
	AdminMFA          middleware.AdminMFAChecker
	Features          *platformfeature.Registry
	Feature           *platformfeature.Handler
	ModuleCatalog     *modulecatalog.Catalog
	PluginManager     *plugin.Manager
	Identity          identitycore.HTTPHandlers
	Community         communitycore.HTTPHandlers
	AcademicTerm      *academicterm.Handler
	UserStorage       *corestorage.Handler
	Space             *space.Handler
	Plugin            *plugin.Handler
	AI                *ai.Handler
	Integration       *integration.Handler
	Webhook           *webhook.Handler
	MCP               *mcp.Handler
	Message           *message.Handler
	Homepage          *homepage.Handler
	WebTheme          *webtheme.Handler
	RichText          *richtext.Handler
	Schedule          *schedule.Handler
	PersonalDocuments *personaldocuments.Handler
	MutualAid         *mutualaid.Handler
	Secondhand        *secondhand.Handler
	PlatformLog       *platformlog.Handler
	Moderation        *moderation.Handler
	Reliability       *reliability.Handler
	EmailDelivery     *emaildelivery.Handler
	Metrics           *observability.Collector
}

// Build preserves the existing /api/v1 route and authorization contract.
func Build(d Dependencies) *Router {
	r := gin.Default()
	routeRegistry := platformroute.NewRegistry()
	userHandler := d.Identity.User
	roleHandler := d.Identity.Role
	challengePolicyHandler := d.Identity.ChallengePolicy
	adminAdmissionHandler := d.Identity.AdminAdmission
	threadHandler := d.Community.Thread
	categoryHandler := d.Community.Category
	postHandler := d.Community.Post
	notificationHandler := d.Community.Notification
	eventHandler := d.Community.Event
	userStorageHandler := d.UserStorage
	runtimeHTTPHandler := plugin.NewRuntimeHTTPHandler(d.PluginManager, func(ctx context.Context, userID, resource, action string) (bool, error) {
		return d.Permissions.Check(ctx, userID, resource, action)
	}, d.Features)
	runtimeHTTPHandler.SetModuleCatalog(d.ModuleCatalog)

	r.Use(observability.Middleware(d.Metrics))
	r.Use(middleware.Recovery(d.Metrics))
	r.Use(middleware.CORS())
	r.Use(middleware.TraceID())
	r.Use(middleware.Logger())
	r.Use(platformfeature.PathGate(d.Features,
		platformfeature.PathRule{Prefix: "/api/v1/spaces", FeatureID: "personal-space"},
		platformfeature.PathRule{Prefix: "/api/v1/space", FeatureID: "personal-space"},
		platformfeature.PathRule{Prefix: "/api/v1/appearance/space-style-packs", FeatureID: "personal-space"},
		platformfeature.PathRule{Prefix: "/api/v1/u/", FeatureID: "personal-space"},
		platformfeature.PathRule{
			Prefix: "/api/v1/richtext", FeatureID: "controlled-richtext-article",
			AllowWhenDisabled: []platformfeature.AllowedPath{{Method: http.MethodGet, Path: "/api/v1/richtext/status"}},
		},
		platformfeature.PathRule{
			Prefix: "/api/v1/schedule", FeatureID: "personal-schedule",
			AllowWhenDisabled: []platformfeature.AllowedPath{{Method: http.MethodGet, Path: "/api/v1/schedule/status"}},
		},
		platformfeature.PathRule{Prefix: "/api/v1/documents", FeatureID: "personal-documents"},
		platformfeature.PathRule{
			Prefix: "/api/v1/mutual-aid", FeatureID: "mutual-aid",
			AllowWhenDisabled: []platformfeature.AllowedPath{{Method: http.MethodGet, Path: "/api/v1/mutual-aid/status"}},
		},
		platformfeature.PathRule{
			Prefix: "/api/v1/secondhand", FeatureID: "secondhand",
			AllowWhenDisabled: []platformfeature.AllowedPath{{Method: http.MethodGet, Path: "/api/v1/secondhand/status"}},
		},
		platformfeature.PathRule{Prefix: "/api/v1/home", FeatureID: "appearance"},
		platformfeature.PathRule{Prefix: "/api/v1/web-themes", FeatureID: "appearance"},
	))
	r.NoRoute(APINoRoute)

	v1 := r.Group("/api/v1")
	public := newOwnedGroup(v1.Group(""), routeRegistry, platformroute.AudiencePublic, d.Permissions)
	{
		public.GET("", APIIndex)
		public.GET("/health", userHandler.HealthCheck)
		public.GET("/home/config", d.Homepage.GetConfig)
		public.GET("/home/logo", d.Homepage.GetLogo)
		public.GET("/web-themes", d.WebTheme.Catalog)
		public.GET("/web-themes/:name", d.WebTheme.Package)
		public.GET("/web-themes/:name/assets/*path", d.WebTheme.Asset)
		public.GET("/ui/runtime-manifest", middleware.OptionalJWT(d.JWT, d.SessionVerifier), runtimeHTTPHandler.RuntimeManifest)
		public.GET("/ui/events", runtimeHTTPHandler.Events)
		public.GET("/richtext/status", d.RichText.Status)
		public.GET("/schedule/status", d.Schedule.Status)
		public.GET("/mutual-aid/status", d.MutualAid.Status)
		public.GET("/secondhand/status", d.Secondhand.Status)
		public.POST("/auth/registration/challenge", userHandler.RequestRegistrationChallenge)
		public.POST("/auth/registration/verify", userHandler.VerifyRegistrationChallenge)
		public.POST("/auth/register", userHandler.Register)
		public.POST("/auth/login", userHandler.Login)
		public.POST("/auth/admin/login", userHandler.AdminLogin)
		public.POST("/auth/mfa/login/complete", userHandler.CompleteMFALogin)
		public.POST("/auth/refresh", userHandler.Refresh)
		public.POST("/auth/password-reset/challenge", userHandler.RequestPasswordReset)
		public.POST("/auth/password-reset/verify", userHandler.VerifyPasswordReset)
		public.POST("/auth/password-reset/complete", userHandler.CompletePasswordReset)
		public.POST("/auth/recovery/complete", userHandler.CompleteAdminRecovery)
		public.GET("/threads", threadHandler.ListThreads)
		public.GET("/threads/:id", threadHandler.GetThread)
		public.GET("/content/assets/images/:user_id/:filename", userStorageHandler.ServeContentImage)
		public.GET("/mutual-aid/threads", d.MutualAid.ListPublic)
		public.GET("/mutual-aid/threads/:id", d.MutualAid.GetPublic)
		public.GET("/secondhand/threads", d.Secondhand.ListPublic)
		public.GET("/secondhand/threads/:id", d.Secondhand.GetPublic)
		public.GET("/richtext/articles/:id", d.RichText.GetPublished)
		public.GET("/richtext/assets/:user_id/:filename", d.RichText.ServeAsset)
		public.GET("/users", userHandler.ListUsers)
		public.GET("/users/:id", userHandler.GetUser)
		public.GET("/space/:user_id/contents", d.Space.ListContentsByUserID)
		public.GET("/space/:user_id", d.Space.GetByUserID)
		public.GET("/spaces/files/:user_id/avatars/:filename", d.Space.ServeAvatarFile)
		public.GET("/u/:username/contents", d.Space.ListContentsByUsername)
		public.GET("/u/:username", d.Space.GetByUsername)
		public.GET("/appearance/space-style-packs/:name/assets/*asset_path", d.Space.ServeSourceStylePackAsset)
		public.GET("/categories", categoryHandler.List)
		public.GET("/categories/tree", categoryHandler.ListTree)
		public.GET("/categories/:id", categoryHandler.Get)
		public.GET("/categories/:id/thread-types", categoryHandler.ListThreadTypePolicies)
		public.GET("/threads/:id/posts", postHandler.ListPosts)
		public.GET("/events", eventHandler.ListEvents)
	}

	authenticated := newOwnedGroup(v1.Group(""), routeRegistry, platformroute.AudienceAuthenticated, d.Permissions)
	authenticated.Use(middleware.JWTAuth(d.JWT, d.SessionVerifier))
	{
		authenticated.POST("/auth/logout", userHandler.Logout)
		authenticated.POST("/auth/logout-all", userHandler.LogoutAll)
		authenticated.GET("/auth/sessions", userHandler.ListSessions)
		authenticated.DELETE("/auth/sessions/:id", userHandler.RevokeSession)
		authenticated.GET("/auth/mfa", userHandler.GetMFAStatus)
		authenticated.POST("/auth/mfa/totp/enrollment", userHandler.StartMFAEnrollment)
		authenticated.POST("/auth/mfa/totp/confirm", userHandler.ConfirmMFAEnrollment)
		authenticated.DELETE("/auth/mfa/totp", userHandler.DisableMFA)
		authenticated.POST("/auth/mfa/recovery-codes/rotate", userHandler.RotateMFARecoveryCodes)
		authenticated.POST("/auth/mfa/step-up", userHandler.StepUpMFA)
		authenticated.POST("/auth/email-binding/challenge", userHandler.RequestEmailBinding)
		authenticated.POST("/auth/email-binding/verify", userHandler.VerifyEmailBinding)
		authenticated.POST("/auth/email-binding/complete", userHandler.CompleteEmailBinding)
		authenticated.GET("/auth/me", userHandler.GetMe)
		authenticated.PUT("/users/:id", userHandler.UpdateUser)
		authenticated.GET("/spaces/me", d.Space.GetMe)
		authenticated.GET("/spaces/me/contents", d.Space.ListOwnContents)
		authenticated.PUT("/spaces/me", d.Space.UpdateMe)
		authenticated.GET("/spaces/me/storage", d.Space.GetStorageStatus)
		authenticated.POST("/spaces/me/avatar", d.Space.UploadAvatar)
		authenticated.GET("/spaces/me/avatars", d.Space.ListAvatars)
		authenticated.PUT("/spaces/me/avatar", d.Space.SelectAvatar)
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
		authenticated.GET("/schedule/terms", d.AcademicTerm.ListOpen)
		authenticated.GET("/schedule/me/terms", d.Schedule.ListTerms)
		authenticated.POST("/schedule/me/terms/activate", d.Schedule.ActivateTerm)
		authenticated.PUT("/schedule/me", d.Schedule.SaveMe)
		authenticated.POST("/schedule/me/import", d.Schedule.ImportMe)
		authenticated.GET("/documents", d.PersonalDocuments.List)
		authenticated.POST("/documents", d.PersonalDocuments.Create)
		authenticated.POST("/documents/upload", d.PersonalDocuments.Upload)
		authenticated.GET("/documents/:id", d.PersonalDocuments.Get)
		authenticated.GET("/documents/:id/content", d.PersonalDocuments.Content)
		authenticated.GET("/documents/:id/preview", d.PersonalDocuments.Preview)
		authenticated.PUT("/documents/:id", d.PersonalDocuments.Save)
		authenticated.GET("/documents/:id/versions", d.PersonalDocuments.Versions)
		authenticated.POST("/documents/:id/versions/:version_id/restore", d.PersonalDocuments.RestoreVersion)
		authenticated.POST("/documents/:id/trash", d.PersonalDocuments.Trash)
		authenticated.POST("/documents/:id/restore", d.PersonalDocuments.Restore)
		authenticated.GET("/documents/:id/download", d.PersonalDocuments.Download)
		authenticated.POST("/richtext/articles", d.RichText.CreateDraft)
		authenticated.POST("/content/preview", threadHandler.PreviewContent)
		authenticated.GET("/content/assets/images/me", userStorageHandler.ListMyContentImages)
		authenticated.POST("/content/assets/images", userStorageHandler.UploadContentImage)
		authenticated.GET("/richtext/articles/:id/me", d.RichText.GetMine)
		authenticated.PUT("/richtext/articles/:id", d.RichText.UpdateDraft)
		authenticated.POST("/richtext/preview", d.RichText.Preview)
		authenticated.GET("/richtext/assets/me", d.RichText.ListMyAssets)
		authenticated.POST("/richtext/assets", d.RichText.UploadAsset)
		authenticated.POST("/richtext/articles/:id/publish", d.RichText.Publish)
		authenticated.POST("/richtext/articles/:id/offline", d.RichText.Offline)
		authenticated.DELETE("/richtext/articles/:id", d.RichText.Delete)
		authenticated.POST("/mutual-aid/threads", d.MutualAid.Create)
		authenticated.GET("/mutual-aid/threads/:id/me", d.MutualAid.GetMine)
		authenticated.PUT("/mutual-aid/threads/:id", d.MutualAid.Update)
		authenticated.POST("/mutual-aid/threads/:id/status", d.MutualAid.UpdateStatus)
		authenticated.POST("/secondhand/threads", d.Secondhand.Create)
		authenticated.GET("/secondhand/threads/:id/me", d.Secondhand.GetMine)
		authenticated.PUT("/secondhand/threads/:id", d.Secondhand.Update)
		authenticated.POST("/secondhand/threads/:id/status", d.Secondhand.UpdateStatus)
		authenticated.POST("/threads", threadHandler.CreateThread)
		authenticated.GET("/threads/:id/me", threadHandler.GetThreadForCurrentUser)
		authenticated.PUT("/threads/:id", threadHandler.UpdateThread)
		authenticated.DELETE("/threads/:id", threadHandler.DeleteThread)
		authenticated.POST("/threads/:id/submit-review", threadHandler.SubmitForReview)
		authenticated.POST("/threads/:id/trash/restore", threadHandler.RestoreOwnTrash)
		authenticated.GET("/threads/:id/posts/me", postHandler.ListPostsForCurrentUser)
		authenticated.POST("/threads/:id/posts", postHandler.CreatePost)
		authenticated.PUT("/threads/:id/posts/:post_id", postHandler.UpdatePost)
		authenticated.DELETE("/threads/:id/posts/:post_id", postHandler.DeletePost)
		authenticated.GET("/notifications", notificationHandler.List)
		authenticated.POST("/notifications/:id/read", notificationHandler.MarkRead)
		authenticated.POST("/notifications/read-all", notificationHandler.MarkAllRead)
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

	admin := newOwnedGroup(v1.Group(""), routeRegistry, platformroute.AudienceAdmin, d.Permissions, d.AdminAccess)
	admin.SetAdminMFA(d.AdminMFA)
	admin.Use(middleware.JWTAuth(d.JWT, d.SessionVerifier))
	{
		admin.Permission("user", "read").Operation("http.identity.user.admin_list").GET("/admin/users", userHandler.ListAdminUsers)
		admin.Permission("user", "suspend").POST("/users/:id/suspend", userHandler.SuspendUser)
		admin.Permission("user", "suspend").POST("/users/:id/activate", userHandler.ActivateUser)
		admin.Permission("thread", "read").GET("/admin/threads", threadHandler.AdminListThreads)
		admin.Permission("thread", "read").GET("/admin/threads/trash", threadHandler.AdminListTrash)
		admin.Permission("thread", "read").GET("/admin/threads/:id/moderation-actions", threadHandler.AdminModerationActions)
		admin.Permission("thread", "pin").POST("/threads/:id/pin", threadHandler.PinThread)
		admin.Permission("thread", "pin").POST("/threads/:id/unpin", threadHandler.UnpinThread)
		admin.Permission("thread", "lock").POST("/threads/:id/lock", threadHandler.LockThread)
		admin.Permission("thread", "lock").POST("/threads/:id/unlock", threadHandler.UnlockThread)
		admin.PermissionCode("community.thread.trash").Operation("http.community.thread.trash").DELETE("/admin/threads/:id", threadHandler.AdminDeleteThread)
		admin.PermissionCode("community.thread.take_down").Scoped("category").Operation("http.community.thread.take_down").POST("/admin/threads/:id/take-down", threadHandler.AdminTakeDown)
		admin.PermissionCode("community.thread.review").Scoped("category").Operation("http.community.thread.review_approve").POST("/admin/threads/:id/review/approve", threadHandler.AdminApprove)
		admin.PermissionCode("community.thread.review").Scoped("category").Operation("http.community.thread.review_reject").POST("/admin/threads/:id/review/reject", threadHandler.AdminReject)
		admin.PermissionCode("community.thread.direct_restore").Operation("http.community.thread.direct_restore").POST("/admin/threads/:id/direct-restore", threadHandler.AdminDirectRestore)
		admin.PermissionCode("community.thread.restore").Operation("http.community.thread.restore_trash").POST("/admin/threads/:id/trash/restore", threadHandler.AdminRestoreTrash)
		admin.PermissionCode("community.thread.purge").Operation("http.community.thread.purge").DELETE("/admin/threads/:id/purge", threadHandler.AdminPurge)
		admin.PermissionCode("community.thread.take_down").Operation("http.community.richtext.take_down").POST("/richtext/articles/:id/admin/offline", d.RichText.AdminOffline)
		admin.PermissionCode("community.thread.direct_restore").Operation("http.community.richtext.restore").POST("/richtext/articles/:id/admin/restore", d.RichText.AdminRestore)
		admin.PermissionCode("community.thread.trash").Operation("http.community.richtext.trash").DELETE("/richtext/articles/:id/admin", d.RichText.AdminDelete)
		admin.Permission("category", "read").Operation("http.community.category.list").GET("/admin/categories", categoryHandler.ListAdmin)
		admin.Permission("category", "read").Operation("http.community.category.tree").GET("/admin/categories/tree", categoryHandler.ListAdminTree)
		admin.Permission("category", "read").Operation("http.community.category.get").GET("/admin/categories/:id", categoryHandler.GetAdmin)
		admin.Permission("category", "read").Operation("http.community.category.thread_types").LegacyOperationAlias("http.community.category.thread-types").GET("/admin/categories/:id/thread-types", categoryHandler.ListThreadTypePolicies)
		admin.PermissionCode("community.category.create").Operation("http.community.category.create").POST("/categories", categoryHandler.Create)
		admin.PermissionCode("community.category.update").Operation("http.community.category.update").PUT("/categories/:id", categoryHandler.Update)
		admin.PermissionCode("community.category.configure_thread_types").Operation("http.community.category.configure_thread_types").PUT("/categories/:id/thread-types", categoryHandler.UpdateThreadTypePolicies)
		admin.PermissionCode("community.category.move").Operation("http.community.category.move").PUT("/categories/:id/parent", categoryHandler.Move)
		admin.PermissionCode("community.category.archive").Operation("http.community.category.archive_impact").LegacyOperationAlias("http.community.category.archive-impact").GET("/categories/:id/archive-impact", categoryHandler.ArchiveImpact)
		admin.PermissionCode("community.category.archive").Operation("http.community.category.archive").POST("/categories/:id/archive", categoryHandler.Archive)
		admin.PermissionCode("community.category.restore").Operation("http.community.category.restore").POST("/categories/:id/restore", categoryHandler.Restore)
		admin.PermissionCode("community.category.archive").Operation("http.community.category.archive_legacy").LegacyOperationAlias("http.community.category.archive-legacy").DELETE("/categories/:id", categoryHandler.Delete)
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
		admin.Permission("feature", "read").Operation("http.platform.feature.list").GET("/features", d.Feature.List)
		admin.Permission("feature", "read").Operation("http.platform.feature.get").GET("/features/:id", d.Feature.Get)
		admin.Permission("feature", "configure").Operation("http.platform.feature.configure").PUT("/features/:id/config", d.Feature.UpdateConfig)
		admin.Permission("feature", "lifecycle").Operation("http.platform.feature.enable").POST("/features/:id/enable", d.Feature.Enable)
		admin.Permission("feature", "lifecycle").Operation("http.platform.feature.disable").POST("/features/:id/disable", d.Feature.Disable)
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
		admin.PermissionCode("schedule.academic_term.read").Operation("http.schedule.academic_term.list").GET("/admin/academic-terms", d.AcademicTerm.ListAdmin)
		admin.PermissionCode("schedule.academic_term.manage").Operation("http.schedule.academic_term.create").POST("/admin/academic-terms", d.AcademicTerm.Create)
		admin.PermissionCode("schedule.academic_term.manage").Operation("http.schedule.academic_term.update").PUT("/admin/academic-terms/:id", d.AcademicTerm.UpdateFirstWeek)
		admin.PermissionCode("schedule.academic_term.manage").Operation("http.schedule.academic_term.open").POST("/admin/academic-terms/:id/open", d.AcademicTerm.Open)
		admin.PermissionCode("schedule.academic_term.manage").Operation("http.schedule.academic_term.close").POST("/admin/academic-terms/:id/close", d.AcademicTerm.Close)
		admin.PermissionCode("schedule.academic_term.manage").Operation("http.schedule.academic_term.default").POST("/admin/academic-terms/:id/default", d.AcademicTerm.SetDefault)
		admin.PermissionCode("schedule.academic_term.delete").Operation("http.schedule.academic_term.delete").DELETE("/admin/academic-terms/:id", d.AcademicTerm.Delete)
		admin.Permission("space", "manage").GET("/spaces/admin/users/:user_id/storage", d.Space.AdminStorageStatus)
		admin.Permission("space", "manage").PUT("/spaces/admin/users/:user_id/storage", d.Space.SetStorageQuota)
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
		admin.PermissionCode("platform.reliability.read").Operation("http.platform.reliability.summary").GET("/platform/reliability/summary", d.Reliability.Summary)
		admin.PermissionCode("platform.reliability.read").Operation("http.platform.reliability.events").GET("/platform/reliability/events", d.Reliability.ListEvents)
		admin.PermissionCode("platform.reliability.read").Operation("http.platform.reliability.attempts").GET("/platform/reliability/attempts", d.Reliability.ListAttempts)
		admin.PermissionCode("platform.reliability.read").Operation("http.platform.reliability.workers").GET("/platform/reliability/workers", d.Reliability.ListWorkers)
		admin.PermissionCode("platform.reliability.read").Operation("http.platform.reliability.operations").GET("/platform/reliability/operations", d.Reliability.ListOperations)
		admin.PermissionCode("platform.reliability.read").Operation("http.platform.reliability.command_audits").GET("/platform/reliability/command-audits", d.Reliability.ListCommandAudits)
		admin.PermissionCode("platform.reliability.read").Operation("http.platform.reliability.compatibility").GET("/platform/reliability/compatibility", d.Reliability.ListCompatibility)
		admin.PermissionCode("platform.retention.preview").Operation("http.platform.reliability.retention_preview").GET("/platform/reliability/retention-preview", d.Reliability.PreviewRetention)
		admin.PermissionCode("platform.retention.preview").Operation("http.platform.reliability.retention_runs").GET("/platform/reliability/retention-runs", d.Reliability.ListRetentionRuns)
		admin.PermissionCode("platform.retention.preview").Operation("http.platform.reliability.retention_preview_create").POST("/platform/reliability/retention-runs/preview", d.Reliability.StartRetentionPreview)
		admin.PermissionCode("platform.reliability.replay").Operation("http.platform.reliability.replay").POST("/platform/reliability/events/:id/replay", d.Reliability.Replay)
		admin.PermissionCode("platform.email_delivery.read").Operation("http.core.email_delivery.status").GET("/platform/email-delivery/status", d.EmailDelivery.Status)
		admin.PermissionCode("identity.challenge_policy.read").Operation("http.identity.challenge_policy.get").GET("/identity/challenge-policy", challengePolicyHandler.Get)
		admin.PermissionCode("identity.challenge_policy.update").Operation("http.identity.challenge_policy.update").PUT("/identity/challenge-policy", challengePolicyHandler.Update)
		admin.PermissionCode("identity.account.recovery.override").Operation("http.identity.recovery.cases.list").GET("/identity/recovery-cases", userHandler.ListAdminRecoveryCases)
		admin.PermissionCode("identity.account.recovery.override").Operation("http.identity.recovery.cases.create").POST("/identity/recovery-cases", userHandler.CreateAdminRecoveryCase)
		admin.PermissionCode("identity.account.recovery.override").Operation("http.identity.recovery.cases.cancel").POST("/identity/recovery-cases/:id/cancel", userHandler.CancelAdminRecoveryCase)
		admin.PermissionCode("identity.session.read").Operation("http.identity.session.admin_list").GET("/identity/users/:id/sessions", userHandler.ListAdminUserSessions)
		admin.PermissionCode("identity.session.revoke").Operation("http.identity.session.admin_revoke_all").POST("/identity/users/:id/sessions/revoke-all", userHandler.RevokeAdminUserSessions)
		admin.PermissionCode("identity.admin_account.read").Operation("http.identity.admin_account.list").GET("/identity/admin-accounts", adminAdmissionHandler.List)
		admin.PermissionCode("identity.admin_account.read_audit").Operation("http.identity.admin_account.audits").GET("/identity/admin-accounts/audits", adminAdmissionHandler.ListAudits)
		admin.PermissionCode("identity.admin_account.read").Operation("http.identity.admin_account.get").GET("/identity/admin-accounts/:id", adminAdmissionHandler.Get)
		admin.PermissionCode("identity.admin_account.suspend").Operation("http.identity.admin_account.suspend").POST("/identity/admin-accounts/:id/suspend", adminAdmissionHandler.Suspend)
		admin.PermissionCode("identity.admin_account.restore").Operation("http.identity.admin_account.restore").POST("/identity/admin-accounts/:id/restore", adminAdmissionHandler.Restore)
		admin.PermissionCode("identity.mfa_policy.read").Operation("http.identity.mfa_policy.get").GET("/identity/mfa-policy", userHandler.GetMFAAdminPolicy)
		admin.PermissionCode("identity.mfa_policy.update").Operation("http.identity.mfa_policy.update").PUT("/identity/mfa-policy", userHandler.UpdateMFAAdminPolicy)
		admin.Permission("homepage", "configure").POST("/home/style-packs/validate", d.Homepage.ValidateStylePack)
		admin.Permission("homepage", "configure").Operation("http.appearance.home.logo_upload").POST("/home/logo", d.Homepage.UploadLogo)
		admin.Permission("homepage", "configure").Operation("http.appearance.home.logo_reset").DELETE("/home/logo", d.Homepage.ResetLogo)
		admin.Permission("homepage", "configure").GET("/home/style-packs/example", d.Homepage.StylePackExample)
		admin.Permission("homepage", "configure").GET("/home/style-packs/example.zip", d.Homepage.StylePackExampleZip)
		admin.Permission("homepage", "configure").GET("/home/style-packs/sources", d.Homepage.ListSourceStylePacks)
		admin.Permission("homepage", "configure").POST("/home/style-packs/apply", d.Homepage.ApplyStylePack)
		admin.Permission("homepage", "configure").POST("/home/style-packs/apply-source", d.Homepage.ApplySourceStylePack)
		admin.Permission("homepage", "configure").POST("/home/style-packs/rollback", d.Homepage.RollbackStylePack)
		admin.Permission("role", "read").GET("/roles", roleHandler.ListRoles)
		admin.Permission("role", "read").GET("/permissions", roleHandler.ListPermissionDefinitions)
		admin.Permission("role", "read").GET("/roles/:id/permissions", roleHandler.ListRolePermissions)
		admin.PermissionCode("identity.role.read_audit").Operation("http.identity.role.authorization_audits").GET("/authorization-audits", roleHandler.ListAuthorizationAudits)
		admin.PermissionCode("identity.role.create").Operation("http.identity.role.create").POST("/roles", roleHandler.CreateCustomRole)
		admin.PermissionCode("identity.role.update_permissions").Operation("http.identity.role.update_permissions").PUT("/roles/:id/permissions", roleHandler.UpdateRolePermissions)
		admin.Permission("role", "read").GET("/users/:id/roles", roleHandler.GetUserRoles)
		admin.Permission("role", "assign").POST("/users/:id/roles", roleHandler.AssignRole)
		admin.Permission("role", "revoke").DELETE("/users/:id/roles", roleHandler.RevokeRole)
		admin.Permission("role", "read").GET("/moderation/admin/moderators", d.Moderation.ListModerators)
		admin.Permission("role", "read").GET("/moderation/admin/moderators/:id", d.Moderation.GetModerator)
		admin.Permission("role", "assign").PUT("/moderation/admin/moderators/:id", d.Moderation.SetModerator)
	}

	return newRouter(r, routeRegistry)
}
