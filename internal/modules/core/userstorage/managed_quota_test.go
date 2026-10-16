package storage

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"reflect"
	"testing"
	"time"
)

type quotaRequiredFailure struct {
	reliability.Store
	mode string
}

func (s quotaRequiredFailure) RecordCommandAudit(ctx context.Context, a reliability.CommandAudit) error {
	if s.mode == "audit" {
		return errors.New("required quota audit rejected")
	}
	return s.Store.RecordCommandAudit(ctx, a)
}
func (s quotaRequiredFailure) Enqueue(ctx context.Context, event *reliability.Event) (*reliability.Event, error) {
	if s.mode == "outbox" {
		return nil, errors.New("required quota event rejected")
	}
	return s.Store.Enqueue(ctx, event)
}
func TestSpaceQuotaAdminManagedAdapterTransactionExactCASRollbackAndCommittedCache(t *testing.T) {
	for _, mode := range []string{"current", "audit", "outbox"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			repo := NewMemoryQuotaRepository()
			old, err := repo.Upsert(ctx, QuotaRecord{UserID: "1001", QuotaBytes: 1048576, UpdatedBy: "1002"})
			if err != nil {
				t.Fatal(err)
			}
			adapter, err := NewLocalAdapterWithQuotaRepository(t.TempDir(), DefaultQuotaBytes, repo)
			if err != nil {
				t.Fatal(err)
			}
			events := reliability.NewMemoryStore()
			command := reliability.NewService(transaction.NewMemory(events), quotaRequiredFailure{Store: events, mode: mode})
			adapter.SetQuotaReliability(command)
			before, eventBefore := repo.Snapshot(), events.Snapshot()
			var receipt QuotaCommandReceipt
			if _, _, err := adapter.CurrentQuotaForAdminCommand(ctx, "1001"); err == nil {
				t.Fatal("read without transaction accepted")
			}
			if _, err := adapter.WriteAdminQuotaForCommand(ctx, "1001", old.FactsVersion, 2097152, "adm.quota", time.Now()); err == nil {
				t.Fatal("write without transaction accepted")
			}
			err = command.Execute(ctx, reliability.Command{Code: "test.quota", ActorID: "adm.quota", ActorType: "admin", ResourceType: "storage_quota", ResourceID: "1001", EventFactory: func() (reliability.Event, error) {
				return reliability.NewEvent("test.quota.updated", "storage_quota", "1001", map[string]string{"owner_id": "1001"})
			}}, func(tx context.Context) error {
				current, exists, err := adapter.CurrentQuotaForAdminCommand(tx, "1001")
				if err != nil || !exists || !reflect.DeepEqual(current, old) {
					t.Fatal(current, exists, err)
				}
				if _, err := adapter.WriteAdminQuotaForCommand(tx, "1001", old.FactsVersion+100, 2097152, "adm.quota", time.Now()); !errors.Is(err, ErrQuotaFactsChanged) {
					t.Fatal("stale CAS accepted", err)
				}
				receipt, err = adapter.WriteAdminQuotaForCommand(tx, "1001", old.FactsVersion, 2097152, "adm.quota", time.Now())
				if err != nil {
					return err
				}
				r := receipt.Record()
				if r.FactsVersion <= old.FactsVersion || r.UpdatedBy != "" || r.UpdatedActorKind != "admin" || r.UpdatedActorID != "adm.quota" {
					t.Fatal(r)
				}
				if adapter.QuotaBytes("1001") != old.QuotaBytes {
					t.Fatal("cache changed before commit")
				}
				if adapter.PublishCommittedQuota(tx, receipt) == nil {
					t.Fatal("uncommitted cache publish accepted")
				}
				return nil
			})
			if mode != "current" {
				if err == nil || !reflect.DeepEqual(before, repo.Snapshot()) || !reflect.DeepEqual(eventBefore, events.Snapshot()) || adapter.QuotaBytes("1001") != old.QuotaBytes {
					t.Fatal("required failure retained quota or cache", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := adapter.PublishCommittedQuota(ctx, receipt); err != nil || adapter.QuotaBytes("1001") != 2097152 {
				t.Fatal(err)
			}
			data, err := json.Marshal(receipt)
			if err != nil || string(data) != "{}" {
				t.Fatal("receipt serialized", string(data), err)
			}
			other, err := NewLocalAdapter(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			if other.PublishCommittedQuota(ctx, receipt) == nil || adapter.PublishCommittedQuota(ctx, QuotaCommandReceipt{}) == nil {
				t.Fatal("forged receipt accepted")
			}
			var newer QuotaCommandReceipt
			if err := transaction.NewMemory(repo).Within(ctx, func(tx context.Context) error {
				var err error
				newer, err = adapter.WriteAdminQuotaForCommand(tx, "1001", receipt.Record().FactsVersion, 4194304, "adm.quota.next", time.Now())
				return err
			}); err != nil {
				t.Fatal(err)
			}
			if err := adapter.PublishCommittedQuota(ctx, newer); err != nil {
				t.Fatal(err)
			}
			if err := adapter.PublishCommittedQuota(ctx, receipt); err != nil || adapter.QuotaBytes("1001") != 4194304 {
				t.Fatal("late older receipt rewound cache", err)
			}
		})
	}
}
