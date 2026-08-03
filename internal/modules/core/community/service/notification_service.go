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

func (s *NotificationService) NotifyPostDeletedByModerator(ctx context.Context, userID, threadID, threadTitle, postID string) error {
	return s.createThreadNotification(ctx, userID, threadID, domain.NotificationTypePostDeletedByModerator,
		"您的评论已被版主或管理员删除",
		fmt.Sprintf("您在帖子《%s》中的评论已被版主或管理员删除，如有疑问请联系板块管理团队。", notificationThreadTitle(threadTitle)),
		map[string]interface{}{"post_id": strings.TrimSpace(postID)}, "")
}

// ModeratorScopeCategory identifies one board in a moderator lifecycle message.
type ModeratorScopeCategory struct {
	ID   string
	Name string
}

func (s *NotificationService) NotifyModeratorScopeGranted(ctx context.Context, userID string, categories []ModeratorScopeCategory) error {
	if len(categories) == 0 {
		return nil
	}
	return s.createNotification(ctx, userID, domain.NotificationTypeModeratorGranted,
		"您已获得版主权限",
		fmt.Sprintf("您已获得%s的版主权限，现在可以在这些板块执行置顶、锁定和删除回复操作。", notificationCategoryNames(categories)),
		moderatorScopeActionURL(categories), moderatorScopeMetadata(categories))
}

func (s *NotificationService) NotifyModeratorScopeRevoked(ctx context.Context, userID string, categories []ModeratorScopeCategory) error {
	if len(categories) == 0 {
		return nil
	}
	return s.createNotification(ctx, userID, domain.NotificationTypeModeratorRevoked,
		"您的版主权限已变更",
		fmt.Sprintf("您在%s的版主权限已被移除，对应板块的治理操作入口将不再可用。", notificationCategoryNames(categories)),
		moderatorScopeActionURL(categories), moderatorScopeMetadata(categories))
}

func (s *NotificationService) createThreadNotification(ctx context.Context, userID, threadID, notificationType, title, content string, metadata map[string]interface{}, postID string) error {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return errors.New("notification thread is required")
	}
	if metadata == nil {
		metadata = make(map[string]interface{})
	}
	metadata["thread_id"] = threadID
	actionURL := "/threads/" + threadID
	if postID != "" {
		actionURL += "#post-" + postID
	}
	return s.createNotification(ctx, userID, notificationType, title, content, actionURL, metadata)
}

func (s *NotificationService) createNotification(ctx context.Context, userID, notificationType, title, content, actionURL string, metadata map[string]interface{}) error {
	if s == nil || s.repo == nil {
		return errors.New("notification repository is unavailable")
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return errors.New("notification recipient is required")
	}
	if metadata == nil {
		metadata = make(map[string]interface{})
	}
	now := time.Now().UTC()
	return s.repo.Create(ctx, &domain.Notification{
		ID: strconv.FormatInt(idgen.New(), 10), UserID: userID, Type: notificationType,
		Title: title, Content: content, ActionURL: actionURL, Metadata: metadata,
		CreatedAt: now, UpdatedAt: now,
	})
}

func moderatorScopeActionURL(categories []ModeratorScopeCategory) string {
	if len(categories) == 1 && strings.TrimSpace(categories[0].ID) != "" {
		return "/threads?category_id=" + strings.TrimSpace(categories[0].ID)
	}
	return "/threads"
}

func moderatorScopeMetadata(categories []ModeratorScopeCategory) map[string]interface{} {
	ids := make([]string, 0, len(categories))
	for _, category := range categories {
		if id := strings.TrimSpace(category.ID); id != "" {
			ids = append(ids, id)
		}
	}
	return map[string]interface{}{"category_ids": ids}
}

func notificationCategoryNames(categories []ModeratorScopeCategory) string {
	names := make([]string, 0, len(categories))
	for _, category := range categories {
		name := truncateNotificationTitle(strings.TrimSpace(category.Name), 24)
		if name == "" {
			name = "未命名板块"
		}
		names = append(names, "「"+name+"」")
	}
	if len(names) <= 3 {
		return strings.Join(names, "、")
	}
	return fmt.Sprintf("%s等 %d 个板块", strings.Join(names[:2], "、"), len(names))
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
