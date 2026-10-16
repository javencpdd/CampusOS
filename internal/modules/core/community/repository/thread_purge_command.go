package repository

import (
	"context"
	"errors"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/jackc/pgx/v5"
)

// Tombstone facts are command-only; normal reads and pages exclude deleted_at.
type ThreadPurgeCommandRepository interface {
	LockThreadGovernanceSnapshot(context.Context, string) (*domain.Thread, error)
	PurgeThreadIfFacts(context.Context, string, int64, int) (*domain.Thread, error)
}

var ErrThreadPurgeFactsUnavailable = errors.New("thread purge facts unavailable")

func (r *PgThreadRepository) requirePurgeFacts(ctx context.Context) error {
	var managed bool
	if err := r.db(ctx).QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_trigger WHERE tgrelid='public.threads'::regclass AND tgname='trg_threads_facts_version' AND NOT tgisinternal AND tgenabled IN('O','A') AND tgtype=23 AND tgfoid=to_regprocedure('public.advance_thread_facts_version()')) AND EXISTS(SELECT 1 FROM pg_sequence WHERE seqrelid=to_regclass('public.thread_facts_version_seq') AND NOT seqcycle AND seqtypid='bigint'::regtype AND seqincrement=1)`).Scan(&managed); err != nil || !managed {
		return ErrThreadPurgeFactsUnavailable
	}
	return nil
}

func (r *PgThreadRepository) LockThreadGovernanceSnapshot(ctx context.Context, id string) (*domain.Thread, error) {
	if _, ok := transaction.FromContext(ctx); !ok {
		return nil, ErrThreadWriteTransactionRequired
	}
	row, err := scanThread(r.db(ctx).QueryRow(ctx, `SELECT `+threadSelectColumns+` FROM threads WHERE id=$1 FOR UPDATE`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrThreadNotFound
	}
	if err == nil {
		if guardErr := r.requirePurgeFacts(ctx); guardErr != nil {
			return nil, guardErr
		}
	}
	return row, err
}
func (r *PgThreadRepository) PurgeThreadIfFacts(ctx context.Context, id string, version int64, revision int) (*domain.Thread, error) {
	if _, ok := transaction.FromContext(ctx); !ok {
		return nil, ErrThreadWriteTransactionRequired
	}
	if version < 1 || revision < 0 {
		return nil, ErrThreadRevisionConflict
	}
	current, err := r.LockThreadGovernanceSnapshot(ctx, id)
	if err != nil {
		return nil, err
	}
	if current.FactsVersion != version || current.CurrentRevision != revision || current.DeletedAt != nil || current.DeletionStatus != domain.DeletionStatusTrashed {
		return nil, ErrThreadRevisionConflict
	}
	row, err := scanThread(r.db(ctx).QueryRow(ctx, `UPDATE threads SET deletion_status='purged',status='archived',deleted_at=NOW(),updated_at=NOW(),current_revision=current_revision+1 WHERE id=$1 AND facts_version=$2 AND current_revision=$3 AND deleted_at IS NULL AND deletion_status='trashed' RETURNING `+threadSelectColumns, id, version, revision))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrThreadRevisionConflict
	}
	return row, err
}
func (r *MemoryThreadRepository) LockThreadGovernanceSnapshot(ctx context.Context, id string) (*domain.Thread, error) {
	if !transaction.Active(ctx) {
		return nil, ErrThreadWriteTransactionRequired
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	row, ok := r.threads[id]
	if !ok {
		return nil, ErrThreadNotFound
	}
	return cloneThread(row), nil
}
func (r *MemoryThreadRepository) PurgeThreadIfFacts(ctx context.Context, id string, version int64, revision int) (*domain.Thread, error) {
	if !transaction.Active(ctx) {
		return nil, ErrThreadWriteTransactionRequired
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	row, ok := r.threads[id]
	if !ok || row.FactsVersion != version || row.CurrentRevision != revision || version < 1 || revision < 0 || row.DeletedAt != nil || row.DeletionStatus != domain.DeletionStatusTrashed {
		return nil, ErrThreadRevisionConflict
	}
	next, err := r.nextFactsVersionLocked()
	if err != nil {
		return nil, err
	}
	current := cloneThread(row)
	at := time.Now().UTC()
	current.DeletedAt = &at
	current.DeletionStatus = domain.DeletionStatusPurged
	current.CurrentRevision++
	current.UpdatedAt = at
	current.FactsVersion = next
	current.SyncLegacyStatus()
	r.threads[id] = current
	return cloneThread(current), nil
}
