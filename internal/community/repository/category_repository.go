package repository

import (
	"context"
	"errors"
	"sync"

	"github.com/campusos/CampusOS/internal/community/domain"
)

var ErrCategoryNotFound = errors.New("category not found")

type CategoryRepository interface {
	Create(ctx context.Context, cat *domain.Category) error
	GetByID(ctx context.Context, id string) (*domain.Category, error)
	List(ctx context.Context) ([]*domain.Category, error)
	Update(ctx context.Context, cat *domain.Category) error
	Delete(ctx context.Context, id string) error
}

type MemoryCategoryRepository struct {
	mu         sync.RWMutex
	categories map[string]*domain.Category
}

func NewMemoryCategoryRepository() *MemoryCategoryRepository {
	return &MemoryCategoryRepository{categories: make(map[string]*domain.Category)}
}

func (r *MemoryCategoryRepository) Create(_ context.Context, cat *domain.Category) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.categories[cat.ID] = cat
	return nil
}

func (r *MemoryCategoryRepository) GetByID(_ context.Context, id string) (*domain.Category, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	cat, ok := r.categories[id]
	if !ok {
		return nil, ErrCategoryNotFound
	}
	return cat, nil
}

func (r *MemoryCategoryRepository) List(_ context.Context) ([]*domain.Category, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var list []*domain.Category
	for _, c := range r.categories {
		list = append(list, c)
	}
	return list, nil
}

func (r *MemoryCategoryRepository) Update(_ context.Context, cat *domain.Category) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.categories[cat.ID]; !ok {
		return ErrCategoryNotFound
	}
	r.categories[cat.ID] = cat
	return nil
}

func (r *MemoryCategoryRepository) Delete(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.categories[id]; !ok {
		return ErrCategoryNotFound
	}
	delete(r.categories, id)
	return nil
}
