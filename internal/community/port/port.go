package port

import (
	"context"
	"errors"

	"github.com/campusos/CampusOS/internal/community/domain"
)

var (
	ErrCategoryNotFound = errors.New("community category not found")
	ErrThreadNotFound   = errors.New("community thread not found")
	ErrPostNotFound     = errors.New("community post not found")
)

type Category struct{ ID, Name string }
type Thread struct{ ID, CategoryID, AuthorID, Status string }
type Post struct{ ID, ThreadID, AuthorID, Status string }
type CategoryReader interface {
	GetCategory(context.Context, string) (Category, error)
}
type ThreadReader interface {
	GetThread(context.Context, string) (Thread, error)
}
type ThreadWriter interface {
	SetThreadStatus(context.Context, string, string) error
}
type PostReader interface {
	GetPost(context.Context, string) (Post, error)
}
type PostWriter interface {
	SetPostStatus(context.Context, string, string) error
}

// ModerationGateway is Community's public governance command contract.
// Moderation receives domain values but cannot access repositories or services.
type ModerationGateway interface {
	GetCategory(context.Context, string) (*domain.Category, error)
	GetThread(context.Context, string) (*domain.Thread, error)
	GetPost(context.Context, string) (*domain.Post, error)
	SetPinned(context.Context, string, bool) (*domain.Thread, error)
	SetLocked(context.Context, string, bool) (*domain.Thread, error)
	DeletePostForModeration(context.Context, string) error
}
