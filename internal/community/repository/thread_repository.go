package repository

import (
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/campusos/CampusOS/internal/community/domain"
)

var ErrThreadNotFound = errors.New("thread not found")

// ThreadRepository 帖子仓储接口
type ThreadRepository interface {
	Create(ctx context.Context, thread *domain.Thread) error
	GetByID(ctx context.Context, id string) (*domain.Thread, error)
	Update(ctx context.Context, thread *domain.Thread) error
	Delete(ctx context.Context, id string) error
	List(ctx context.Context, filter domain.ThreadListFilter) ([]*domain.Thread, int64, error)
}

// MemoryThreadRepository 内存帖子仓储（Demo 用）
type MemoryThreadRepository struct {
	mu      sync.RWMutex
	threads map[string]*domain.Thread
}

// NewMemoryThreadRepository 创建内存帖子仓储
func NewMemoryThreadRepository() *MemoryThreadRepository {
	return &MemoryThreadRepository{
		threads: make(map[string]*domain.Thread),
	}
}

func (r *MemoryThreadRepository) Create(_ context.Context, thread *domain.Thread) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.threads[thread.ID] = thread
	return nil
}

func (r *MemoryThreadRepository) GetByID(_ context.Context, id string) (*domain.Thread, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	thread, ok := r.threads[id]
	if !ok {
		return nil, ErrThreadNotFound
	}
	return thread, nil
}

func (r *MemoryThreadRepository) Update(_ context.Context, thread *domain.Thread) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.threads[thread.ID]; !ok {
		return ErrThreadNotFound
	}
	r.threads[thread.ID] = thread
	return nil
}

func (r *MemoryThreadRepository) Delete(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.threads[id]; !ok {
		return ErrThreadNotFound
	}
	delete(r.threads, id)
	return nil
}

func (r *MemoryThreadRepository) List(_ context.Context, filter domain.ThreadListFilter) ([]*domain.Thread, int64, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// 过滤
	var filtered []*domain.Thread
	for _, t := range r.threads {
		if filter.CategoryID != "" && t.CategoryID != filter.CategoryID {
			continue
		}
		if filter.AuthorID != "" && t.AuthorID != filter.AuthorID {
			continue
		}
		if filter.Status != "" && string(t.Status) != filter.Status {
			continue
		}
		if filter.Keyword != "" && !strings.Contains(strings.ToLower(t.Title), strings.ToLower(filter.Keyword)) && !strings.Contains(strings.ToLower(t.Content), strings.ToLower(filter.Keyword)) {
			continue
		}
		filtered = append(filtered, t)
	}

	total := int64(len(filtered))

	// 分页
	page := filter.Page
	pageSize := filter.PageSize
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	start := (page - 1) * pageSize
	if start >= len(filtered) {
		return []*domain.Thread{}, total, nil
	}
	end := start + pageSize
	if end > len(filtered) {
		end = len(filtered)
	}

	return filtered[start:end], total, nil
}
