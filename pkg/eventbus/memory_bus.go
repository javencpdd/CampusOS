package eventbus

import (
	"context"
	"log"
	"sync"
)

// MemoryEventBus 内存事件总线（NATS 不可用时回退）
type MemoryEventBus struct {
	mu       sync.RWMutex
	handlers map[string][]EventHandler
	history  []Event // 事件历史记录
}

func NewMemoryEventBus() *MemoryEventBus {
	return &MemoryEventBus{handlers: make(map[string][]EventHandler)}
}

// GetHistory 获取最近的事件历史
func (b *MemoryEventBus) GetHistory(limit int) []Event {
	b.mu.RLock()
	defer b.mu.RUnlock()
	total := len(b.history)
	if limit <= 0 || limit > total {
		limit = total
	}
	start := total - limit
	if start < 0 {
		start = 0
	}
	result := make([]Event, limit)
	copy(result, b.history[start:])
	return result
}

func (b *MemoryEventBus) Publish(_ context.Context, event Event) error {
	b.mu.Lock()
	b.history = append(b.history, event)
	// 保留最近 1000 条
	if len(b.history) > 1000 {
		b.history = b.history[len(b.history)-1000:]
	}
	handlers := b.handlers[event.Type]
	b.mu.Unlock()

	go func() {
		for _, h := range handlers {
			if err := h(context.Background(), event); err != nil {
				log.Printf("❌ 内存事件处理失败 [%s]: %v", event.Type, err)
			}
		}
	}()
	return nil
}

func (b *MemoryEventBus) Subscribe(eventType string, handler EventHandler) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.handlers[eventType] = append(b.handlers[eventType], handler)
	return nil
}

func (b *MemoryEventBus) Close() error { return nil }
