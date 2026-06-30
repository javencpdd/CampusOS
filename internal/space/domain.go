package space

import "time"

type Visibility string

const (
	VisibilityPublic   Visibility = "public"
	VisibilityUnlisted Visibility = "unlisted"
	VisibilityPrivate  Visibility = "private"
)

type Space struct {
	ID             string     `json:"id"`
	UserID         string     `json:"user_id"`
	Title          string     `json:"title"`
	Bio            string     `json:"bio"`
	Avatar         string     `json:"avatar,omitempty"`
	CoverImage     string     `json:"cover_image,omitempty"`
	Theme          string     `json:"theme"`
	Layout         string     `json:"layout"`
	Visibility     Visibility `json:"visibility"`
	SyncEnabled    bool       `json:"sync_enabled"`
	SyncCategories []string   `json:"sync_categories,omitempty"`
	SyncTags       []string   `json:"sync_tags,omitempty"`
	IsDefault      bool       `json:"is_default"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
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
