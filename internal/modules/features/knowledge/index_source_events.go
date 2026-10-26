package knowledge

import (
	"context"
	"sync/atomic"

	"github.com/campusos/CampusOS/internal/platform/reliability"
)

const IndexSourceConsumerName = "knowledge:source-invalidation:v1"

var indexSourceEvents = map[string]string{
	"thread.created": "thread", "thread.updated": "thread", "thread.deleted": "thread",
	"category.created": "category", "category.updated": "category", "category.moved": "category",
	"category.archived": "category", "category.restored": "category",
}

// RichText publishes its owning thread.updated/deleted after edits and lifecycle
// commands. Payloads can contain private text: only envelope IDs are inspected.
// Broad invalidation is bounded by MaxInventory; it grants no body-read rights.
type indexSourceConsumer struct {
	store    indexStore
	reliable *reliability.Service
	stopped  atomic.Bool
}

func (c *indexSourceConsumer) consume(ctx context.Context, event reliability.Event) error {
	if c.stopped.Load() {
		return ErrIndexConsumerStopped
	}
	kind, ok := indexSourceEvents[event.Type]
	if !ok || event.SchemaVersion != reliability.EventSchemaV1 || event.AggregateType != kind || !sourceID(event.AggregateID) {
		return reliability.Permanent(ErrIndexEventInvalid)
	}
	return c.reliable.CheckedRead(ctx, func(tx context.Context) error {
		if c.stopped.Load() {
			return ErrIndexConsumerStopped
		}
		if err := c.store.invalidateIndexes(tx); err != nil {
			return err
		}
		if c.stopped.Load() {
			return ErrIndexConsumerStopped
		}
		return tx.Err()
	})
}
