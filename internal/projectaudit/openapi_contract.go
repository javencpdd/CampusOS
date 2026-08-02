package projectaudit

type operationProfile struct {
	RequestSchema    string
	ResponseSchema   string
	ContentType      string
	SuccessStatus    string
	AdditionalErrors []string
	NoBody           bool
	Paginated        bool
	RequestOptional  bool
}

var operationProfiles = map[string]operationProfile{
	"userHandler.RequestRegistrationChallenge": {RequestSchema: "RegistrationChallengeRequest", ResponseSchema: "ChallengeReceipt", AdditionalErrors: []string{"429", "503"}},
	"userHandler.VerifyRegistrationChallenge":  {RequestSchema: "RegistrationChallengeVerifyRequest", ResponseSchema: "ChallengeTicket"},
	"userHandler.Register":                     {RequestSchema: "RegisterRequest", ResponseSchema: "User", SuccessStatus: "201"},
	"userHandler.Login":                        {RequestSchema: "LoginRequest", ResponseSchema: "LoginResponse", AdditionalErrors: []string{"401", "503"}},
	"userHandler.AdminLogin":                   {RequestSchema: "LoginRequest", ResponseSchema: "LoginResponse", AdditionalErrors: []string{"401", "503"}},
	"userHandler.Refresh":                      {RequestSchema: "RefreshRequest", ResponseSchema: "RefreshResponse", RequestOptional: true},
	"userHandler.ListSessions":                 {ResponseSchema: "SessionListResponse"},
	"userHandler.RequestPasswordReset":         {RequestSchema: "PasswordResetChallengeRequest", ResponseSchema: "PasswordResetChallengeResponse"},
	"userHandler.VerifyPasswordReset":          {RequestSchema: "PasswordResetVerifyRequest", ResponseSchema: "ChallengeTicket"},
	"userHandler.CompletePasswordReset":        {RequestSchema: "PasswordResetCompleteRequest", ResponseSchema: "RecoveryCompletionResponse"},
	"userHandler.RequestEmailBinding":          {RequestSchema: "EmailBindingChallengeRequest", ResponseSchema: "ChallengeReceipt"},
	"userHandler.VerifyEmailBinding":           {RequestSchema: "EmailBindingVerifyRequest", ResponseSchema: "ChallengeTicket"},
	"userHandler.CompleteEmailBinding":         {RequestSchema: "EmailBindingCompleteRequest", ResponseSchema: "RecoveryCompletionResponse"},
	"userHandler.CompleteAdminRecovery":        {RequestSchema: "RecoveryCaseCompleteRequest", ResponseSchema: "RecoveryCompletionResponse"},
	"userHandler.ListAdminRecoveryCases":       {ResponseSchema: "RecoveryCaseListResponse"},
	"userHandler.CreateAdminRecoveryCase":      {RequestSchema: "AdminRecoveryCaseCreateRequest", ResponseSchema: "RecoveryCase", SuccessStatus: "201"},
	"userHandler.ListAdminUserSessions":        {ResponseSchema: "SessionListResponse"},
	"challengePolicyHandler.Get":               {ResponseSchema: "ChallengePolicy"},
	"challengePolicyHandler.Update":            {RequestSchema: "ChallengePolicyUpdateRequest", ResponseSchema: "ChallengePolicy"},
	"userHandler.UpdateUser":                   {RequestSchema: "UpdateUserRequest", ResponseSchema: "User"},
	"userHandler.ListUsers":                    {ResponseSchema: "UserListData", Paginated: true},
	"userHandler.ListAdminUsers":               {ResponseSchema: "AdminUserListData", Paginated: true},
	"userHandler.GetUser":                      {ResponseSchema: "PublicUser"},
	"userHandler.GetMe":                        {ResponseSchema: "User"},
	"threadHandler.CreateThread":               {RequestSchema: "CreateThreadRequest", ResponseSchema: "Thread", SuccessStatus: "201"},
	"threadHandler.UpdateThread":               {RequestSchema: "UpdateThreadRequest", ResponseSchema: "Thread"},
	"threadHandler.ListThreads":                {ResponseSchema: "ThreadListData", Paginated: true},
	"threadHandler.AdminListThreads":           {ResponseSchema: "ThreadListData", Paginated: true},
	"threadHandler.GetThread":                  {ResponseSchema: "Thread"},
	"threadHandler.GetThreadForCurrentUser":    {ResponseSchema: "Thread"},
	"threadHandler.PreviewContent":             {RequestSchema: "ContentPreviewRequest", ResponseSchema: "ContentPreviewResponse"},
	"userStorageHandler.UploadContentImage":    {ResponseSchema: "ContentImage"},
	"postHandler.CreatePost":                   {RequestSchema: "CreatePostRequest", ResponseSchema: "Post", SuccessStatus: "201"},
	"postHandler.UpdatePost":                   {RequestSchema: "UpdatePostRequest", ResponseSchema: "Post"},
	"postHandler.ListPosts":                    {ResponseSchema: "PostListData", Paginated: true},
	"postHandler.ListPostsForCurrentUser":      {ResponseSchema: "PostListData", Paginated: true},
	"notificationHandler.List":                 {ResponseSchema: "NotificationListData", Paginated: true},
	"categoryHandler.Create":                   {RequestSchema: "CreateCategoryRequest", ResponseSchema: "Category", SuccessStatus: "201"},
	"categoryHandler.Update":                   {RequestSchema: "UpdateCategoryRequest", ResponseSchema: "Category"},
	"categoryHandler.Get":                      {ResponseSchema: "Category"},
	"categoryHandler.ListThreadTypePolicies":   {ResponseSchema: "CategoryThreadTypePolicies"},
	"categoryHandler.UpdateThreadTypePolicies": {RequestSchema: "UpdateCategoryThreadTypePolicyRequest", ResponseSchema: "CategoryThreadTypePolicyUpdate"},
	"spaceHandler.UpdateMe":                    {RequestSchema: "UpsertSpaceRequest", ResponseSchema: "Space"},
	"spaceHandler.StorageStatus":               {ResponseSchema: "SpaceStorageStatus"},
	"spaceHandler.UploadAvatar":                {ResponseSchema: "AvatarUploadResult"},
	"spaceHandler.ListAvatars":                 {ResponseSchema: "AvatarHistory"},
	"spaceHandler.SelectAvatar":                {RequestSchema: "SelectAvatarRequest", ResponseSchema: "AvatarUploadResult"},
	"spaceHandler.AdminStorageStatus":          {ResponseSchema: "SpaceStorageStatus"},
	"spaceHandler.SetStorageQuota":             {RequestSchema: "SetStorageQuotaRequest", ResponseSchema: "SpaceStorageStatus"},
	"spaceHandler.ListContentsByUserID":        {Paginated: true},
	"spaceHandler.ListContentsByUsername":      {Paginated: true},
	"scheduleHandler.ActivateTerm":             {RequestSchema: "ActivateTermRequest"},
	"scheduleHandler.SaveMe":                   {RequestSchema: "ScheduleUpsertRequest"},
	"richTextHandler.CreateDraft":              {RequestSchema: "RichtextSaveRequest", ResponseSchema: "RichtextArticle", SuccessStatus: "201"},
	"richTextHandler.UpdateDraft":              {RequestSchema: "RichtextSaveRequest", ResponseSchema: "RichtextArticle"},
	"richTextHandler.Preview":                  {RequestSchema: "RichtextSaveRequest"},
	"roleHandler.AssignRole":                   {RequestSchema: "RoleAssignmentRequest"},
	"roleHandler.RevokeRole":                   {RequestSchema: "RoleRevokeRequest"},
	"moderationHandler.SetModerator":           {RequestSchema: "ModeratorAssignmentRequest"},
	"pluginHandler.RollbackVersionSnapshot":    {RequestSchema: "PluginRollbackRequest"},
	"pluginHandler.UpdatePluginConfig":         {RequestSchema: "GenericObject"},
	"webhookHandler.CreateEndpoint":            {RequestSchema: "GenericObject"},
	"mcpHandler.CallTool":                      {RequestSchema: "GenericObject"},
	"mcpHandler.UpdateSettings":                {RequestSchema: "GenericObject"},
	"messageHandler.ReceiveLocal":              {RequestSchema: "GenericObject"},
	"messageHandler.CreateBinding":             {RequestSchema: "GenericObject"},
	"spaceHandler.ApplySourceStylePack":        {RequestSchema: "StylePackSourceRequest"},
	"homepageHandler.ApplySourceStylePack":     {RequestSchema: "StylePackSourceRequest"},
	"spaceHandler.ExportStylePackage":          {RequestSchema: "StyleExportRequest", RequestOptional: true},
	"spaceHandler.DisableSpace":                {RequestSchema: "DisableSpaceRequest", RequestOptional: true},
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
	"richTextHandler.AdminOffline":        true,
	"richTextHandler.AdminRestore":        true,
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
	if multipartHandlers[route.Handler] {
		profile.ContentType = "multipart/form-data"
		profile.RequestSchema = "MultipartRequest"
	}
	if noBodyHandlers[route.Handler] {
		profile.NoBody = true
	}
	if noContentHandlers[route.Handler] {
		profile.SuccessStatus = "204"
		profile.NoBody = true
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
  schemas:
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
    Space: { type: object, additionalProperties: true }
    SpaceStorageStatus:
      type: object
      required: [user_id, quota_bytes, default_quota_bytes, used_bytes, available_bytes, avatar_keep_limit, custom_quota]
      properties:
        user_id: { type: string, pattern: '^[0-9]+$' }
        quota_bytes: { type: integer, format: int64, minimum: 1 }
        default_quota_bytes: { type: integer, format: int64, minimum: 1 }
        used_bytes: { type: integer, format: int64, minimum: 0 }
        available_bytes: { type: integer, format: int64, minimum: 0 }
        avatar_keep_limit: { type: integer, minimum: 1 }
        custom_quota: { type: boolean }
        quota_updated_by: { type: string }
        quota_updated_at: { type: string, format: date-time, nullable: true }
    SetStorageQuotaRequest:
      type: object
      required: [quota_bytes]
      properties:
        quota_bytes: { type: integer, format: int64, minimum: 1048576, maximum: 107374182400 }
      additionalProperties: false
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
        reason: { type: string }
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
