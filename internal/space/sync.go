package space

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	communitydomain "github.com/campusos/CampusOS/internal/community/domain"
	"github.com/campusos/CampusOS/pkg/eventbus"
	"github.com/campusos/CampusOS/pkg/idgen"
)

var ErrContentRepositoryUnavailable = errors.New("space content repository unavailable")

func (s *Service) RegisterEventHandlers(bus eventbus.EventBus) error {
	if bus == nil {
		return nil
	}
	for _, eventType := range []string{
		eventbus.EventThreadCreated,
		eventbus.EventThreadUpdated,
		eventbus.EventThreadDeleted,
	} {
		if err := bus.Subscribe(eventType, s.HandleThreadEvent); err != nil {
			return fmt.Errorf("subscribe %s: %w", eventType, err)
		}
	}
	return nil
}

func (s *Service) HandleThreadEvent(ctx context.Context, event eventbus.Event) error {
	thread, err := threadFromEventData(event.Data)
	if err != nil {
		return err
	}
	if event.Type == eventbus.EventThreadDeleted {
		return s.deleteSyncedContent(ctx, thread.ID)
	}
	return s.SyncThread(ctx, thread)
}

func (s *Service) SyncThread(ctx context.Context, thread *communitydomain.Thread) error {
	if thread == nil || thread.ID == "" {
		return nil
	}
	if s.contentRepo == nil {
		return ErrContentRepositoryUnavailable
	}

	space, err := s.spaceForSync(ctx, thread.AuthorID)
	if err != nil {
		return err
	}

	if !shouldSyncThread(space, thread) {
		return s.deleteSyncedContent(ctx, thread.ID)
	}

	now := time.Now().UTC()
	content := &SpaceContent{
		ID:              fmt.Sprintf("%d", idgen.New()),
		UserID:          thread.AuthorID,
		ThreadID:        thread.ID,
		Title:           strings.TrimSpace(thread.Title),
		Excerpt:         excerpt(thread.Content, 240),
		AuthorName:      thread.AuthorName,
		CategoryID:      thread.CategoryID,
		Tags:            append([]string(nil), thread.Tags...),
		Status:          string(thread.Status),
		ThreadCreatedAt: thread.CreatedAt,
		ThreadUpdatedAt: thread.UpdatedAt,
		SyncedAt:        now,
	}
	if content.ThreadCreatedAt.IsZero() {
		content.ThreadCreatedAt = now
	}
	if content.ThreadUpdatedAt.IsZero() {
		content.ThreadUpdatedAt = content.ThreadCreatedAt
	}
	return s.contentRepo.UpsertContent(ctx, content)
}

func (s *Service) ListPublicContentsByUserID(ctx context.Context, userID string, page, pageSize int) ([]*SpaceContent, int64, error) {
	if s.contentRepo == nil {
		return nil, 0, ErrContentRepositoryUnavailable
	}
	if _, err := s.GetPublicByUserID(ctx, userID); err != nil {
		return nil, 0, err
	}
	return s.contentRepo.ListContentsByUserID(ctx, userID, normalizePage(page), normalizePageSize(pageSize))
}

func (s *Service) ListPublicContentsByUsername(ctx context.Context, username string, page, pageSize int) ([]*SpaceContent, int64, error) {
	if s.contentRepo == nil {
		return nil, 0, ErrContentRepositoryUnavailable
	}
	user, err := s.users.GetByUsername(ctx, username)
	if err != nil {
		return nil, 0, fmt.Errorf("get owner: %w", err)
	}
	if _, err := s.getPublicSpace(ctx, user); err != nil {
		return nil, 0, err
	}
	return s.contentRepo.ListContentsByUserID(ctx, user.ID, normalizePage(page), normalizePageSize(pageSize))
}

func (s *Service) deleteSyncedContent(ctx context.Context, threadID string) error {
	if s.contentRepo == nil {
		return ErrContentRepositoryUnavailable
	}
	return s.contentRepo.DeleteContent(ctx, threadID)
}

func (s *Service) spaceForSync(ctx context.Context, userID string) (*Space, error) {
	space, err := s.repo.GetByUserID(ctx, userID)
	if err == nil {
		return space, nil
	}
	if !errors.Is(err, ErrSpaceNotFound) {
		return nil, fmt.Errorf("get space: %w", err)
	}
	user, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("get owner: %w", err)
	}
	return defaultSpace(user), nil
}

func shouldSyncThread(space *Space, thread *communitydomain.Thread) bool {
	if space == nil || thread == nil {
		return false
	}
	if space.Visibility != VisibilityPublic || !space.SyncEnabled {
		return false
	}
	if thread.Status != communitydomain.ThreadStatusPublished {
		return false
	}
	if !matchesCategory(space.SyncCategories, thread.CategoryID) {
		return false
	}
	return matchesAnyTag(space.SyncTags, thread.Tags)
}

func matchesCategory(allowed []string, categoryID string) bool {
	if len(allowed) == 0 {
		return true
	}
	categoryID = strings.TrimSpace(categoryID)
	for _, value := range allowed {
		if strings.TrimSpace(value) == categoryID {
			return true
		}
	}
	return false
}

func matchesAnyTag(allowed, tags []string) bool {
	if len(allowed) == 0 {
		return true
	}
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, value := range allowed {
		value = strings.ToLower(strings.TrimSpace(value))
		if value != "" {
			allowedSet[value] = struct{}{}
		}
	}
	for _, tag := range tags {
		if _, ok := allowedSet[strings.ToLower(strings.TrimSpace(tag))]; ok {
			return true
		}
	}
	return false
}

func threadFromEventData(data interface{}) (*communitydomain.Thread, error) {
	switch value := data.(type) {
	case *communitydomain.Thread:
		return value, nil
	case communitydomain.Thread:
		return &value, nil
	default:
		raw, err := json.Marshal(value)
		if err != nil {
			return nil, fmt.Errorf("marshal thread event data: %w", err)
		}
		var thread communitydomain.Thread
		if err := json.Unmarshal(raw, &thread); err != nil {
			return nil, fmt.Errorf("decode thread event data: %w", err)
		}
		return &thread, nil
	}
}

func excerpt(content string, limit int) string {
	content = strings.Join(strings.Fields(strings.TrimSpace(content)), " ")
	if limit <= 0 || utf8.RuneCountInString(content) <= limit {
		return content
	}
	runes := []rune(content)
	return string(runes[:limit])
}

func normalizePage(page int) int {
	if page < 1 {
		return 1
	}
	return page
}

func normalizePageSize(pageSize int) int {
	if pageSize < 1 {
		return 20
	}
	if pageSize > 100 {
		return 100
	}
	return pageSize
}

func sortContents(contents []*SpaceContent) {
	sort.SliceStable(contents, func(i, j int) bool {
		if contents[i].ThreadCreatedAt.Equal(contents[j].ThreadCreatedAt) {
			return contents[i].SyncedAt.After(contents[j].SyncedAt)
		}
		return contents[i].ThreadCreatedAt.After(contents[j].ThreadCreatedAt)
	})
}
