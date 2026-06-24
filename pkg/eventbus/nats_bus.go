package eventbus

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"

	"github.com/nats-io/nats.go"
)

// NATSEventBus 基于 NATS 的事件总线实现
type NATSEventBus struct {
	conn     *nats.Conn
	mu       sync.RWMutex
	handlers map[string][]EventHandler
}

// NewNATSEventBus 创建 NATS 事件总线
func NewNATSEventBus(url string) (*NATSEventBus, error) {
	nc, err := nats.Connect(url,
		nats.MaxReconnects(-1),
		nats.DisconnectErrHandler(func(_ *nats.Conn, err error) {
			log.Printf("⚠️  NATS 断开连接: %v", err)
		}),
		nats.ReconnectHandler(func(nc *nats.Conn) {
			log.Printf("✅ NATS 重连成功: %s", nc.ConnectedUrl())
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("connect nats: %w", err)
	}
	bus := &NATSEventBus{
		conn:     nc,
		handlers: make(map[string][]EventHandler),
	}
	log.Printf("✅ NATS 连接成功: %s", nc.ConnectedUrl())
	return bus, nil
}

func (b *NATSEventBus) Publish(_ context.Context, event Event) error {
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}
	return b.conn.Publish(event.Type, data)
}

func (b *NATSEventBus) Subscribe(eventType string, handler EventHandler) error {
	b.mu.Lock()
	b.handlers[eventType] = append(b.handlers[eventType], handler)
	b.mu.Unlock()

	_, err := b.conn.Subscribe(eventType, func(msg *nats.Msg) {
		var event Event
		if err := json.Unmarshal(msg.Data, &event); err != nil {
			log.Printf("❌ 事件反序列化失败: %v", err)
			return
		}
		b.mu.RLock()
		handlers := b.handlers[eventType]
		b.mu.RUnlock()
		for _, h := range handlers {
			if err := h(context.Background(), event); err != nil {
				log.Printf("❌ 事件处理失败 [%s]: %v", event.Type, err)
			}
		}
	})
	return err
}

func (b *NATSEventBus) Close() error {
	b.conn.Close()
	return nil
}
