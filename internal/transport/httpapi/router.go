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
	"github.com/campusos/CampusOS/internal/modules/features/knowledge"
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
	AdminJWT          *auth.JWTManager
	AdminSessions     middleware.AdminDomainSessionVerifier
	AdminCodes        middleware.AdminCodeChecker
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
	PluginV5          *plugin.V5HTTPHandler
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
	Knowledge         *knowledge.Handler
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
	// Structured recovery/request logging below owns this boundary. Gin's
	// duplicate default logger would also record raw credential query strings.
	r := gin.New()
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
	r.Use(knowledgeNoStore)
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
		platformfeature.PathRule{Prefix: "/api/v1/knowledge", FeatureID: "knowledge", DisabledHandler: knowledge.DisabledHTTP},
		platformfeature.PathRule{Prefix: "/api/v1/integration/knowledge", FeatureID: "knowledge", DisabledHandler: knowledge.DisabledHTTP},
		platformfeature.PathRule{Prefix: "/api/v1/admin/knowledge", FeatureID: "knowledge", DisabledHandler: knowledge.DisabledHTTP},
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
	integration := newOwnedGroup(v1.Group(""), routeRegistry, platformroute.AudienceIntegration, d.Permissions)
	integration.Operation("http.identity.integration_self.read").GET("/integration/identity/me", d.Identity.Integration.Me)
	integration.Operation("http.integration.public_thread.read").GET("/integration/threads/:id", d.Integration.IntegrationThread)
	integration.Operation("http.integration.public_thread.mcp_read").POST("/integration/mcp/tools/:name/call", d.Integration.IntegrationMCP)
	integration.Operation("http.integration.knowledge.source.read").GET("/integration/knowledge/collections/:id/sources/:member_id/revisions/:revision", d.Knowledge.IntegrationSource)
	integration.Operation("http.integration.knowledge.search").POST("/integration/knowledge/collections/:id/search", d.Knowledge.IntegrationSearch)
	integration.Operation("http.integration.knowledge.mcp").POST("/integration/knowledge/collections/:id/mcp", d.Knowledge.IntegrationMCP)
	integration.Operation("http.integration.knowledge.mcp.stream").GET("/integration/knowledge/collections/:id/mcp", d.Knowledge.IntegrationMCP)
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
		public.Operation("http.knowledge.collection.list").GET("/knowledge/collections", d.Knowledge.Collections)
		public.Operation("http.knowledge.search.public").GET("/knowledge/collections/:id/search", d.Knowledge.Search)
		public.Operation("http.knowledge.source.list").GET("/knowledge/collections/:id/sources", d.Knowledge.Sources)
		public.Operation("http.knowledge.source.read").GET("/knowledge/collections/:id/sources/:member_id", d.Knowledge.Source)
		public.GET("/richtext/status", d.RichText.Status)
		public.GET("/schedule/status", d.Schedule.Status)
		public.GET("/mutual-aid/status", d.MutualAid.Status)
		public.GET("/secondhand/status", d.Secondhand.Status)
		public.POST("/auth/registration/challenge", userHandler.RequestRegistrationChallenge)
		public.POST("/auth/registration/verify", userHandler.VerifyRegistrationChallenge)
		public.POST("/auth/register", userHandler.Register)
		public.POST("/auth/login", userHandler.Login)
		public.POST("/auth/admin/login", userHandler.AdminLogin)
		public.Operation("http.identity.admin_session.login").POST("/admin/identity/login", d.Identity.AdminDomain.Login)
		public.Operation("http.identity.admin_session.refresh").POST("/admin/identity/refresh", d.Identity.AdminDomain.Refresh)
		public.POST("/admin/identity/bootstrap", d.Identity.AdminDomain.Bootstrap)
		public.POST("/auth/mfa/login/complete", userHandler.CompleteMFALogin)
		public.POST("/auth/refresh", userHandler.Refresh)
		public.POST("/auth/password-reset/challenge", userHandler.RequestPasswordReset)
		public.POST("/auth/password-reset/verify", userHandler.VerifyPasswordReset)
		public.POST("/auth/password-reset/complete", userHandler.CompletePasswordReset)
		public.POST("/auth/recovery/complete", userHandler.CompleteAdminRecovery)
		public.GET("/threads", threadHandler.ListThreads)
		public.GET("/threads/:id", threadHandler.GetThread)
		public.GET("/content/assets/images/:user_id/:filename", middleware.OptionalJWT(d.JWT, d.SessionVerifier), userStorageHandler.ServeContentImage)
		public.GET("/mutual-aid/threads", d.MutualAid.ListPublic)
		public.GET("/mutual-aid/threads/:id", d.MutualAid.GetPublic)
		public.GET("/secondhand/threads", d.Secondhand.ListPublic)
		public.GET("/secondhand/threads/:id", d.Secondhand.GetPublic)
		public.GET("/richtext/articles/:id", d.RichText.GetPublished)
		public.GET("/richtext/assets/:user_id/:filename", middleware.OptionalJWT(d.JWT, d.SessionVerifier), d.RichText.ServeAsset)
		public.GET("/users", userHandler.ListUsers)
		public.GET("/users/:id", userHandler.GetUser)
		public.Operation("http.personal_space.public_contents.by_id").GET("/space/:user_id/contents", d.Space.ListContentsByUserID)
		public.Operation("http.personal_space.public_profile.by_id").GET("/space/:user_id", d.Space.GetByUserID)
		public.GET("/spaces/files/:user_id/avatars/:filename", middleware.OptionalJWT(d.JWT, d.SessionVerifier), d.Space.ServeAvatarFile)
		public.Operation("http.personal_space.public_contents.by_username").GET("/u/:username/contents", d.Space.ListContentsByUsername)
		public.Operation("http.personal_space.public_profile.by_username").GET("/u/:username", d.Space.GetByUsername)
		public.GET("/appearance/space-style-packs/:name/assets/*asset_path", d.Space.ServeSourceStylePackAsset)
		public.GET("/categories", categoryHandler.List)
		public.GET("/categories/tree", categoryHandler.ListTree)
		public.GET("/categories/:id", categoryHandler.Get)
		public.GET("/categories/:id/thread-types", categoryHandler.ListThreadTypePolicies)
		public.GET("/threads/:id/posts", postHandler.ListPosts)
	}

	authenticated := newOwnedGroup(v1.Group(""), routeRegistry, platformroute.AudienceAuthenticated, d.Permissions)
	authenticated.Use(middleware.JWTAuth(d.JWT, d.SessionVerifier))
	{
		authenticated.Operation("http.knowledge.rag.status").GET("/knowledge/rag/status", d.Knowledge.RAGStatus)
		authenticated.Operation("http.knowledge.rag.public").POST("/knowledge/rag/public", d.Knowledge.RAGPublicAnswer)
		authenticated.Operation("http.knowledge.private.rag.prepare").POST("/knowledge/private/rag/prepare", d.Knowledge.RAGPrivatePrepare)
		authenticated.Operation("http.knowledge.private.rag.answer").POST("/knowledge/private/rag/answer", d.Knowledge.RAGPrivateAnswer)
		authenticated.Operation("http.knowledge.private.rag.revoke").POST("/knowledge/private/rag/revoke", d.Knowledge.RAGPrivateRevoke)
		authenticated.Operation("http.knowledge.private.status").GET("/knowledge/private/status", d.Knowledge.PrivateStatus)
		authenticated.Operation("http.knowledge.private.preview").POST("/knowledge/private/preview", d.Knowledge.PrivatePreview)
		authenticated.Operation("http.knowledge.private.list").GET("/knowledge/private/selections", d.Knowledge.PrivateSelections)
		authenticated.Operation("http.knowledge.private.select").POST("/knowledge/private/selections", d.Knowledge.PrivateSelect)
		authenticated.Operation("http.knowledge.private.withdraw").DELETE("/knowledge/private/selections/:selection_id", d.Knowledge.PrivateWithdraw)
		authenticated.Operation("http.knowledge.private.retry").POST("/knowledge/private/selections/:selection_id/retry", d.Knowledge.PrivateRetry)
		authenticated.Operation("http.knowledge.private.search").POST("/knowledge/private/search", d.Knowledge.PrivateSearch)
		authenticated.Operation("http.knowledge.private.source").POST("/knowledge/private/selections/:selection_id/source", d.Knowledge.PrivateSource)
		authenticated.Operation("http.knowledge.private.source.preflight").POST("/knowledge/private/sources/preflight", d.Knowledge.PrivateSourcePreflight)
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
		authenticated.Operation("http.personal_space.owner_profile.read").GET("/spaces/me", d.Space.GetMe)
		authenticated.Operation("http.personal_space.owner_contents.read").GET("/spaces/me/contents", d.Space.ListOwnContents)
		authenticated.Operation("http.personal_space.owner_profile.update").PUT("/spaces/me", d.Space.UpdateMe)
		authenticated.GET("/spaces/me/storage", d.Space.GetStorageStatus)
		authenticated.Operation("http.personal_space.owner_avatar.upload").POST("/spaces/me/avatar", d.Space.UploadAvatar)
		authenticated.GET("/spaces/me/avatars", d.Space.ListAvatars)
		authenticated.Operation("http.personal_space.owner_avatar.cleanup").POST("/spaces/me/avatars/cleanup", d.Space.CleanupAvatarHistory)
		authenticated.Operation("http.personal_space.owner_avatar.select").PUT("/spaces/me/avatar", d.Space.SelectAvatar)
		authenticated.Operation("http.personal_space.owner_style_read.validate").POST("/spaces/me/styles/validate", d.Space.ValidateStylePackage)
		authenticated.Operation("http.personal_space.owner_style_read.preview").POST("/spaces/me/styles/preview", d.Space.PreviewStylePackage)
		authenticated.Operation("http.personal_space.owner_style_read.export").POST("/spaces/me/styles/export", d.Space.ExportStylePackage)
		authenticated.Operation("http.personal_space.owner_style.apply").POST("/spaces/me/styles/apply", d.Space.ApplyStylePackage)
		authenticated.Operation("http.personal_space.owner_style_read.html_validate").POST("/spaces/me/styles/html/validate", d.Space.ValidateCustomHTML)
		authenticated.Operation("http.personal_space.owner_style_read.html_example").GET("/spaces/me/styles/html-example", d.Space.CustomHTMLExample)
		authenticated.Operation("http.personal_space.owner_style.html").POST("/spaces/me/styles/html/apply", d.Space.ApplyCustomHTML)
		authenticated.Operation("http.personal_space.owner_style_read.zip_validate").POST("/spaces/me/styles/packs/validate", d.Space.ValidateStylePackZip)
		authenticated.Operation("http.personal_space.owner_style_read.pack_example").GET("/spaces/me/styles/packs/example", d.Space.StylePackExample)
		authenticated.Operation("http.personal_space.owner_style_read.pack_zip").GET("/spaces/me/styles/packs/example.zip", d.Space.StylePackExampleZip)
		authenticated.Operation("http.personal_space.owner_style.catalog").GET("/spaces/me/styles/packs/sources", d.Space.ListSourceStylePacks)
		authenticated.Operation("http.personal_space.owner_style.zip").POST("/spaces/me/styles/packs/apply", d.Space.ApplyStylePackZip)
		authenticated.Operation("http.personal_space.owner_style.source").POST("/spaces/me/styles/packs/apply-source", d.Space.ApplySourceStylePack)
		authenticated.Operation("http.personal_space.owner_style.rollback").POST("/spaces/me/styles/rollback", d.Space.RollbackStyle)
		authenticated.Operation("http.personal_space.owner_style.default").POST("/spaces/me/styles/default", d.Space.RestoreDefaultStyle)
		authenticated.Operation("http.personal_space.owner_sync_status.read").GET("/spaces/me/sync-status", d.Space.GetSyncStatus)
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
		authenticated.POST("/documents/:id/pdf-invocations", d.PersonalDocuments.CreatePDFInvocation)
		authenticated.PUT("/documents/:id", d.PersonalDocuments.Save)
		authenticated.GET("/documents/:id/versions", d.PersonalDocuments.Versions)
		authenticated.POST("/documents/:id/versions/:version_id/restore", d.PersonalDocuments.RestoreVersion)
		authenticated.DELETE("/documents/:id", d.PersonalDocuments.Trash)
		authenticated.POST("/documents/:id/trash", d.PersonalDocuments.Trash)
		authenticated.POST("/documents/:id/restore", d.PersonalDocuments.Restore)
		authenticated.GET("/documents/:id/download", d.PersonalDocuments.Download)
		authenticated.POST("/richtext/articles", d.RichText.CreateDraft)
		authenticated.POST("/content/preview", threadHandler.PreviewContent)
		authenticated.GET("/content/assets/images/me", userStorageHandler.ListMyContentImages)
		authenticated.Operation("http.storage.inline_image.author_upload").Scoped("self-or-resource-owner").POST("/content/assets/images", userStorageHandler.UploadContentImage)
		authenticated.GET("/richtext/articles/:id/me", d.RichText.GetMine)
		authenticated.PUT("/richtext/articles/:id", d.RichText.UpdateDraft)
		authenticated.POST("/richtext/preview", d.RichText.Preview)
		authenticated.GET("/richtext/assets/me", d.RichText.ListMyAssets)
		authenticated.Operation("http.feature.richtext.inline_image.author_upload").Scoped("self-or-resource-owner").POST("/richtext/assets", d.RichText.UploadAsset)
		authenticated.GET("/assets", d.RichText.ListUserAssets)
		authenticated.GET("/assets/:id/download", d.RichText.DownloadMyUserAsset)
		authenticated.POST("/assets/:id/pdf-invocations", d.RichText.CreatePersonalAssetPDFInvocation)
		authenticated.Operation("http.feature.richtext.asset.owner_trash").POST("/assets/:id/trash", d.RichText.TrashUserAsset)
		authenticated.Operation("http.feature.richtext.asset.owner_restore").POST("/assets/:id/restore", d.RichText.RestoreUserAsset)
		authenticated.GET("/richtext/articles/:id/attachments", d.RichText.ListAttachments)
		authenticated.Operation("http.feature.richtext.attachment.bind").POST("/richtext/articles/:id/attachments", d.RichText.BindAttachment)
		authenticated.Operation("http.feature.richtext.attachment.copy_document").POST("/richtext/articles/:id/attachments/from-personal-document", d.RichText.AttachPersonalDocument)
		authenticated.Operation("http.feature.richtext.attachment.upload").POST("/richtext/articles/:id/attachments/upload", d.RichText.UploadAttachment)
		authenticated.Operation("http.feature.richtext.attachment.reorder").PATCH("/richtext/articles/:id/attachments/order", d.RichText.ReorderAttachments)
		authenticated.Operation("http.feature.richtext.attachment.rename").PATCH("/richtext/articles/:id/attachments/:attachment_id", d.RichText.RenameAttachment)
		authenticated.Operation("http.feature.richtext.attachment.remove").DELETE("/richtext/articles/:id/attachments/:attachment_id", d.RichText.RemoveAttachment)
		authenticated.GET("/richtext/articles/:id/attachments/:attachment_id/download", d.RichText.DownloadAttachment)
		authenticated.POST("/plugin-ui/invocations", d.RichText.CreatePDFInvocation)
		authenticated.GET("/plugin-ui/invocations/:id", d.RichText.GetPDFInvocation)
		authenticated.GET("/plugin-ui/invocations/:id/content", d.RichText.PDFInvocationContent)
		authenticated.GET("/plugin-ui/invocations/:id/download", d.RichText.DownloadPDFInvocation)
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
		authenticated.Operation("http.community.thread.user_take_down").POST("/threads/:id/take-down", threadHandler.TakeDown)
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
		authenticated.Operation("http.community.moderation.current_preview").GET("/moderation/me", d.Moderation.MyAccess)
		authenticated.Operation("http.community.moderation.current_preview_batch").POST("/moderation/threads/:id/access", d.Moderation.PreviewBatch)
		authenticated.Operation("http.community.thread.user_pin").POST("/moderation/threads/:id/pin", d.Moderation.Pin)
		authenticated.Operation("http.community.thread.user_unpin").POST("/moderation/threads/:id/unpin", d.Moderation.Unpin)
		authenticated.Operation("http.community.thread.user_lock").POST("/moderation/threads/:id/lock", d.Moderation.Lock)
		authenticated.Operation("http.community.thread.user_unlock").POST("/moderation/threads/:id/unlock", d.Moderation.Unlock)
		authenticated.Operation("http.community.post.user_governance_delete").DELETE("/moderation/threads/:id/posts/:post_id", d.Moderation.DeletePost)
		authenticated.Operation("http.plugin.v5.user.catalog").GET("/plugin-v5", d.PluginV5.Inventory(false))
		authenticated.Operation("http.plugin.v5.user.detail").GET("/plugin-v5/:publisher/:key/:version", d.PluginV5.Detail(false))
		authenticated.Operation("http.plugin.v5.user.detail").GET("/plugin-v5/:publisher/:key/:version/config-options", d.PluginV5.ConfigOptions(false))
		authenticated.Operation("http.plugin.v5.user.detail").GET("/plugin-v5/:publisher/:key/:version/ui", d.PluginV5.UIState(false))
		authenticated.Operation("http.plugin.v5.user.command").POST("/plugin-v5/:publisher/:key/commands", d.PluginV5.Mutate(false))
		authenticated.Operation("http.plugin.v5.user.catalog").GET("/plugin-v5/ui", d.PluginV5.UIManifest(false))
		authenticated.Operation("http.plugin.v5.user.operation").GET("/plugin-v5/operations/:id", d.PluginV5.Operation(false))
		authenticated.GET("/plugin-market", plugin.V5RetiredHTTP)
		authenticated.GET("/plugin-capabilities", plugin.V5RetiredHTTP)
		authenticated.GET("/plugin-authorizations/:name", plugin.V5RetiredHTTP)
		authenticated.PUT("/plugin-authorizations/:name/versions/:version_id/consents/:capability", plugin.V5RetiredHTTP)
		authenticated.POST("/plugin-authorizations/:name/versions/:version_id/delegations", plugin.V5RetiredHTTP)
		authenticated.DELETE("/plugin-authorizations/:name/delegations/:delegation_id", plugin.V5RetiredHTTP)
		authenticated.GET("/plugin-authorizations/:name/secrets", plugin.V5RetiredHTTP)
		authenticated.PUT("/plugin-authorizations/:name/secrets/:secret", plugin.V5RetiredHTTP)
		authenticated.DELETE("/plugin-authorizations/:name/secrets/:secret", plugin.V5RetiredHTTP)
		authenticated.GET("/plugin-market/me", plugin.V5RetiredHTTP)
		authenticated.GET("/plugin-market/me/usage", plugin.V5RetiredHTTP)
		authenticated.GET("/plugin-market/sources", plugin.V5RetiredHTTP)
		authenticated.GET("/plugin-market/sources/:source_id/plugins", plugin.V5RetiredHTTP)
		authenticated.POST("/plugin-market/requests", plugin.V5RetiredHTTP)
		authenticated.POST("/plugin-market/:name/enable", plugin.V5RetiredHTTP)
		authenticated.POST("/plugin-market/:name/revoke", plugin.V5RetiredHTTP)
		authenticated.GET("/plugin-market/search", plugin.V5RetiredHTTP)
		authenticated.POST("/plugin-market/:name/records/:collection", plugin.V5RetiredHTTP)
		authenticated.GET("/plugin-market/:name/records/:collection", plugin.V5RetiredHTTP)
		authenticated.GET("/plugin-market/:name/records/:collection/:key", plugin.V5RetiredHTTP)
		authenticated.PUT("/plugin-market/:name/records/:collection/:key", plugin.V5RetiredHTTP)
		authenticated.DELETE("/plugin-market/:name/records/:collection/:key", plugin.V5RetiredHTTP)
		authenticated.POST("/plugin-market/:name/files", plugin.V5RetiredHTTP)
		authenticated.GET("/plugin-market/:name/files", plugin.V5RetiredHTTP)
		authenticated.GET("/plugin-market/:name/files/:file_id/download", plugin.V5RetiredHTTP)
		authenticated.DELETE("/plugin-market/:name/files/:file_id", plugin.V5RetiredHTTP)
		authenticated.GET("/plugin-market/:name/export", plugin.V5RetiredHTTP)
		authenticated.DELETE("/plugin-market/:name/data", plugin.V5RetiredHTTP)
		authenticated.GET("/extensions/:plugin/*path", runtimeHTTPHandler.Extension)
		authenticated.POST("/extensions/:plugin/*path", runtimeHTTPHandler.Extension)
		authenticated.PUT("/extensions/:plugin/*path", runtimeHTTPHandler.Extension)
		authenticated.PATCH("/extensions/:plugin/*path", runtimeHTTPHandler.Extension)
		authenticated.DELETE("/extensions/:plugin/*path", runtimeHTTPHandler.Extension)
	}

	// V12-02d: the management plane runs on the independent admin identity
	// domain. Admin routes verify admin-domain sessions only; user tokens are
	// rejected at the entry.
	authenticatedAdminSelf := newOwnedGroup(v1.Group(""), routeRegistry, platformroute.AudienceAuthenticated, nil)
	authenticatedAdminSelf.Use(func(c *gin.Context) { c.Header("Cache-Control", "private, no-store"); c.Next() }, middleware.RequireAdminDomainAccess(d.AdminJWT, d.AdminSessions, nil))
	authenticatedAdminSelf.Operation("http.identity.admin_self.read").Scoped("current-admin-self").GET("/admin/identity/me", d.Identity.AdminDomain.Me)
	authenticatedAdminSelf.Operation("http.identity.admin_self.logout").Scoped("current-admin-self").POST("/admin/identity/logout", d.Identity.AdminDomain.Logout)
	authenticatedAdminSelf.Operation("http.identity.admin_self.mfa.enroll").Scoped("current-admin-self").POST("/admin/identity/mfa/enroll", d.Identity.AdminDomain.EnrollTOTP)
	authenticatedAdminSelf.Operation("http.identity.admin_self.mfa.confirm").Scoped("current-admin-self").POST("/admin/identity/mfa/confirm", d.Identity.AdminDomain.ConfirmTOTP)
	authenticatedAdminSelf.Operation("http.identity.admin_self.mfa.step_up").Scoped("current-admin-self").POST("/admin/identity/mfa/step-up", d.Identity.AdminDomain.StepUp)
	admin := newOwnedGroup(v1.Group(""), routeRegistry, platformroute.AudienceAdmin, d.Permissions, d.AdminAccess)
	admin.SetAdminDomain(d.AdminJWT, d.AdminSessions, d.AdminCodes)
	admin.Use(middleware.RequireAdminDomainAccess(d.AdminJWT, d.AdminSessions, nil))
	admin.PermissionCode("knowledge.collection.read").Operation("http.knowledge.collection.inventory").GET("/admin/knowledge/collections", d.Knowledge.Inventory)
	admin.PermissionCode("knowledge.collection.read").Operation("http.knowledge.collection.read").GET("/admin/knowledge/collections/:id", d.Knowledge.State)
	admin.PermissionCode("knowledge.collection.read").Operation("http.knowledge.index.diagnostics").GET("/admin/knowledge/collections/:id/index", d.Knowledge.IndexDiagnostics)
	admin.PermissionCode("knowledge.collection.manage").Operation("http.knowledge.index.retry").POST("/admin/knowledge/collections/:id/index/retry", d.Knowledge.RetryIndex)
	admin.PermissionCode("knowledge.collection.read").Operation("http.knowledge.official.inventory").GET("/admin/knowledge/officials", d.Knowledge.Inventory)
	admin.PermissionCode("knowledge.collection.read").Operation("http.knowledge.official.history").GET("/admin/knowledge/officials/:id/revisions", d.Knowledge.History)
	admin.PermissionCode("knowledge.collection.manage").Operation("http.knowledge.collection.create").POST("/admin/knowledge/collections", d.Knowledge.SaveCollection)
	admin.PermissionCode("knowledge.collection.manage").Operation("http.knowledge.collection.update").PUT("/admin/knowledge/collections/:id", d.Knowledge.SaveCollection)
	admin.PermissionCode("knowledge.collection.manage").Operation("http.knowledge.official.create").POST("/admin/knowledge/officials", d.Knowledge.SaveOfficial)
	admin.PermissionCode("knowledge.collection.manage").Operation("http.knowledge.official.update").PUT("/admin/knowledge/officials/:id", d.Knowledge.SaveOfficial)
	admin.PermissionCode("knowledge.collection.manage").Operation("http.knowledge.selection.create").POST("/admin/knowledge/collections/:id/selections", d.Knowledge.SaveSelection)
	admin.PermissionCode("knowledge.collection.manage").Operation("http.knowledge.selection.update").PUT("/admin/knowledge/collections/:id/selections/:selection_id", d.Knowledge.SaveSelection)
	admin.PermissionCode("knowledge.collection.manage").Operation("http.knowledge.selection.refresh").POST("/admin/knowledge/collections/:id/selections/:selection_id/refresh", d.Knowledge.Refresh)
	admin.PermissionCode("knowledge.collection.manage").Operation("http.knowledge.member.remove").POST("/admin/knowledge/collections/:id/members/:member_id/remove", d.Knowledge.RemoveMember)
	admin.PermissionCode("identity.integration.read").Operation("http.identity.integration.list").GET("/admin/identity/integrations", d.Identity.Integration.List)
	admin.PermissionCode("identity.integration.read").Operation("http.identity.integration.credentials").GET("/admin/identity/integrations/:id/credentials", d.Identity.Integration.List)
	admin.PermissionCode("identity.integration.read").Operation("http.identity.integration.endpoints").GET("/admin/identity/integration-endpoints", d.Identity.Integration.Endpoints)
	admin.PermissionCode("identity.integration.manage").Operation("http.identity.integration.create").POST("/admin/identity/integrations", d.Identity.Integration.Create)
	admin.PermissionCode("identity.integration.manage").Operation("http.identity.integration.update").PUT("/admin/identity/integrations/:id", d.Identity.Integration.Update)
	admin.PermissionCode("identity.integration.manage").Operation("http.identity.integration.issue").POST("/admin/identity/integrations/:id/credentials", d.Identity.Integration.Issue)
	admin.PermissionCode("identity.integration.manage").Operation("http.identity.integration.rotate").POST("/admin/identity/integrations/:id/credentials/:credential_id/rotate", d.Identity.Integration.Rotate)
	admin.PermissionCode("identity.integration.manage").Operation("http.identity.integration.scope").PUT("/admin/identity/integrations/:id/credentials/:credential_id", d.Identity.Integration.Scope)
	admin.PermissionCode("identity.integration.manage").Operation("http.identity.integration.revoke").POST("/admin/identity/integrations/:id/credentials/:credential_id/revoke", d.Identity.Integration.Revoke)
	{
		admin.Permission("admin_account", "create").POST("/admin/identity/accounts", d.Identity.AdminDomain.CreateAccount)
		admin.Permission("admin_account", "suspend").POST("/admin/identity/accounts/:id/suspend", d.Identity.AdminDomain.SuspendAccount)
		admin.Permission("admin_account", "revoke").POST("/admin/identity/accounts/:id/revoke", d.Identity.AdminDomain.RevokeAccount)
		admin.Permission("admin_account", "restore").POST("/admin/identity/accounts/:id/restore", d.Identity.AdminDomain.RestoreAccount)
	}
	admin.Permission("user", "read").Operation("http.identity.user.admin_list").GET("/admin/users", userHandler.ListAdminUsers)
	admin.Permission("user", "suspend").POST("/users/:id/suspend", userHandler.SuspendUser)
	admin.Permission("user", "suspend").POST("/users/:id/activate", userHandler.ActivateUser)
	admin.Permission("thread", "read").Operation("http.community.thread.admin_permissions_preview").POST("/admin/threads/permissions-preview", threadHandler.AdminPermissionsPreview)
	admin.Permission("thread", "read").Operation("http.community.thread.admin_list").GET("/admin/threads", threadHandler.AdminListThreads)
	admin.PermissionCode("platform.reliability.read").Operation("http.community.event.metadata").GET("/events", eventHandler.ListEvents)
	admin.Permission("thread", "read").Operation("http.community.thread.admin_trash_list").GET("/admin/threads/trash", threadHandler.AdminListTrash)
	admin.Permission("thread", "read").Operation("http.community.thread.admin_history").GET("/admin/threads/:id/moderation-actions", threadHandler.AdminModerationActions)
	admin.Permission("thread", "pin").POST("/threads/:id/pin", threadHandler.PinThread)
	admin.Permission("thread", "pin").POST("/threads/:id/unpin", threadHandler.UnpinThread)
	admin.Permission("thread", "lock").POST("/threads/:id/lock", threadHandler.LockThread)
	admin.Permission("thread", "lock").POST("/threads/:id/unlock", threadHandler.UnlockThread)
	admin.PermissionCode("community.thread.trash").Operation("http.community.thread.trash").DELETE("/admin/threads/:id", threadHandler.AdminDeleteThread)
	admin.PermissionCode("community.post.delete").Scoped("category").Operation("http.community.post.admin_delete").DELETE("/admin/posts/:post_id", postHandler.AdminGovernanceDelete)
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
	admin.Permission("plugin", "read").Operation("http.plugin.v5.admin.catalog").GET("/admin/plugin-v5", d.PluginV5.Inventory(true))
	admin.Permission("plugin", "read").Operation("http.plugin.v5.admin.detail").GET("/admin/plugin-v5/:publisher/:key/:version", d.PluginV5.Detail(true))
	admin.Permission("plugin", "read").Operation("http.plugin.v5.admin.detail").GET("/admin/plugin-v5/:publisher/:key/:version/config-options", d.PluginV5.ConfigOptions(true))
	admin.Permission("plugin", "read").Operation("http.plugin.v5.admin.detail").GET("/admin/plugin-v5/:publisher/:key/:version/ui", d.PluginV5.UIState(true))
	admin.Permission("plugin", "install").Operation("http.plugin.v5.admin.import").POST("/admin/plugin-v5/packages", d.PluginV5.Import)
	admin.Permission("plugin", "read").Operation("http.plugin.v5.admin.catalog").GET("/admin/plugin-v5/ui", d.PluginV5.UIManifest(true))
	admin.Permission("plugin", "read").Operation("http.plugin.v5.admin.operation").GET("/admin/plugin-v5/operations/:id", d.PluginV5.Operation(true))
	// Service derives the exact current permission and MFA requirement per command.
	admin.Permission("plugin", "read").Operation("http.plugin.v5.admin.command").POST("/admin/plugin-v5/:publisher/:key/commands", d.PluginV5.Mutate(true))
	admin.Permission("plugin", "read").GET("/plugins", plugin.V5RetiredHTTP)
	admin.Permission("plugin", "read").GET("/plugins/:name", plugin.V5RetiredHTTP)
	admin.Permission("plugin", "read").GET("/plugins/:name/authorization", plugin.V5RetiredHTTP)
	admin.Permission("plugin", "read").GET("/plugins/:name/authorization/decisions", plugin.V5RetiredHTTP)
	admin.Permission("plugin", "configure").POST("/plugins/:name/authorization/sync", plugin.V5RetiredHTTP)
	admin.Permission("plugin", "configure").PUT("/plugins/:name/versions/:version_id/grants/:capability", plugin.V5RetiredHTTP)
	admin.Permission("plugin", "configure").GET("/plugins/:name/secrets", plugin.V5RetiredHTTP)
	admin.Permission("plugin", "configure").PUT("/plugins/:name/secrets/:secret", plugin.V5RetiredHTTP)
	admin.Permission("plugin", "configure").DELETE("/plugins/:name/secrets/:secret", plugin.V5RetiredHTTP)
	admin.Permission("plugin", "read").GET("/plugins/:name/logs", plugin.V5RetiredHTTP)
	admin.Permission("plugin", "read").GET("/plugins/:name/export", plugin.V5RetiredHTTP)
	admin.Permission("plugin", "configure").PUT("/plugins/:name/config", plugin.V5RetiredHTTP)
	admin.Permission("plugin", "lifecycle").POST("/plugins/:name/enable", plugin.V5RetiredHTTP)
	admin.Permission("plugin", "lifecycle").POST("/plugins/:name/disable", plugin.V5RetiredHTTP)
	admin.Permission("plugin", "lifecycle").POST("/plugins/:name/reload", plugin.V5RetiredHTTP)
	admin.Permission("plugin", "uninstall").DELETE("/plugins/:name", plugin.V5RetiredHTTP)
	admin.Permission("plugin", "install").POST("/plugin-packages/import", plugin.V5RetiredHTTP)
	admin.Permission("plugin", "install").POST("/plugin-packages/precheck", plugin.V5RetiredHTTP)
	admin.Permission("plugin", "read").GET("/plugins/:name/snapshots", plugin.V5RetiredHTTP)
	admin.Permission("plugin", "install").POST("/plugins/:name/rollback", plugin.V5RetiredHTTP)
	admin.Permission("feature", "read").Operation("http.platform.feature.list").GET("/features", d.Feature.List)
	admin.Permission("feature", "read").Operation("http.platform.feature.get").GET("/features/:id", d.Feature.Get)
	admin.Permission("feature", "configure").Operation("http.platform.feature.configure").PUT("/features/:id/config", d.Feature.UpdateConfig)
	admin.Permission("feature", "lifecycle").Operation("http.platform.feature.enable").POST("/features/:id/enable", d.Feature.Enable)
	admin.Permission("feature", "lifecycle").Operation("http.platform.feature.disable").POST("/features/:id/disable", d.Feature.Disable)
	admin.Permission("plugin", "read").GET("/plugin-market/admin/overview", plugin.V5RetiredHTTP)
	admin.Permission("plugin", "read").GET("/plugin-market/admin/sources", plugin.V5RetiredHTTP)
	admin.Permission("plugin", "configure").PUT("/plugin-market/admin/sources/:source_id", plugin.V5RetiredHTTP)
	admin.Permission("plugin", "configure").DELETE("/plugin-market/admin/sources/:source_id", plugin.V5RetiredHTTP)
	admin.Permission("plugin", "configure").PUT("/plugin-market/admin/catalog/:name", plugin.V5RetiredHTTP)
	admin.Permission("plugin", "read").GET("/plugin-market/admin/requests", plugin.V5RetiredHTTP)
	admin.Permission("plugin", "configure").POST("/plugin-market/admin/requests/:id/review", plugin.V5RetiredHTTP)
	admin.Permission("plugin", "read").GET("/plugin-market/admin/releases/:name", plugin.V5RetiredHTTP)
	admin.Permission("plugin", "read").GET("/plugin-market/admin/audits", plugin.V5RetiredHTTP)
	admin.Permission("plugin", "install").POST("/plugin-market/admin/releases/:name", plugin.V5RetiredHTTP)
	admin.Permission("ai", "read").Operation("http.ai.status").GET("/ai/status", d.AI.GetStatus)
	admin.Permission("ai", "read").Operation("http.ai.logs").GET("/ai/logs", d.AI.ListLogs)
	admin.Permission("ai", "read").Operation("http.ai.profiles").GET("/ai/profiles", d.AI.Profiles)
	admin.Permission("ai", "manage").Operation("http.ai.binding.change").POST("/ai/bindings/chat", d.AI.BindChat)
	admin.Permission("ai", "manage").Operation("http.ai.profile.state").POST("/ai/profiles/:profile_id/state", d.AI.ProfileState)
	admin.Permission("integration", "read").GET("/integrations/overview", d.Integration.Overview)
	admin.Permission("metrics", "read").GET("/metrics/summary", d.Integration.Metrics)
	admin.Permission("space", "manage").Operation("http.personal_space.admin.summary").GET("/spaces/admin/summary", d.Space.AdminSummary)
	admin.PermissionCode("personal_space.asset.read_audit").Operation("http.personal_space.asset.governance_summary").GET("/admin/assets/summary", d.RichText.AssetGovernanceSummary)
	admin.PermissionCode("personal_space.asset.manage").Operation("http.personal_space.asset.quarantine").POST("/admin/assets/:id/quarantine", d.RichText.AdminQuarantineUserAsset)
	admin.PermissionCode("personal_space.asset.manage").Operation("http.personal_space.asset.restore").POST("/admin/assets/:id/restore", d.RichText.AdminRestoreQuarantinedUserAsset)
	admin.PermissionCode("personal_space.asset.manage").Operation("http.personal_space.asset.purge_preview").GET("/admin/assets/:id/purge-preview", d.RichText.AdminPreviewUserAssetPurge)
	admin.PermissionCode("personal_space.asset.manage").Operation("http.personal_space.asset.purge").DELETE("/admin/assets/:id/purge", d.RichText.AdminPurgeUserAsset)
	admin.PermissionCode("schedule.academic_term.read").Operation("http.schedule.academic_term.list").GET("/admin/academic-terms", d.AcademicTerm.ListAdmin)
	admin.PermissionCode("schedule.academic_term.manage").Operation("http.schedule.academic_term.create").POST("/admin/academic-terms", d.AcademicTerm.Create)
	admin.PermissionCode("schedule.academic_term.manage").Operation("http.schedule.academic_term.update").PUT("/admin/academic-terms/:id", d.AcademicTerm.UpdateFirstWeek)
	admin.PermissionCode("schedule.academic_term.manage").Operation("http.schedule.academic_term.open").POST("/admin/academic-terms/:id/open", d.AcademicTerm.Open)
	admin.PermissionCode("schedule.academic_term.manage").Operation("http.schedule.academic_term.close").POST("/admin/academic-terms/:id/close", d.AcademicTerm.Close)
	admin.PermissionCode("schedule.academic_term.manage").Operation("http.schedule.academic_term.default").POST("/admin/academic-terms/:id/default", d.AcademicTerm.SetDefault)
	admin.PermissionCode("schedule.academic_term.delete").Operation("http.schedule.academic_term.delete").DELETE("/admin/academic-terms/:id", d.AcademicTerm.Delete)
	admin.Permission("space", "manage").Operation("http.personal_space.admin.storage").GET("/spaces/admin/users/:user_id/storage", d.Space.AdminStorageStatus)
	admin.Permission("space", "manage").Operation("http.personal_space.admin.quota").PUT("/spaces/admin/users/:user_id/storage", d.Space.SetStorageQuota)
	admin.Permission("space", "manage").Operation("http.personal_space.admin.disable").POST("/spaces/:user_id/disable", d.Space.DisableSpace)
	admin.Permission("space", "manage").Operation("http.personal_space.admin.enable").POST("/spaces/:user_id/enable", d.Space.EnableSpace)
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

	return newRouter(r, routeRegistry)
}
