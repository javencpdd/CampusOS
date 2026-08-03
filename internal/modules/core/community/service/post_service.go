package service

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	"github.com/campusos/CampusOS/internal/modules/core/community/repository"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/campusos/CampusOS/pkg/cache"
	"github.com/campusos/CampusOS/pkg/eventbus"
	"github.com/campusos/CampusOS/pkg/idgen"
)

type PostService struct {
	repo          repository.PostRepository
	threadRepo    repository.ThreadRepository
	categoryRepo  repository.CategoryRepository
	bus           eventbus.EventBus
	cache         cache.Cache
	reliable      *reliability.Service
	notifications PostNotificationWriter
}

type PostNotificationWriter interface {
	NotifyThreadReplied(context.Context, string, string, string, string, string) error
	NotifyPostReplied(context.Context, string, string, string, string, string, string) error
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

func (s *PostService) SetNotificationWriter(writer PostNotificationWriter) {
	s.notifications = writer
}

func (s *PostService) SetReliability(reliable *reliability.Service) {
	s.reliable = reliable
	if reliable == nil {
		return
	}
	if snapshotter, ok := s.repo.(transaction.Snapshotter); ok {
		reliable.RegisterMemorySnapshotters(snapshotter)
	}
	if snapshotter, ok := s.threadRepo.(transaction.Snapshotter); ok {
		reliable.RegisterMemorySnapshotters(snapshotter)
	}
}

func (s *PostService) CreatePost(ctx context.Context, threadID, authorID, authorName string, req domain.CreatePostRequest) (*domain.Post, error) {
	now := time.Now().UTC()
	post := &domain.Post{
		ID: strconv.FormatInt(idgen.New(), 10), ThreadID: strings.TrimSpace(threadID),
		AuthorID: authorID, AuthorName: authorName, ParentID: normalizePostParentID(req.ParentID),
		Content: req.Content, Status: "published", CreatedAt: now, UpdatedAt: now,
	}
	if s.reliable != nil && !transaction.Active(ctx) {
		err := s.reliable.Execute(ctx, reliability.Command{
			Code: "community.post.create", ActorID: strings.TrimSpace(authorID), ActorType: "user",
			ResourceType: "post", ResourceID: post.ID, OperationCode: "community.post.create",
			EventFactory: func() (reliability.Event, error) {
				return reliability.NewEvent(eventbus.EventPostCreated, "post", post.ID, post)
			},
		}, func(commandCtx context.Context) error {
			return s.createPost(commandCtx, post)
		})
		if err != nil {
			return nil, err
		}
		s.afterPostCreated(ctx, post)
		return post, nil
	}
	if err := s.createPost(ctx, post); err != nil {
		return nil, err
	}
	if !transaction.Active(ctx) {
		s.afterPostCreated(ctx, post)
	}
	return post, nil
}

func (s *PostService) createPost(ctx context.Context, post *domain.Post) error {
	var thread *domain.Thread
	var parent *domain.Post
	if s.threadRepo != nil {
		var err error
		thread, err = s.threadRepo.GetByID(ctx, post.ThreadID)
		if err != nil {
			return fmt.Errorf("get thread: %w", err)
		}
		if thread.IsLocked {
			return fmt.Errorf("thread is locked")
		}
		if thread.Status != domain.ThreadStatusPublished && thread.AuthorID != post.AuthorID {
			return repository.ErrThreadNotFound
		}
		if s.categoryRepo != nil {
			if _, err := validatePostingCategory(ctx, s.categoryRepo, thread.CategoryID); err != nil {
				return fmt.Errorf("validate posting category: %w", err)
			}
		}
	}
	if post.ParentID != nil {
		var err error
		parent, err = s.repo.GetByID(ctx, *post.ParentID)
		if err != nil {
			return fmt.Errorf("get parent post: %w", err)
		}
		if parent.ThreadID != post.ThreadID || parent.Status != "published" {
			return repository.ErrPostNotFound
		}
	}
	if err := s.repo.Create(ctx, post); err != nil {
		return fmt.Errorf("create post: %w", err)
	}
	if s.threadRepo != nil {
		if err := s.threadRepo.IncrementReplyCount(ctx, post.ThreadID, 1); err != nil {
			return fmt.Errorf("update reply count: %w", err)
		}
	}
	if s.notifications != nil && thread != nil {
		parentRecipient := ""
		if parent != nil && parent.AuthorID != post.AuthorID {
			parentRecipient = parent.AuthorID
			if err := s.notifications.NotifyPostReplied(ctx, parent.AuthorID, thread.ID, thread.Title, parent.ID, post.ID, post.AuthorName); err != nil {
				return err
			}
		}
		if thread.AuthorID != post.AuthorID && thread.AuthorID != parentRecipient {
			if err := s.notifications.NotifyThreadReplied(ctx, thread.AuthorID, thread.ID, thread.Title, post.ID, post.AuthorName); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *PostService) afterPostCreated(ctx context.Context, post *domain.Post) {
	s.invalidateThreadListCache(ctx)
	if s.bus != nil {
		_ = s.bus.Publish(ctx, eventbus.NewEvent(eventbus.EventPostCreated, "campusos.community", "post."+post.ID, post))
	}
}

func normalizePostParentID(parentID *string) *string {
	if parentID == nil {
		return nil
	}
	value := strings.TrimSpace(*parentID)
	if value == "" {
		return nil
	}
	return &value
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
