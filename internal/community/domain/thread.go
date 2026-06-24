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
	ThreadStatusArchived      ThreadStatus = "archived"
)

// Thread 帖子领域实体
type Thread struct {
	ID            string       `json:"id"`
	Title         string       `json:"title"`
	Content       string       `json:"content"`
	AuthorID      string       `json:"author_id"`
	AuthorName    string       `json:"author_name"`
	CategoryID    string       `json:"category_id"`
	Status        ThreadStatus `json:"status"`
	IsPinned      bool         `json:"is_pinned"`
	IsLocked      bool         `json:"is_locked"`
	IsHighlighted bool         `json:"is_highlighted"`
	ViewCount     int64        `json:"view_count"`
	ReplyCount    int64        `json:"reply_count"`
	LikeCount     int64        `json:"like_count"`
	Tags          []string     `json:"tags,omitempty"`
	CreatedAt     time.Time    `json:"created_at"`
	UpdatedAt     time.Time    `json:"updated_at"`
}

// CreateThreadRequest 创建帖子请求
type CreateThreadRequest struct {
	Title      string   `json:"title" binding:"required,min=1,max=255"`
	Content    string   `json:"content" binding:"required,min=1"`
	CategoryID string   `json:"category_id" binding:"required"`
	Tags       []string `json:"tags,omitempty"`
}

// UpdateThreadRequest 更新帖子请求
type UpdateThreadRequest struct {
	Title   *string  `json:"title,omitempty"`
	Content *string  `json:"content,omitempty"`
	Tags    []string `json:"tags,omitempty"`
}

// ThreadListFilter 帖子列表过滤条件
type ThreadListFilter struct {
	CategoryID string
	AuthorID   string
	Status     string
	Keyword    string
	Page       int
	PageSize   int
}
