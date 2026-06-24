package service

import (
	"context"
	"fmt"
	"time"

	"github.com/campusos/CampusOS/internal/community/domain"
	"github.com/campusos/CampusOS/internal/community/repository"
	"github.com/campusos/CampusOS/pkg/eventbus"
	"github.com/google/uuid"
)

type PostService struct {
	repo repository.PostRepository
	bus  eventbus.EventBus
}

func NewPostService(repo repository.PostRepository, bus eventbus.EventBus) *PostService {
	return &PostService{repo: repo, bus: bus}
}

func (s *PostService) CreatePost(ctx context.Context, threadID, authorID, authorName string, req domain.CreatePostRequest) (*domain.Post, error) {
	now := time.Now().UTC()
	post := &domain.Post{
		ID:         uuid.New().String(),
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

func (s *PostService) ListByThread(ctx context.Context, threadID string, page, pageSize int) ([]*domain.Post, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	return s.repo.ListByThread(ctx, threadID, page, pageSize)
}
