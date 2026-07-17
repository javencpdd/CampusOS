package domain

import (
	"time"
)

// ThreadStatus 帖子状态
type ThreadStatus string

const (
	ThreadStatusDraft         ThreadStatus = "draft"
	ThreadStatusPendingReview ThreadStatus = "pending_review"
	ThreadStatusPublished     ThreadStatus = "published"
	ThreadStatusPrivate       ThreadStatus = "private"
	ThreadStatusArchived      ThreadStatus = "archived"
)

// Thread 帖子领域实体
type Thread struct {
	ID                string            `json:"id"`
	ThreadType        ThreadType        `json:"thread_type"`
	Title             string            `json:"title"`
	Content           string            `json:"content"`
	ContentFormat     string            `json:"content_format"`
	AuthorID          string            `json:"author_id"`
	AuthorName        string            `json:"author_name"`
	CategoryID        string            `json:"category_id"`
	Status            ThreadStatus      `json:"status"`
	PublicationStatus PublicationStatus `json:"publication_status,omitempty"`
	ModerationStatus  ModerationStatus  `json:"moderation_status,omitempty"`
	DeletionStatus    DeletionStatus    `json:"deletion_status,omitempty"`
	ModerationReason  string            `json:"moderation_reason,omitempty"`
	ModerationBy      string            `json:"moderation_by,omitempty"`
	ModerationAt      *time.Time        `json:"moderation_at,omitempty"`
	CurrentRevision   int               `json:"current_revision,omitempty"`
	IsPinned          bool              `json:"is_pinned"`
	IsLocked          bool              `json:"is_locked"`
	IsHighlighted     bool              `json:"is_highlighted"`
	ViewCount         int64             `json:"view_count"`
	ReplyCount        int64             `json:"reply_count"`
	LikeCount         int64             `json:"like_count"`
	Tags              []string          `json:"tags,omitempty"`
	CreatedAt         time.Time         `json:"created_at"`
	UpdatedAt         time.Time         `json:"updated_at"`
}

// CreateThreadRequest 创建帖子请求
type CreateThreadRequest struct {
	Title      string   `json:"title" binding:"required,min=1,max=255"`
	Content    string   `json:"content" binding:"required,min=1"`
	CategoryID string   `json:"category_id" binding:"required"`
	Tags       []string `json:"tags,omitempty"`
	IsPrivate  bool     `json:"is_private,omitempty"`
}

// UpdateThreadRequest 更新帖子请求
type UpdateThreadRequest struct {
	Title   *string       `json:"title,omitempty" binding:"omitempty,min=1,max=255"`
	Content *string       `json:"content,omitempty" binding:"omitempty,min=1"`
	Tags    []string      `json:"tags,omitempty"`
	Status  *ThreadStatus `json:"status,omitempty"`
}

// ThreadListFilter 帖子列表过滤条件
type ThreadListFilter struct {
	CategoryID        string
	CategoryIDs       []string
	AuthorID          string
	Status            string
	ThreadType        ThreadType
	ContentFormat     string
	Keyword           string
	Tag               string
	AnyTags           []string
	PublicationStatus string
	ModerationStatus  string
	DeletionStatus    string
	IncludeTrashed    bool
	Page              int
	PageSize          int
}
