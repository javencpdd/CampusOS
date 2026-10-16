package storage

import (
	"context"
	"errors"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"math"
	"reflect"
	"testing"
)

func TestSpaceQuotaAdminMemoryFullFactsABARollbackHighwaterAndExhaustion(t *testing.T) {
	ctx := context.Background()
	repo := NewMemoryQuotaRepository()
	first, err := repo.Upsert(ctx, QuotaRecord{UserID: "1001", QuotaBytes: 1048576, UpdatedBy: "1002"})
	if err != nil || first.FactsVersion < 1 || first.UpdatedActorKind != "user" || first.UpdatedActorID != "1002" {
		t.Fatal(first, err)
	}
	second, err := repo.Upsert(ctx, QuotaRecord{UserID: "1001", QuotaBytes: 2097152, UpdatedActorKind: "admin", UpdatedActorID: "adm.quota.opaque"})
	if err != nil || second.UpdatedBy != "" || second.UpdatedActorKind != "admin" || second.UpdatedActorID != "adm.quota.opaque" || second.FactsVersion <= first.FactsVersion {
		t.Fatal(second, err)
	}
	third, err := repo.Upsert(ctx, QuotaRecord{UserID: "1001", QuotaBytes: first.QuotaBytes, UpdatedBy: first.UpdatedBy})
	if err != nil || third.FactsVersion <= second.FactsVersion {
		t.Fatal("ABA proof reused", third, err)
	}
	before := repo.Snapshot()
	var abandoned int64
	rollback := errors.New("force rollback")
	err = transaction.NewMemory(repo).Within(ctx, func(tx context.Context) error {
		item, err := repo.Upsert(tx, QuotaRecord{UserID: "1001", QuotaBytes: 4194304, UpdatedActorKind: "admin", UpdatedActorID: "adm.rollback"})
		if err != nil {
			return err
		}
		abandoned = item.FactsVersion
		return rollback
	})
	if !errors.Is(err, rollback) || !reflect.DeepEqual(before, repo.Snapshot()) {
		t.Fatal("quota rollback lost metadata or actor", err)
	}
	current, err := repo.Upsert(ctx, QuotaRecord{UserID: "1001", QuotaBytes: 8388608})
	if err != nil || current.FactsVersion <= abandoned {
		t.Fatal("allocation rewound", current, abandoned, err)
	}
	before = repo.Snapshot()
	repo.factsCounter = math.MaxInt64
	if _, err := repo.Upsert(ctx, QuotaRecord{UserID: "1002", QuotaBytes: 1048576}); !errors.Is(err, ErrQuotaFactsUnavailable) || !reflect.DeepEqual(before, repo.Snapshot()) {
		t.Fatal("exhausted allocation retained mutation", err)
	}
}
