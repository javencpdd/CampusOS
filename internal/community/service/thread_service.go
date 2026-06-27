package service

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"time"

	"github.com/campusos/CampusOS/internal/community/domain"
	"github.com/campusos/CampusOS/internal/community/repository"
	"github.com/campusos/CampusOS/pkg/cache"
	"github.com/campusos/CampusOS/pkg/eventbus"
	"github.com/campusos/CampusOS/pkg/idgen"
)

// ThreadService 帖子服务
type ThreadService struct {
	repo  repository.ThreadRepository
	bus   eventbus.EventBus
	cache cache.Cache
}

// NewThreadService 创建帖子服务
func NewThreadService(repo repository.ThreadRepository, bus eventbus.EventBus) *ThreadService {
	return &ThreadService{repo: repo, bus: bus}
}

// SetCache 设置缓存实例
func (s *ThreadService) SetCache(c cache.Cache) {
	s.cache = c
}

// CreateThread 创建帖子
func (s *ThreadService) CreateThread(ctx context.Context, authorID, authorName string, req domain.CreateThreadRequest) (*domain.Thread, error) {
	now := time.Now().UTC()
	thread := &domain.Thread{
		ID:         strconv.FormatInt(idgen.New(), 10),
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

	// 清除列表缓存
	s.invalidateListCache(ctx)

	// 发布 thread.created 事件
	if s.bus != nil {
		_ = s.bus.Publish(ctx, eventbus.NewEvent(
			eventbus.EventThreadCreated, "campusos.community", "thread."+thread.ID, thread,
		))
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

// ListThreads 获取帖子列表（支持缓存）
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

	// 尝试从缓存获取（仅缓存第一页无筛选条件的查询）
	cacheKey := fmt.Sprintf("threads:list:%d:%d:%s", filter.Page, filter.PageSize, filter.Status)
	if s.cache != nil && filter.Keyword == "" && filter.CategoryID == "" && filter.AuthorID == "" {
		type cachedResult struct {
			Threads []*domain.Thread `json:"threads"`
			Total   int64            `json:"total"`
		}
		var cached cachedResult
		if err := s.cache.Get(ctx, cacheKey, &cached); err == nil {
			log.Printf("📦 缓存命中: %s", cacheKey)
			return cached.Threads, cached.Total, nil
		}
	}

	threads, total, err := s.repo.List(ctx, filter)
	if err != nil {
		return nil, 0, err
	}

	// 写入缓存（5 分钟 TTL）
	if s.cache != nil && filter.Keyword == "" && filter.CategoryID == "" && filter.AuthorID == "" {
		type cachedResult struct {
			Threads []*domain.Thread `json:"threads"`
			Total   int64            `json:"total"`
		}
		_ = s.cache.Set(ctx, cacheKey, cachedResult{Threads: threads, Total: total}, 5*time.Minute)
	}

	return threads, total, nil
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

	// 清除列表缓存
	s.invalidateListCache(ctx)

	// 发布 thread.updated 事件
	if s.bus != nil {
		_ = s.bus.Publish(ctx, eventbus.NewEvent(
			eventbus.EventThreadUpdated, "campusos.community", "thread."+thread.ID, thread,
		))
	}

	return thread, nil
}

// PinThread 置顶帖子
func (s *ThreadService) PinThread(ctx context.Context, id string) (*domain.Thread, error) {
	thread, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get thread: %w", err)
	}
	thread.IsPinned = true
	thread.UpdatedAt = time.Now().UTC()
	if err := s.repo.Update(ctx, thread); err != nil {
		return nil, fmt.Errorf("pin thread: %w", err)
	}
	return thread, nil
}

// UnpinThread 取消置顶
func (s *ThreadService) UnpinThread(ctx context.Context, id string) (*domain.Thread, error) {
	thread, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get thread: %w", err)
	}
	thread.IsPinned = false
	thread.UpdatedAt = time.Now().UTC()
	if err := s.repo.Update(ctx, thread); err != nil {
		return nil, fmt.Errorf("unpin thread: %w", err)
	}
	return thread, nil
}

// LockThread 锁定帖子
func (s *ThreadService) LockThread(ctx context.Context, id string) (*domain.Thread, error) {
	thread, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get thread: %w", err)
	}
	thread.IsLocked = true
	thread.UpdatedAt = time.Now().UTC()
	if err := s.repo.Update(ctx, thread); err != nil {
		return nil, fmt.Errorf("lock thread: %w", err)
	}
	return thread, nil
}

// UnlockThread 解锁帖子
func (s *ThreadService) UnlockThread(ctx context.Context, id string) (*domain.Thread, error) {
	thread, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get thread: %w", err)
	}
	thread.IsLocked = false
	thread.UpdatedAt = time.Now().UTC()
	if err := s.repo.Update(ctx, thread); err != nil {
		return nil, fmt.Errorf("unlock thread: %w", err)
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

	if err := s.repo.Delete(ctx, id); err != nil {
		return err
	}

	// 清除列表缓存
	s.invalidateListCache(ctx)

	// 发布 thread.deleted 事件
	if s.bus != nil {
		_ = s.bus.Publish(ctx, eventbus.NewEvent(
			eventbus.EventThreadDeleted, "campusos.community", "thread."+id, thread,
		))
	}

	return nil
}

// invalidateListCache 清除帖子列表缓存
func (s *ThreadService) invalidateListCache(ctx context.Context) {
	if s.cache == nil {
		return
	}
	// 清除常见的列表缓存 key
	keys := []string{
		"threads:list:1:20:published",
		"threads:list:1:10:published",
		"threads:list:1:5:published",
	}
	for _, key := range keys {
		_ = s.cache.Delete(ctx, key)
	}
}
