package projectaudit

type operationProfile struct {
	RequestSchema   string
	ResponseSchema  string
	ContentType     string
	SuccessStatus   string
	NoBody          bool
	Paginated       bool
	RequestOptional bool
}

var operationProfiles = map[string]operationProfile{
	"userHandler.Register":                  {RequestSchema: "RegisterRequest", ResponseSchema: "User", SuccessStatus: "201"},
	"userHandler.Login":                     {RequestSchema: "LoginRequest", ResponseSchema: "LoginResponse"},
	"userHandler.UpdateUser":                {RequestSchema: "UpdateUserRequest", ResponseSchema: "User"},
	"userHandler.ListUsers":                 {ResponseSchema: "UserListData", Paginated: true},
	"userHandler.GetUser":                   {ResponseSchema: "PublicUser"},
	"userHandler.GetMe":                     {ResponseSchema: "User"},
	"threadHandler.CreateThread":            {RequestSchema: "CreateThreadRequest", ResponseSchema: "Thread", SuccessStatus: "201"},
	"threadHandler.UpdateThread":            {RequestSchema: "UpdateThreadRequest", ResponseSchema: "Thread"},
	"threadHandler.ListThreads":             {ResponseSchema: "ThreadListData", Paginated: true},
	"threadHandler.AdminListThreads":        {ResponseSchema: "ThreadListData", Paginated: true},
	"threadHandler.GetThread":               {ResponseSchema: "Thread"},
	"threadHandler.GetThreadForCurrentUser": {ResponseSchema: "Thread"},
	"postHandler.CreatePost":                {RequestSchema: "CreatePostRequest", ResponseSchema: "Post", SuccessStatus: "201"},
	"postHandler.UpdatePost":                {RequestSchema: "UpdatePostRequest", ResponseSchema: "Post"},
	"postHandler.ListPosts":                 {ResponseSchema: "PostListData", Paginated: true},
	"postHandler.ListPostsForCurrentUser":   {ResponseSchema: "PostListData", Paginated: true},
	"categoryHandler.Create":                {RequestSchema: "CreateCategoryRequest", ResponseSchema: "Category", SuccessStatus: "201"},
	"categoryHandler.Update":                {RequestSchema: "UpdateCategoryRequest", ResponseSchema: "Category"},
	"categoryHandler.Get":                   {ResponseSchema: "Category"},
	"spaceHandler.UpdateMe":                 {RequestSchema: "UpsertSpaceRequest", ResponseSchema: "Space"},
	"spaceHandler.ListContentsByUserID":     {Paginated: true},
	"spaceHandler.ListContentsByUsername":   {Paginated: true},
	"scheduleHandler.ActivateTerm":          {RequestSchema: "ActivateTermRequest"},
	"scheduleHandler.SaveMe":                {RequestSchema: "ScheduleUpsertRequest"},
	"richTextHandler.CreateDraft":           {RequestSchema: "RichtextSaveRequest", ResponseSchema: "RichtextArticle", SuccessStatus: "201"},
	"richTextHandler.UpdateDraft":           {RequestSchema: "RichtextSaveRequest", ResponseSchema: "RichtextArticle"},
	"richTextHandler.Preview":               {RequestSchema: "RichtextSaveRequest"},
	"roleHandler.AssignRole":                {RequestSchema: "RoleAssignmentRequest"},
	"roleHandler.RevokeRole":                {RequestSchema: "RoleRevokeRequest"},
	"moderationHandler.SetModerator":        {RequestSchema: "ModeratorAssignmentRequest"},
	"pluginHandler.RollbackVersionSnapshot": {RequestSchema: "PluginRollbackRequest"},
	"pluginHandler.UpdatePluginConfig":      {RequestSchema: "GenericObject"},
	"webhookHandler.CreateEndpoint":         {RequestSchema: "GenericObject"},
	"mcpHandler.CallTool":                   {RequestSchema: "GenericObject"},
	"mcpHandler.UpdateSettings":             {RequestSchema: "GenericObject"},
	"messageHandler.ReceiveLocal":           {RequestSchema: "GenericObject"},
	"messageHandler.CreateBinding":          {RequestSchema: "GenericObject"},
	"spaceHandler.ApplySourceStylePack":     {RequestSchema: "StylePackSourceRequest"},
	"homepageHandler.ApplySourceStylePack":  {RequestSchema: "StylePackSourceRequest"},
	"spaceHandler.ExportStylePackage":       {RequestSchema: "StyleExportRequest", RequestOptional: true},
	"spaceHandler.DisableSpace":             {RequestSchema: "DisableSpaceRequest", RequestOptional: true},
}

var multipartHandlers = map[string]bool{
	"spaceHandler.UploadAvatar":           true,
	"spaceHandler.ValidateStylePackZip":   true,
	"spaceHandler.ApplyStylePackZip":      true,
	"scheduleHandler.ImportMe":            true,
	"richTextHandler.UploadAsset":         true,
	"pluginHandler.ImportPluginPackage":   true,
	"pluginHandler.PrecheckPluginPackage": true,
	"homepageHandler.ValidateStylePack":   true,
	"homepageHandler.ApplyStylePack":      true,
}

var noBodyHandlers = map[string]bool{
	"spaceHandler.RollbackStyle":        true,
	"spaceHandler.RestoreDefaultStyle":  true,
	"richTextHandler.Publish":           true,
	"richTextHandler.Offline":           true,
	"moderationHandler.Pin":             true,
	"moderationHandler.Unpin":           true,
	"moderationHandler.Lock":            true,
	"moderationHandler.Unlock":          true,
	"userHandler.SuspendUser":           true,
	"userHandler.ActivateUser":          true,
	"threadHandler.PinThread":           true,
	"threadHandler.UnpinThread":         true,
	"threadHandler.LockThread":          true,
	"threadHandler.UnlockThread":        true,
	"richTextHandler.AdminOffline":      true,
	"richTextHandler.AdminRestore":      true,
	"pluginHandler.EnablePlugin":        true,
	"pluginHandler.DisablePlugin":       true,
	"pluginHandler.ReloadUserPlugin":    true,
	"spaceHandler.EnableSpace":          true,
	"webhookHandler.TestEndpoint":       true,
	"webhookHandler.EnableEndpoint":     true,
	"webhookHandler.DisableEndpoint":    true,
	"homepageHandler.RollbackStylePack": true,
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
    RegisterRequest:
      type: object
      required: [username, nickname, email, password]
      properties:
        username: { type: string, minLength: 3, maxLength: 32 }
        nickname: { type: string, minLength: 1, maxLength: 64 }
        email: { type: string, format: email }
        password: { type: string, minLength: 6, maxLength: 64, writeOnly: true }
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
      required: [user, roles, access_token, refresh_token, token_type, expires_in]
      properties:
        user: { $ref: '#/components/schemas/User' }
        roles: { type: array, items: { $ref: '#/components/schemas/Role' } }
        access_token: { type: string }
        refresh_token: { type: string }
        token_type: { type: string, enum: [Bearer] }
        expires_in: { type: integer }
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
`
}
