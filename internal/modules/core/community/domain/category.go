package domain

import (
	"encoding/json"
	"errors"
	"strings"
	"time"
)

type CategoryNodeKind string

const (
	CategoryNodeGroup CategoryNodeKind = "group"
	CategoryNodeBoard CategoryNodeKind = "board"
)

type CategoryLifecycleStatus string

const (
	CategoryLifecycleActive   CategoryLifecycleStatus = "active"
	CategoryLifecycleArchived CategoryLifecycleStatus = "archived"
)

type Category struct {
	ID              string                  `json:"id"`
	Name            string                  `json:"name"`
	Slug            string                  `json:"slug"`
	Description     string                  `json:"description"`
	Icon            string                  `json:"icon,omitempty"`
	Color           string                  `json:"color,omitempty"`
	DefaultTags     []string                `json:"default_tags,omitempty"`
	ParentID        *string                 `json:"parent_id,omitempty"`
	NodeKind        CategoryNodeKind        `json:"node_kind"`
	LifecycleStatus CategoryLifecycleStatus `json:"lifecycle_status"`
	Version         int64                   `json:"version"`
	SortOrder       int                     `json:"sort_order"`
	ThreadCount     int64                   `json:"thread_count"`
	PostCount       int64                   `json:"post_count"`
	IsClosed        bool                    `json:"is_closed"`
	Children        []*Category             `json:"children,omitempty"`
	CreatedAt       time.Time               `json:"created_at"`
	UpdatedAt       time.Time               `json:"updated_at"`
}

func (c *Category) NormalizeHierarchy() {
	if c == nil {
		return
	}
	if c.NodeKind == "" {
		c.NodeKind = CategoryNodeBoard
	}
	if c.LifecycleStatus == "" {
		c.LifecycleStatus = CategoryLifecycleActive
	}
	if c.Version < 1 {
		c.Version = 1
	}
	if c.NodeKind == CategoryNodeGroup {
		c.ParentID = nil
	}
}

func (c *Category) CanAcceptThreads() bool {
	if c == nil {
		return false
	}
	c.NormalizeHierarchy()
	return c.NodeKind == CategoryNodeBoard && c.LifecycleStatus == CategoryLifecycleActive && !c.IsClosed
}

func (c *Category) IsActiveGroup() bool {
	if c == nil {
		return false
	}
	c.NormalizeHierarchy()
	return c.NodeKind == CategoryNodeGroup && c.LifecycleStatus == CategoryLifecycleActive
}

type CreateCategoryRequest struct {
	Name        string           `json:"name" binding:"required,min=1,max=64"`
	Slug        string           `json:"slug" binding:"omitempty,min=1,max=64"`
	Description string           `json:"description" binding:"omitempty,max=500"`
	Icon        string           `json:"icon" binding:"omitempty,max=512"`
	Color       string           `json:"color" binding:"omitempty,max=9"`
	DefaultTags []string         `json:"default_tags,omitempty"`
	ParentID    *string          `json:"parent_id,omitempty"`
	NodeKind    CategoryNodeKind `json:"node_kind" binding:"omitempty,oneof=group board"`
	SortOrder   int              `json:"sort_order"`
	IsClosed    bool             `json:"is_closed"`
}

type UpdateCategoryRequest struct {
	Name        *string  `json:"name,omitempty" binding:"omitempty,min=1,max=64"`
	Slug        *string  `json:"slug,omitempty" binding:"omitempty,min=1,max=64"`
	Description *string  `json:"description,omitempty" binding:"omitempty,max=500"`
	Icon        *string  `json:"icon,omitempty" binding:"omitempty,max=512"`
	Color       *string  `json:"color,omitempty" binding:"omitempty,max=9"`
	DefaultTags []string `json:"default_tags,omitempty"`
	// ParentID remains accepted for one compatibility window but cannot move a
	// category. Movement is an explicit versioned command below.
	ParentID  *string `json:"parent_id,omitempty"`
	IsClosed  *bool   `json:"is_closed,omitempty"`
	SortOrder *int    `json:"sort_order,omitempty"`
	Version   int64   `json:"version" binding:"required,min=1"`
}

type MoveCategoryRequest struct {
	ParentID        *string `json:"-"`
	ParentSpecified bool    `json:"-"`
	Version         int64   `json:"version"`
}

// UnmarshalJSON preserves the difference between an omitted parent_id and an
// explicit JSON null. That difference is required for safe moves to root.
func (r *MoveCategoryRequest) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	parent, ok := raw["parent_id"]
	if !ok {
		return errors.New("parent_id must be supplied; use null to move to root")
	}
	r.ParentSpecified = true
	if string(parent) != "null" {
		var value string
		if err := json.Unmarshal(parent, &value); err != nil {
			return errors.New("parent_id must be a string or null")
		}
		value = strings.TrimSpace(value)
		if value == "" {
			return errors.New("parent_id must be a non-empty string or null")
		}
		r.ParentID = &value
	} else {
		r.ParentID = nil
	}
	version, ok := raw["version"]
	if !ok {
		return errors.New("version is required")
	}
	if err := json.Unmarshal(version, &r.Version); err != nil || r.Version < 1 {
		return errors.New("version must be a positive integer")
	}
	return nil
}

type CategoryArchiveImpact struct {
	CategoryID          string `json:"category_id"`
	NodeKind            string `json:"node_kind"`
	ActiveChildBoards   int    `json:"active_child_boards"`
	AssociatedThreads   int64  `json:"associated_threads"`
	WillBlockNewPosting bool   `json:"will_block_new_posting"`
}
