package repository

import (
	"context"
	"errors"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
)

var ErrThreadNotFound = errors.New("thread not found")
var ErrThreadRevisionConflict = errors.New("thread revision conflict")
var ErrThreadFactsVersionExhausted = errors.New("thread facts version exhausted")
var ErrThreadPaginationOverflow = errors.New("thread pagination offset overflow")

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

// PublicThreadSnapshotRepository atomically rechecks current public facts and
// returns their matching content snapshot. An optional view increment belongs
// to the same operation, so it can never return a newly private body.
type PublicThreadSnapshotRepository interface {
	RecheckPublicThread(context.Context, string, int64, bool) (*domain.Thread, error)
}

// OwnerThreadSnapshotRepository returns the matching owner's current content
// without changing view counters. Ownership, version and explicit states are
// checked together; legacy state defaults cannot authorize an owner read.
type OwnerThreadSnapshotRepository interface {
	RecheckOwnerThread(context.Context, string, string, int64) (*domain.Thread, error)
}

// MemoryThreadRepository 内存帖子仓储（Demo 用）
type MemoryThreadRepository struct {
	mu             sync.RWMutex
	threads        map[string]*domain.Thread
	versionCounter int64
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
	items := make(map[string]*domain.Thread, len(r.threads))
	for id, thread := range r.threads {
		items[id] = cloneThread(thread)
	}
	return items
}

func (r *MemoryThreadRepository) Restore(value any) {
	items, ok := value.(map[string]*domain.Thread)
	if !ok {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.threads = make(map[string]*domain.Thread, len(items))
	for id, thread := range items {
		r.threads[id] = cloneThread(thread)
		if thread.FactsVersion > r.versionCounter {
			r.versionCounter = thread.FactsVersion
		}
	}
	// Like a PostgreSQL sequence, the allocation counter is never rolled back.
}

func (r *MemoryThreadRepository) Create(_ context.Context, thread *domain.Thread) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	version, err := r.nextFactsVersionLocked()
	if err != nil {
		return err
	}
	thread.NormalizeContentState()
	thread.FactsVersion = version
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

func (r *MemoryThreadRepository) RecheckPublicThread(_ context.Context, id string, expectedVersion int64, incrementView bool) (*domain.Thread, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	thread, ok := r.threads[id]
	if !ok || expectedVersion < 1 || thread.FactsVersion != expectedVersion ||
		thread.PublicationStatus != domain.PublicationStatusPublished ||
		thread.ModerationStatus != domain.ModerationStatusClear ||
		thread.DeletionStatus != domain.DeletionStatusActive {
		return nil, ErrThreadNotFound
	}
	if incrementView {
		thread.ViewCount++
	}
	return cloneThread(thread), nil
}

func (r *MemoryThreadRepository) RecheckOwnerThread(_ context.Context, id, ownerID string, expectedVersion int64) (*domain.Thread, error) {
	if expectedVersion < 1 || ownerID == "" {
		return nil, ErrThreadNotFound
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	thread, ok := r.threads[id]
	if !ok || thread == nil || thread.ID != id || thread.AuthorID != ownerID || thread.FactsVersion != expectedVersion ||
		!ownerThreadSnapshotStateValid(thread) {
		return nil, ErrThreadNotFound
	}
	return cloneThread(thread), nil
}

func ownerThreadSnapshotStateValid(thread *domain.Thread) bool {
	switch thread.PublicationStatus {
	case domain.PublicationStatusDraft, domain.PublicationStatusPublished, domain.PublicationStatusPrivate:
	default:
		return false
	}
	switch thread.ModerationStatus {
	case domain.ModerationStatusClear, domain.ModerationStatusPending, domain.ModerationStatusRejected, domain.ModerationStatusTakenDown:
	default:
		return false
	}
	return thread.DeletionStatus == domain.DeletionStatusActive || thread.DeletionStatus == domain.DeletionStatusTrashed
}

func (r *MemoryThreadRepository) nextFactsVersionLocked() (int64, error) {
	if r.versionCounter == math.MaxInt64 {
		return 0, ErrThreadFactsVersionExhausted
	}
	r.versionCounter++
	return r.versionCounter, nil
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
	version, err := r.nextFactsVersionLocked()
	if err != nil {
		return err
	}
	thread.NormalizeContentState()
	thread.FactsVersion = version
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
	version, err := r.nextFactsVersionLocked()
	if err != nil {
		return err
	}
	thread.NormalizeContentState()
	thread.FactsVersion = version
	r.threads[thread.ID] = cloneThread(thread)
	return nil
}

func (r *MemoryThreadRepository) Delete(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.threads[id]; !ok {
		return ErrThreadNotFound
	}
	version, err := r.nextFactsVersionLocked()
	if err != nil {
		return err
	}
	thread := r.threads[id]
	thread.NormalizeContentState()
	thread.DeletionStatus = domain.DeletionStatusTrashed
	thread.SyncLegacyStatus()
	thread.UpdatedAt = time.Now().UTC()
	thread.FactsVersion = version
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
		if filtered[i].CreatedAt.Equal(filtered[j].CreatedAt) {
			return filtered[i].ID > filtered[j].ID
		}
		return filtered[i].CreatedAt.After(filtered[j].CreatedAt)
	})

	total := int64(len(filtered))

	pageSize, start, err := threadListPagination(filter)
	if err != nil {
		return nil, 0, err
	}
	if start >= len(filtered) {
		return []*domain.Thread{}, total, nil
	}
	end := len(filtered)
	if pageSize < len(filtered)-start {
		end = start + pageSize
	}

	return filtered[start:end], total, nil
}

func threadListPagination(filter domain.ThreadListFilter) (pageSize, offset int, err error) {
	page, pageSize := filter.Page, filter.PageSize
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	maxInt := int(^uint(0) >> 1)
	if page-1 > maxInt/pageSize {
		return 0, 0, ErrThreadPaginationOverflow
	}
	return pageSize, (page - 1) * pageSize, nil
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
