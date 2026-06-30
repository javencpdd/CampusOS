package space

import (
	"context"
	"errors"
	"sync"
)

var ErrSpaceNotFound = errors.New("space not found")

type Repository interface {
	GetByUserID(ctx context.Context, userID string) (*Space, error)
	Upsert(ctx context.Context, space *Space) error
}

type MemoryRepository struct {
	mu     sync.RWMutex
	spaces map[string]*Space
}

func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{spaces: make(map[string]*Space)}
}

func (r *MemoryRepository) GetByUserID(_ context.Context, userID string) (*Space, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	space, ok := r.spaces[userID]
	if !ok {
		return nil, ErrSpaceNotFound
	}
	return cloneSpace(space), nil
}

func (r *MemoryRepository) Upsert(_ context.Context, space *Space) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.spaces[space.UserID] = cloneSpace(space)
	return nil
}

func cloneSpace(space *Space) *Space {
	if space == nil {
		return nil
	}
	clone := *space
	clone.SyncCategories = append([]string(nil), space.SyncCategories...)
	clone.SyncTags = append([]string(nil), space.SyncTags...)
	return &clone
}
