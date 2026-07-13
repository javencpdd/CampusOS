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
