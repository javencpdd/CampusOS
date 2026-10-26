package service

import (
	"context"
	"errors"
)

func (s *ThreadService) SourceReadPinned(ctx context.Context, id string) bool {
	if s == nil {
		return false
	}
	if r, ok := s.repo.(interface {
		SourceReadPinned(context.Context, string) bool
	}); ok {
		return r.SourceReadPinned(ctx, id)
	}
	return false
}

// WithSourceReadSnapshot only optimizes repeated checked reads. The callback
// is trusted, compiled-in and read-only; no SQL handle or raw/private rows cross
// the Community Port. Adapters without locked snapshots retain live rechecks.
func (s *ThreadService) WithSourceReadSnapshot(ctx context.Context, ids []string, read func(context.Context) error) error {
	if s == nil || read == nil {
		return errors.New("source read callback unavailable")
	}
	thread, ok := s.repo.(interface {
		WithSourceThreadReadSnapshot(context.Context, []string, func(context.Context) error) error
	})
	if !ok {
		return read(ctx)
	}
	load := func(c context.Context) error { return thread.WithSourceThreadReadSnapshot(c, ids, read) }
	// Same order as ordinary Knowledge source reads: board/parent, then thread.
	if categories, ok := s.categoryRepo.(interface {
		WithSourceCategoryReadSnapshot(context.Context, func(context.Context) error) error
	}); ok {
		return categories.WithSourceCategoryReadSnapshot(ctx, load)
	}
	return load(ctx)
}
