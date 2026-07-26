package repository

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
)

var ErrNotificationNotFound = errors.New("notification not found")

type NotificationRepository interface {
	Create(context.Context, *domain.Notification) error
	ListByUser(context.Context, string, int, int) ([]*domain.Notification, int64, error)
	CountUnread(context.Context, string) (int64, error)
	MarkRead(context.Context, string, string, time.Time) error
	MarkAllRead(context.Context, string, time.Time) (int64, error)
}

type MemoryNotificationRepository struct {
	mu    sync.RWMutex
	items map[string]*domain.Notification
}

func NewMemoryNotificationRepository() *MemoryNotificationRepository {
	return &MemoryNotificationRepository{items: make(map[string]*domain.Notification)}
}

func (r *MemoryNotificationRepository) Snapshot() any {
	r.mu.RLock()
	defer r.mu.RUnlock()
	payload, err := json.Marshal(r.items)
	if err != nil {
		return []byte(nil)
	}
	return append([]byte(nil), payload...)
}

func (r *MemoryNotificationRepository) Restore(value any) {
	payload, ok := value.([]byte)
	if !ok || len(payload) == 0 {
		return
	}
	items := make(map[string]*domain.Notification)
	if err := json.Unmarshal(payload, &items); err != nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.items = items
}

func (r *MemoryNotificationRepository) Create(_ context.Context, value *domain.Notification) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.items[value.ID] = cloneNotification(value)
	return nil
}

func (r *MemoryNotificationRepository) ListByUser(_ context.Context, userID string, page, pageSize int) ([]*domain.Notification, int64, error) {
	page, pageSize = normalizeNotificationPage(page, pageSize)
	r.mu.RLock()
	defer r.mu.RUnlock()
	items := make([]*domain.Notification, 0)
	for _, value := range r.items {
		if value.UserID == userID {
			items = append(items, cloneNotification(value))
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].ID > items[j].ID
		}
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	total := int64(len(items))
	start := (page - 1) * pageSize
	if start >= len(items) {
		return []*domain.Notification{}, total, nil
	}
	end := start + pageSize
	if end > len(items) {
		end = len(items)
	}
	return items[start:end], total, nil
}

func (r *MemoryNotificationRepository) CountUnread(_ context.Context, userID string) (int64, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var count int64
	for _, value := range r.items {
		if value.UserID == userID && !value.IsRead {
			count++
		}
	}
	return count, nil
}

func (r *MemoryNotificationRepository) MarkRead(_ context.Context, userID, id string, at time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	value, ok := r.items[id]
	if !ok || value.UserID != userID {
		return ErrNotificationNotFound
	}
	if !value.IsRead {
		value.IsRead = true
		readAt := at
		value.ReadAt = &readAt
		value.UpdatedAt = at
	}
	return nil
}

func (r *MemoryNotificationRepository) MarkAllRead(_ context.Context, userID string, at time.Time) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var count int64
	for _, value := range r.items {
		if value.UserID != userID || value.IsRead {
			continue
		}
		value.IsRead = true
		readAt := at
		value.ReadAt = &readAt
		value.UpdatedAt = at
		count++
	}
	return count, nil
}

func normalizeNotificationPage(page, pageSize int) (int, int) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	return page, pageSize
}

func cloneNotification(value *domain.Notification) *domain.Notification {
	if value == nil {
		return nil
	}
	copy := *value
	if value.ReadAt != nil {
		readAt := *value.ReadAt
		copy.ReadAt = &readAt
	}
	copy.Metadata = make(map[string]interface{}, len(value.Metadata))
	for key, item := range value.Metadata {
		copy.Metadata[key] = item
	}
	return &copy
}
