package storage

import (
	"context"
	"errors"

	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/jackc/pgx/v5"
)

// CurrentQuotaReader is a request-local persisted owner snapshot. The local
// provider's startup cache is not a current read authority for HTTP metadata.
type CurrentQuotaReader interface {
	CurrentQuota(context.Context, string) (QuotaRecord, bool, error)
}

type currentQuotaRepository interface {
	GetOwnerQuota(context.Context, string) (QuotaRecord, bool, error)
}

func (a *LocalAdapter) CurrentQuota(ctx context.Context, owner string) (QuotaRecord, bool, error) {
	if !SafeSegment(owner) {
		return QuotaRecord{}, false, ErrUnsafePath
	}
	r, ok := a.quotaRepository.(currentQuotaRepository)
	if !ok || NilImageAdapter(r) {
		return QuotaRecord{}, false, errors.New("current quota repository unavailable")
	}
	return r.GetOwnerQuota(ctx, owner)
}

func (r *MemoryQuotaRepository) GetOwnerQuota(ctx context.Context, owner string) (QuotaRecord, bool, error) {
	if err := ctx.Err(); err != nil {
		return QuotaRecord{}, false, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	record, exists := r.items[owner]
	return record, exists, nil
}

func (r *PgQuotaRepository) GetOwnerQuota(ctx context.Context, owner string) (QuotaRecord, bool, error) {
	if err := r.CheckQuotaFacts(ctx); err != nil {
		return QuotaRecord{}, false, err
	}
	var record QuotaRecord
	err := transaction.ExecutorFor(ctx, r.pool).QueryRow(ctx, `SELECT `+quotaRecordColumns+` FROM user_storage_quotas WHERE user_id=$1`, owner).Scan(quotaRecordTargets(&record)...)
	if errors.Is(err, pgx.ErrNoRows) {
		return QuotaRecord{}, false, nil
	}
	return record, err == nil, err
}

type objectQuotaRepository interface {
	GetOwnerQuotaForObjectCommand(context.Context, string) (QuotaRecord, bool, func(), error)
}

func (a *LocalAdapter) CurrentQuotaForObjectCommand(ctx context.Context, owner string) (QuotaRecord, bool, func(), error) {
	if !SafeSegment(owner) {
		return QuotaRecord{}, false, func() {}, ErrUnsafePath
	}
	r, ok := a.quotaRepository.(objectQuotaRepository)
	if !ok || NilImageAdapter(r) {
		return QuotaRecord{}, false, func() {}, ErrObjectUnavailable
	}
	return r.GetOwnerQuotaForObjectCommand(ctx, owner)
}
func (r *MemoryQuotaRepository) GetOwnerQuotaForObjectCommand(ctx context.Context, owner string) (QuotaRecord, bool, func(), error) {
	if err := ctx.Err(); err != nil {
		return QuotaRecord{}, false, func() {}, err
	}
	r.mu.RLock()
	record, exists := r.items[owner]
	return record, exists, r.mu.RUnlock, nil
}
func (r *PgQuotaRepository) GetOwnerQuotaForObjectCommand(ctx context.Context, owner string) (QuotaRecord, bool, func(), error) {
	tx, ok := transaction.FromContext(ctx)
	if !ok {
		return QuotaRecord{}, false, func() {}, ErrObjectUnavailable
	}
	if err := r.CheckQuotaFacts(ctx); err != nil {
		return QuotaRecord{}, false, func() {}, err
	}
	var record QuotaRecord
	err := tx.QueryRow(ctx, "SELECT "+quotaRecordColumns+" FROM user_storage_quotas WHERE user_id=$1 FOR SHARE", owner).Scan(quotaRecordTargets(&record)...)
	if errors.Is(err, pgx.ErrNoRows) {
		return QuotaRecord{}, false, func() {}, nil
	}
	return record, err == nil, func() {}, err
}
