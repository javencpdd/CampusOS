package storage

import (
	"context"
	"errors"
	"math"
	"sync"

	"github.com/campusos/CampusOS/internal/platform/transaction"
)

// AvatarRetentionAccounting is only for first-party checked legacy cleanup.
// The owning workflow binds exact bytes and atomically persists its once marker.
type AvatarRetentionAccounting interface {
	PrepareAvatarRetentionAccounting(context.Context, string) error
	ReleaseAvatarRetentionBytes(context.Context, string, int64) (func() error, error)
}

func (s *ObjectService) PrepareAvatarRetentionAccounting(ctx context.Context, owner string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	_, pg := transaction.FromContext(ctx)
	_, memory := s.repository.(*MemoryObjectRepository)
	if !transaction.Active(ctx) || !SafeSegment(owner) || !pg && !memory {
		return ErrObjectUnavailable
	}
	_, err := s.ensureAccount(ctx, owner)
	return err
}

func (s *ObjectService) ReleaseAvatarRetentionBytes(ctx context.Context, owner string, size int64) (func() error, error) {
	noop := func() error { return nil }
	if err := ctx.Err(); err != nil {
		return noop, err
	}
	_, pg := transaction.FromContext(ctx)
	_, memory := s.repository.(*MemoryObjectRepository)
	if !transaction.Active(ctx) || !SafeSegment(owner) || size <= 0 || !pg && !memory {
		return noop, ErrObjectUnavailable
	}
	account, exists, err := s.repository.Account(ctx, owner)
	if err != nil || !exists || account.UsedBytes < size {
		return noop, errors.Join(ErrObjectUnavailable, err)
	}
	if err := s.repository.AdjustUsage(ctx, owner, -size, 0, 0); err != nil {
		return noop, err
	}
	if pg {
		return noop, nil
	}
	var once sync.Once
	var result error
	return func() error {
		once.Do(func() { result = s.repository.AdjustUsage(context.WithoutCancel(ctx), owner, size, math.MaxInt64, 0) })
		if result != nil {
			return errors.Join(ErrInlineImageCleanupPending, result)
		}
		return nil
	}, nil
}
