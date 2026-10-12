package storage

import (
	"context"
	"errors"

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
	var record QuotaRecord
	err := r.pool.QueryRow(ctx, `SELECT user_id::text,quota_bytes,COALESCE(updated_by::text,''),created_at,updated_at FROM user_storage_quotas WHERE user_id=$1`, owner).
		Scan(&record.UserID, &record.QuotaBytes, &record.UpdatedBy, &record.CreatedAt, &record.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return QuotaRecord{}, false, nil
	}
	return record, err == nil, err
}
