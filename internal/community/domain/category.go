package domain

import "time"

type Category struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Slug        string    `json:"slug"`
	Description string    `json:"description"`
	Icon        string    `json:"icon,omitempty"`
	ParentID    *string   `json:"parent_id,omitempty"`
	SortOrder   int       `json:"sort_order"`
	ThreadCount int64     `json:"thread_count"`
	PostCount   int64     `json:"post_count"`
	IsClosed    bool      `json:"is_closed"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type CreateCategoryRequest struct {
	Name        string  `json:"name" binding:"required,min=1,max=64"`
	Slug        string  `json:"slug" binding:"omitempty,min=1,max=64"`
	Description string  `json:"description" binding:"omitempty,max=500"`
	Icon        string  `json:"icon" binding:"omitempty,max=512"`
	ParentID    *string `json:"parent_id,omitempty"`
	SortOrder   int     `json:"sort_order"`
	IsClosed    bool    `json:"is_closed"`
}

type UpdateCategoryRequest struct {
	Name        *string `json:"name,omitempty"`
	Slug        *string `json:"slug,omitempty"`
	Description *string `json:"description,omitempty"`
	Icon        *string `json:"icon,omitempty"`
	ParentID    *string `json:"parent_id,omitempty"`
	IsClosed    *bool   `json:"is_closed,omitempty"`
	SortOrder   *int    `json:"sort_order,omitempty"`
}
