package domain

import (
	"strings"
	"time"
)

// ThreadType is the stable business classification for a Community thread.
// ContentFormat only controls how the body is rendered; it must not be used
// to infer a feature's business lifecycle or schema.
type ThreadType string

const (
	ThreadTypeDiscussion ThreadType = "discussion"
	ThreadTypeArticle    ThreadType = "article"
	ThreadTypeMutualAid  ThreadType = "mutual_aid"
	ThreadTypeSecondhand ThreadType = "secondhand"
)

func NormalizeThreadType(value ThreadType) ThreadType {
	value = ThreadType(strings.TrimSpace(strings.ToLower(string(value))))
	if value == "" {
		return ThreadTypeDiscussion
	}
	return value
}

func IsKnownThreadType(value ThreadType) bool {
	switch NormalizeThreadType(value) {
	case ThreadTypeDiscussion, ThreadTypeArticle, ThreadTypeMutualAid, ThreadTypeSecondhand:
		return true
	default:
		return false
	}
}

func DefaultCategoryThreadTypes() []ThreadType {
	return []ThreadType{ThreadTypeDiscussion, ThreadTypeArticle}
}

type CategoryThreadTypePolicy struct {
	CategoryID string     `json:"category_id"`
	ThreadType ThreadType `json:"thread_type"`
	Enabled    bool       `json:"enabled"`
	UpdatedAt  time.Time  `json:"updated_at,omitempty"`
}

type UpdateCategoryThreadTypePolicyRequest struct {
	AllowedTypes []ThreadType `json:"allowed_types" binding:"required,min=1"`
	Version      int64        `json:"version" binding:"required,min=1"`
}
