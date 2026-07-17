package service

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	"github.com/campusos/CampusOS/internal/modules/core/community/repository"
	"github.com/campusos/CampusOS/pkg/cache"
	"github.com/campusos/CampusOS/pkg/eventbus"
	"github.com/campusos/CampusOS/pkg/idgen"
)

type PostService struct {
	repo         repository.PostRepository
	threadRepo   repository.ThreadRepository
	categoryRepo repository.CategoryRepository
	bus          eventbus.EventBus
	cache        cache.Cache
}

func NewPostService(repo repository.PostRepository, bus eventbus.EventBus) *PostService {
	return &PostService{repo: repo, bus: bus}
}

func (s *PostService) SetThreadRepository(repo repository.ThreadRepository) {
	s.threadRepo = repo
}

func (s *PostService) SetCategoryRepository(repo repository.CategoryRepository) {
	s.categoryRepo = repo
}

func (s *PostService) SetCache(c cache.Cache) {
	s.cache = c
}

func (s *PostService) CreatePost(ctx context.Context, threadID, authorID, authorName string, req domain.CreatePostRequest) (*domain.Post, error) {
	if s.threadRepo != nil {
		thread, err := s.threadRepo.GetByID(ctx, threadID)
		if err != nil {
			return nil, fmt.Errorf("get thread: %w", err)
		}
		if thread.IsLocked {
			return nil, fmt.Errorf("thread is locked")
		}
		if thread.Status != domain.ThreadStatusPublished && thread.AuthorID != authorID {
			return nil, repository.ErrThreadNotFound
		}
		if s.categoryRepo != nil {
			if _, err := validatePostingCategory(ctx, s.categoryRepo, thread.CategoryID); err != nil {
				return nil, fmt.Errorf("validate posting category: %w", err)
			}
		}
	}

	now := time.Now().UTC()
	post := &domain.Post{
		ID:         strconv.FormatInt(idgen.New(), 10),
		ThreadID:   threadID,
		AuthorID:   authorID,
		AuthorName: authorName,
		ParentID:   req.ParentID,
		Content:    req.Content,
		Status:     "published",
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := s.repo.Create(ctx, post); err != nil {
		return nil, fmt.Errorf("create post: %w", err)
	}
	if s.threadRepo != nil {
		if err := s.threadRepo.IncrementReplyCount(ctx, threadID, 1); err != nil {
			return nil, fmt.Errorf("update reply count: %w", err)
		}
		s.invalidateThreadListCache(ctx)
	}

	if s.bus != nil {
		_ = s.bus.Publish(ctx, eventbus.NewEvent(
			eventbus.EventPostCreated, "campusos.community", "post."+post.ID, post,
		))
	}

	return post, nil
}

func (s *PostService) GetPost(ctx context.Context, id string) (*domain.Post, error) {
	return s.repo.GetByID(ctx, id)
}

func (s *PostService) UpdatePost(ctx context.Context, id, authorID string, content string) (*domain.Post, error) {
	post, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get post: %w", err)
	}
	if post.AuthorID != authorID {
		return nil, fmt.Errorf("permission denied: you can only edit your own posts")
	}
	post.Content = content
	post.UpdatedAt = time.Now().UTC()
	if err := s.repo.Update(ctx, post); err != nil {
		return nil, fmt.Errorf("update post: %w", err)
	}
	return post, nil
}

func (s *PostService) DeletePost(ctx context.Context, id, authorID string) error {
	post, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return fmt.Errorf("get post: %w", err)
	}
	if post.AuthorID != authorID {
		return fmt.Errorf("permission denied: you can only delete your own posts")
	}
	if err := s.repo.Delete(ctx, id); err != nil {
		return err
	}
	if s.threadRepo != nil {
		if err := s.threadRepo.IncrementReplyCount(ctx, post.ThreadID, -1); err != nil {
			return fmt.Errorf("update reply count: %w", err)
		}
		s.invalidateThreadListCache(ctx)
	}
	return nil
}

// AdminDeletePost removes a reply after an external authorization layer has
// already checked the actor's governance scope.
func (s *PostService) AdminDeletePost(ctx context.Context, id string) error {
	post, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return fmt.Errorf("get post: %w", err)
	}
	if err := s.repo.Delete(ctx, id); err != nil {
		return err
	}
	if s.threadRepo != nil {
		if err := s.threadRepo.IncrementReplyCount(ctx, post.ThreadID, -1); err != nil {
			return fmt.Errorf("update reply count: %w", err)
		}
		s.invalidateThreadListCache(ctx)
	}
	return nil
}

func (s *PostService) ListByThread(ctx context.Context, threadID string, page, pageSize int) ([]*domain.Post, int64, error) {
	if err := s.ensureThreadVisible(ctx, threadID, ""); err != nil {
		return nil, 0, err
	}
	return s.listByThread(ctx, threadID, page, pageSize)
}

func (s *PostService) ListByThreadForViewer(ctx context.Context, threadID, viewerID string, page, pageSize int) ([]*domain.Post, int64, error) {
	if err := s.ensureThreadVisible(ctx, threadID, viewerID); err != nil {
		return nil, 0, err
	}
	return s.listByThread(ctx, threadID, page, pageSize)
}

func (s *PostService) listByThread(ctx context.Context, threadID string, page, pageSize int) ([]*domain.Post, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	return s.repo.ListByThread(ctx, threadID, page, pageSize)
}

func (s *PostService) ensureThreadVisible(ctx context.Context, threadID, viewerID string) error {
	if s.threadRepo == nil {
		return nil
	}
	thread, err := s.threadRepo.GetByID(ctx, threadID)
	if err != nil {
		return fmt.Errorf("get thread: %w", err)
	}
	if thread.Status == domain.ThreadStatusPublished {
		return nil
	}
	if viewerID != "" && thread.AuthorID == viewerID {
		return nil
	}
	return repository.ErrThreadNotFound
}

func (s *PostService) invalidateThreadListCache(ctx context.Context) {
	if s.cache == nil {
		return
	}
	keys := []string{
		"threads:list:1:20:published",
		"threads:list:1:20:published:",
		"threads:list:1:10:published",
		"threads:list:1:10:published:",
		"threads:list:1:5:published",
		"threads:list:1:5:published:",
	}
	for _, key := range keys {
		_ = s.cache.Delete(ctx, key)
	}
}
