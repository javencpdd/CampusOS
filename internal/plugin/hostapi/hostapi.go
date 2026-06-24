package hostapi

import (
	"context"
	"log"

	"github.com/campusos/CampusOS/internal/community/domain"
	"github.com/campusos/CampusOS/internal/community/repository"
	identityrepo "github.com/campusos/CampusOS/internal/core/identity/repository"
	"github.com/campusos/CampusOS/pkg/eventbus"
)

// HostAPI 插件调用核心能力的统一接口
type HostAPI struct {
	identity *IdentityAPI
	data     *DataAPI
	event    *EventAPI
}

// NewHostAPI 创建 Host API
func NewHostAPI(
	userRepo identityrepo.UserRepository,
	threadRepo repository.ThreadRepository,
	categoryRepo repository.CategoryRepository,
	postRepo repository.PostRepository,
	bus eventbus.EventBus,
) *HostAPI {
	return &HostAPI{
		identity: &IdentityAPI{userRepo: userRepo},
		data:     &DataAPI{threadRepo: threadRepo, categoryRepo: categoryRepo, postRepo: postRepo},
		event:    &EventAPI{bus: bus},
	}
}

func (h *HostAPI) Identity() *IdentityAPI { return h.identity }
func (h *HostAPI) Data() *DataAPI         { return h.data }
func (h *HostAPI) Event() *EventAPI       { return h.event }

// IdentityAPI 身份查询接口
type IdentityAPI struct {
	userRepo identityrepo.UserRepository
}

// GetUser 查询用户信息
func (api *IdentityAPI) GetUser(ctx context.Context, userID string) (map[string]interface{}, error) {
	user, err := api.userRepo.GetByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"id":       user.ID,
		"username": user.Username,
		"nickname": user.Nickname,
		"email":    user.Email,
		"status":   user.Status,
	}, nil
}

// DataAPI 数据查询接口
type DataAPI struct {
	threadRepo   repository.ThreadRepository
	categoryRepo repository.CategoryRepository
	postRepo     repository.PostRepository
}

// QueryThreads 查询帖子列表
func (api *DataAPI) QueryThreads(ctx context.Context, filter map[string]interface{}) ([]map[string]interface{}, error) {
	f := domain.ThreadListFilter{}
	if v, ok := filter["category_id"].(string); ok {
		f.CategoryID = v
	}
	if v, ok := filter["author_id"].(string); ok {
		f.AuthorID = v
	}
	if v, ok := filter["keyword"].(string); ok {
		f.Keyword = v
	}
	f.Page = 1
	f.PageSize = 20
	if v, ok := filter["page"].(int); ok {
		f.Page = v
	}
	if v, ok := filter["page_size"].(int); ok {
		f.PageSize = v
	}

	threads, _, err := api.threadRepo.List(ctx, f)
	if err != nil {
		return nil, err
	}

	result := make([]map[string]interface{}, 0, len(threads))
	for _, t := range threads {
		result = append(result, map[string]interface{}{
			"id":          t.ID,
			"title":       t.Title,
			"content":     t.Content,
			"author_id":   t.AuthorID,
			"author_name": t.AuthorName,
			"category_id": t.CategoryID,
			"status":      t.Status,
			"view_count":  t.ViewCount,
			"reply_count": t.ReplyCount,
			"created_at":  t.CreatedAt,
		})
	}
	return result, nil
}

// EventAPI 事件发布接口
type EventAPI struct {
	bus eventbus.EventBus
}

// Publish 发布事件
func (api *EventAPI) Publish(ctx context.Context, eventType, source, subject string, data interface{}) error {
	if api.bus == nil {
		log.Printf("⚠️  EventBus 不可用，无法发布事件: %s", eventType)
		return nil
	}
	return api.bus.Publish(ctx, eventbus.NewEvent(eventType, source, subject, data))
}
