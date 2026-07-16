package hostapi

import (
	"context"
	"errors"
	"log"

	"github.com/campusos/CampusOS/internal/community/domain"
	communityport "github.com/campusos/CampusOS/internal/community/port"
	identityport "github.com/campusos/CampusOS/internal/core/identity/port"
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
	users identityport.UserReader,
	threads ThreadReader,
	posts PostReader,
	bus eventbus.EventBus,
) *HostAPI {
	return &HostAPI{
		identity: &IdentityAPI{users: users},
		data:     &DataAPI{threads: threads, posts: posts},
		event:    &EventAPI{bus: bus},
	}
}

// NewHostAPIWithContentQuery is the production composition path. External
// plugins receive only Community's public visibility query, never a concrete
// repository or an unrestricted content service.
func NewHostAPIWithContentQuery(
	users identityport.UserReader,
	threads communityport.ContentQuery,
	posts PostReader,
	bus eventbus.EventBus,
) *HostAPI {
	return &HostAPI{
		identity: &IdentityAPI{users: users},
		data:     &DataAPI{publicThreads: threads, posts: posts},
		event:    &EventAPI{bus: bus},
	}
}

func (h *HostAPI) Identity() *IdentityAPI { return h.identity }
func (h *HostAPI) Data() *DataAPI         { return h.data }
func (h *HostAPI) Event() *EventAPI       { return h.event }

// IdentityAPI 身份查询接口
type IdentityAPI struct {
	users identityport.UserReader
}

// GetUser 查询用户信息
func (api *IdentityAPI) GetUser(ctx context.Context, userID string) (map[string]interface{}, error) {
	user, err := api.users.GetUser(ctx, userID)
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
	threads       ThreadReader
	publicThreads communityport.ContentQuery
	posts         PostReader
}

type ThreadReader interface {
	GetThread(context.Context, string) (*domain.Thread, error)
	ListThreads(context.Context, domain.ThreadListFilter) ([]*domain.Thread, int64, error)
}

type PostReader interface {
	GetPost(context.Context, string) (*domain.Post, error)
}

// GetThread 查询主题详情
func (api *DataAPI) GetThread(ctx context.Context, threadID string) (map[string]interface{}, error) {
	var (
		thread *domain.Thread
		err    error
	)
	if api.publicThreads != nil {
		thread, err = api.publicThreads.GetPublicThread(ctx, threadID)
	} else if api.threads != nil {
		thread, err = api.threads.GetThread(ctx, threadID)
		if err == nil && !thread.IsPublic() {
			return nil, errors.New("public thread not found")
		}
	} else {
		return nil, errors.New("public thread query is unavailable")
	}
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"id":             thread.ID,
		"title":          thread.Title,
		"content":        thread.Content,
		"author_id":      thread.AuthorID,
		"author_name":    thread.AuthorName,
		"category_id":    thread.CategoryID,
		"status":         thread.Status,
		"is_pinned":      thread.IsPinned,
		"is_locked":      thread.IsLocked,
		"is_highlighted": thread.IsHighlighted,
		"view_count":     thread.ViewCount,
		"reply_count":    thread.ReplyCount,
		"like_count":     thread.LikeCount,
		"tags":           thread.Tags,
		"created_at":     thread.CreatedAt,
		"updated_at":     thread.UpdatedAt,
	}, nil
}

// GetReply 查询回复详情
func (api *DataAPI) GetReply(ctx context.Context, replyID string) (map[string]interface{}, error) {
	post, err := api.posts.GetPost(ctx, replyID)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"id":           post.ID,
		"thread_id":    post.ThreadID,
		"author_id":    post.AuthorID,
		"author_name":  post.AuthorName,
		"parent_id":    post.ParentID,
		"content":      post.Content,
		"status":       post.Status,
		"like_count":   post.LikeCount,
		"floor_number": post.FloorNumber,
		"created_at":   post.CreatedAt,
		"updated_at":   post.UpdatedAt,
	}, nil
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

	var threads []*domain.Thread
	var err error
	if api.publicThreads != nil {
		threads, _, err = api.publicThreads.ListPublicThreads(ctx, f)
	} else if api.threads != nil {
		f.Status = string(domain.ThreadStatusPublished)
		f.PublicationStatus = string(domain.PublicationStatusPublished)
		f.ModerationStatus = string(domain.ModerationStatusClear)
		f.DeletionStatus = string(domain.DeletionStatusActive)
		threads, _, err = api.threads.ListThreads(ctx, f)
	} else {
		return nil, errors.New("public thread query is unavailable")
	}
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
