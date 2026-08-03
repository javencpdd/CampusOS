package repository

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"sync"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
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
	mu              sync.RWMutex
	posts           map[string]*domain.Post
	nextFloorNumber map[string]int
}

func NewMemoryPostRepository() *MemoryPostRepository {
	return &MemoryPostRepository{
		posts:           make(map[string]*domain.Post),
		nextFloorNumber: make(map[string]int),
	}
}

func (r *MemoryPostRepository) Create(_ context.Context, post *domain.Post) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if post.FloorNumber <= 0 {
		r.nextFloorNumber[post.ThreadID]++
		post.FloorNumber = r.nextFloorNumber[post.ThreadID]
	} else if post.FloorNumber > r.nextFloorNumber[post.ThreadID] {
		r.nextFloorNumber[post.ThreadID] = post.FloorNumber
	}
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
	sort.Slice(filtered, func(i, j int) bool {
		if filtered[i].FloorNumber == filtered[j].FloorNumber {
			return filtered[i].CreatedAt.Before(filtered[j].CreatedAt)
		}
		return filtered[i].FloorNumber < filtered[j].FloorNumber
	})
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

func (r *MemoryPostRepository) Snapshot() any {
	r.mu.RLock()
	defer r.mu.RUnlock()
	payload, err := json.Marshal(struct {
		Posts map[string]*domain.Post `json:"posts"`
		Next  map[string]int          `json:"next"`
	}{Posts: r.posts, Next: r.nextFloorNumber})
	if err != nil {
		return []byte(nil)
	}
	return append([]byte(nil), payload...)
}

func (r *MemoryPostRepository) Restore(value any) {
	payload, ok := value.([]byte)
	if !ok || len(payload) == 0 {
		return
	}
	state := struct {
		Posts map[string]*domain.Post `json:"posts"`
		Next  map[string]int          `json:"next"`
	}{}
	if err := json.Unmarshal(payload, &state); err != nil {
		return
	}
	r.mu.Lock()
	r.posts = state.Posts
	r.nextFloorNumber = state.Next
	r.mu.Unlock()
}
