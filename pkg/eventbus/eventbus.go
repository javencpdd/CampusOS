package eventbus

import "context"

// Event 事件结构（遵循 CloudEvents 1.0 规范子集）
type Event struct {
	ID         string            `json:"id"`
	Source     string            `json:"source"`
	Type       string            `json:"type"`    // 例如: user.created, thread.created
	Subject    string            `json:"subject"` // 例如: user.123, thread.456
	Time       string            `json:"time"`
	Data       interface{}       `json:"data"`
	Extensions map[string]string `json:"extensions,omitempty"`
}

// EventHandler 事件处理函数
type EventHandler func(ctx context.Context, event Event) error

// EventBus 事件总线接口
type EventBus interface {
	// Publish 发布事件
	Publish(ctx context.Context, event Event) error
	// Subscribe 订阅事件
	Subscribe(eventType string, handler EventHandler) error
	// Close 关闭连接
	Close() error
}
