package eventbus

import (
	"time"

	"github.com/google/uuid"
)

// 预定义事件类型常量
const (
	EventUserCreated     = "user.created"
	EventUserUpdated     = "user.updated"
	EventThreadCreated   = "thread.created"
	EventThreadUpdated   = "thread.updated"
	EventThreadDeleted   = "thread.deleted"
	EventPostCreated     = "post.created"
	EventCategoryCreated = "category.created"
)

// NewEvent 创建标准事件
func NewEvent(eventType, source, subject string, data interface{}) Event {
	return Event{
		ID:      uuid.New().String(),
		Source:  source,
		Type:    eventType,
		Subject: subject,
		Time:    time.Now().UTC().Format(time.RFC3339),
		Data:    data,
	}
}
