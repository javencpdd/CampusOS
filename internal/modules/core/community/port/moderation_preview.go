package port

import (
	"context"
	"errors"

	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
)

const MaxModerationPreviewPosts = 50

var ErrModerationPreviewInvalid = errors.New("moderation preview request invalid")

// This bounded batch has fixed resource types and named actions. Callers
// supply IDs only; all resource, actor and execution facts come from the host.
type ModerationPreviewConfig struct {
	Pin, Lock, DeletePost bool
}

type PostModerationPreview struct {
	ID     string `json:"id"`
	Delete bool   `json:"delete"`
}

type UserModerationPreview struct {
	ThreadID string                  `json:"thread_id"`
	Pin      bool                    `json:"pin"`
	Lock     bool                    `json:"lock"`
	Posts    []PostModerationPreview `json:"posts"`
}

type CheckedModerationPreview interface {
	PreviewUserModeration(context.Context, string, []string, identityport.SessionPrincipalResolver, func(context.Context) (ModerationPreviewConfig, error)) (*UserModerationPreview, error)
}
