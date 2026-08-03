package repository

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
)

var ErrThreadNotFound = errors.New("thread not found")
var ErrThreadRevisionConflict = errors.New("thread revision conflict")

// ThreadRepository 帖子仓储接口
type ThreadRepository interface {
	Create(ctx context.Context, thread *domain.Thread) error
	GetByID(ctx context.Context, id string) (*domain.Thread, error)
	IncrementViewCount(ctx context.Context, id string) (*domain.Thread, error)
	IncrementReplyCount(ctx context.Context, id string, delta int64) error
	Update(ctx context.Context, thread *domain.Thread) error
	Delete(ctx context.Context, id string) error
	Purge(ctx context.Context, id string) error
	List(ctx context.Context, filter domain.ThreadListFilter) ([]*domain.Thread, int64, error)
}

// GovernedThreadRepository is an optional optimistic-concurrency adapter for
// high-risk moderation transitions. The base repository stays compatible with
// legacy callers while PostgreSQL and Memory profiles both provide it.
type GovernedThreadRepository interface {
	UpdateIfRevision(context.Context, *domain.Thread, int) error
	PurgeIfRevision(context.Context, string, int) error
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

// Snapshot and Restore give the local profile the same all-or-nothing command
// semantics that PostgreSQL provides through TxKernel. They are only used by
// the in-memory transaction adapter during tests and local development.
func (r *MemoryThreadRepository) Snapshot() any {
	r.mu.RLock()
	defer r.mu.RUnlock()
	payload, err := json.Marshal(r.threads)
	if err != nil {
		return []byte(nil)
	}
	return append([]byte(nil), payload...)
}

func (r *MemoryThreadRepository) Restore(value any) {
	payload, ok := value.([]byte)
	if !ok || len(payload) == 0 {
		return
	}
	items := make(map[string]*domain.Thread)
	if err := json.Unmarshal(payload, &items); err != nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.threads = items
}

func (r *MemoryThreadRepository) Create(_ context.Context, thread *domain.Thread) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	thread.NormalizeContentState()
	r.threads[thread.ID] = cloneThread(thread)
	return nil
}

func (r *MemoryThreadRepository) GetByID(_ context.Context, id string) (*domain.Thread, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	thread, ok := r.threads[id]
	if !ok {
		return nil, ErrThreadNotFound
	}
	return cloneThread(thread), nil
}

func (r *MemoryThreadRepository) IncrementViewCount(_ context.Context, id string) (*domain.Thread, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	thread, ok := r.threads[id]
	if !ok {
		return nil, ErrThreadNotFound
	}
	thread.ViewCount++
	return cloneThread(thread), nil
}

func (r *MemoryThreadRepository) IncrementReplyCount(_ context.Context, id string, delta int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	thread, ok := r.threads[id]
	if !ok {
		return ErrThreadNotFound
	}
	thread.ReplyCount += delta
	if thread.ReplyCount < 0 {
		thread.ReplyCount = 0
	}
	thread.UpdatedAt = time.Now().UTC()
	return nil
}

func (r *MemoryThreadRepository) Update(_ context.Context, thread *domain.Thread) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.threads[thread.ID]; !ok {
		return ErrThreadNotFound
	}
	thread.NormalizeContentState()
	r.threads[thread.ID] = cloneThread(thread)
	return nil
}

func (r *MemoryThreadRepository) UpdateIfRevision(_ context.Context, thread *domain.Thread, expected int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	current, ok := r.threads[thread.ID]
	if !ok {
		return ErrThreadNotFound
	}
	if current.CurrentRevision != expected {
		return ErrThreadRevisionConflict
	}
	thread.NormalizeContentState()
	r.threads[thread.ID] = cloneThread(thread)
	return nil
}

func (r *MemoryThreadRepository) Delete(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.threads[id]; !ok {
		return ErrThreadNotFound
	}
	thread := r.threads[id]
	thread.NormalizeContentState()
	thread.DeletionStatus = domain.DeletionStatusTrashed
	thread.SyncLegacyStatus()
	thread.UpdatedAt = time.Now().UTC()
	r.threads[id] = cloneThread(thread)
	return nil
}

func (r *MemoryThreadRepository) Purge(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.threads[id]; !ok {
		return ErrThreadNotFound
	}
	delete(r.threads, id)
	return nil
}

func (r *MemoryThreadRepository) PurgeIfRevision(_ context.Context, id string, expected int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	thread, ok := r.threads[id]
	if !ok {
		return ErrThreadNotFound
	}
	if thread.CurrentRevision != expected {
		return ErrThreadRevisionConflict
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
		t = cloneThread(t)
		t.NormalizeContentState()
		if filter.CategoryID != "" && t.CategoryID != filter.CategoryID {
			continue
		}
		if filter.CategoryIDs != nil && !containsString(filter.CategoryIDs, t.CategoryID) {
			continue
		}
		if filter.AuthorID != "" && t.AuthorID != filter.AuthorID {
			continue
		}
		if filter.Status != "" && string(t.Status) != filter.Status {
			continue
		}
		if filter.ContentFormat != "" && t.ContentFormat != filter.ContentFormat {
			continue
		}
		if filter.ThreadType != "" && t.ThreadType != domain.NormalizeThreadType(filter.ThreadType) {
			continue
		}
		if filter.Tag != "" && !containsTag(t.Tags, filter.Tag) {
			continue
		}
		if len(filter.AnyTags) > 0 && !containsAnyTag(t.Tags, filter.AnyTags) {
			continue
		}
		if filter.PublicationStatus != "" && string(t.PublicationStatus) != filter.PublicationStatus {
			continue
		}
		if filter.ModerationStatus != "" && string(t.ModerationStatus) != filter.ModerationStatus {
			continue
		}
		if filter.DeletionStatus != "" && string(t.DeletionStatus) != filter.DeletionStatus {
			continue
		}
		if !filter.IncludeTrashed && t.DeletionStatus != domain.DeletionStatusActive {
			continue
		}
		if filter.Keyword != "" && !strings.Contains(strings.ToLower(t.Title), strings.ToLower(filter.Keyword)) && !strings.Contains(strings.ToLower(t.Content), strings.ToLower(filter.Keyword)) {
			continue
		}
		filtered = append(filtered, t)
	}
	sort.Slice(filtered, func(i, j int) bool {
		if filtered[i].IsPinned != filtered[j].IsPinned {
			return filtered[i].IsPinned
		}
		return filtered[i].CreatedAt.After(filtered[j].CreatedAt)
	})

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

func containsTag(tags []string, wanted string) bool {
	for _, tag := range tags {
		if strings.EqualFold(strings.TrimSpace(tag), strings.TrimSpace(wanted)) {
			return true
		}
	}
	return false
}

func containsAnyTag(tags, wanted []string) bool {
	for _, item := range wanted {
		if containsTag(tags, item) {
			return true
		}
	}
	return false
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) == wanted {
			return true
		}
	}
	return false
}

func cloneThread(thread *domain.Thread) *domain.Thread {
	if thread == nil {
		return nil
	}
	clone := *thread
	clone.Tags = append([]string(nil), thread.Tags...)
	if thread.ModerationAt != nil {
		value := *thread.ModerationAt
		clone.ModerationAt = &value
	}
	return &clone
}
