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

// CategoryCatalog is the public read contract for appearance and other
// presentation features that need category display metadata.
type CategoryCatalog interface {
	ListCategories(context.Context) ([]*domain.Category, error)
}

// ContentQuery is Community's read-only content fact contract. Consumers may
// choose author/category/tag/pagination filters, but cannot widen public
// visibility by submitting publication, moderation, or deletion states.
type ContentQuery interface {
	GetPublicThread(context.Context, string) (*domain.Thread, error)
	ListPublicThreads(context.Context, domain.ThreadListFilter) ([]*domain.Thread, int64, error)
	ListAuthorThreads(context.Context, string, domain.ThreadListFilter) ([]*domain.Thread, int64, error)
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

// ContentGateway is the stable Community contract consumed by built-in
// content features. It deliberately exposes application commands rather than
// repositories or Community service implementations.
type ContentGateway interface {
	CreateThread(context.Context, string, string, domain.CreateThreadRequest, ThreadCreateOptions) (*domain.Thread, error)
	GetThread(context.Context, string) (*domain.Thread, error)
	SaveFeatureThread(context.Context, *domain.Thread, string, string) (*domain.Thread, error)
	TrashThread(context.Context, string, string, string, string) error
	SubmitThreadForReview(context.Context, string, string) (*domain.Thread, error)
	TakeDownThread(context.Context, string, string, string) (*domain.Thread, error)
	RestoreThreadDirectly(context.Context, string, string, string) (*domain.Thread, error)
	RestoreThreadFromTrash(context.Context, string, string) (*domain.Thread, error)
	ListThreads(context.Context, domain.ThreadListFilter) ([]*domain.Thread, int64, error)
	InvalidateThreadList(context.Context)
}

type ThreadCreateOptions struct {
	Status        domain.ThreadStatus
	ContentFormat string
}
