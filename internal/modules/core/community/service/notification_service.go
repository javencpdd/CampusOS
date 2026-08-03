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
	return s.createThreadNotification(ctx, userID, threadID, domain.NotificationTypeThreadTrashed,
		"帖子已被管理员移入回收站",
		fmt.Sprintf("您的帖子《%s》已被管理员移入回收站。内容不会继续公开，您可以打开详情查看或恢复。", notificationThreadTitle(threadTitle)),
		map[string]interface{}{"reason": strings.TrimSpace(reason)}, "")
}

func (s *NotificationService) NotifyThreadTakenDown(ctx context.Context, userID, threadID, threadTitle, reason string) error {
	return s.createThreadNotification(ctx, userID, threadID, domain.NotificationTypeThreadTakenDown,
		"帖子已被管理员下架",
		fmt.Sprintf("您的帖子《%s》已被管理员下架，请查看原因并在整改后重新提交审核。", notificationThreadTitle(threadTitle)),
		map[string]interface{}{"reason": strings.TrimSpace(reason)}, "")
}

func (s *NotificationService) NotifyThreadReplied(ctx context.Context, userID, threadID, threadTitle, postID, actorName string) error {
	actorName = notificationActorName(actorName)
	return s.createThreadNotification(ctx, userID, threadID, domain.NotificationTypeThreadReplied,
		"您的帖子收到新回复",
		fmt.Sprintf("%s 回复了您的帖子《%s》。", actorName, notificationThreadTitle(threadTitle)),
		map[string]interface{}{"post_id": strings.TrimSpace(postID), "actor_name": actorName}, strings.TrimSpace(postID))
}

func (s *NotificationService) NotifyPostReplied(ctx context.Context, userID, threadID, threadTitle, parentPostID, postID, actorName string) error {
	actorName = notificationActorName(actorName)
	return s.createThreadNotification(ctx, userID, threadID, domain.NotificationTypePostReplied,
		"您的评论收到新回复",
		fmt.Sprintf("%s 回复了您在《%s》中的评论。", actorName, notificationThreadTitle(threadTitle)),
		map[string]interface{}{
			"parent_post_id": strings.TrimSpace(parentPostID), "post_id": strings.TrimSpace(postID), "actor_name": actorName,
		}, strings.TrimSpace(postID))
}

func (s *NotificationService) createThreadNotification(ctx context.Context, userID, threadID, notificationType, title, content string, metadata map[string]interface{}, postID string) error {
	if s == nil || s.repo == nil {
		return errors.New("notification repository is unavailable")
	}
	userID = strings.TrimSpace(userID)
	threadID = strings.TrimSpace(threadID)
	if userID == "" || threadID == "" {
		return errors.New("notification recipient and thread are required")
	}
	if metadata == nil {
		metadata = make(map[string]interface{})
	}
	metadata["thread_id"] = threadID
	actionURL := "/threads/" + threadID
	if postID != "" {
		actionURL += "#post-" + postID
	}
	now := time.Now().UTC()
	return s.repo.Create(ctx, &domain.Notification{
		ID: strconv.FormatInt(idgen.New(), 10), UserID: userID, Type: notificationType,
		Title: title, Content: content, ActionURL: actionURL, Metadata: metadata,
		CreatedAt: now, UpdatedAt: now,
	})
}

func notificationThreadTitle(value string) string {
	value = truncateNotificationTitle(strings.TrimSpace(value), 80)
	if value == "" {
		return "未命名帖子"
	}
	return value
}

func notificationActorName(value string) string {
	value = truncateNotificationTitle(strings.TrimSpace(value), 40)
	if value == "" {
		return "一位用户"
	}
	return value
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
