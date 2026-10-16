package storage

import (
	"context"
	"errors"
	"github.com/campusos/CampusOS/internal/platform/transaction"
)

func (r *PgQuotaRepository) CheckQuotaFacts(ctx context.Context) error {
	if r == nil || r.pool == nil {
		return ErrQuotaFactsUnavailable
	}
	var valid bool
	err := transaction.ExecutorFor(ctx, r.pool).QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_sequence WHERE seqrelid=to_regclass('public.storage_quota_facts_version_seq') AND NOT seqcycle AND seqtypid='bigint'::regtype AND seqincrement=1)
 AND EXISTS(SELECT 1 FROM pg_trigger WHERE tgrelid=to_regclass('public.user_storage_quotas') AND tgname='trg_user_storage_quotas_facts_version' AND NOT tgisinternal AND tgenabled IN('O','A') AND tgtype=23 AND tgfoid=to_regprocedure('public.advance_storage_quota_facts_version()'))
 AND EXISTS(SELECT 1 FROM pg_attribute WHERE attrelid=to_regclass('public.user_storage_quotas') AND attname='facts_version' AND attnotnull AND atttypid='bigint'::regtype AND NOT attisdropped)`).Scan(&valid)
	if err != nil || !valid {
		return errors.Join(ErrQuotaFactsUnavailable, err)
	}
	return nil
}
