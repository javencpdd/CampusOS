package repository

import (
	"context"
	"errors"
	"sync"

	"github.com/campusos/CampusOS/internal/community/domain"
)

var ErrPostNotFound = errors.New("post not found")

type PostRepository interface {
	Create(ctx context.Context, post *domain.Post) error
	GetByID(ctx context.Context, id string) (*domain.Post, error)
	ListByThread(ctx context.Context, threadID string, page, pageSize int) ([]*domain.Post, int64, error)
	Update(ctx context.Context, post *domain.Post) error
	Delete(ctx context.Context, id string) error
}

type MemoryPostRepository struct {
	mu    sync.RWMutex
	posts map[string]*domain.Post
}

func NewMemoryPostRepository() *MemoryPostRepository {
	return &MemoryPostRepository{posts: make(map[string]*domain.Post)}
}

func (r *MemoryPostRepository) Create(_ context.Context, post *domain.Post) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.posts[post.ID] = post
	return nil
}

func (r *MemoryPostRepository) GetByID(_ context.Context, id string) (*domain.Post, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.posts[id]
	if !ok {
		return nil, ErrPostNotFound
	}
	return p, nil
}

func (r *MemoryPostRepository) Update(_ context.Context, post *domain.Post) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.posts[post.ID]; !ok {
		return ErrPostNotFound
	}
	r.posts[post.ID] = post
	return nil
}

func (r *MemoryPostRepository) Delete(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.posts[id]; !ok {
		return ErrPostNotFound
	}
	delete(r.posts, id)
	return nil
}

func (r *MemoryPostRepository) ListByThread(_ context.Context, threadID string, page, pageSize int) ([]*domain.Post, int64, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var filtered []*domain.Post
	for _, p := range r.posts {
		if p.ThreadID == threadID {
			filtered = append(filtered, p)
		}
	}
	total := int64(len(filtered))
	start := (page - 1) * pageSize
	if start >= len(filtered) {
		return []*domain.Post{}, total, nil
	}
	end := start + pageSize
	if end > len(filtered) {
		end = len(filtered)
	}
	return filtered[start:end], total, nil
}
