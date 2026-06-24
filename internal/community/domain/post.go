package domain

import "time"

type Post struct {
	ID          string    `json:"id"`
	ThreadID    string    `json:"thread_id"`
	AuthorID    string    `json:"author_id"`
	AuthorName  string    `json:"author_name"`
	ParentID    *string   `json:"parent_id,omitempty"`
	Content     string    `json:"content"`
	Status      string    `json:"status"`
	LikeCount   int64     `json:"like_count"`
	FloorNumber int       `json:"floor_number"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type CreatePostRequest struct {
	Content  string  `json:"content" binding:"required,min=1"`
	ParentID *string `json:"parent_id,omitempty"`
}
