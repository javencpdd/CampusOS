package space

import (
	"context"
	"errors"
	"sync"
	"time"
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

type StyleSnapshotRepository interface {
	SaveStyleSnapshot(ctx context.Context, snapshot *StyleSnapshot) error
	GetLatestStyleSnapshot(ctx context.Context, userID string) (*StyleSnapshot, error)
}

type OperationalRepository interface {
	UpdateSyncStatus(ctx context.Context, userID string, syncedAt *time.Time, syncErr string) error
	CountContentsByUserID(ctx context.Context, userID string) (int64, error)
	SetDisabled(ctx context.Context, userID string, disabledAt *time.Time, disabledBy, reason string) error
	AdminSummary(ctx context.Context) (*SpaceAdminSummary, error)
}

type MemoryRepository struct {
	mu        sync.RWMutex
	spaces    map[string]*Space
	contents  map[string]*SpaceContent
	snapshots map[string][]*StyleSnapshot
}

func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{
		spaces:    make(map[string]*Space),
		contents:  make(map[string]*SpaceContent),
		snapshots: make(map[string][]*StyleSnapshot),
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
	if space, ok := r.spaces[content.UserID]; ok {
		now := content.SyncedAt
		space.LastSyncAt = &now
		space.LastSyncError = ""
	}
	return nil
}

func (r *MemoryRepository) DeleteContent(_ context.Context, threadID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.contents, threadID)
	return nil
}

func (r *MemoryRepository) SaveStyleSnapshot(_ context.Context, snapshot *StyleSnapshot) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.snapshots[snapshot.UserID] = append(r.snapshots[snapshot.UserID], cloneSnapshot(snapshot))
	return nil
}

func (r *MemoryRepository) GetLatestStyleSnapshot(_ context.Context, userID string) (*StyleSnapshot, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	items := r.snapshots[userID]
	if len(items) == 0 {
		return nil, ErrSpaceNotFound
	}
	return cloneSnapshot(items[len(items)-1]), nil
}

func (r *MemoryRepository) UpdateSyncStatus(_ context.Context, userID string, syncedAt *time.Time, syncErr string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	space, ok := r.spaces[userID]
	if !ok {
		return ErrSpaceNotFound
	}
	space.LastSyncAt = syncedAt
	space.LastSyncError = syncErr
	return nil
}

func (r *MemoryRepository) CountContentsByUserID(_ context.Context, userID string) (int64, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var total int64
	for _, content := range r.contents {
		if content.UserID == userID {
			total++
		}
	}
	return total, nil
}

func (r *MemoryRepository) SetDisabled(_ context.Context, userID string, disabledAt *time.Time, disabledBy, reason string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	space, ok := r.spaces[userID]
	if !ok {
		return ErrSpaceNotFound
	}
	space.DisabledAt = disabledAt
	space.DisabledBy = disabledBy
	space.DisabledReason = reason
	if disabledAt == nil {
		space.DisabledBy = ""
		space.DisabledReason = ""
	}
	space.UpdatedAt = time.Now().UTC()
	return nil
}

func (r *MemoryRepository) AdminSummary(_ context.Context) (*SpaceAdminSummary, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	summary := &SpaceAdminSummary{}
	for _, space := range r.spaces {
		summary.TotalSpaces++
		if space.Visibility == VisibilityPublic && space.DisabledAt == nil {
			summary.PublicSpaces++
		}
		if space.DisabledAt != nil {
			summary.DisabledSpaces++
		}
		if space.StyleName != "" {
			summary.StyledSpaces++
		}
		if space.SyncEnabled {
			summary.SyncEnabledSpaces++
		}
		if space.LastSyncError != "" {
			summary.SyncErrorSpaces++
		}
		if space.LastSyncAt != nil && (summary.LastSyncAt == nil || space.LastSyncAt.After(*summary.LastSyncAt)) {
			last := *space.LastSyncAt
			summary.LastSyncAt = &last
		}
	}
	return summary, nil
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
	clone.SyncCategories = append([]string{}, space.SyncCategories...)
	clone.SyncTags = append([]string{}, space.SyncTags...)
	if space.DisabledAt != nil {
		disabledAt := *space.DisabledAt
		clone.DisabledAt = &disabledAt
	}
	if space.LastSyncAt != nil {
		lastSyncAt := *space.LastSyncAt
		clone.LastSyncAt = &lastSyncAt
	}
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

func cloneSnapshot(snapshot *StyleSnapshot) *StyleSnapshot {
	if snapshot == nil {
		return nil
	}
	clone := *snapshot
	if snapshot.StyleManifest != nil {
		manifest := *snapshot.StyleManifest
		manifest.CompatibleCampusOS = append([]string(nil), snapshot.StyleManifest.CompatibleCampusOS...)
		manifest.Components = append([]StyleComponent(nil), snapshot.StyleManifest.Components...)
		for i := range manifest.Components {
			manifest.Components[i].Props = copyInterfaceMap(snapshot.StyleManifest.Components[i].Props)
		}
		manifest.Tokens = copyStringMap(snapshot.StyleManifest.Tokens)
		manifest.Assets = append([]StyleAsset(nil), snapshot.StyleManifest.Assets...)
		clone.StyleManifest = &manifest
	}
	return &clone
}

func cloneContent(content *SpaceContent) *SpaceContent {
	if content == nil {
		return nil
	}
	clone := *content
	clone.Tags = append([]string{}, content.Tags...)
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
