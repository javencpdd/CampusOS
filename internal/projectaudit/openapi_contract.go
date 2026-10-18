package projectaudit

type operationProfile struct {
	RequestSchema       string
	ResponseSchema      string
	ResponseContentType string
	ContentType         string
	SuccessStatus       string
	AdditionalErrors    []string
	NoBody              bool
	Paginated           bool
	RequestOptional     bool
}

var operationProfiles = map[string]operationProfile{
	"pluginV5Handler.Inventory":            {ResponseSchema: "PluginV5Inventory", NoBody: true, AdditionalErrors: []string{"401", "403", "409", "503"}},
	"pluginV5Handler.Detail":               {ResponseSchema: "PluginV5InventoryEntry", NoBody: true, AdditionalErrors: []string{"401", "403", "404", "409", "503"}},
	"pluginV5Handler.Operation":            {ResponseSchema: "PluginV5Operation", NoBody: true, AdditionalErrors: []string{"401", "403", "404", "503"}},
	"pluginV5Handler.Mutate":               {RequestSchema: "PluginV5UserCommand", ResponseSchema: "PluginV5Operation", AdditionalErrors: []string{"401", "403", "404", "409", "503"}},
	"pluginV5Handler.Import":               {ContentType: "multipart/form-data", RequestSchema: "PluginV5Import", ResponseSchema: "PluginV5Operation", AdditionalErrors: []string{"401", "403", "409", "413", "503"}},
	"plugin.V5RetiredHTTP":                 {SuccessStatus: "410", NoBody: true},
	"d.Identity.Integration.List":          {ResponseSchema: "IntegrationInventory", NoBody: true, AdditionalErrors: []string{"401", "403", "404", "409", "503"}},
	"d.Identity.Integration.Endpoints":     {ResponseSchema: "IntegrationEndpointInventory", NoBody: true, AdditionalErrors: []string{"401", "403", "404", "409", "503"}},
	"d.Identity.Integration.Me":            {ResponseSchema: "IntegrationCurrentSelf", NoBody: true, AdditionalErrors: []string{"401", "403", "404", "409", "503"}},
	"d.Identity.Integration.Create":        {RequestSchema: "IntegrationCreateRequest", ResponseSchema: "IntegrationPrincipal", SuccessStatus: "201", AdditionalErrors: []string{"401", "403", "404", "409", "503"}},
	"d.Identity.Integration.Update":        {RequestSchema: "IntegrationUpdateRequest", ResponseSchema: "IntegrationPrincipal", AdditionalErrors: []string{"401", "403", "404", "409", "503"}},
	"d.Identity.Integration.Issue":         {RequestSchema: "IntegrationIssueRequest", ResponseSchema: "IntegrationIssueResponse", SuccessStatus: "201", AdditionalErrors: []string{"401", "403", "404", "409", "503"}},
	"d.Identity.Integration.Rotate":        {RequestSchema: "IntegrationRotateRequest", ResponseSchema: "IntegrationIssueResponse", SuccessStatus: "201", AdditionalErrors: []string{"401", "403", "404", "409", "503"}},
	"d.Identity.Integration.Scope":         {RequestSchema: "IntegrationScopeRequest", ResponseSchema: "IntegrationCredential", AdditionalErrors: []string{"401", "403", "404", "409", "503"}},
	"d.Identity.Integration.Revoke":        {RequestSchema: "IntegrationVersionRequest", ResponseSchema: "IntegrationCredential", AdditionalErrors: []string{"401", "403", "404", "409", "503"}},
	"integrationHandler.IntegrationThread": {ResponseSchema: "IntegrationPublicThread", NoBody: true, AdditionalErrors: []string{"401", "403", "404", "409", "503"}},
	"integrationHandler.IntegrationMCP":    {RequestSchema: "IntegrationPublicThreadRequest", ResponseSchema: "IntegrationPublicThread", AdditionalErrors: []string{"401", "403", "404", "409", "503"}},
	"d.Identity.AdminDomain.Login":         {RequestSchema: "AdminDomainLoginRequest", ResponseSchema: "AdminDomainTokenResponse", AdditionalErrors: []string{"401", "503"}},
	"d.Identity.AdminDomain.Refresh":       {RequestSchema: "AdminDomainRefreshRequest", ResponseSchema: "AdminDomainTokenResponse", RequestOptional: true, AdditionalErrors: []string{"401", "403", "503"}},
	"d.Identity.AdminDomain.Me":            {ResponseSchema: "AdminCurrentSelf", NoBody: true, AdditionalErrors: []string{"401", "409", "503"}},
	"d.Identity.AdminDomain.Logout":        {RequestSchema: "EmptyObject", ResponseSchema: "AdminOwnLogoutReceipt", RequestOptional: true, AdditionalErrors: []string{"401", "403", "409", "503"}},
	"d.Identity.AdminDomain.EnrollTOTP":    {RequestSchema: "AdminTOTPEnrollRequest", ResponseSchema: "AdminTOTPEnrollment", AdditionalErrors: []string{"401", "403", "409", "503"}},
	"d.Identity.AdminDomain.ConfirmTOTP":   {RequestSchema: "AdminTOTPCodeRequest", ResponseSchema: "AdminTOTPConfirmation", AdditionalErrors: []string{"401", "403", "409", "503"}},
	"d.Identity.AdminDomain.StepUp":        {RequestSchema: "AdminTOTPCodeRequest", ResponseSchema: "AdminStepUpReceipt", AdditionalErrors: []string{"401", "403", "409", "503"}},

	"userHandler.RequestRegistrationChallenge":         {RequestSchema: "RegistrationChallengeRequest", ResponseSchema: "ChallengeReceipt", AdditionalErrors: []string{"429", "503"}},
	"userHandler.VerifyRegistrationChallenge":          {RequestSchema: "RegistrationChallengeVerifyRequest", ResponseSchema: "ChallengeTicket"},
	"userHandler.Register":                             {RequestSchema: "RegisterRequest", ResponseSchema: "User", SuccessStatus: "201"},
	"userHandler.Login":                                {RequestSchema: "LoginRequest", ResponseSchema: "LoginResponse", AdditionalErrors: []string{"401", "503"}},
	"userHandler.AdminLogin":                           {RequestSchema: "LoginRequest", ResponseSchema: "LoginResponse", AdditionalErrors: []string{"401", "503"}},
	"userHandler.Refresh":                              {RequestSchema: "RefreshRequest", ResponseSchema: "RefreshResponse", RequestOptional: true},
	"userHandler.ListSessions":                         {ResponseSchema: "SessionListResponse"},
	"userHandler.RequestPasswordReset":                 {RequestSchema: "PasswordResetChallengeRequest", ResponseSchema: "PasswordResetChallengeResponse"},
	"userHandler.VerifyPasswordReset":                  {RequestSchema: "PasswordResetVerifyRequest", ResponseSchema: "ChallengeTicket"},
	"userHandler.CompletePasswordReset":                {RequestSchema: "PasswordResetCompleteRequest", ResponseSchema: "RecoveryCompletionResponse"},
	"userHandler.RequestEmailBinding":                  {RequestSchema: "EmailBindingChallengeRequest", ResponseSchema: "ChallengeReceipt"},
	"userHandler.VerifyEmailBinding":                   {RequestSchema: "EmailBindingVerifyRequest", ResponseSchema: "ChallengeTicket"},
	"userHandler.CompleteEmailBinding":                 {RequestSchema: "EmailBindingCompleteRequest", ResponseSchema: "RecoveryCompletionResponse"},
	"userHandler.CompleteAdminRecovery":                {RequestSchema: "RecoveryCaseCompleteRequest", ResponseSchema: "RecoveryCompletionResponse"},
	"userHandler.ListAdminRecoveryCases":               {ResponseSchema: "RecoveryCaseListResponse"},
	"userHandler.CreateAdminRecoveryCase":              {RequestSchema: "AdminRecoveryCaseCreateRequest", ResponseSchema: "RecoveryCase", SuccessStatus: "201"},
	"userHandler.ListAdminUserSessions":                {ResponseSchema: "SessionListResponse"},
	"challengePolicyHandler.Get":                       {ResponseSchema: "ChallengePolicy"},
	"challengePolicyHandler.Update":                    {RequestSchema: "ChallengePolicyUpdateRequest", ResponseSchema: "ChallengePolicy"},
	"userHandler.UpdateUser":                           {RequestSchema: "UpdateUserRequest", ResponseSchema: "User"},
	"userHandler.ListUsers":                            {ResponseSchema: "UserListData", Paginated: true},
	"userHandler.ListAdminUsers":                       {ResponseSchema: "AdminUserListData", Paginated: true},
	"userHandler.GetUser":                              {ResponseSchema: "PublicUser"},
	"userHandler.GetMe":                                {ResponseSchema: "User"},
	"threadHandler.CreateThread":                       {RequestSchema: "CreateThreadRequest", ResponseSchema: "Thread", SuccessStatus: "201"},
	"threadHandler.UpdateThread":                       {RequestSchema: "UpdateThreadRequest", ResponseSchema: "Thread"},
	"threadHandler.ListThreads":                        {ResponseSchema: "ThreadListData", Paginated: true},
	"threadHandler.AdminPermissionsPreview":            {RequestSchema: "AdminThreadPermissionsPreviewRequest", ResponseSchema: "AdminThreadPermissionsPreviewData", AdditionalErrors: []string{"401", "403", "404", "409", "503"}},
	"threadHandler.AdminListThreads":                   {ResponseSchema: "AdminThreadMetadataListData", Paginated: true, NoBody: true, AdditionalErrors: []string{"401", "403", "409", "503"}},
	"threadHandler.AdminListTrash":                     {ResponseSchema: "AdminThreadMetadataListData", Paginated: true, NoBody: true, AdditionalErrors: []string{"401", "403", "409", "503"}},
	"threadHandler.AdminModerationActions":             {ResponseSchema: "AdminModerationMetadataListData", Paginated: true, NoBody: true, AdditionalErrors: []string{"401", "403", "404", "409", "503"}},
	"threadHandler.GetThread":                          {ResponseSchema: "Thread"},
	"threadHandler.GetThreadForCurrentUser":            {ResponseSchema: "Thread"},
	"threadHandler.PreviewContent":                     {RequestSchema: "ContentPreviewRequest", ResponseSchema: "ContentPreviewResponse"},
	"userStorageHandler.UploadContentImage":            {ContentType: "multipart/form-data", RequestSchema: "ContentImageUploadRequest", ResponseSchema: "ContentImage", AdditionalErrors: []string{"401", "403", "409", "413", "503"}},
	"richTextHandler.UploadAsset":                      {ContentType: "multipart/form-data", RequestSchema: "InlineRichTextUploadRequest", ResponseSchema: "InlineRichTextAsset", AdditionalErrors: []string{"401", "403", "409", "413", "503"}},
	"postHandler.CreatePost":                           {RequestSchema: "CreatePostRequest", ResponseSchema: "Post", SuccessStatus: "201"},
	"postHandler.UpdatePost":                           {RequestSchema: "UpdatePostRequest", ResponseSchema: "Post"},
	"postHandler.ListPosts":                            {ResponseSchema: "PostListData", Paginated: true},
	"postHandler.ListPostsForCurrentUser":              {ResponseSchema: "PostListData", Paginated: true},
	"notificationHandler.List":                         {ResponseSchema: "NotificationListData", Paginated: true},
	"categoryHandler.Create":                           {RequestSchema: "CreateCategoryRequest", ResponseSchema: "Category", SuccessStatus: "201"},
	"categoryHandler.Update":                           {RequestSchema: "UpdateCategoryRequest", ResponseSchema: "Category"},
	"categoryHandler.Get":                              {ResponseSchema: "Category"},
	"categoryHandler.ListThreadTypePolicies":           {ResponseSchema: "CategoryThreadTypePolicies"},
	"categoryHandler.UpdateThreadTypePolicies":         {RequestSchema: "UpdateCategoryThreadTypePolicyRequest", ResponseSchema: "CategoryThreadTypePolicyUpdate"},
	"spaceHandler.UpdateMe":                            {RequestSchema: "UpsertSpaceRequest", ResponseSchema: "SpaceMutationAck", AdditionalErrors: []string{"401", "403", "409", "503"}},
	"spaceHandler.GetByUserID":                         {ResponseSchema: "SpacePublicProfile", NoBody: true, AdditionalErrors: []string{"403", "404", "409", "503"}},
	"spaceHandler.GetByUsername":                       {ResponseSchema: "SpacePublicProfile", NoBody: true, AdditionalErrors: []string{"403", "404", "409", "503"}},
	"spaceHandler.ListOwnContents":                     {ResponseSchema: "SpaceOwnerContentListData", Paginated: true, NoBody: true, AdditionalErrors: []string{"401", "403", "409", "503"}},
	"spaceHandler.GetSyncStatus":                       {ResponseSchema: "SpaceCurrentSyncStatus", NoBody: true, AdditionalErrors: []string{"401", "403", "409", "503"}},
	"spaceHandler.GetMe":                               {ResponseSchema: "PublicSpace", NoBody: true, AdditionalErrors: []string{"401", "403", "409", "503"}},
	"spaceHandler.ValidateStylePackage":                {RequestSchema: "PersonalSpaceStylePackage", ResponseSchema: "SpaceStyleReadValidation", AdditionalErrors: []string{"401", "403", "409", "413", "503"}},
	"spaceHandler.PreviewStylePackage":                 {RequestSchema: "PersonalSpaceStylePackage", ResponseSchema: "SpaceStyleReadPreview", AdditionalErrors: []string{"401", "403", "409", "413", "503"}},
	"spaceHandler.ValidateCustomHTML":                  {RequestSchema: "PersonalSpaceStyleHTMLRequest", ResponseSchema: "SpaceStyleReadValidation", AdditionalErrors: []string{"401", "403", "409", "413", "503"}},
	"spaceHandler.CustomHTMLExample":                   {NoBody: true, ResponseSchema: "SpaceStyleHTMLExample", AdditionalErrors: []string{"401", "403", "409", "503"}},
	"spaceHandler.ValidateStylePackZip":                {RequestSchema: "SpaceStyleReadZipRequest", ResponseSchema: "SpaceStyleZipValidation", AdditionalErrors: []string{"401", "403", "409", "413", "503"}},
	"spaceHandler.StylePackExample":                    {NoBody: true, ResponseSchema: "SpaceStylePackExample", AdditionalErrors: []string{"401", "403", "409", "503"}},
	"spaceHandler.StylePackExampleZip":                 {NoBody: true, ResponseContentType: "application/zip", AdditionalErrors: []string{"401", "403", "409", "503"}},
	"spaceHandler.ListSourceStylePacks":                {ResponseSchema: "SpaceSourceStylePackList", NoBody: true, AdditionalErrors: []string{"401", "403", "409", "503"}},
	"spaceHandler.SelectAvatar":                        {RequestSchema: "AvatarSelectionRequest", ResponseSchema: "SpaceMutationAck", AdditionalErrors: []string{"401", "403", "404", "409", "503"}},
	"spaceHandler.StorageStatus":                       {ResponseSchema: "SpaceStorageStatus"},
	"spaceHandler.UploadAvatar":                        {ResponseSchema: "AvatarMutationAck", AdditionalErrors: []string{"401", "403", "409", "413", "503"}},
	"spaceHandler.CleanupAvatarHistory":                {ResponseSchema: "AvatarCleanupAck", AdditionalErrors: []string{"401", "403", "409", "503"}},
	"spaceHandler.ApplyStylePackage":                   {RequestSchema: "PersonalSpaceStylePackage", ResponseSchema: "SpaceMutationAck", AdditionalErrors: []string{"401", "403", "409", "503"}},
	"spaceHandler.ApplyCustomHTML":                     {RequestSchema: "PersonalSpaceStyleHTMLRequest", ResponseSchema: "SpaceMutationAck", AdditionalErrors: []string{"401", "403", "409", "413", "503"}},
	"spaceHandler.ApplyStylePackZip":                   {ResponseSchema: "SpaceMutationAck", AdditionalErrors: []string{"401", "403", "409", "413", "503"}},
	"spaceHandler.RollbackStyle":                       {NoBody: true, ResponseSchema: "SpaceMutationAck", AdditionalErrors: []string{"401", "403", "404", "409", "503"}},
	"spaceHandler.RestoreDefaultStyle":                 {NoBody: true, ResponseSchema: "SpaceMutationAck", AdditionalErrors: []string{"401", "403", "409", "503"}},
	"spaceHandler.ListAvatars":                         {ResponseSchema: "AvatarHistory"},
	"spaceHandler.AdminSummary":                        {NoBody: true, ResponseSchema: "SpaceAdminSummary", AdditionalErrors: []string{"401", "403", "409", "503"}},
	"spaceHandler.AdminStorageStatus":                  {NoBody: true, ResponseSchema: "SpaceStorageStatus", AdditionalErrors: []string{"401", "403", "404", "409", "503"}},
	"spaceHandler.SetStorageQuota":                     {RequestSchema: "SetStorageQuotaRequest", ResponseSchema: "SpaceQuotaMutationAck", AdditionalErrors: []string{"401", "403", "404", "409", "503"}},
	"spaceHandler.ListContentsByUserID":                {ResponseSchema: "SpacePublicContentListData", Paginated: true, NoBody: true, AdditionalErrors: []string{"403", "404", "409", "503"}},
	"spaceHandler.ListContentsByUsername":              {ResponseSchema: "SpacePublicContentListData", Paginated: true, NoBody: true, AdditionalErrors: []string{"403", "404", "409", "503"}},
	"scheduleHandler.ActivateTerm":                     {RequestSchema: "ActivateTermRequest"},
	"scheduleHandler.SaveMe":                           {RequestSchema: "ScheduleUpsertRequest"},
	"richTextHandler.CreateDraft":                      {RequestSchema: "RichtextSaveRequest", ResponseSchema: "RichtextArticle", SuccessStatus: "201"},
	"richTextHandler.UpdateDraft":                      {RequestSchema: "RichtextSaveRequest", ResponseSchema: "RichtextArticle"},
	"richTextHandler.AdminOffline":                     {RequestSchema: "RichtextGovernanceReason", RequestOptional: true, ResponseSchema: "RichtextGovernanceAck", AdditionalErrors: []string{"401", "403", "409", "503"}},
	"richTextHandler.AdminRestore":                     {RequestSchema: "RichtextGovernanceReason", RequestOptional: true, ResponseSchema: "RichtextGovernanceAck", AdditionalErrors: []string{"401", "403", "409", "503"}},
	"richTextHandler.AdminDelete":                      {RequestSchema: "RichtextGovernanceReason", RequestOptional: true, SuccessStatus: "204", AdditionalErrors: []string{"401", "403", "409", "503"}},
	"richTextHandler.BindAttachment":                   {RequestSchema: "AttachmentBindRequest", ResponseSchema: "AttachmentMutationAck", SuccessStatus: "201", AdditionalErrors: []string{"401", "403", "409", "503"}},
	"richTextHandler.UploadAttachment":                 {ContentType: "multipart/form-data", RequestSchema: "AttachmentUploadRequest", ResponseSchema: "AttachmentMutationAck", SuccessStatus: "201", AdditionalErrors: []string{"401", "403", "404", "409", "413", "503"}},
	"richTextHandler.AttachPersonalDocument":           {RequestSchema: "AttachmentDocumentCopyRequest", ResponseSchema: "AttachmentMutationAck", SuccessStatus: "201", AdditionalErrors: []string{"401", "403", "404", "409", "413", "503"}},
	"richTextHandler.TrashUserAsset":                   {ResponseSchema: "AssetMutationAck", NoBody: true, AdditionalErrors: []string{"401", "403", "404", "409", "503"}},
	"richTextHandler.RestoreUserAsset":                 {ResponseSchema: "AssetMutationAck", NoBody: true, AdditionalErrors: []string{"401", "403", "404", "409", "503"}},
	"richTextHandler.AdminQuarantineUserAsset":         {RequestSchema: "AssetAdminLifecycleRequest", ResponseSchema: "AssetAdminMutationAck", AdditionalErrors: []string{"401", "403", "404", "409", "503"}},
	"richTextHandler.AdminRestoreQuarantinedUserAsset": {RequestSchema: "AssetAdminLifecycleRequest", ResponseSchema: "AssetAdminMutationAck", AdditionalErrors: []string{"401", "403", "404", "409", "503"}},
	"richTextHandler.AssetGovernanceSummary":           {ResponseSchema: "AssetGovernanceSummary", NoBody: true, AdditionalErrors: []string{"401", "403", "409", "503"}},
	"richTextHandler.AdminPreviewUserAssetPurge":       {ResponseSchema: "AssetPurgePreview", NoBody: true, AdditionalErrors: []string{"401", "403", "404", "409", "503"}},
	"richTextHandler.AdminPurgeUserAsset":              {RequestSchema: "AssetPurgeRequest", ResponseSchema: "AssetPurgeAck", AdditionalErrors: []string{"401", "403", "404", "409", "503"}},
	"richTextHandler.RenameAttachment":                 {RequestSchema: "AttachmentRenameRequest", ResponseSchema: "AttachmentMutationAck", AdditionalErrors: []string{"401", "403", "409", "503"}},
	"richTextHandler.ReorderAttachments":               {RequestSchema: "AttachmentReorderRequest", ResponseSchema: "AttachmentMutationAck", AdditionalErrors: []string{"401", "403", "409", "503"}},
	"richTextHandler.RemoveAttachment":                 {NoBody: true, SuccessStatus: "204", AdditionalErrors: []string{"401", "403", "409", "503"}},
	"richTextHandler.Preview":                          {RequestSchema: "RichtextSaveRequest"},
	"roleHandler.AssignRole":                           {RequestSchema: "RoleAssignmentRequest"},
	"roleHandler.RevokeRole":                           {RequestSchema: "RoleRevokeRequest"},
	"moderationHandler.SetModerator":                   {RequestSchema: "ModeratorAssignmentRequest"},
	"moderationHandler.MyAccess":                       {ResponseSchema: "UserModerationAccess", AdditionalErrors: []string{"401", "409", "503"}},
	"moderationHandler.PreviewBatch":                   {RequestSchema: "ModerationPreviewBatchRequest", ResponseSchema: "UserModerationPreview", AdditionalErrors: []string{"401", "409", "503"}},
	"moderationHandler.Pin":                            {ResponseSchema: "ThreadFlagAck", AdditionalErrors: []string{"401", "403", "409", "503"}},
	"moderationHandler.Unpin":                          {ResponseSchema: "ThreadFlagAck", AdditionalErrors: []string{"401", "403", "409", "503"}},
	"moderationHandler.Lock":                           {ResponseSchema: "ThreadFlagAck", AdditionalErrors: []string{"401", "403", "409", "503"}},
	"moderationHandler.Unlock":                         {ResponseSchema: "ThreadFlagAck", AdditionalErrors: []string{"401", "403", "409", "503"}},
	"pluginHandler.RollbackVersionSnapshot":            {RequestSchema: "PluginRollbackRequest"},
	"pluginHandler.UpdatePluginConfig":                 {RequestSchema: "GenericObject"},
	"webhookHandler.CreateEndpoint":                    {RequestSchema: "GenericObject"},
	"mcpHandler.CallTool":                              {RequestSchema: "GenericObject"},
	"mcpHandler.UpdateSettings":                        {RequestSchema: "GenericObject"},
	"messageHandler.ReceiveLocal":                      {RequestSchema: "GenericObject"},
	"messageHandler.CreateBinding":                     {RequestSchema: "GenericObject"},
	"spaceHandler.ApplySourceStylePack":                {RequestSchema: "StylePackSourceRequest", ResponseSchema: "SpaceMutationAck", AdditionalErrors: []string{"401", "403", "409", "503"}},
	"homepageHandler.ApplySourceStylePack":             {RequestSchema: "StylePackSourceRequest"},
	"spaceHandler.ExportStylePackage":                  {RequestSchema: "StyleExportRequest", RequestOptional: true, ResponseSchema: "SpaceStyleExport", AdditionalErrors: []string{"401", "403", "409", "413", "503"}},
	"spaceHandler.DisableSpace":                        {RequestSchema: "DisableSpaceRequest", RequestOptional: true, ResponseSchema: "SpaceAdminMutationAck", AdditionalErrors: []string{"401", "403", "404", "409", "503"}},
	"spaceHandler.EnableSpace":                         {NoBody: true, ResponseSchema: "SpaceAdminMutationAck", AdditionalErrors: []string{"401", "403", "404", "409", "503"}},
}

var multipartHandlers = map[string]bool{
	"spaceHandler.UploadAvatar":             true,
	"spaceHandler.ValidateStylePackZip":     true,
	"spaceHandler.ApplyStylePackZip":        true,
	"scheduleHandler.ImportMe":              true,
	"richTextHandler.UploadAsset":           true,
	"userStorageHandler.UploadContentImage": true,
	"pluginHandler.ImportPluginPackage":     true,
	"pluginHandler.PrecheckPluginPackage":   true,
	"homepageHandler.ValidateStylePack":     true,
	"homepageHandler.ApplyStylePack":        true,
}

var noBodyHandlers = map[string]bool{
	"spaceHandler.RollbackStyle":          true,
	"spaceHandler.RestoreDefaultStyle":    true,
	"richTextHandler.Publish":             true,
	"richTextHandler.Offline":             true,
	"moderationHandler.Pin":               true,
	"moderationHandler.Unpin":             true,
	"moderationHandler.Lock":              true,
	"moderationHandler.Unlock":            true,
	"userHandler.SuspendUser":             true,
	"userHandler.ActivateUser":            true,
	"threadHandler.PinThread":             true,
	"threadHandler.UnpinThread":           true,
	"threadHandler.LockThread":            true,
	"threadHandler.UnlockThread":          true,
	"pluginHandler.EnablePlugin":          true,
	"pluginHandler.DisablePlugin":         true,
	"pluginHandler.ReloadUserPlugin":      true,
	"spaceHandler.EnableSpace":            true,
	"webhookHandler.TestEndpoint":         true,
	"webhookHandler.EnableEndpoint":       true,
	"webhookHandler.DisableEndpoint":      true,
	"homepageHandler.RollbackStylePack":   true,
	"userHandler.Logout":                  true,
	"userHandler.LogoutAll":               true,
	"userHandler.RevokeSession":           true,
	"userHandler.CancelAdminRecoveryCase": true,
	"userHandler.RevokeAdminUserSessions": true,
	"notificationHandler.MarkRead":        true,
	"notificationHandler.MarkAllRead":     true,
}

var noContentHandlers = map[string]bool{
	"richTextHandler.Delete":          true,
	"threadHandler.DeleteThread":      true,
	"threadHandler.AdminDeleteThread": true,
	"postHandler.DeletePost":          true,
	"moderationHandler.DeletePost":    true,
	"categoryHandler.Delete":          true,
	"pluginHandler.UninstallPlugin":   true,
	"richTextHandler.AdminDelete":     true,
	"notificationHandler.MarkRead":    true,
}

func profileFor(route RouteContract) operationProfile {
	profile := operationProfiles[route.Handler]
	if route.Handler == "pluginV5Handler.Mutate" && route.Audience == "admin" {
		profile.RequestSchema = "PluginV5AdminCommand"
	}
	if multipartHandlers[route.Handler] {
		profile.ContentType = "multipart/form-data"
		if profile.RequestSchema == "" {
			profile.RequestSchema = "MultipartRequest"
		}
	}
	if noBodyHandlers[route.Handler] {
		profile.NoBody = true
	}
	if noContentHandlers[route.Handler] {
		profile.SuccessStatus = "204"
		if profile.RequestSchema == "" {
			profile.NoBody = true
		}
	}
	if profile.SuccessStatus == "" {
		profile.SuccessStatus = "200"
	}
	if profile.ContentType == "" {
		profile.ContentType = "application/json"
	}
	if profile.RequestSchema == "" && !profile.NoBody && (route.Method == "POST" || route.Method == "PUT" || route.Method == "PATCH") {
		profile.RequestSchema = "GenericObject"
	}
	return profile
}

func openAPIComponents() string {
	return `components:
  securitySchemes:
    bearerAuth:
      type: http
      scheme: bearer
      bearerFormat: JWT
    refreshCookie:
      type: apiKey
      in: cookie
      name: campusos_refresh
    adminRefreshCookie:
      type: apiKey
      in: cookie
      name: campusos_admin_refresh
    integrationCredential:
      type: apiKey
      in: header
      name: Authorization
      description: "Integration cint1_<selector>.<secret>; independent of JWT and ApiKey."
  schemas:
` + integrationOpenAPISchemas() + v5InstallationOpenAPISchemas() + `    EmptyObject:
      type: object
      additionalProperties: false
    AdminDomainLoginRequest:
      type: object
      additionalProperties: false
      required: [username, password]
      properties:
        username: {type: string, description: Independent Admin username or contact email}
        password: {type: string, format: password}
    AdminDomainRefreshRequest:
      type: object
      additionalProperties: false
      properties:
        refresh_token: {type: string, description: Explicit non-browser CLI compatibility only; browser uses HttpOnly Admin cookie and Admin CSRF header}
    AdminDomainTokenResponse:
      type: object
      additionalProperties: false
      required: [access_token, token_type, expires_at]
      properties:
        access_token: {type: string}
        token_type: {type: string, enum: [Bearer]}
        expires_at: {type: string, format: date-time}
        refresh_token: {type: string, description: Omitted for browser and default configuration; explicit CLI compatibility only}
    AdminCurrentSelf:
      type: object
      additionalProperties: false
      required: [domain, account, roles, permission_codes, authentication_strength, mfa_enrolled, session_expires_at]
      properties:
        domain: {type: string, enum: [admin]}
        account: {$ref: '#/components/schemas/AdminSelfAccount'}
        roles: {type: array, items: {$ref: '#/components/schemas/AdminSelfRole'}}
        permission_codes: {type: array, items: {type: string}}
        authentication_strength: {type: string, enum: [password, mfa]}
        mfa_enrolled: {type: boolean}
        session_expires_at: {type: string, format: date-time}
        mfa_expires_at: {type: string, format: date-time}
    AdminSelfAccount:
      type: object
      additionalProperties: false
      required: [id, username, display_name, status]
      properties:
        id: {type: string}
        username: {type: string}
        display_name: {type: string}
        status: {type: string, enum: [active]}
    AdminSelfRole:
      type: object
      additionalProperties: false
      required: [id, name]
      properties:
        id: {type: string}
        name: {type: string}
    AdminTOTPEnrollRequest:
      type: object
      additionalProperties: false
      properties:
        issuer: {type: string, maxLength: 64}
    AdminTOTPCodeRequest:
      type: object
      additionalProperties: false
      required: [code]
      properties:
        code: {type: string, pattern: '^[0-9]{6}$'}
    AdminTOTPEnrollment:
      type: object
      additionalProperties: false
      required: [otpauth_url]
      properties:
        otpauth_url: {type: string, description: Shown once for current Admin enrollment; never persist in browser storage}
    AdminTOTPConfirmation:
      type: object
      additionalProperties: false
      required: [status]
      properties:
        status: {type: string, enum: [active]}
    AdminStepUpReceipt:
      type: object
      additionalProperties: false
      required: [authentication_strength]
      properties:
        authentication_strength: {type: string, enum: [mfa]}
    AdminOwnLogoutReceipt:
      type: object
      additionalProperties: false
      required: [revoked]
      properties:
        revoked: {type: boolean, enum: [true]}
    Envelope:
      type: object
      required: [code, msg]
      properties:
        code: { type: integer }
        msg: { type: string }
        data: {}
        request_id: { type: string }
    StructuredError:
      type: object
      required: [code, message, retryable]
      properties:
        code: { type: string, example: permission.denied }
        message: { type: string }
        details: {}
        request_id: { type: string }
        retryable: { type: boolean }
    ErrorEnvelope:
      type: object
      required: [code, msg, error]
      properties:
        code: { type: integer }
        msg: { type: string }
        error: { $ref: '#/components/schemas/StructuredError' }
        request_id: { type: string }
    Pagination:
      type: object
      required: [page, page_size, total, total_pages]
      properties:
        page: { type: integer, minimum: 1 }
        page_size: { type: integer, minimum: 1, maximum: 100 }
        total: { type: integer, format: int64, minimum: 0 }
        total_pages: { type: integer, minimum: 0 }
    GenericObject: { type: object, additionalProperties: true }
    MultipartRequest: { type: object, additionalProperties: true }
    User:
      type: object
      required: [id, username, nickname, status, created_at, updated_at]
      properties:
        id: { type: string }
        username: { type: string }
        nickname: { type: string }
        email: { type: string, format: email }
        avatar: { type: string }
        bio: { type: string }
        status: { type: string, enum: [active, suspended, deactivated] }
        created_at: { type: string, format: date-time }
        updated_at: { type: string, format: date-time }
    PublicUser:
      type: object
      required: [id, username, nickname, status, created_at, updated_at]
      properties:
        id: { type: string }
        username: { type: string }
        nickname: { type: string }
        avatar: { type: string }
        bio: { type: string }
        status: { type: string, enum: [active, suspended, deactivated] }
        created_at: { type: string, format: date-time }
        updated_at: { type: string, format: date-time }
    AdminUser:
      type: object
      required: [id, username, nickname, status, created_at, updated_at]
      properties:
        id: { type: string }
        username: { type: string }
        nickname: { type: string }
        email: { type: string, format: email }
        avatar: { type: string }
        status: { type: string, enum: [active, suspended, deactivated] }
        created_at: { type: string, format: date-time }
        updated_at: { type: string, format: date-time }
    RegisterRequest:
      type: object
      required: [username, nickname, email, password, challenge_id, ticket]
      properties:
        username: { type: string, minLength: 3, maxLength: 32 }
        nickname: { type: string, minLength: 1, maxLength: 64 }
        email: { type: string, format: email }
        password: { type: string, minLength: 6, maxLength: 64, writeOnly: true }
        challenge_id: { type: string, minLength: 16, maxLength: 256 }
        ticket: { type: string, minLength: 32, writeOnly: true }
      additionalProperties: false
    RegistrationChallengeRequest:
      type: object
      required: [email]
      properties:
        email: { type: string, format: email }
      additionalProperties: false
    RegistrationChallengeVerifyRequest:
      type: object
      required: [challenge_id, code]
      properties:
        challenge_id: { type: string, minLength: 16, maxLength: 256 }
        code: { type: string, minLength: 6, maxLength: 6, pattern: '^[0-9]{6}$', writeOnly: true }
      additionalProperties: false
    ChallengeReceipt:
      type: object
      required: [challenge_id, purpose, expires_at]
      properties:
        challenge_id: { type: string }
        purpose: { type: string, enum: [registration, email_binding, password_reset] }
        expires_at: { type: string, format: date-time }
    ChallengeTicket:
      type: object
      required: [challenge_id, purpose, ticket, expires_at]
      properties:
        challenge_id: { type: string }
        purpose: { type: string, enum: [registration, email_binding, password_reset] }
        ticket: { type: string, writeOnly: true }
        expires_at: { type: string, format: date-time }
    PasswordResetChallengeRequest:
      type: object
      required: [email]
      properties:
        email: { type: string, format: email }
      additionalProperties: false
    PasswordResetChallengeResponse:
      type: object
      required: [accepted, challenge_id]
      properties:
        accepted: { type: boolean }
        challenge_id: { type: string }
    PasswordResetVerifyRequest:
      type: object
      required: [challenge_id, code]
      properties:
        challenge_id: { type: string, minLength: 16, maxLength: 256 }
        code: { type: string, minLength: 6, maxLength: 6, pattern: '^[0-9]{6}$', writeOnly: true }
      additionalProperties: false
    PasswordResetCompleteRequest:
      type: object
      required: [email, challenge_id, ticket, password]
      properties:
        email: { type: string, format: email }
        challenge_id: { type: string, minLength: 16, maxLength: 256 }
        ticket: { type: string, minLength: 16, maxLength: 256, writeOnly: true }
        password: { type: string, minLength: 6, maxLength: 64, writeOnly: true }
      additionalProperties: false
    EmailBindingChallengeRequest:
      type: object
      required: [email]
      properties:
        email: { type: string, format: email }
      additionalProperties: false
    EmailBindingVerifyRequest:
      type: object
      required: [challenge_id, code]
      properties:
        challenge_id: { type: string, minLength: 16, maxLength: 256 }
        code: { type: string, minLength: 6, maxLength: 6, pattern: '^[0-9]{6}$', writeOnly: true }
      additionalProperties: false
    EmailBindingCompleteRequest:
      type: object
      required: [email, challenge_id, ticket]
      properties:
        email: { type: string, format: email }
        challenge_id: { type: string, minLength: 16, maxLength: 256 }
        ticket: { type: string, minLength: 16, maxLength: 256, writeOnly: true }
      additionalProperties: false
    RecoveryCaseCompleteRequest:
      type: object
      required: [challenge_id, ticket, password]
      properties:
        challenge_id: { type: string, minLength: 16, maxLength: 256 }
        ticket: { type: string, minLength: 16, maxLength: 256, writeOnly: true }
        password: { type: string, minLength: 6, maxLength: 64, writeOnly: true }
      additionalProperties: false
    RecoveryCompletionResponse:
      type: object
      required: [relogin_required]
      properties:
        reset: { type: boolean }
        bound: { type: boolean }
        relogin_required: { type: boolean }
    AdminRecoveryCaseCreateRequest:
      type: object
      required: [user_id, email, proof_reference]
      properties:
        user_id: { type: string, pattern: '^[0-9]+$' }
        email: { type: string, format: email }
        proof_reference: { type: string, minLength: 3, maxLength: 160, writeOnly: true }
      additionalProperties: false
    RecoveryCase:
      type: object
      required: [id, user_id, target_email_masked, status, expires_at, created_at]
      properties:
        id: { type: string }
        user_id: { type: string }
        target_email_masked: { type: string }
        status: { type: string, enum: [pending, completed, cancelled, expired] }
        expires_at: { type: string, format: date-time }
        completed_at: { type: string, format: date-time, nullable: true }
        cancelled_at: { type: string, format: date-time, nullable: true }
        created_at: { type: string, format: date-time }
    RecoveryCaseListResponse:
      type: object
      required: [items, total]
      properties:
        items: { type: array, items: { $ref: '#/components/schemas/RecoveryCase' } }
        total: { type: integer, minimum: 0 }
    ChallengePolicy:
      type: object
      required: [id, email_window_minutes, email_max_requests, ip_window_minutes, ip_max_requests, version, updated_at]
      properties:
        id: { type: string, enum: [email_verification] }
        email_window_minutes: { type: integer, minimum: 1, maximum: 1440 }
        email_max_requests: { type: integer, minimum: 1, maximum: 100 }
        ip_window_minutes: { type: integer, minimum: 1, maximum: 1440 }
        ip_max_requests: { type: integer, minimum: 1, maximum: 1000 }
        version: { type: integer, minimum: 1 }
        updated_by: { type: string }
        updated_at: { type: string, format: date-time }
    ChallengePolicyUpdateRequest:
      type: object
      required: [email_window_minutes, email_max_requests, ip_window_minutes, ip_max_requests, expected_version]
      properties:
        email_window_minutes: { type: integer, minimum: 1, maximum: 1440 }
        email_max_requests: { type: integer, minimum: 1, maximum: 100 }
        ip_window_minutes: { type: integer, minimum: 1, maximum: 1440 }
        ip_max_requests: { type: integer, minimum: 1, maximum: 1000 }
        expected_version: { type: integer, minimum: 1 }
      additionalProperties: false
    LoginRequest:
      type: object
      required: [email, password]
      properties:
        email: { type: string, format: email }
        password: { type: string, writeOnly: true }
      additionalProperties: false
    UpdateUserRequest:
      type: object
      properties:
        nickname: { type: string, maxLength: 64 }
        bio: { type: string, maxLength: 500 }
        avatar: { type: string, maxLength: 512 }
      additionalProperties: false
    LoginResponse:
      type: object
      required: [user, roles, access_token, token_type, expires_in]
      properties:
        user: { $ref: '#/components/schemas/User' }
        roles: { type: array, items: { $ref: '#/components/schemas/Role' } }
        access_token: { type: string }
        refresh_token: { type: string, deprecated: true, description: Present only when AUTH_REFRESH_BODY_COMPAT=true. }
        token_type: { type: string, enum: [Bearer] }
        expires_in: { type: integer }
    RefreshRequest:
      type: object
      properties:
        refresh_token: { type: string, writeOnly: true, deprecated: true }
      additionalProperties: false
    RefreshResponse:
      type: object
      required: [access_token, token_type, expires_in]
      properties:
        access_token: { type: string }
        refresh_token: { type: string, deprecated: true, description: Present only when AUTH_REFRESH_BODY_COMPAT=true. }
        token_type: { type: string, enum: [Bearer] }
        expires_in: { type: integer }
    Session:
      type: object
      required: [id, current, last_active_at, expires_at, created_at]
      properties:
        id: { type: string }
        current: { type: boolean }
        device_name: { type: string }
        device_type: { type: string }
        last_active_at: { type: string, format: date-time }
        expires_at: { type: string, format: date-time }
        revoked_at: { type: string, format: date-time, nullable: true }
        created_at: { type: string, format: date-time }
    SessionListResponse:
      type: object
      required: [items]
      properties:
        items: { type: array, items: { $ref: '#/components/schemas/Session' } }
    Role:
      type: object
      required: [id, name]
      properties:
        id: { type: integer, format: int64 }
        name: { type: string }
        description: { type: string }
    ModerationPreviewBatchRequest:
      type: object
      additionalProperties: false
      properties:
        post_ids:
          type: array
          maxItems: 50
          uniqueItems: true
          items: { type: string, minLength: 1, maxLength: 128 }
    UserModerationPreview:
      type: object
      additionalProperties: false
      required: [thread_id, pin, lock, posts]
      properties:
        thread_id: { type: string }
        pin: { type: boolean }
        lock: { type: boolean }
        posts:
          type: array
          maxItems: 50
          items:
            type: object
            additionalProperties: false
            required: [id, delete]
            properties:
              id: { type: string }
              delete: { type: boolean }
    UserModerationAccess:
      type: object
      additionalProperties: false
      required: [thread_id, can_moderate, actions]
      properties:
        thread_id: { type: string }
        can_moderate: { type: boolean }
        actions:
          type: object
          additionalProperties: false
          required: [pin, lock, delete_post]
          properties:
            pin: { type: boolean }
            lock: { type: boolean }
            delete_post: { type: boolean, enum: [false] }
    AttachmentMutationAck:
      type: object
      additionalProperties: false
      required: [thread_id]
      properties:
        thread_id: { type: string }
        attachment_id: { type: string }
    AssetMutationAck:
      type: object
      additionalProperties: false
      required: [asset_id, status]
      properties:
        asset_id: { type: string }
        status: { type: string, enum: [active, trashed] }
    AssetGovernanceSummary:
      type: object
      additionalProperties: false
      required: [statuses, actions, expired_invocations, generated_at]
      properties:
        statuses:
          type: array
          maxItems: 5
          items:
            type: object
            additionalProperties: false
            required: [status, count, size_bytes]
            properties:
              status: { type: string, enum: [active, trashed, quarantined, purging, deleted] }
              count: { type: integer, minimum: 0 }
              size_bytes: { type: integer, minimum: 0 }
        actions:
          type: array
          maxItems: 6
          items:
            type: object
            additionalProperties: false
            required: [action, count]
            properties:
              action: { type: string, enum: [trashed, restored, quarantined, purge_started, purge_failed, purged] }
              count: { type: integer, minimum: 0 }
        expired_invocations: { type: integer, minimum: 0 }
        generated_at: { type: string, format: date-time }
    AssetPurgeRequest:
      type: object
      additionalProperties: false
      required: [reason, confirm_asset_id]
      properties:
        reason: { type: string, minLength: 1, maxLength: 500 }
        confirm_asset_id: { type: string, minLength: 1 }
    AssetPurgeAck:
      type: object
      additionalProperties: false
      required: [asset_id, status]
      properties:
        asset_id: { type: string }
        status: { type: string, enum: [deleted] }
    AssetPurgePreview:
      type: object
      additionalProperties: false
      required: [asset_id, status, size_bytes, reference_count, eligible, reason]
      properties:
        asset_id: { type: string }
        status: { type: string, enum: [active, trashed, quarantined, purging, deleted] }
        size_bytes: { type: integer, minimum: 0 }
        reference_count: { type: integer, minimum: 0 }
        eligible: { type: boolean }
        reason: { type: string }
    AssetAdminMutationAck:
      type: object
      additionalProperties: false
      required: [asset_id, status]
      properties:
        asset_id: { type: string }
        status: { type: string, enum: [active, quarantined] }
    AssetAdminLifecycleRequest:
      type: object
      additionalProperties: false
      required: [reason]
      properties:
        reason: { type: string, minLength: 1, maxLength: 500 }
    AttachmentBindRequest:
      type: object
      additionalProperties: false
      required: [asset_id]
      properties:
        asset_id: { type: string, minLength: 1 }
        display_name: { type: string, maxLength: 255 }
    AttachmentUploadRequest:
      type: object
      additionalProperties: false
      required: [file]
      properties:
        file: { type: string, format: binary, maxLength: 20971520 }
    AttachmentDocumentCopyRequest:
      type: object
      additionalProperties: false
      required: [document_id]
      properties:
        document_id: { type: string, minLength: 1 }
        display_name: { type: string, maxLength: 255 }
    AttachmentRenameRequest:
      type: object
      additionalProperties: false
      required: [display_name]
      properties:
        display_name: { type: string, minLength: 1, maxLength: 255 }
    AttachmentReorderRequest:
      type: object
      additionalProperties: false
      required: [attachment_ids]
      properties:
        attachment_ids:
          type: array
          minItems: 1
          maxItems: 10
          uniqueItems: true
          items: { type: string, minLength: 1 }
    ThreadFlagAck:
      type: object
      additionalProperties: false
      required: [id, is_pinned, is_locked]
      properties:
        id: { type: string }
        is_pinned: { type: boolean }
        is_locked: { type: boolean }
    Thread:
      type: object
      required: [id, title, content, author_id, category_id, status]
      properties:
        id: { type: string }
        thread_type: { type: string, enum: [discussion, article, mutual_aid, secondhand] }
        title: { type: string }
        content: { type: string }
        content_format: { type: string }
        author_id: { type: string }
        author_name: { type: string }
        category_id: { type: string }
        status: { type: string, enum: [draft, pending_review, published, private, archived] }
        is_pinned: { type: boolean }
        is_locked: { type: boolean }
        tags: { type: array, items: { type: string } }
        created_at: { type: string, format: date-time }
        updated_at: { type: string, format: date-time }
    CreateThreadRequest:
      type: object
      required: [title, content, category_id]
      properties:
        title: { type: string, minLength: 1, maxLength: 255 }
        content: { type: string, minLength: 1 }
        category_id: { type: string }
        tags: { type: array, items: { type: string } }
        is_private: { type: boolean }
      additionalProperties: false
    UpdateThreadRequest:
      type: object
      properties:
        title: { type: string, minLength: 1, maxLength: 255 }
        content: { type: string, minLength: 1 }
        tags: { type: array, items: { type: string } }
        status: { type: string, enum: [draft, pending_review, published, private, archived] }
      additionalProperties: false
    Post:
      type: object
      required: [id, thread_id, author_id, content, status, floor_number]
      properties:
        id: { type: string }
        thread_id: { type: string }
        author_id: { type: string }
        author_name: { type: string }
        parent_id: { type: string, nullable: true }
        parent_floor_number: { type: integer, minimum: 0 }
        content: { type: string }
        status: { type: string }
        floor_number: { type: integer, minimum: 0 }
        created_at: { type: string, format: date-time }
        updated_at: { type: string, format: date-time }
    Notification:
      type: object
      required: [id, user_id, type, title, content, action_url, is_read, metadata, created_at, updated_at]
      properties:
        id: { type: string }
        user_id: { type: string }
        type: { type: string }
        title: { type: string }
        content: { type: string }
        action_url: { type: string }
        is_read: { type: boolean }
        read_at: { type: string, format: date-time, nullable: true }
        metadata: { type: object, additionalProperties: true }
        created_at: { type: string, format: date-time }
        updated_at: { type: string, format: date-time }
    CreatePostRequest:
      type: object
      required: [content]
      properties:
        content: { type: string, minLength: 1 }
        parent_id: { type: string, nullable: true }
      additionalProperties: false
    UpdatePostRequest:
      type: object
      required: [content]
      properties:
        content: { type: string, minLength: 1 }
      additionalProperties: false
    Category:
      type: object
      required: [id, name, slug]
      properties:
        id: { type: string }
        name: { type: string }
        slug: { type: string }
        description: { type: string }
        icon: { type: string }
        default_tags: { type: array, items: { type: string } }
        parent_id: { type: string, nullable: true }
        sort_order: { type: integer }
        is_closed: { type: boolean }
    CreateCategoryRequest:
      type: object
      required: [name]
      properties:
        name: { type: string, minLength: 1, maxLength: 64 }
        slug: { type: string, maxLength: 64 }
        description: { type: string, maxLength: 500 }
        icon: { type: string, maxLength: 512 }
        default_tags: { type: array, items: { type: string } }
        parent_id: { type: string, nullable: true }
        sort_order: { type: integer }
        is_closed: { type: boolean }
      additionalProperties: false
    UpdateCategoryRequest:
      type: object
      properties:
        name: { type: string, minLength: 1, maxLength: 64 }
        slug: { type: string, minLength: 1, maxLength: 64 }
        description: { type: string, maxLength: 500 }
        icon: { type: string, maxLength: 512 }
        default_tags: { type: array, items: { type: string } }
        parent_id: { type: string, nullable: true }
        sort_order: { type: integer }
        is_closed: { type: boolean }
      additionalProperties: false
    CategoryThreadTypePolicy:
      type: object
      required: [category_id, thread_type, enabled]
      properties:
        category_id: { type: string }
        thread_type: { type: string, enum: [discussion, article, mutual_aid, secondhand] }
        enabled: { type: boolean }
        updated_at: { type: string, format: date-time }
    CategoryThreadTypePolicies:
      type: object
      required: [category_id, items]
      properties:
        category_id: { type: string }
        items: { type: array, items: { $ref: '#/components/schemas/CategoryThreadTypePolicy' } }
    UpdateCategoryThreadTypePolicyRequest:
      type: object
      required: [allowed_types, version]
      properties:
        allowed_types:
          type: array
          minItems: 1
          uniqueItems: true
          items: { type: string, enum: [discussion, article, mutual_aid, secondhand] }
        version: { type: integer, minimum: 1 }
      additionalProperties: false
    CategoryThreadTypePolicyUpdate:
      type: object
      required: [category, items]
      properties:
        category: { $ref: '#/components/schemas/Category' }
        items: { type: array, items: { $ref: '#/components/schemas/CategoryThreadTypePolicy' } }
    AvatarSelectionRequest:
      type: object
      required: [file_name]
      properties:
        file_name: { type: string, minLength: 1, maxLength: 255 }
      additionalProperties: false
    SpaceMutationAck:
      type: object
      required: [space_id, owner_id]
      properties:
        space_id: { type: string }
        owner_id: { type: string }
      additionalProperties: false
    SpaceAdminMutationAck:
      type: object
      required: [owner_id, disabled]
      properties:
        owner_id: { type: string }
        space_id: { type: string }
        disabled: { type: boolean }
      additionalProperties: false
    UpsertSpaceRequest:
      type: object
      properties:
        title: { type: string, maxLength: 120 }
        bio: { type: string, maxLength: 500 }
        avatar: { type: string, maxLength: 512 }
        cover_image: { type: string, maxLength: 512 }
        theme: { type: string, maxLength: 64 }
        layout: { type: string, maxLength: 64 }
        visibility: { type: string, enum: [public, unlisted, private] }
        sync_enabled: { type: boolean }
        sync_categories: { type: array, items: { type: string } }
        sync_tags: { type: array, items: { type: string } }
      additionalProperties: false
    PublicSpace:
      type: object
      required: [owner, space]
      properties:
        owner: { type: object, additionalProperties: true }
        space: { $ref: '#/components/schemas/Space' }
      additionalProperties: false
    Space: { type: object, additionalProperties: true }
    SpacePublicProfile:
      type: object
      additionalProperties: false
      required: [owner, space]
      properties:
        owner: { $ref: '#/components/schemas/SpacePublicOwner' }
        space: { $ref: '#/components/schemas/SpacePublicAppearance' }
    SpacePublicOwner:
      type: object
      additionalProperties: false
      required: [id, username, nickname]
      properties:
        id: {type: string}
        username: {type: string}
        nickname: {type: string}
        avatar: {type: string}
        bio: {type: string}
    SpacePublicAppearance:
      type: object
      additionalProperties: false
      required: [id, user_id, title, bio, theme, layout, visibility, sync_enabled, is_default, created_at, updated_at]
      properties:
        id: {type: string}
        user_id: {type: string}
        title: {type: string}
        bio: {type: string}
        avatar: {type: string}
        cover_image: {type: string}
        theme: {type: string}
        layout: {type: string}
        style_name: {type: string}
        style_version: {type: string}
        style_manifest: {$ref: '#/components/schemas/PersonalSpaceStyleManifest'}
        visibility: {type: string, enum: [public]}
        sync_enabled: {type: boolean}
        is_default: {type: boolean}
        created_at: {type: string, format: date-time}
        updated_at: {type: string, format: date-time}
    SpaceCurrentSyncStatus:
      type: object
      additionalProperties: false
      required: [user_id, sync_enabled, content_total, disabled]
      properties:
        user_id: {type: string}
        sync_enabled: {type: boolean}
        content_total: {type: integer, format: int64, minimum: 0}
        disabled: {type: boolean}
        last_sync_at: {type: string, format: date-time}
        last_sync_error: {type: string, description: 'Fixed safe user guidance; never raw host errors'}
    SpacePublicContent:
      type: object
      additionalProperties: false
      required: [id, user_id, thread_id, title, excerpt, author_name, category_id, status, thread_created_at, thread_updated_at, synced_at]
      properties:
        id: {type: string}
        user_id: {type: string}
        thread_id: {type: string}
        title: {type: string}
        excerpt: {type: string}
        author_name: {type: string}
        category_id: {type: string}
        tags: {type: array, items: {type: string}}
        status: {type: string}
        publication_status: {type: string, enum: [published]}
        moderation_status: {type: string, enum: [clear]}
        deletion_status: {type: string, enum: [active]}
        thread_created_at: {type: string, format: date-time}
        thread_updated_at: {type: string, format: date-time}
        synced_at: {type: string, format: date-time}
    SpacePublicContentListData:
      type: object
      additionalProperties: false
      required: [items, pagination]
      properties:
        items: {type: array, items: {$ref: '#/components/schemas/SpacePublicContent'}}
        pagination: {$ref: '#/components/schemas/Pagination'}
    SpaceOwnerContent:
      type: object
      additionalProperties: false
      required: [id, user_id, thread_id, title, excerpt, author_name, category_id, status, thread_created_at, thread_updated_at, synced_at]
      properties:
        id: {type: string}
        user_id: {type: string}
        thread_id: {type: string}
        title: {type: string}
        excerpt: {type: string}
        author_name: {type: string}
        category_id: {type: string}
        tags: {type: array, items: {type: string}}
        status: {type: string}
        publication_status: {type: string, enum: [draft, published, private]}
        moderation_status: {type: string, enum: [clear, pending, rejected, taken_down]}
        deletion_status: {type: string, enum: [active]}
        thread_created_at: {type: string, format: date-time}
        thread_updated_at: {type: string, format: date-time}
        synced_at: {type: string, format: date-time}
        moderation_reason: {type: string}
    SpaceOwnerContentListData:
      type: object
      additionalProperties: false
      required: [items, pagination]
      properties:
        items: {type: array, items: {$ref: '#/components/schemas/SpaceOwnerContent'}}
        pagination: {$ref: '#/components/schemas/Pagination'}
    SpaceStorageStatus:
      type: object
      required: [user_id, quota_bytes, default_quota_bytes, used_bytes, available_bytes, avatar_keep_limit, max_avatar_bytes, custom_quota]
      properties:
        user_id: { type: string, pattern: '^[0-9]+$' }
        quota_bytes: { type: integer, format: int64, minimum: 1 }
        default_quota_bytes: { type: integer, format: int64, minimum: 1 }
        used_bytes: { type: integer, format: int64, minimum: 0 }
        available_bytes: { type: integer, format: int64, minimum: 0 }
        max_avatar_bytes: { type: integer, format: int64, minimum: 1 }
        avatar_keep_limit: { type: integer, minimum: 1 }
        custom_quota: { type: boolean }
        quota_updated_by: { type: string }
        quota_updated_at: { type: string, format: date-time, nullable: true }
      additionalProperties: false
    SetStorageQuotaRequest:
      type: object
      required: [quota_bytes]
      properties:
        quota_bytes: { type: integer, format: int64, minimum: 1048576, maximum: 107374182400 }
      additionalProperties: false
    SpaceQuotaMutationAck:
      type: object
      required: [owner_id, quota_bytes]
      properties:
        owner_id: { type: string }
        quota_bytes: { type: integer, format: int64, minimum: 1048576, maximum: 107374182400 }
      additionalProperties: false
    SpaceAdminSummary:
      type: object
      required: [total_spaces, public_spaces, disabled_spaces, styled_spaces, sync_enabled_spaces, sync_error_spaces]
      properties:
        total_spaces: { type: integer, format: int64, minimum: 0 }
        public_spaces: { type: integer, format: int64, minimum: 0 }
        disabled_spaces: { type: integer, format: int64, minimum: 0 }
        styled_spaces: { type: integer, format: int64, minimum: 0 }
        sync_enabled_spaces: { type: integer, format: int64, minimum: 0 }
        sync_error_spaces: { type: integer, format: int64, minimum: 0 }
        last_sync_at: { type: string, format: date-time }
      additionalProperties: false
    SpaceStyleReadValidation:
      type: object
      additionalProperties: false
      required: [valid]
      properties:
        valid: { type: boolean }
        errors: { type: array, items: { type: string } }
        warnings: { type: array, items: { type: string } }
    SpaceStyleReadPreview:
      type: object
      additionalProperties: false
      required: [validation]
      properties:
        validation: { $ref: '#/components/schemas/SpaceStyleReadValidation' }
        manifest: { $ref: '#/components/schemas/PersonalSpaceStyleManifest' }
        layout: { type: string }
        components:
          type: array
          items:
            type: object
            additionalProperties: false
            properties:
              slot: { type: string }
              type: { type: string }
              props: { type: object, additionalProperties: true }
        tokens: { type: object, additionalProperties: { type: string } }
        assets:
          type: array
          items:
            type: object
            additionalProperties: false
            properties:
              name: { type: string }
              path: { type: string }
              type: { type: string }
    SpaceStyleExport:
      type: object
      additionalProperties: false
      required: [package, filename, validation]
      properties:
        package: { $ref: '#/components/schemas/PersonalSpaceStylePackage' }
        filename: { type: string }
        validation: { $ref: '#/components/schemas/SpaceStyleReadValidation' }
    SpaceStyleHTMLExample:
      type: object
      additionalProperties: false
      required: [html, package, validation]
      properties:
        html: { type: string }
        package: { $ref: '#/components/schemas/PersonalSpaceStylePackage' }
        validation: { $ref: '#/components/schemas/SpaceStyleReadValidation' }
    SpaceStyleReadZipRequest:
      type: object
      additionalProperties: false
      required: [file]
      properties:
        file: { type: string, format: binary, maxLength: 16777216 }
    SpaceStyleZipValidation:
      type: object
      additionalProperties: false
      required: [validation]
      properties:
        package: { $ref: '#/components/schemas/SpaceStylePackPackage' }
        validation:
          type: object
          additionalProperties: false
          required: [valid]
          properties:
            valid: { type: boolean }
            delivery_status: { type: string }
            errors: { type: array, items: { type: string } }
            warnings: { type: array, items: { type: string } }
            issues:
              type: array
              items:
                type: object
                additionalProperties: false
                required: [code, message]
                properties:
                  code: { type: string }
                  message: { type: string }
                  file: { type: string }
                  viewport: { type: string }
    SpaceStylePackPackage:
      type: object
      additionalProperties: false
      required: [manifest, html]
      properties:
        manifest: { type: object, additionalProperties: true }
        html: { type: string }
        css: { type: string }
        effect_js: { type: string }
        config_schema: { type: object, additionalProperties: true }
        files:
          type: array
          items:
            type: object
            additionalProperties: false
            required: [path, size, type]
            properties:
              path: { type: string }
              size: { type: integer, minimum: 0 }
              type: { type: string }
    SpaceStylePackExample:
      type: object
      additionalProperties: false
      required: [filename, files, package]
      properties:
        filename: { type: string }
        files: { type: object, additionalProperties: { type: string } }
        package: { $ref: '#/components/schemas/SpaceStylePackPackage' }
    SpaceSourceStylePackList:
      type: object
      additionalProperties: false
      required: [items]
      properties:
        items:
          type: array
          items: { $ref: '#/components/schemas/SpaceSourceStylePack' }
    SpaceSourceStylePack:
      type: object
      additionalProperties: false
      required: [name, validation]
      properties:
        name: { type: string }
        target: { type: string }
        version: { type: string }
        display_name: { type: string }
        description: { type: string }
        checksum: { type: string }
        previews:
          type: object
          additionalProperties: false
          properties:
            desktop: { type: string }
            mobile: { type: string }
        manifest:
          type: object
          description: Package manifest; paths are package-relative declarations.
          additionalProperties: true
        validation:
          type: object
          additionalProperties: false
          required: [valid]
          properties:
            valid: { type: boolean }
            delivery_status: { type: string }
            errors:
              type: array
              items: { type: string }
            warnings:
              type: array
              items: { type: string }
            issues:
              type: array
              items:
                type: object
                additionalProperties: false
                required: [code, message]
                properties:
                  code: { type: string }
                  message: { type: string }
                  file: { type: string }
                  viewport: { type: string }
    PersonalSpaceStyleHTMLRequest:
      type: object
      additionalProperties: false
      required: [html]
      properties:
        html: { type: string, maxLength: 20000 }
    PersonalSpaceStylePackage:
      type: object
      additionalProperties: false
      required: [manifest]
      properties:
        manifest: { $ref: '#/components/schemas/PersonalSpaceStyleManifest' }
    PersonalSpaceStyleManifest:
      type: object
      additionalProperties: false
      properties:
        schema_version: { type: string }
        name: { type: string }
        version: { type: string }
        author: { type: string }
        description: { type: string }
        preview_image: { type: string }
        compatible_campusos: { type: array, items: { type: string } }
        layout: { type: string }
        components:
          type: array
          items:
            type: object
            additionalProperties: false
            properties:
              slot: { type: string }
              type: { type: string }
              props: { type: object, additionalProperties: true }
        tokens: { type: object, additionalProperties: { type: string } }
        assets:
          type: array
          items:
            type: object
            additionalProperties: false
            properties:
              name: { type: string }
              path: { type: string }
              type: { type: string }
        custom_html_enabled: { type: boolean }
        custom_html: { type: string }
        custom_css: { type: string }
        custom_effect_js: { type: string }
        source_style_pack:
          type: object
          additionalProperties: false
          properties:
            name: { type: string }
            version: { type: string }
            target: { type: string }
        capabilities: { type: array, items: { type: string } }
    AvatarCleanupAck:
      type: object
      additionalProperties: false
      required: [owner_id, cleanup_pending]
      properties:
        owner_id: { type: string }
        cleanup_pending: { type: boolean }
    AvatarMutationAck:
      type: object
      additionalProperties: false
      required: [space_id, owner_id]
      properties:
        space_id: { type: string }
        owner_id: { type: string }
        cleanup_pending: { type: boolean }
    SelectAvatarRequest:
      type: object
      required: [file_name]
      properties:
        file_name: { type: string, minLength: 1, maxLength: 255 }
      additionalProperties: false
    AvatarHistoryItem:
      type: object
      required: [file_name, url, size, uploaded_at, active]
      properties:
        file_name: { type: string }
        url: { type: string }
        size: { type: integer, format: int64, minimum: 0 }
        uploaded_at: { type: string, format: date-time }
        active: { type: boolean }
    AvatarHistory:
      type: object
      required: [items, storage]
      properties:
        items: { type: array, maxItems: 3, items: { $ref: '#/components/schemas/AvatarHistoryItem' } }
        storage: { $ref: '#/components/schemas/SpaceStorageStatus' }
    AvatarUploadResult:
      type: object
      required: [file_name, url, size, storage, owner, space, avatars]
      properties:
        file_name: { type: string }
        url: { type: string }
        size: { type: integer, format: int64, minimum: 0 }
        storage: { $ref: '#/components/schemas/SpaceStorageStatus' }
        owner: { type: object, additionalProperties: true }
        space: { $ref: '#/components/schemas/Space' }
        avatars: { type: array, maxItems: 3, items: { $ref: '#/components/schemas/AvatarHistoryItem' } }
    ActivateTermRequest:
      type: object
      required: [term_year, semester]
      properties:
        term_year: { type: integer, minimum: 2000, maximum: 2200 }
        semester: { type: string, enum: [spring, fall] }
      additionalProperties: false
    ScheduleUpsertRequest:
      type: object
      required: [term_year, semester, first_week_start, settings, courses]
      properties:
        term_year: { type: integer }
        semester: { type: string, enum: [spring, fall] }
        first_week_start: { type: string, format: date }
        settings: { type: object, additionalProperties: true }
        courses: { type: array, items: { type: object, additionalProperties: true } }
        metadata: { type: object, additionalProperties: true }
      additionalProperties: false
    RichtextSaveRequest:
      type: object
      required: [title, content_html]
      properties:
        title: { type: string, minLength: 1, maxLength: 255 }
        summary: { type: string }
        cover_url: { type: string }
        content_html: { type: string, minLength: 1 }
        content_json: {}
        category_id: { type: string }
        tags: { type: array, items: { type: string } }
      additionalProperties: false
    RichtextArticle: { type: object, additionalProperties: true }
    RichtextGovernanceReason:
      type: object
      required: [reason]
      properties:
        reason: { type: string, minLength: 1 }
      additionalProperties: false
    RichtextGovernanceAck:
      type: object
      required: [thread_id, status]
      properties:
        thread_id: { type: string, minLength: 1 }
        status: { type: string, enum: [draft, published, offline, trashed] }
      additionalProperties: false
    ContentPreviewRequest:
      type: object
      required: [content]
      properties:
        content: { type: string, minLength: 1, maxLength: 100000 }
      additionalProperties: false
    ContentPreviewResponse:
      type: object
      required: [sanitized_html, text]
      properties:
        sanitized_html: { type: string }
        text: { type: string }
        warnings: { type: array, items: { type: string } }
    ContentImageUploadRequest:
      type: object
      required: [file]
      properties:
        file: { type: string, format: binary }
      additionalProperties: false
    InlineRichTextUploadRequest:
      type: object
      required: [file]
      description: Article bindings are resolved from the actual author-editable article; article_content_id requires thread_id.
      properties:
        file: { type: string, format: binary }
        thread_id: { type: string }
        article_content_id: { type: string }
      additionalProperties: false
    InlineRichTextAsset:
      type: object
      required: [id, uploader_id, file_url, file_name, file_size, mime_type, created_at]
      properties:
        id: { type: string }
        uploader_id: { type: string }
        thread_id: { type: string }
        article_content_id: { type: string }
        file_url: { type: string }
        file_name: { type: string }
        file_size: { type: integer, minimum: 1 }
        mime_type: { type: string, enum: [image/png, image/jpeg, image/gif, image/webp] }
        width: { type: integer, minimum: 1 }
        height: { type: integer, minimum: 1 }
        created_at: { type: string, format: date-time }
      additionalProperties: false
    ContentImage:
      type: object
      required: [file_url, file_name, file_size, mime_type]
      properties:
        file_url: { type: string }
        file_name: { type: string }
        file_size: { type: integer, format: int64 }
        mime_type: { type: string, enum: [image/jpeg, image/png, image/gif, image/webp] }
        width: { type: integer }
        height: { type: integer }
    RoleAssignmentRequest:
      type: object
      required: [role_id]
      properties:
        role_id: { type: integer, format: int64 }
      additionalProperties: false
    RoleRevokeRequest:
      type: object
      required: [role_id]
      properties:
        role_id: { type: integer, format: int64 }
      additionalProperties: false
    ModeratorAssignmentRequest:
      type: object
      required: [category_ids]
      properties:
        category_ids: { type: array, items: { type: string }, uniqueItems: true }
      additionalProperties: false
    PluginRollbackRequest:
      type: object
      required: [snapshot_id]
      properties:
        snapshot_id: { type: string }
      additionalProperties: false
    StylePackSourceRequest:
      type: object
      required: [name]
      properties:
        name: { type: string }
      additionalProperties: false
    StyleExportRequest:
      type: object
      properties:
        name: { type: string }
        version: { type: string }
        description: { type: string }
      additionalProperties: false
    DisableSpaceRequest:
      type: object
      properties:
        reason: { type: string, maxLength: 500 }
      additionalProperties: false
    UserListData:
      type: object
      required: [items, pagination]
      properties:
        items: { type: array, items: { $ref: '#/components/schemas/PublicUser' } }
        pagination: { $ref: '#/components/schemas/Pagination' }
    AdminUserListData:
      type: object
      required: [items, pagination]
      properties:
        items: { type: array, items: { $ref: '#/components/schemas/AdminUser' } }
        pagination: { $ref: '#/components/schemas/Pagination' }
    AdminThreadPermissionsPreviewRequest:
      type: object
      additionalProperties: false
      required: [thread_ids]
      properties:
        thread_ids: {type: array, minItems: 1, maxItems: 20, uniqueItems: true, items: {type: string, minLength: 1, maxLength: 128}}
    AdminThreadPermissionFlags:
      type: object
      additionalProperties: false
      required: [pin, unpin, lock, unlock, approve, reject, take_down, direct_restore, trash, restore_trash, purge]
      properties:
        pin: {type: boolean}
        unpin: {type: boolean}
        lock: {type: boolean}
        unlock: {type: boolean}
        approve: {type: boolean}
        reject: {type: boolean}
        take_down: {type: boolean}
        direct_restore: {type: boolean}
        trash: {type: boolean}
        restore_trash: {type: boolean}
        purge: {type: boolean}
    AdminThreadPermissionsPreviewItem:
      type: object
      additionalProperties: false
      required: [id, permissions]
      properties:
        id: {type: string}
        permissions: {$ref: '#/components/schemas/AdminThreadPermissionFlags'}
    AdminThreadPermissionsPreviewData:
      type: object
      additionalProperties: false
      required: [items]
      properties:
        items: {type: array, maxItems: 20, items: {$ref: '#/components/schemas/AdminThreadPermissionsPreviewItem'}}
    AdminThreadMetadata:
      type: object
      additionalProperties: false
      required: [id, author_id, category_id, thread_type, content_format, publication_status, moderation_status, deletion_status, is_pinned, is_locked, created_at, updated_at]
      properties:
        id: {type: string}
        author_id: {type: string}
        category_id: {type: string}
        thread_type: {type: string}
        content_format: {type: string}
        publication_status: {type: string, enum: [draft, published, private]}
        moderation_status: {type: string, enum: [clear, pending, rejected, taken_down]}
        deletion_status: {type: string, enum: [active, trashed, purged]}
        is_pinned: {type: boolean}
        is_locked: {type: boolean}
        created_at: {type: string, format: date-time}
        updated_at: {type: string, format: date-time}
    AdminModerationMetadata:
      type: object
      additionalProperties: false
      required: [id, thread_id, action, actor_kind, actor_ref_id, created_at]
      properties:
        id: {type: string}
        thread_id: {type: string}
        action: {type: string}
        actor_kind: {type: string, enum: [user, admin, legacy_unknown]}
        actor_ref_id: {type: string}
        created_at: {type: string, format: date-time}
    AdminThreadMetadataListData:
      type: object
      additionalProperties: false
      required: [items, pagination]
      properties:
        items: {type: array, items: {$ref: '#/components/schemas/AdminThreadMetadata'}}
        pagination: {$ref: '#/components/schemas/Pagination'}
    AdminModerationMetadataListData:
      type: object
      additionalProperties: false
      required: [items, pagination]
      properties:
        items: {type: array, items: {$ref: '#/components/schemas/AdminModerationMetadata'}}
        pagination: {$ref: '#/components/schemas/Pagination'}
    ThreadListData:
      type: object
      required: [items, pagination]
      properties:
        items: { type: array, items: { $ref: '#/components/schemas/Thread' } }
        pagination: { $ref: '#/components/schemas/Pagination' }
    PostListData:
      type: object
      required: [items, pagination]
      properties:
        items: { type: array, items: { $ref: '#/components/schemas/Post' } }
        pagination: { $ref: '#/components/schemas/Pagination' }
    NotificationListData:
      type: object
      required: [items, pagination, unread_count]
      properties:
        items: { type: array, items: { $ref: '#/components/schemas/Notification' } }
        pagination: { $ref: '#/components/schemas/Pagination' }
        unread_count: { type: integer, format: int64, minimum: 0 }
  responses:
    NotFound:
      description: Resource unavailable to this caller
      content:
        application/json:
          schema: { $ref: '#/components/schemas/ErrorEnvelope' }
    Conflict:
      description: Current facts, quota or resource state changed
      content:
        application/json:
          schema: { $ref: '#/components/schemas/ErrorEnvelope' }
    PayloadTooLarge:
      description: File or request exceeds the bounded limit
      content:
        application/json:
          schema: { $ref: '#/components/schemas/ErrorEnvelope' }
    Unauthorized:
      description: Missing or invalid authentication
      content:
        application/json:
          schema: { $ref: '#/components/schemas/ErrorEnvelope' }
    Forbidden:
      description: Authenticated subject lacks the required permission or scope
      content:
        application/json:
          schema: { $ref: '#/components/schemas/ErrorEnvelope' }
    BadRequest:
      description: Invalid request parameters or body
      content:
        application/json:
          schema: { $ref: '#/components/schemas/ErrorEnvelope' }
    TooManyRequests:
      description: Request frequency exceeded the active bounded policy
      content:
        application/json:
          schema: { $ref: '#/components/schemas/ErrorEnvelope' }
    ServiceUnavailable:
      description: A required internal dependency is temporarily unavailable
      content:
        application/json:
          schema: { $ref: '#/components/schemas/ErrorEnvelope' }
`
}
