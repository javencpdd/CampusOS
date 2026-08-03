package domain

import "time"

const (
	NotificationTypeThreadTrashed          = "community.thread.trashed"
	NotificationTypeThreadTakenDown        = "community.thread.taken_down"
	NotificationTypeThreadReplied          = "community.thread.replied"
	NotificationTypePostReplied            = "community.post.replied"
	NotificationTypePostDeletedByModerator = "community.post.deleted_by_moderator"
	NotificationTypeModeratorGranted       = "identity.moderator.granted"
	NotificationTypeModeratorRevoked       = "identity.moderator.revoked"
)

type Notification struct {
	ID        string                 `json:"id"`
	UserID    string                 `json:"user_id"`
	Type      string                 `json:"type"`
	Title     string                 `json:"title"`
	Content   string                 `json:"content"`
	ActionURL string                 `json:"action_url"`
	IsRead    bool                   `json:"is_read"`
	ReadAt    *time.Time             `json:"read_at,omitempty"`
	Metadata  map[string]interface{} `json:"metadata"`
	CreatedAt time.Time              `json:"created_at"`
	UpdatedAt time.Time              `json:"updated_at"`
}

type NotificationList struct {
	Items       []*Notification `json:"items"`
	Page        int             `json:"page"`
	PageSize    int             `json:"page_size"`
	Total       int64           `json:"total"`
	UnreadCount int64           `json:"unread_count"`
}
