package repository

import (
	"context"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
)

type ModerationThreadSnapshotReader interface {
	ReadModerationThreadSnapshot(context.Context, string) (*domain.Thread, error)
}

func (r *MemoryThreadRepository) ReadModerationThreadSnapshot(ctx context.Context, id string) (*domain.Thread, error) {
	return r.GetByID(ctx, id)
}

func (r *PgThreadRepository) ReadModerationThreadSnapshot(ctx context.Context, id string) (*domain.Thread, error) {
	if err := r.requirePurgeFacts(ctx); err != nil {
		return nil, err
	}
	row, err := r.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := r.requirePurgeFacts(ctx); err != nil {
		return nil, err
	}
	return row, nil
}
