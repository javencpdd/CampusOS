package knowledge

import (
	"context"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
)

const (
	indexBackfillPageSize               = 32
	indexBackfillIdle                   = time.Minute
	indexBackfillCheckpoints recordKind = "index_backfill_checkpoints"
)

// A checkpoint is enumeration progress, never an indexing/readiness claim or
// an event-ID watermark. Every completed pass returns to the beginning.
type indexBackfillCheckpoint struct {
	TargetKind      string    `json:"target_kind"`
	AfterID         string    `json:"after_id"`
	CompletedPasses int64     `json:"completed_passes,string"`
	NextScanAt      time.Time `json:"next_scan_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}
type indexBackfillPage struct {
	Checkpoint indexBackfillCheckpoint
	Targets    int
	Advanced   bool
}
type indexBackfillStore interface {
	backfillIndexPage(context.Context, string, int) (indexBackfillPage, error)
}

type indexBackfiller struct {
	store    indexBackfillStore
	reliable *reliability.Service
	failed   atomic.Bool
	mu       sync.Mutex
	cancel   context.CancelFunc
	done     chan struct{}
	stopping bool
}

func newIndexBackfiller(store indexBackfillStore, reliable *reliability.Service) (*indexBackfiller, error) {
	if nilSourceAdapter(store) || reliable == nil {
		return nil, ErrSourceUnavailable
	}
	if snap, ok := store.(transaction.Snapshotter); ok {
		reliable.RegisterMemorySnapshotters(snap)
	}
	return &indexBackfiller{store: store, reliable: reliable}, nil
}
func (b *indexBackfiller) page(ctx context.Context, kind string) (indexBackfillPage, error) {
	var result indexBackfillPage
	err := b.reliable.CheckedRead(ctx, func(tx context.Context) error {
		var err error
		result, err = b.store.backfillIndexPage(tx, kind, indexBackfillPageSize)
		if err != nil {
			return err
		}
		return tx.Err()
	})
	if err != nil {
		return indexBackfillPage{}, err
	}
	return result, nil
}
func (b *indexBackfiller) start(ctx context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.cancel != nil {
		select {
		case <-b.done:
		default:
			if b.stopping {
				return ErrIndexConsumerStopped
			}
			return nil
		}
	}
	worker, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	b.cancel, b.done, b.stopping = cancel, done, false
	go func() {
		defer close(done)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for worker.Err() == nil {
			failed := false
			for _, kind := range []string{"collection", "official"} {
				page, stop := context.WithTimeout(worker, 5*time.Second)
				_, err := b.page(page, kind)
				stop()
				if err != nil && worker.Err() == nil {
					failed = true
					// Do not log SQL, source metadata, credentials or arbitrary errors.
					log.Print("knowledge metadata backfill page failed; checkpoint retained, retry scheduled")
				}
			}
			b.failed.Store(failed)
			select {
			case <-worker.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	return nil
}
func (b *indexBackfiller) stop(ctx context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.cancel == nil {
		return nil
	}
	b.stopping = true
	b.cancel()
	select {
	case <-b.done:
		b.cancel = nil
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
