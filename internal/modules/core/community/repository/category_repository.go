package repository

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"sync"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
)

var (
	ErrCategoryNotFound        = errors.New("category not found")
	ErrCategoryVersionConflict = errors.New("category version conflict")
)

type CategoryRepository interface {
	Create(context.Context, *domain.Category) error
	GetByID(context.Context, string) (*domain.Category, error)
	GetByIDForUpdate(context.Context, string) (*domain.Category, error)
	List(context.Context) ([]*domain.Category, error)
	ListChildren(context.Context, string) ([]*domain.Category, error)
	Update(context.Context, *domain.Category) error
	UpdateIfVersion(context.Context, *domain.Category, int64) error
	Delete(context.Context, string) error
}

type MemoryCategoryRepository struct {
	mu         sync.RWMutex
	categories map[string]*domain.Category
}

func NewMemoryCategoryRepository() *MemoryCategoryRepository {
	return &MemoryCategoryRepository{categories: make(map[string]*domain.Category)}
}

func (r *MemoryCategoryRepository) Create(_ context.Context, cat *domain.Category) error {
	if cat == nil || cat.ID == "" {
		return ErrCategoryNotFound
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.categories[cat.ID]; exists {
		return ErrCategoryVersionConflict
	}
	cat.NormalizeHierarchy()
	r.categories[cat.ID] = cloneCategory(cat)
	return nil
}

func (r *MemoryCategoryRepository) GetByID(_ context.Context, id string) (*domain.Category, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	cat, ok := r.categories[id]
	if !ok {
		return nil, ErrCategoryNotFound
	}
	return cloneCategory(cat), nil
}

func (r *MemoryCategoryRepository) GetByIDForUpdate(ctx context.Context, id string) (*domain.Category, error) {
	return r.GetByID(ctx, id)
}

func (r *MemoryCategoryRepository) List(_ context.Context) ([]*domain.Category, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	items := make([]*domain.Category, 0, len(r.categories))
	for _, cat := range r.categories {
		items = append(items, cloneCategory(cat))
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].SortOrder != items[j].SortOrder {
			return items[i].SortOrder < items[j].SortOrder
		}
		return items[i].CreatedAt.Before(items[j].CreatedAt)
	})
	return items, nil
}

func (r *MemoryCategoryRepository) ListChildren(_ context.Context, parentID string) ([]*domain.Category, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	items := make([]*domain.Category, 0)
	for _, cat := range r.categories {
		if cat.ParentID != nil && *cat.ParentID == parentID {
			items = append(items, cloneCategory(cat))
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].SortOrder != items[j].SortOrder {
			return items[i].SortOrder < items[j].SortOrder
		}
		return items[i].CreatedAt.Before(items[j].CreatedAt)
	})
	return items, nil
}

func (r *MemoryCategoryRepository) Update(_ context.Context, cat *domain.Category) error {
	if cat == nil {
		return ErrCategoryNotFound
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.categories[cat.ID]; !ok {
		return ErrCategoryNotFound
	}
	cat.NormalizeHierarchy()
	r.categories[cat.ID] = cloneCategory(cat)
	return nil
}

func (r *MemoryCategoryRepository) UpdateIfVersion(_ context.Context, cat *domain.Category, expected int64) error {
	if cat == nil {
		return ErrCategoryNotFound
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	current, ok := r.categories[cat.ID]
	if !ok {
		return ErrCategoryNotFound
	}
	current.NormalizeHierarchy()
	if current.Version != expected {
		return ErrCategoryVersionConflict
	}
	cat.NormalizeHierarchy()
	r.categories[cat.ID] = cloneCategory(cat)
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

func (r *MemoryCategoryRepository) Snapshot() any {
	r.mu.RLock()
	defer r.mu.RUnlock()
	payload, err := json.Marshal(r.categories)
	if err != nil {
		return []byte(nil)
	}
	return append([]byte(nil), payload...)
}

func (r *MemoryCategoryRepository) Restore(value any) {
	payload, ok := value.([]byte)
	if !ok || len(payload) == 0 {
		return
	}
	items := make(map[string]*domain.Category)
	if err := json.Unmarshal(payload, &items); err != nil {
		return
	}
	for _, cat := range items {
		cat.NormalizeHierarchy()
	}
	r.mu.Lock()
	r.categories = items
	r.mu.Unlock()
}

func cloneCategory(value *domain.Category) *domain.Category {
	if value == nil {
		return nil
	}
	copy := *value
	copy.DefaultTags = append([]string(nil), value.DefaultTags...)
	if value.ParentID != nil {
		parent := *value.ParentID
		copy.ParentID = &parent
	}
	copy.Children = make([]*domain.Category, 0, len(value.Children))
	for _, child := range value.Children {
		copy.Children = append(copy.Children, cloneCategory(child))
	}
	copy.NormalizeHierarchy()
	return &copy
}

var _ CategoryRepository = (*MemoryCategoryRepository)(nil)
var _ interface {
	Snapshot() any
	Restore(any)
} = (*MemoryCategoryRepository)(nil)
