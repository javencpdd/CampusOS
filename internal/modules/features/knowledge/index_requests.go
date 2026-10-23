package knowledge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"sync/atomic"
	"time"

	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
)

const (
	selectionChangedEvent               = "knowledge.selection.changed"
	IndexRequestConsumerName            = "knowledge:index-request:v1"
	indexRequestRecords      recordKind = "index_tasks"
)

var (
	ErrIndexEventInvalid    = errors.New("knowledge index event invalid")
	ErrIndexConsumerStopped = errors.New("knowledge index request consumer stopped")
)

// IndexRequest is an invalidation/reconciliation request, not a ready index or
// authority to read a body. A zero version asks a future executor to recheck an
// absent target. Consumers/executors must never replay this as an ACL snapshot.
type IndexRequest struct {
	ID            string    `json:"id"`
	TargetKind    string    `json:"target_kind"`
	TargetID      string    `json:"target_id"`
	TargetVersion int64     `json:"target_version,string"`
	Status        string    `json:"status"`
	CreatedAt     time.Time `json:"created_at"`
}
type indexRequestStore interface {
	requestCurrentIndex(context.Context, string, string) (IndexRequest, error)
}

func indexRequestID(kind, id string, version int64) string {
	h := sha256.Sum256([]byte("campusos.knowledge.index-request/v1\x00" + kind + "\x00" + id + "\x00" + strconv.FormatInt(version, 10)))
	return hex.EncodeToString(h[:])
}
func validIndexTarget(kind, id string) bool {
	return kind == "collection" && opaque("kc_", id) || kind == "official" && opaque("ko_", id)
}

type indexRequestConsumer struct {
	store    indexRequestStore
	reliable *reliability.Service
	stopped  atomic.Bool
}

func newIndexRequestConsumer(store indexRequestStore, reliable *reliability.Service) (*indexRequestConsumer, error) {
	if nilSourceAdapter(store) || reliable == nil {
		return nil, ErrSourceUnavailable
	}
	if snap, ok := store.(transaction.Snapshotter); ok {
		reliable.RegisterMemorySnapshotters(snap)
	}
	return &indexRequestConsumer{store: store, reliable: reliable}, nil
}
func (c *indexRequestConsumer) consume(ctx context.Context, event reliability.Event) error {
	if c == nil || c.stopped.Load() {
		return ErrIndexConsumerStopped
	}
	if event.Type != selectionChangedEvent || event.SchemaVersion != reliability.EventSchemaV1 || event.AggregateType != "knowledge" || identityport.ValidateClosedHTTPObject(event.Payload, []string{"resource_id", "operation"}, 1024) != nil {
		return reliability.Permanent(ErrIndexEventInvalid)
	}
	var payload struct {
		ResourceID string `json:"resource_id"`
		Operation  string `json:"operation"`
	}
	if json.Unmarshal(event.Payload, &payload) != nil || event.AggregateID != payload.ResourceID {
		return reliability.Permanent(ErrIndexEventInvalid)
	}
	kind := "collection"
	switch payload.Operation {
	case "official.save":
		kind = "official"
	case "collection.save", "selection.save", "selection.refresh", "member.remove", "index.retry":
	default:
		return reliability.Permanent(ErrIndexEventInvalid)
	}
	if !validIndexTarget(kind, payload.ResourceID) {
		return reliability.Permanent(ErrIndexEventInvalid)
	}
	// Feature-off still records cleanup intent. No source reader, principal,
	// index, remote call or body enters this metadata-only transaction.
	err := c.reliable.CheckedRead(ctx, func(tx context.Context) error {
		if c.stopped.Load() {
			return ErrIndexConsumerStopped
		}
		if _, e := c.store.requestCurrentIndex(tx, kind, payload.ResourceID); e != nil {
			return e
		}
		if c.stopped.Load() {
			return ErrIndexConsumerStopped
		}
		return tx.Err()
	})
	// Release the official row lock before taking job locks: execution locks the
	// collection/job before reading an official. Both commits precede the Core
	// receipt; failure here is retried, with the immutable request deduplicated.
	if err == nil && kind == "official" {
		if store, ok := c.store.(indexStore); ok {
			err = c.reliable.CheckedRead(ctx, func(tx context.Context) error {
				if c.stopped.Load() {
					return ErrIndexConsumerStopped
				}
				if e := store.invalidateIndexes(tx); e != nil {
					return e
				}
				if c.stopped.Load() {
					return ErrIndexConsumerStopped
				}
				return tx.Err()
			})
		}
	}
	return err
}
func (c *indexRequestConsumer) stop() {
	if c != nil {
		c.stopped.Store(true)
	}
}
func (c *indexRequestConsumer) resume() {
	if c != nil {
		c.stopped.Store(false)
	}
}
