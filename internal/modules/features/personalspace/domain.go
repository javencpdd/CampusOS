package space

import "time"

type Visibility string

const (
	VisibilityPublic   Visibility = "public"
	VisibilityUnlisted Visibility = "unlisted"
	VisibilityPrivate  Visibility = "private"
)

type Space struct {
	ID             string         `json:"id"`
	UserID         string         `json:"user_id"`
	Title          string         `json:"title"`
	Bio            string         `json:"bio"`
	Avatar         string         `json:"avatar,omitempty"`
	CoverImage     string         `json:"cover_image,omitempty"`
	Theme          string         `json:"theme"`
	Layout         string         `json:"layout"`
	StyleName      string         `json:"style_name,omitempty"`
	StyleVersion   string         `json:"style_version,omitempty"`
	StyleManifest  *StyleManifest `json:"style_manifest,omitempty"`
	Visibility     Visibility     `json:"visibility"`
	SyncEnabled    bool           `json:"sync_enabled"`
	SyncCategories []string       `json:"sync_categories,omitempty"`
	SyncTags       []string       `json:"sync_tags,omitempty"`
	DisabledAt     *time.Time     `json:"disabled_at,omitempty"`
	DisabledBy     string         `json:"disabled_by,omitempty"`
	DisabledReason string         `json:"disabled_reason,omitempty"`
	LastSyncAt     *time.Time     `json:"last_sync_at,omitempty"`
	LastSyncError  string         `json:"last_sync_error,omitempty"`
	IsDefault      bool           `json:"is_default"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
}

type Owner struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Nickname string `json:"nickname"`
	Avatar   string `json:"avatar,omitempty"`
	Bio      string `json:"bio,omitempty"`
}

type PublicSpace struct {
	Owner Owner  `json:"owner"`
	Space *Space `json:"space"`
}

type SpaceContent struct {
	ID                string    `json:"id"`
	UserID            string    `json:"user_id"`
	ThreadID          string    `json:"thread_id"`
	Title             string    `json:"title"`
	Excerpt           string    `json:"excerpt"`
	AuthorName        string    `json:"author_name"`
	CategoryID        string    `json:"category_id"`
	Tags              []string  `json:"tags,omitempty"`
	Status            string    `json:"status"`
	PublicationStatus string    `json:"publication_status,omitempty"`
	ModerationStatus  string    `json:"moderation_status,omitempty"`
	DeletionStatus    string    `json:"deletion_status,omitempty"`
	ModerationReason  string    `json:"moderation_reason,omitempty"`
	ThreadCreatedAt   time.Time `json:"thread_created_at"`
	ThreadUpdatedAt   time.Time `json:"thread_updated_at"`
	SyncedAt          time.Time `json:"synced_at"`
}

type UpsertSpaceRequest struct {
	Title          *string  `json:"title,omitempty" binding:"omitempty,max=120"`
	Bio            *string  `json:"bio,omitempty" binding:"omitempty,max=500"`
	Avatar         *string  `json:"avatar,omitempty" binding:"omitempty,max=512"`
	CoverImage     *string  `json:"cover_image,omitempty" binding:"omitempty,max=512"`
	Theme          *string  `json:"theme,omitempty" binding:"omitempty,max=64"`
	Layout         *string  `json:"layout,omitempty" binding:"omitempty,max=64"`
	Visibility     *string  `json:"visibility,omitempty" binding:"omitempty,oneof=public unlisted private"`
	SyncEnabled    *bool    `json:"sync_enabled,omitempty"`
	SyncCategories []string `json:"sync_categories,omitempty"`
	SyncTags       []string `json:"sync_tags,omitempty"`
}

type StyleSnapshot struct {
	ID            string         `json:"id"`
	UserID        string         `json:"user_id"`
	SnapshotType  string         `json:"snapshot_type"`
	StyleName     string         `json:"style_name,omitempty"`
	StyleVersion  string         `json:"style_version,omitempty"`
	Theme         string         `json:"theme"`
	Layout        string         `json:"layout"`
	StyleManifest *StyleManifest `json:"style_manifest,omitempty"`
	CreatedAt     time.Time      `json:"created_at"`
}

type SpaceSyncStatus struct {
	UserID        string     `json:"user_id"`
	SyncEnabled   bool       `json:"sync_enabled"`
	LastSyncAt    *time.Time `json:"last_sync_at,omitempty"`
	LastSyncError string     `json:"last_sync_error,omitempty"`
	ContentTotal  int64      `json:"content_total"`
	Disabled      bool       `json:"disabled"`
}

type SpaceAdminSummary struct {
	TotalSpaces       int64      `json:"total_spaces"`
	PublicSpaces      int64      `json:"public_spaces"`
	DisabledSpaces    int64      `json:"disabled_spaces"`
	StyledSpaces      int64      `json:"styled_spaces"`
	SyncEnabledSpaces int64      `json:"sync_enabled_spaces"`
	LastSyncAt        *time.Time `json:"last_sync_at,omitempty"`
	SyncErrorSpaces   int64      `json:"sync_error_spaces"`
}
