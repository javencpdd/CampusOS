package storage

import (
	"context"
	"errors"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"sync"
)

// Only built-in commands receive this accounting seam. It creates no Object
// or read permission. SQL rollback owns PG compensation; Memory undoes just
// this allocation and never snapshots independently committed Object states.
type InlineImageAccounting interface {
	AccountInlineImageForCommand(context.Context, string, int64) (func() error, error)
}

func (s *ObjectService) AccountInlineImageForCommand(ctx context.Context, owner string, size int64) (func() error, error) {
	noop := func() error { return nil }
	if !transaction.Active(ctx) || !SafeSegment(owner) || size <= 0 {
		return noop, ErrObjectUnavailable
	}
	_, pg := transaction.FromContext(ctx)
	_, memory := s.repository.(*MemoryObjectRepository)
	if !pg && !memory {
		return noop, ErrObjectUnavailable
	}
	if err := ctx.Err(); err != nil {
		return noop, err
	}
	if _, err := s.ensureAccount(ctx, owner); err != nil {
		return noop, err
	}
	ctx = context.WithValue(ctx, objectQuotaCommandKey{}, objectQuotaResolver(s.resolveCurrentObjectQuota))
	if err := s.repository.AdjustUsage(ctx, owner, size, s.quota.QuotaBytes(owner), 0); err != nil {
		return noop, err
	}
	if pg {
		return noop, nil
	}
	var once sync.Once
	var result error
	undo := func() error {
		once.Do(func() { result = s.repository.AdjustUsage(context.WithoutCancel(ctx), owner, -size, 0, 0) })
		if result != nil {
			return errors.Join(ErrInlineImageCleanupPending, result)
		}
		return nil
	}
	return undo, nil
}
