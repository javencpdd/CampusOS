package storage

import (
	"context"
	"errors"
)

const objectQuotaAdmissionSQL = "SELECT pg_advisory_xact_lock(hashtextextended('storage-quota:' || $1::text,0))"

type objectQuotaCommandKey struct{}
type objectQuotaResolver func(context.Context, string) (int64, func(), error)
type objectQuotaReader interface {
	CurrentQuotaForObjectCommand(context.Context, string) (QuotaRecord, bool, func(), error)
}

func currentObjectQuota(ctx context.Context, owner string, fallback int64) (int64, func(), error) {
	if resolve, ok := ctx.Value(objectQuotaCommandKey{}).(objectQuotaResolver); ok {
		return resolve(ctx, owner)
	}
	return fallback, func() {}, nil
}
func (s *ObjectService) resolveCurrentObjectQuota(ctx context.Context, owner string) (int64, func(), error) {
	reader, ok := s.quota.(objectQuotaReader)
	if !ok {
		if _, persisted := s.quota.(CurrentQuotaReader); persisted {
			return 0, func() {}, ErrObjectUnavailable
		}
		return s.quota.QuotaBytes(owner), func() {}, nil
	}
	record, exists, release, err := reader.CurrentQuotaForObjectCommand(ctx, owner)
	if release == nil {
		release = func() {}
	}
	if err != nil {
		return 0, release, errors.Join(ErrObjectUnavailable, err)
	}
	if exists {
		if record.UserID != owner || record.QuotaBytes <= 0 {
			return 0, release, ErrObjectUnavailable
		}
		return record.QuotaBytes, release, nil
	}
	defaults, ok := s.quota.(interface{ DefaultQuotaBytes() int64 })
	if !ok || defaults.DefaultQuotaBytes() <= 0 {
		return 0, release, ErrObjectUnavailable
	}
	return defaults.DefaultQuotaBytes(), release, nil
}
