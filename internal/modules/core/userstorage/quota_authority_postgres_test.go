package storage

import (
	"context"
	"errors"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/jackc/pgx/v5/pgconn"
	"strings"
	"testing"
	"time"
)

func TestSpaceQuotaAdminPostgreSQLFullFactsRealABAActorLocksRollbackAndGuards(t *testing.T) {
	ctx := context.Background()
	_, adapter, quotas, _, pool := objectQuotaPostgres(t, "2540201")
	before, exists, err := adapter.CurrentQuota(ctx, "2540201")
	if err != nil || !exists || before.FactsVersion < 1 {
		t.Fatal(before, exists, err)
	}
	var receipt QuotaCommandReceipt
	err = transaction.NewPostgreSQL(pool).Within(ctx, func(tx context.Context) error {
		current, exists, err := adapter.CurrentQuotaForAdminCommand(tx, "2540201")
		if err != nil || !exists || current.FactsVersion != before.FactsVersion {
			t.Fatal(current, exists, err)
		}
		_, err = pool.Exec(ctx, "SELECT user_id FROM user_storage_quotas WHERE user_id=$1 FOR UPDATE NOWAIT", "2540201")
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "55P03" {
			t.Fatal("actual quota row lock missing", err)
		}
		var acquired bool
		if err := pool.QueryRow(ctx, "SELECT pg_try_advisory_xact_lock(hashtextextended('storage-quota:'||$1::text,0))", "2540201").Scan(&acquired); err != nil || acquired {
			t.Fatal("actual owner quota admission missing", acquired, err)
		}
		db := transaction.ExecutorFor(tx, pool)
		var rawBefore, rawAfter string
		if err := db.QueryRow(tx, "SELECT (to_jsonb(q)-'facts_version')::text FROM user_storage_quotas q WHERE user_id=$1", "2540201").Scan(&rawBefore); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(tx, "UPDATE user_storage_quotas SET quota_bytes=quota_bytes+1 WHERE user_id=$1", "2540201"); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(tx, "UPDATE user_storage_quotas SET quota_bytes=quota_bytes-1 WHERE user_id=$1", "2540201"); err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRow(tx, "SELECT (to_jsonb(q)-'facts_version')::text FROM user_storage_quotas q WHERE user_id=$1", "2540201").Scan(&rawAfter); err != nil || rawBefore != rawAfter {
			t.Fatal("ABA did not restore original metadata", rawBefore, rawAfter, err)
		}
		after, exists, err := adapter.CurrentQuotaForAdminCommand(tx, "2540201")
		if err != nil || !exists || after.FactsVersion <= before.FactsVersion {
			t.Fatal("real ABA reused quota proof", after, err)
		}
		if _, err := adapter.WriteAdminQuotaForCommand(tx, "2540201", before.FactsVersion, 2097152, "adm.quota.pg", time.Now()); !errors.Is(err, ErrQuotaFactsChanged) {
			t.Fatal("stale quota CAS accepted", err)
		}
		receipt, err = adapter.WriteAdminQuotaForCommand(tx, "2540201", after.FactsVersion, 2097152, "adm.quota.pg", time.Now())
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := adapter.PublishCommittedQuota(ctx, receipt); err != nil {
		t.Fatal(err)
	}
	written, exists, err := adapter.CurrentQuota(ctx, "2540201")
	if err != nil || !exists || written != receipt.Record() || written.UpdatedBy != "" || written.UpdatedActorKind != "admin" || written.UpdatedActorID != "adm.quota.pg" {
		t.Fatal(written, receipt.Record(), exists, err)
	}
	var rawBefore, rawAfter string
	if err := pool.QueryRow(ctx, "SELECT to_jsonb(q)::text FROM user_storage_quotas q WHERE user_id=$1", "2540201").Scan(&rawBefore); err != nil {
		t.Fatal(err)
	}
	abandoned := int64(0)
	rollback := errors.New("quota SQL rollback")
	err = transaction.NewPostgreSQL(pool).Within(ctx, func(tx context.Context) error {
		r, err := adapter.WriteAdminQuotaForCommand(tx, "2540201", written.FactsVersion, 4194304, "adm.quota.rolledback", time.Now())
		if err != nil {
			return err
		}
		abandoned = r.Record().FactsVersion
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, "SELECT to_jsonb(q)::text FROM user_storage_quotas q WHERE user_id=$1", "2540201").Scan(&rawAfter); err != nil || rawAfter != rawBefore {
		t.Fatal("SQL rollback lost quota or actor", err)
	}
	next, err := quotas.Upsert(ctx, QuotaRecord{UserID: "2540201", QuotaBytes: 8388608, UpdatedActorKind: "admin", UpdatedActorID: "adm.quota.next"})
	if err != nil || next.FactsVersion <= abandoned {
		t.Fatal("SQL allocation rewound", next, abandoned, err)
	}
	for _, guard := range []struct{ disable, enable string }{
		{"ALTER TABLE user_storage_quotas DISABLE TRIGGER trg_user_storage_quotas_facts_version", "ALTER TABLE user_storage_quotas ENABLE TRIGGER trg_user_storage_quotas_facts_version"},
		{"ALTER SEQUENCE storage_quota_facts_version_seq CYCLE", "ALTER SEQUENCE storage_quota_facts_version_seq NO CYCLE"},
		{"ALTER TABLE user_storage_quotas ALTER COLUMN facts_version DROP NOT NULL", "ALTER TABLE user_storage_quotas ALTER COLUMN facts_version SET NOT NULL"},
	} {
		t.Run(strings.Fields(guard.disable)[1]+"/"+strings.Fields(guard.disable)[3], func(t *testing.T) {
			if _, err := pool.Exec(ctx, guard.disable); err != nil {
				t.Fatal(err)
			}
			_, _, readErr := adapter.CurrentQuota(ctx, "2540201")
			_, writeErr := quotas.Upsert(ctx, QuotaRecord{UserID: "2540201", QuotaBytes: 16 * 1048576})
			if _, err := pool.Exec(ctx, guard.enable); err != nil {
				t.Fatal(err)
			}
			if !errors.Is(readErr, ErrQuotaFactsUnavailable) || !errors.Is(writeErr, ErrQuotaFactsUnavailable) {
				t.Fatal("guard drift accepted", readErr, writeErr)
			}
		})
	}
}
