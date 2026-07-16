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
	ErrPluginDisabled     = errors.New("controlled-richtext-article plugin is disabled")
	ErrArticleNotFound    = errors.New("richtext article not found")
	ErrPermissionDenied   = errors.New("permission denied")
	ErrInvalidArticle     = errors.New("invalid richtext article")
	ErrAssetUnavailable   = errors.New("richtext asset store is unavailable")
	ErrAssetInvalid       = errors.New("invalid richtext asset")
	ErrAssetTooLarge      = errors.New("richtext asset exceeds allowed size")
	ErrAssetQuotaExceeded = errors.New("richtext asset exceeds personal space quota")
	ErrAssetUnsupported   = errors.New("unsupported richtext asset type")
	ErrAssetNotFound      = errors.New("richtext asset not found")
)

type Article struct {
	ID            string          `json:"id"`
	ThreadID      string          `json:"thread_id"`
	Title         string          `json:"title"`
	Summary       string          `json:"summary,omitempty"`
	CoverURL      string          `json:"cover_url,omitempty"`
	ContentHTML   string          `json:"content_html,omitempty"`
	ContentJSON   json.RawMessage `json:"content_json,omitempty"`
	SanitizedHTML string          `json:"sanitized_html"`
	RenderHTML    string          `json:"render_html,omitempty"`
	Status        string          `json:"status"`
	CreatedBy     string          `json:"created_by"`
	UpdatedBy     string          `json:"updated_by,omitempty"`
	PublishedAt   *time.Time      `json:"published_at,omitempty"`
	CreatedAt     time.Time       `json:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at"`
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

type PreviewResult struct {
	SanitizedHTML string `json:"sanitized_html"`
	RenderHTML    string `json:"render_html"`
}
