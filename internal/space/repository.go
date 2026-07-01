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

type ContentRepository interface {
	UpsertContent(ctx context.Context, content *SpaceContent) error
	DeleteContent(ctx context.Context, threadID string) error
	ListContentsByUserID(ctx context.Context, userID string, page, pageSize int) ([]*SpaceContent, int64, error)
}

type MemoryRepository struct {
	mu       sync.RWMutex
	spaces   map[string]*Space
	contents map[string]*SpaceContent
}

func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{
		spaces:   make(map[string]*Space),
		contents: make(map[string]*SpaceContent),
	}
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

func (r *MemoryRepository) UpsertContent(_ context.Context, content *SpaceContent) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.contents[content.ThreadID] = cloneContent(content)
	return nil
}

func (r *MemoryRepository) DeleteContent(_ context.Context, threadID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.contents, threadID)
	return nil
}

func (r *MemoryRepository) ListContentsByUserID(_ context.Context, userID string, page, pageSize int) ([]*SpaceContent, int64, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}

	filtered := make([]*SpaceContent, 0)
	for _, content := range r.contents {
		if content.UserID == userID {
			filtered = append(filtered, cloneContent(content))
		}
	}
	sortContents(filtered)

	total := int64(len(filtered))
	start := (page - 1) * pageSize
	if start >= len(filtered) {
		return []*SpaceContent{}, total, nil
	}
	end := start + pageSize
	if end > len(filtered) {
		end = len(filtered)
	}
	return filtered[start:end], total, nil
}

func cloneSpace(space *Space) *Space {
	if space == nil {
		return nil
	}
	clone := *space
	clone.SyncCategories = append([]string(nil), space.SyncCategories...)
	clone.SyncTags = append([]string(nil), space.SyncTags...)
	if space.StyleManifest != nil {
		manifest := *space.StyleManifest
		manifest.CompatibleCampusOS = append([]string(nil), space.StyleManifest.CompatibleCampusOS...)
		manifest.Components = append([]StyleComponent(nil), space.StyleManifest.Components...)
		for i := range manifest.Components {
			manifest.Components[i].Props = copyInterfaceMap(space.StyleManifest.Components[i].Props)
		}
		manifest.Tokens = copyStringMap(space.StyleManifest.Tokens)
		manifest.Assets = append([]StyleAsset(nil), space.StyleManifest.Assets...)
		clone.StyleManifest = &manifest
	}
	return &clone
}

func cloneContent(content *SpaceContent) *SpaceContent {
	if content == nil {
		return nil
	}
	clone := *content
	clone.Tags = append([]string(nil), content.Tags...)
	return &clone
}

func copyInterfaceMap(values map[string]interface{}) map[string]interface{} {
	if len(values) == 0 {
		return nil
	}
	clone := make(map[string]interface{}, len(values))
	for key, value := range values {
		clone[key] = value
	}
	return clone
}
