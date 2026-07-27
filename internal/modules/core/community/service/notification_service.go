package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	"github.com/campusos/CampusOS/internal/modules/core/community/repository"
	"github.com/campusos/CampusOS/pkg/idgen"
)

type NotificationService struct {
	repo repository.NotificationRepository
}

func NewNotificationService(repo repository.NotificationRepository) *NotificationService {
	return &NotificationService{repo: repo}
}

func (s *NotificationService) NotifyThreadTrashed(ctx context.Context, userID, threadID, threadTitle, reason string) error {
	if s == nil || s.repo == nil {
		return errors.New("notification repository is unavailable")
	}
	userID = strings.TrimSpace(userID)
	threadID = strings.TrimSpace(threadID)
	if userID == "" || threadID == "" {
		return errors.New("notification recipient and thread are required")
	}
	now := time.Now().UTC()
	title := truncateNotificationTitle(strings.TrimSpace(threadTitle), 80)
	if title == "" {
		title = "未命名帖子"
	}
	return s.repo.Create(ctx, &domain.Notification{
		ID:        strconv.FormatInt(idgen.New(), 10),
		UserID:    userID,
		Type:      domain.NotificationTypeThreadTrashed,
		Title:     "帖子已被管理员移入回收站",
		Content:   fmt.Sprintf("您的帖子《%s》已被管理员移入回收站。内容不会继续公开，您可以打开详情查看或恢复。", title),
		ActionURL: "/threads/" + threadID,
		Metadata: map[string]interface{}{
			"thread_id": threadID,
			"reason":    strings.TrimSpace(reason),
		},
		CreatedAt: now,
		UpdatedAt: now,
	})
}

func (s *NotificationService) List(ctx context.Context, userID string, page, pageSize int) (*domain.NotificationList, error) {
	if s == nil || s.repo == nil {
		return nil, errors.New("notification repository is unavailable")
	}
	page, pageSize = normalizeNotificationListPage(page, pageSize)
	items, total, err := s.repo.ListByUser(ctx, strings.TrimSpace(userID), page, pageSize)
	if err != nil {
		return nil, fmt.Errorf("list notifications: %w", err)
	}
	unread, err := s.repo.CountUnread(ctx, strings.TrimSpace(userID))
	if err != nil {
		return nil, fmt.Errorf("count unread notifications: %w", err)
	}
	return &domain.NotificationList{
		Items:       items,
		Page:        page,
		PageSize:    pageSize,
		Total:       total,
		UnreadCount: unread,
	}, nil
}

func (s *NotificationService) MarkRead(ctx context.Context, userID, id string) error {
	if s == nil || s.repo == nil {
		return errors.New("notification repository is unavailable")
	}
	if err := s.repo.MarkRead(ctx, strings.TrimSpace(userID), strings.TrimSpace(id), time.Now().UTC()); err != nil {
		return fmt.Errorf("mark notification read: %w", err)
	}
	return nil
}

func (s *NotificationService) MarkAllRead(ctx context.Context, userID string) (int64, error) {
	if s == nil || s.repo == nil {
		return 0, errors.New("notification repository is unavailable")
	}
	updated, err := s.repo.MarkAllRead(ctx, strings.TrimSpace(userID), time.Now().UTC())
	if err != nil {
		return 0, fmt.Errorf("mark all notifications read: %w", err)
	}
	return updated, nil
}

func normalizeNotificationListPage(page, pageSize int) (int, int) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	return page, pageSize
}

func truncateNotificationTitle(value string, limit int) string {
	if limit <= 0 || utf8.RuneCountInString(value) <= limit {
		return value
	}
	runes := []rune(value)
	return strings.TrimSpace(string(runes[:limit])) + "..."
}
