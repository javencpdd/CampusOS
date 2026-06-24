package service

import (
	"context"
	"fmt"
	"time"

	"github.com/campusos/CampusOS/internal/community/domain"
	"github.com/campusos/CampusOS/internal/community/repository"
	"github.com/google/uuid"
)

type PostService struct {
	repo repository.PostRepository
}

func NewPostService(repo repository.PostRepository) *PostService {
	return &PostService{repo: repo}
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
