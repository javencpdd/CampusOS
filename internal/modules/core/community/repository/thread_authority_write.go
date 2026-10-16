package repository

import (
	"context"
	"errors"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/jackc/pgx/v5"
)

// FactsGuardedThreadRepository is a command-local authority seam. Locking
// requires the command transaction; update guards its nonreused facts version.
// Display counters may change independently and are preserved by this write.
type FactsGuardedThreadRepository interface {
	LockThreadForWrite(context.Context, string) (*domain.Thread, error)
	UpdateIfFactsVersion(context.Context, *domain.Thread, int64) error
}

var ErrThreadWriteTransactionRequired = errors.New("thread authority write requires transaction")

func (r *PgThreadRepository) LockThreadForWrite(ctx context.Context, id string) (*domain.Thread, error) {
	if _, ok := transaction.FromContext(ctx); !ok {
		return nil, ErrThreadWriteTransactionRequired
	}
	thread, err := scanThread(r.db(ctx).QueryRow(ctx, selectThreadSQL("id=$1")+" FOR UPDATE", id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrThreadNotFound
	}
	return thread, err
}
func (r *PgThreadRepository) UpdateIfFactsVersion(ctx context.Context, thread *domain.Thread, expected int64) error {
	if _, ok := transaction.FromContext(ctx); !ok {
		return ErrThreadWriteTransactionRequired
	}
	if thread == nil || expected < 1 {
		return ErrThreadRevisionConflict
	}
	revision := thread.CurrentRevision - 1
	return r.update(ctx, thread, &revision, &expected)
}
func (r *MemoryThreadRepository) LockThreadForWrite(ctx context.Context, id string) (*domain.Thread, error) {
	if !transaction.Active(ctx) {
		return nil, ErrThreadWriteTransactionRequired
	}
	return r.GetByID(ctx, id)
}
func (r *MemoryThreadRepository) UpdateIfFactsVersion(ctx context.Context, thread *domain.Thread, expected int64) error {
	if !transaction.Active(ctx) {
		return ErrThreadWriteTransactionRequired
	}
	if thread == nil || expected < 1 {
		return ErrThreadRevisionConflict
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	current, ok := r.threads[thread.ID]
	if !ok {
		return ErrThreadNotFound
	}
	if current.FactsVersion != expected || current.CurrentRevision != thread.CurrentRevision-1 {
		return ErrThreadRevisionConflict
	}
	version, err := r.nextFactsVersionLocked()
	if err != nil {
		return err
	}
	thread.NormalizeContentState()
	thread.NormalizeModerationActor()
	thread.FactsVersion = version
	thread.ViewCount, thread.ReplyCount, thread.LikeCount = current.ViewCount, current.ReplyCount, current.LikeCount
	r.threads[thread.ID] = cloneThread(thread)
	return nil
}
