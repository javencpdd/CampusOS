package service

import (
	"context"
	"fmt"
	"time"

	"github.com/campusos/CampusOS/internal/community/domain"
	"github.com/campusos/CampusOS/internal/community/repository"
	"github.com/google/uuid"
)

// ThreadService 帖子服务
type ThreadService struct {
	repo repository.ThreadRepository
}

// NewThreadService 创建帖子服务
func NewThreadService(repo repository.ThreadRepository) *ThreadService {
	return &ThreadService{repo: repo}
}

// CreateThread 创建帖子
func (s *ThreadService) CreateThread(ctx context.Context, authorID, authorName string, req domain.CreateThreadRequest) (*domain.Thread, error) {
	now := time.Now().UTC()
	thread := &domain.Thread{
		ID:         uuid.New().String(),
		Title:      req.Title,
		Content:    req.Content,
		AuthorID:   authorID,
		AuthorName: authorName,
		CategoryID: req.CategoryID,
		Status:     domain.ThreadStatusPublished,
		Tags:       req.Tags,
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	if err := s.repo.Create(ctx, thread); err != nil {
		return nil, fmt.Errorf("create thread: %w", err)
	}

	return thread, nil
}

// GetThread 获取帖子详情
func (s *ThreadService) GetThread(ctx context.Context, id string) (*domain.Thread, error) {
	thread, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get thread: %w", err)
	}
	// 简单的浏览计数
	thread.ViewCount++
	_ = s.repo.Update(ctx, thread)
	return thread, nil
}

// ListThreads 获取帖子列表
func (s *ThreadService) ListThreads(ctx context.Context, filter domain.ThreadListFilter) ([]*domain.Thread, int64, error) {
	if filter.Page < 1 {
		filter.Page = 1
	}
	if filter.PageSize < 1 || filter.PageSize > 100 {
		filter.PageSize = 20
	}
	// 默认只显示已发布的帖子
	if filter.Status == "" {
		filter.Status = string(domain.ThreadStatusPublished)
	}
	return s.repo.List(ctx, filter)
}

// UpdateThread 更新帖子
func (s *ThreadService) UpdateThread(ctx context.Context, id, authorID string, req domain.UpdateThreadRequest) (*domain.Thread, error) {
	thread, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get thread: %w", err)
	}

	if thread.AuthorID != authorID {
		return nil, fmt.Errorf("permission denied: you can only edit your own threads")
	}

	if req.Title != nil {
		thread.Title = *req.Title
	}
	if req.Content != nil {
		thread.Content = *req.Content
	}
	if req.Tags != nil {
		thread.Tags = req.Tags
	}
	thread.UpdatedAt = time.Now().UTC()

	if err := s.repo.Update(ctx, thread); err != nil {
		return nil, fmt.Errorf("update thread: %w", err)
	}

	return thread, nil
}

// DeleteThread 删除帖子
func (s *ThreadService) DeleteThread(ctx context.Context, id, authorID string) error {
	thread, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return fmt.Errorf("get thread: %w", err)
	}

	if thread.AuthorID != authorID {
		return fmt.Errorf("permission denied: you can only delete your own threads")
	}

	return s.repo.Delete(ctx, id)
}
