package community

import (
	"context"
	"errors"

	"github.com/campusos/CampusOS/internal/community/domain"
	communityport "github.com/campusos/CampusOS/internal/community/port"
	"github.com/campusos/CampusOS/internal/community/repository"
	"github.com/campusos/CampusOS/internal/community/service"
)

type moderationGateway struct {
	categories repository.CategoryRepository
	threads    repository.ThreadRepository
	posts      repository.PostRepository
	threadSvc  *service.ThreadService
	postSvc    *service.PostService
}

type moduleModerationGateway struct{ module *Module }

type moduleContentGateway struct{ module *Module }
type moduleCategoryCatalog struct{ module *Module }

type contentGateway struct {
	threads   repository.ThreadRepository
	threadSvc *service.ThreadService
}

func (g *moduleModerationGateway) gateway() (*moderationGateway, error) {
	if g == nil || g.module == nil || g.module.threadService == nil || g.module.postService == nil {
		return nil, errors.New("community moderation gateway is not started")
	}
	return &moderationGateway{categories: g.module.categories, threads: g.module.threads, posts: g.module.posts, threadSvc: g.module.threadService, postSvc: g.module.postService}, nil
}

func (g *moduleModerationGateway) GetCategory(ctx context.Context, id string) (*domain.Category, error) {
	delegate, err := g.gateway()
	if err != nil {
		return nil, err
	}
	return delegate.GetCategory(ctx, id)
}

func (g *moduleModerationGateway) GetThread(ctx context.Context, id string) (*domain.Thread, error) {
	delegate, err := g.gateway()
	if err != nil {
		return nil, err
	}
	return delegate.GetThread(ctx, id)
}

func (g *moduleModerationGateway) GetPost(ctx context.Context, id string) (*domain.Post, error) {
	delegate, err := g.gateway()
	if err != nil {
		return nil, err
	}
	return delegate.GetPost(ctx, id)
}

func (g *moduleModerationGateway) SetPinned(ctx context.Context, id string, pinned bool) (*domain.Thread, error) {
	delegate, err := g.gateway()
	if err != nil {
		return nil, err
	}
	return delegate.SetPinned(ctx, id, pinned)
}

func (g *moduleModerationGateway) SetLocked(ctx context.Context, id string, locked bool) (*domain.Thread, error) {
	delegate, err := g.gateway()
	if err != nil {
		return nil, err
	}
	return delegate.SetLocked(ctx, id, locked)
}

func (g *moduleModerationGateway) DeletePostForModeration(ctx context.Context, id string) error {
	delegate, err := g.gateway()
	if err != nil {
		return err
	}
	return delegate.DeletePostForModeration(ctx, id)
}

func (g *moduleContentGateway) gateway() (*moderationGateway, error) {
	if g == nil || g.module == nil || g.module.threadService == nil {
		return nil, errors.New("community content gateway is not started")
	}
	return &moderationGateway{categories: g.module.categories, threads: g.module.threads, posts: g.module.posts, threadSvc: g.module.threadService, postSvc: g.module.postService}, nil
}

func (g *moduleContentGateway) CreateThread(ctx context.Context, authorID, authorName string, request domain.CreateThreadRequest, options communityport.ThreadCreateOptions) (*domain.Thread, error) {
	delegate, err := g.gateway()
	if err != nil {
		return nil, err
	}
	return delegate.threadSvc.CreateThreadWithOptions(ctx, authorID, authorName, request, service.CreateThreadOptions{
		Status:        options.Status,
		ContentFormat: options.ContentFormat,
	})
}

func (g *moduleContentGateway) GetThread(ctx context.Context, id string) (*domain.Thread, error) {
	delegate, err := g.gateway()
	if err != nil {
		return nil, err
	}
	return delegate.threads.GetByID(ctx, id)
}

func (g *moduleContentGateway) UpdateThread(ctx context.Context, thread *domain.Thread) error {
	delegate, err := g.gateway()
	if err != nil {
		return err
	}
	return delegate.threads.Update(ctx, thread)
}

func (g *moduleContentGateway) DeleteThread(ctx context.Context, id string) error {
	delegate, err := g.gateway()
	if err != nil {
		return err
	}
	return delegate.threads.Delete(ctx, id)
}

func (g *moduleContentGateway) ListThreads(ctx context.Context, filter domain.ThreadListFilter) ([]*domain.Thread, int64, error) {
	delegate, err := g.gateway()
	if err != nil {
		return nil, 0, err
	}
	return delegate.threads.List(ctx, filter)
}

func (g *moduleContentGateway) InvalidateThreadList(ctx context.Context) {
	if delegate, err := g.gateway(); err == nil {
		delegate.threadSvc.InvalidateListCache(ctx)
	}
}

func (g *moduleCategoryCatalog) ListCategories(ctx context.Context) ([]*domain.Category, error) {
	if g == nil || g.module == nil || g.module.categories == nil {
		return nil, errors.New("community category catalog is unavailable")
	}
	return g.module.categories.List(ctx)
}

// NewContentGateway is the compatibility constructor for tests and legacy
// composition. Production modules obtain the module-owned gateway through
// the "community.content-gateway" port.
func NewContentGateway(threads repository.ThreadRepository, threadSvc *service.ThreadService) communityport.ContentGateway {
	return &contentGateway{threads: threads, threadSvc: threadSvc}
}

func (g *contentGateway) CreateThread(ctx context.Context, authorID, authorName string, request domain.CreateThreadRequest, options communityport.ThreadCreateOptions) (*domain.Thread, error) {
	if g == nil || g.threadSvc == nil {
		return nil, errors.New("community content gateway is unavailable")
	}
	return g.threadSvc.CreateThreadWithOptions(ctx, authorID, authorName, request, service.CreateThreadOptions{Status: options.Status, ContentFormat: options.ContentFormat})
}

func (g *contentGateway) GetThread(ctx context.Context, id string) (*domain.Thread, error) {
	if g == nil || g.threads == nil {
		return nil, errors.New("community content gateway is unavailable")
	}
	return g.threads.GetByID(ctx, id)
}

func (g *contentGateway) UpdateThread(ctx context.Context, thread *domain.Thread) error {
	if g == nil || g.threads == nil {
		return errors.New("community content gateway is unavailable")
	}
	return g.threads.Update(ctx, thread)
}

func (g *contentGateway) DeleteThread(ctx context.Context, id string) error {
	if g == nil || g.threads == nil {
		return errors.New("community content gateway is unavailable")
	}
	return g.threads.Delete(ctx, id)
}

func (g *contentGateway) ListThreads(ctx context.Context, filter domain.ThreadListFilter) ([]*domain.Thread, int64, error) {
	if g == nil || g.threads == nil {
		return nil, 0, errors.New("community content gateway is unavailable")
	}
	return g.threads.List(ctx, filter)
}

func (g *contentGateway) InvalidateThreadList(ctx context.Context) {
	if g != nil && g.threadSvc != nil {
		g.threadSvc.InvalidateListCache(ctx)
	}
}

func NewModerationGateway(categories repository.CategoryRepository, threads repository.ThreadRepository, posts repository.PostRepository, threadSvc *service.ThreadService, postSvc *service.PostService) communityport.ModerationGateway {
	return &moderationGateway{categories: categories, threads: threads, posts: posts, threadSvc: threadSvc, postSvc: postSvc}
}

func (g *moderationGateway) GetCategory(ctx context.Context, id string) (*domain.Category, error) {
	value, err := g.categories.GetByID(ctx, id)
	if errors.Is(err, repository.ErrCategoryNotFound) {
		return nil, communityport.ErrCategoryNotFound
	}
	return value, err
}

func (g *moderationGateway) GetThread(ctx context.Context, id string) (*domain.Thread, error) {
	value, err := g.threads.GetByID(ctx, id)
	if errors.Is(err, repository.ErrThreadNotFound) {
		return nil, communityport.ErrThreadNotFound
	}
	return value, err
}

func (g *moderationGateway) GetPost(ctx context.Context, id string) (*domain.Post, error) {
	value, err := g.posts.GetByID(ctx, id)
	if errors.Is(err, repository.ErrPostNotFound) {
		return nil, communityport.ErrPostNotFound
	}
	return value, err
}

func (g *moderationGateway) SetPinned(ctx context.Context, id string, pinned bool) (*domain.Thread, error) {
	var value *domain.Thread
	var err error
	if pinned {
		value, err = g.threadSvc.PinThread(ctx, id)
	} else {
		value, err = g.threadSvc.UnpinThread(ctx, id)
	}
	if errors.Is(err, repository.ErrThreadNotFound) {
		return nil, communityport.ErrThreadNotFound
	}
	return value, err
}

func (g *moderationGateway) SetLocked(ctx context.Context, id string, locked bool) (*domain.Thread, error) {
	var value *domain.Thread
	var err error
	if locked {
		value, err = g.threadSvc.LockThread(ctx, id)
	} else {
		value, err = g.threadSvc.UnlockThread(ctx, id)
	}
	if errors.Is(err, repository.ErrThreadNotFound) {
		return nil, communityport.ErrThreadNotFound
	}
	return value, err
}

func (g *moderationGateway) DeletePostForModeration(ctx context.Context, id string) error {
	err := g.postSvc.AdminDeletePost(ctx, id)
	if errors.Is(err, repository.ErrPostNotFound) {
		return communityport.ErrPostNotFound
	}
	return err
}
