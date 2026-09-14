package richtext

import (
	"encoding/json"
	"errors"
	"time"
)

const (
	PluginName    = "controlled-richtext-article"
	ContentFormat = "richtext_article"

	StatusDraft         = "draft"
	StatusPublished     = "published"
	StatusPendingReview = "pending_review"
	StatusOffline       = "offline"
	StatusTrashed       = "trashed"
	StatusDeleted       = "deleted"
)

var (
	ErrPluginDisabled         = errors.New("controlled-richtext-article plugin is disabled")
	ErrArticleNotFound        = errors.New("richtext article not found")
	ErrPermissionDenied       = errors.New("permission denied")
	ErrInvalidArticle         = errors.New("invalid richtext article")
	ErrAssetUnavailable       = errors.New("richtext asset store is unavailable")
	ErrAssetInvalid           = errors.New("invalid richtext asset")
	ErrAssetTooLarge          = errors.New("richtext asset exceeds allowed size")
	ErrAssetQuotaExceeded     = errors.New("richtext asset exceeds personal space quota")
	ErrAssetUnsupported       = errors.New("unsupported richtext asset type")
	ErrAssetNotFound          = errors.New("richtext asset not found")
	ErrAttachmentNotFound     = errors.New("richtext article attachment not found")
	ErrAttachmentAlreadyBound = errors.New("richtext article attachment is already bound")
	ErrAttachmentLimit        = errors.New("richtext article attachment limit reached")
	ErrAttachmentTotalSize    = errors.New("richtext article attachment total size exceeded")
	ErrAttachmentType         = errors.New("richtext article attachment type is not allowed")
	ErrAttachmentContent      = errors.New("richtext article attachment content is invalid")
	ErrAssetReferenced        = errors.New("richtext asset is still referenced")
	ErrAssetPurgeIneligible   = errors.New("richtext asset is not eligible for purge")
	ErrInvocationNotFound     = errors.New("plugin ui invocation was not found")
	ErrInvocationExpired      = errors.New("plugin ui invocation has expired")
	ErrInvocationPresentation = errors.New("plugin ui invocation presentation is invalid")
)

const (
	AssetKindAttachment = "article_attachment"
	AssetKindImage      = "richtext_image"

	AssetStatusActive      = "active"
	AssetStatusTrashed     = "trashed"
	AssetStatusQuarantined = "quarantined"
	AssetStatusPurging     = "purging"
	AssetStatusDeleted     = "deleted"

	PDFViewerPluginKey = "builtin.pdf-viewer"
	PDFViewerSurfaceID = "builtin.pdf-viewer.preview"

	// Invocation contexts deliberately distinguish an article attachment from a
	// private owner asset.  The opaque invocation ID is never a file-download
	// credential, and the context is re-authorized every time its bytes are read.
	InvocationContextArticleAttachment = "article_attachment"
	InvocationContextPersonalAsset     = "personal_asset"
)

type Article struct {
	ID            string              `json:"id"`
	ThreadID      string              `json:"thread_id"`
	Title         string              `json:"title"`
	Summary       string              `json:"summary,omitempty"`
	CoverURL      string              `json:"cover_url,omitempty"`
	ContentHTML   string              `json:"content_html,omitempty"`
	ContentJSON   json.RawMessage     `json:"content_json,omitempty"`
	SanitizedHTML string              `json:"sanitized_html"`
	RenderHTML    string              `json:"render_html,omitempty"`
	Status        string              `json:"status"`
	CreatedBy     string              `json:"created_by"`
	UpdatedBy     string              `json:"updated_by,omitempty"`
	PublishedAt   *time.Time          `json:"published_at,omitempty"`
	Attachments   []ArticleAttachment `json:"attachments,omitempty"`
	CreatedAt     time.Time           `json:"created_at"`
	UpdatedAt     time.Time           `json:"updated_at"`
}

type Asset struct {
	ID               string    `json:"id"`
	ThreadID         string    `json:"thread_id,omitempty"`
	ArticleContentID string    `json:"article_content_id,omitempty"`
	UploaderID       string    `json:"uploader_id"`
	FileURL          string    `json:"file_url"`
	FileName         string    `json:"file_name"`
	FileSize         int64     `json:"file_size"`
	MimeType         string    `json:"mime_type"`
	Width            int       `json:"width,omitempty"`
	Height           int       `json:"height,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
}

// UserAsset is the minimal owner-scoped identity that a RichText attachment
// can reference.  It deliberately never exposes a provider key or host path.
type UserAsset struct {
	ID              string     `json:"id"`
	OwnerID         string     `json:"owner_id"`
	Kind            string     `json:"kind"`
	OriginalName    string     `json:"original_name"`
	StorageObjectID string     `json:"storage_object_id"`
	MimeType        string     `json:"mime_type"`
	SizeBytes       int64      `json:"size_bytes"`
	Status          string     `json:"status"`
	Version         int64      `json:"version"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
	TrashedAt       *time.Time `json:"trashed_at,omitempty"`
	DeletedAt       *time.Time `json:"deleted_at,omitempty"`
}

// AssetLifecycleAudit intentionally contains operational facts only. It must
// never gain an object key, host path, original filename, MIME payload or
// document content, so the Admin aggregate view cannot become a file browser.
type AssetLifecycleAudit struct {
	ID        string    `json:"id"`
	AssetID   string    `json:"asset_id,omitempty"`
	ActorID   string    `json:"actor_id,omitempty"`
	ActorType string    `json:"actor_type"`
	Action    string    `json:"action"`
	Reason    string    `json:"reason,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type AssetStatusSummary struct {
	Status    string `json:"status"`
	Count     int64  `json:"count"`
	SizeBytes int64  `json:"size_bytes"`
}

type AssetActionSummary struct {
	Action string `json:"action"`
	Count  int64  `json:"count"`
}

// AssetGovernanceSummary is deliberately aggregate-only: it is safe to show
// in the Admin control plane without disclosing a user's files or identities.
type AssetGovernanceSummary struct {
	Statuses           []AssetStatusSummary `json:"statuses"`
	Actions            []AssetActionSummary `json:"actions"`
	ExpiredInvocations int64                `json:"expired_invocations"`
	GeneratedAt        time.Time            `json:"generated_at"`
}

type AssetPurgePreview struct {
	AssetID        string `json:"asset_id"`
	Status         string `json:"status"`
	SizeBytes      int64  `json:"size_bytes"`
	ReferenceCount int    `json:"reference_count"`
	Eligible       bool   `json:"eligible"`
	Reason         string `json:"reason"`
}

// ArticleAttachment is an explicit article-to-asset binding.  The display
// metadata belongs here rather than in sanitized article HTML.
type ArticleAttachment struct {
	ID               string    `json:"id"`
	ArticleContentID string    `json:"article_content_id"`
	AssetID          string    `json:"asset_id"`
	DisplayName      string    `json:"display_name"`
	DisplayOrder     int       `json:"display_order"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
	Asset            UserAsset `json:"asset"`
}

// PluginUIInvocation is a short-lived, opaque server-side context.  Its ID
// is safe to put in a same-origin route, but is not a download credential.
type PluginUIInvocation struct {
	ID               string     `json:"id"`
	UserID           string     `json:"user_id"`
	PluginKey        string     `json:"plugin_key"`
	SurfaceID        string     `json:"surface_id"`
	ContextKind      string     `json:"context_kind"`
	ArticleContentID string     `json:"article_content_id"`
	AssetID          string     `json:"asset_id,omitempty"`
	AttachmentID     string     `json:"attachment_id,omitempty"`
	Presentation     string     `json:"presentation"`
	Purpose          string     `json:"purpose"`
	ExpiresAt        time.Time  `json:"expires_at"`
	OpenedAt         *time.Time `json:"opened_at,omitempty"`
	RevokedAt        *time.Time `json:"revoked_at,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
}

type StatusResult struct {
	Enabled       bool   `json:"enabled"`
	DefaultEditor string `json:"default_editor"`
	PluginName    string `json:"plugin_name"`
}

type SaveArticleRequest struct {
	Title       string          `json:"title" binding:"required,min=1,max=255"`
	Summary     string          `json:"summary,omitempty"`
	CoverURL    string          `json:"cover_url,omitempty"`
	CategoryID  string          `json:"category_id,omitempty"`
	Tags        []string        `json:"tags,omitempty"`
	ContentHTML string          `json:"content_html" binding:"required,min=1"`
	ContentJSON json.RawMessage `json:"content_json,omitempty"`
}

type PreviewRequest struct {
	ContentHTML string `json:"content_html" binding:"required,min=1"`
}

type ArticleResult struct {
	ThreadID         string   `json:"thread_id"`
	ArticleContentID string   `json:"article_content_id"`
	Status           string   `json:"status"`
	Article          *Article `json:"article"`
}

type AttachmentReorderRequest struct {
	AttachmentIDs []string `json:"attachment_ids" binding:"required,min=1,max=10"`
}

type AttachmentBindRequest struct {
	AssetID     string `json:"asset_id" binding:"required"`
	DisplayName string `json:"display_name,omitempty"`
}

type AttachmentUpdateRequest struct {
	DisplayName string `json:"display_name" binding:"required"`
}

type PluginUIInvocationRequest struct {
	AttachmentID string `json:"attachment_id" binding:"required"`
	Presentation string `json:"presentation" binding:"required"`
}

type PersonalAssetPDFInvocationRequest struct {
	Presentation string `json:"presentation" binding:"required"`
}

type AssetGovernanceActionRequest struct {
	Reason         string `json:"reason" binding:"required,max=500"`
	ConfirmAssetID string `json:"confirm_asset_id,omitempty"`
}

type PreviewResult struct {
	SanitizedHTML string `json:"sanitized_html"`
	RenderHTML    string `json:"render_html"`
}
