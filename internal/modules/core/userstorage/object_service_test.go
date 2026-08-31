package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/campusos/CampusOS/pkg/observability"
)

type countingStorage struct {
	Port
	usageCalls int
}

type commitFailRepository struct{ ObjectRepository }

func (commitFailRepository) Commit(context.Context, string, reservation, int64, string, string) (storedObject, error) {
	return storedObject{}, errors.New("injected metadata commit failure")
}

func (s *countingStorage) Usage(userID string) (int64, error) {
	s.usageCalls++
	return s.Port.Usage(userID)
}

func TestObjectServiceOwnerQuotaAndVersionedDelete(t *testing.T) {
	adapter, err := NewLocalAdapterWithQuota(t.TempDir(), 10)
	if err != nil {
		t.Fatalf("new adapter: %v", err)
	}
	svc, err := NewObjectService(adapter, adapter, NewMemoryObjectRepository(), 10)
	if err != nil {
		t.Fatalf("new object service: %v", err)
	}
	ctx := context.Background()
	first, err := svc.Put(ctx, "1001", PutRequest{Namespace: "documents", Purpose: "document.source", OriginalName: "a.txt", MimeType: "text/plain", SizeHint: 6, Reader: bytes.NewReader([]byte("abcdef"))})
	if err != nil {
		t.Fatalf("put first: %v", err)
	}
	if first.SizeBytes != 6 || first.Status != ObjectStatusReady || len(first.SHA256) != 64 {
		t.Fatalf("unexpected stored object: %#v", first)
	}
	if _, err := svc.Stat(ctx, "1002", first.ID); !errors.Is(err, ErrObjectNotFound) {
		t.Fatalf("cross-owner Stat must look absent, got %v", err)
	}
	if _, err := svc.Put(ctx, "1001", PutRequest{Namespace: "documents", Purpose: "document.source", OriginalName: "b.txt", MimeType: "text/plain", SizeHint: 5, Reader: bytes.NewReader([]byte("12345"))}); !errors.Is(err, ErrObjectQuota) {
		t.Fatalf("expected quota rejection, got %v", err)
	}
	if err := svc.Delete(ctx, "1001", first.ID, first.Version); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := svc.Stat(ctx, "1001", first.ID); !errors.Is(err, ErrObjectNotFound) {
		t.Fatalf("deleted object must not be readable, got %v", err)
	}
	if _, err := svc.Put(ctx, "1001", PutRequest{Namespace: "documents", Purpose: "document.source", OriginalName: "b.txt", MimeType: "text/plain", SizeHint: 5, Reader: bytes.NewReader([]byte("12345"))}); err != nil {
		t.Fatalf("quota should be released after delete: %v", err)
	}
}

func TestObjectServiceConcurrentReservationsStayWithinQuota(t *testing.T) {
	adapter, err := NewLocalAdapterWithQuota(t.TempDir(), 10)
	if err != nil {
		t.Fatalf("new adapter: %v", err)
	}
	svc, err := NewObjectService(adapter, adapter, NewMemoryObjectRepository(), 10)
	if err != nil {
		t.Fatalf("new object service: %v", err)
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	successes := 0
	for index := 0; index < 50; index++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			_, putErr := svc.Put(context.Background(), "1001", PutRequest{Namespace: "documents", Purpose: "document.source", OriginalName: fmt.Sprintf("%d.txt", index), MimeType: "text/plain", SizeHint: 1, Reader: bytes.NewReader([]byte("x"))})
			if putErr == nil {
				mu.Lock()
				successes++
				mu.Unlock()
				return
			}
			if !errors.Is(putErr, ErrObjectQuota) {
				t.Errorf("unexpected concurrent put error: %v", putErr)
			}
		}(index)
	}
	wg.Wait()
	if successes != 10 {
		t.Fatalf("expected exactly 10 accepted one-byte objects, got %d", successes)
	}
	page, err := svc.List(context.Background(), "1001", ObjectFilter{}, PageRequest{Limit: 100})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(page.Items) != 10 {
		t.Fatalf("expected 10 ready objects, got %d", len(page.Items))
	}
}

func TestObjectServiceMetricsAreAggregateAndBounded(t *testing.T) {
	adapter, err := NewLocalAdapterWithQuota(t.TempDir(), 10)
	if err != nil {
		t.Fatalf("new adapter: %v", err)
	}
	svc, err := NewObjectService(adapter, adapter, NewMemoryObjectRepository(), 10)
	if err != nil {
		t.Fatalf("new object service: %v", err)
	}
	collector := observability.NewCollector()
	svc.SetMeter(collector)
	if _, err := svc.Put(context.Background(), "1001", PutRequest{Namespace: "documents", Purpose: "source.text", OriginalName: "private-name.txt", MimeType: "text/plain", SizeHint: 1, Reader: bytes.NewReader([]byte("x"))}); err != nil {
		t.Fatalf("put: %v", err)
	}
	metrics := collector.PrometheusText()
	if !strings.Contains(metrics, `campusos_storage_objects{provider="local",status="ready"} 1`) {
		t.Fatalf("ready aggregate metric missing: %s", metrics)
	}
	if strings.Contains(metrics, "1001") || strings.Contains(metrics, "private-name") {
		t.Fatalf("storage metric must not expose owner or filename: %s", metrics)
	}
}

func TestObjectServiceUsesPersistentLedgerAfterInitialLegacyObservation(t *testing.T) {
	adapter, err := NewLocalAdapterWithQuota(t.TempDir(), 1024)
	if err != nil {
		t.Fatal(err)
	}
	storage := &countingStorage{Port: adapter}
	svc, err := NewObjectService(storage, adapter, NewMemoryObjectRepository(), 1024)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err = svc.Put(ctx, "1001", PutRequest{Namespace: "test", Purpose: "ledger", OriginalName: "first.txt", SizeHint: 3, Reader: bytes.NewBufferString("one")}); err != nil {
		t.Fatalf("first put: %v", err)
	}
	if storage.usageCalls != 1 {
		t.Fatalf("first handoff should observe legacy files once, calls=%d", storage.usageCalls)
	}
	usage, err := svc.Usage(ctx, "1001")
	if err != nil {
		t.Fatalf("ledger usage: %v", err)
	}
	if usage.UsedBytes != 3 || usage.RemainingBytes != 1021 {
		t.Fatalf("unexpected ledger usage: %#v", usage)
	}
	if _, err = svc.Put(ctx, "1001", PutRequest{Namespace: "test", Purpose: "ledger", OriginalName: "second.txt", SizeHint: 3, Reader: bytes.NewBufferString("two")}); err != nil {
		t.Fatalf("second put: %v", err)
	}
	if storage.usageCalls != 1 {
		t.Fatalf("normal object operations must not rescan the directory, calls=%d", storage.usageCalls)
	}
}

func TestObjectServiceCompatibilityLedgerTracksReplacementWithoutRescan(t *testing.T) {
	adapter, err := NewLocalAdapterWithQuota(t.TempDir(), 100)
	if err != nil {
		t.Fatal(err)
	}
	storage := &countingStorage{Port: adapter}
	svc, err := NewObjectService(storage, adapter, NewMemoryObjectRepository(), 100)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := svc.ReplaceCompatibility(ctx, "1001", 0, 20); err != nil {
		t.Fatalf("account compatibility bytes: %v", err)
	}
	if err := svc.ReplaceCompatibility(ctx, "1001", 20, 27); err != nil {
		t.Fatalf("replace compatibility bytes: %v", err)
	}
	usage, err := svc.Usage(ctx, "1001")
	if err != nil {
		t.Fatal(err)
	}
	if usage.UsedBytes != 27 || storage.usageCalls != 1 {
		t.Fatalf("expected durable 27-byte ledger and one observation, usage=%#v calls=%d", usage, storage.usageCalls)
	}
	if err := svc.ReplaceCompatibility(ctx, "1001", 27, 101); !errors.Is(err, ErrObjectQuota) {
		t.Fatalf("expected quota error, got %v", err)
	}
}

func TestObjectServiceCommitFailureCleansRenamedProviderFileAndReservation(t *testing.T) {
	adapter, err := NewLocalAdapterWithQuota(t.TempDir(), 100)
	if err != nil {
		t.Fatal(err)
	}
	svc, err := NewObjectService(adapter, adapter, commitFailRepository{NewMemoryObjectRepository()}, 100)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Put(context.Background(), "1001", PutRequest{Namespace: "test", Purpose: "failure", OriginalName: "x.txt", SizeHint: 3, Reader: bytes.NewBufferString("abc")}); err == nil {
		t.Fatal("injected commit failure should be returned")
	}
	objectDir, err := adapter.Path("1001", FileDir, "objects")
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(objectDir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("commit failure must not leave a ready provider file: %#v", entries)
	}
	usage, err := svc.Usage(context.Background(), "1001")
	if err != nil {
		t.Fatal(err)
	}
	if usage.UsedBytes != 0 {
		t.Fatalf("failed put must release its ledger bytes: %#v", usage)
	}
}

func TestObjectRepositoryListUsesNumericStableKeysetCursor(t *testing.T) {
	repository := NewMemoryObjectRepository()
	for _, id := range []string{"9", "10", "11"} {
		repository.objects[id] = storedObject{Object: Object{
			ID: id, OwnerID: "1001", Namespace: "documents", Purpose: "document.source", Status: ObjectStatusReady,
		}}
	}
	first, err := repository.ListOwned(context.Background(), "1001", ObjectFilter{}, PageRequest{Limit: 2})
	if err != nil {
		t.Fatalf("first page: %v", err)
	}
	if len(first.Items) != 2 || first.Items[0].ID != "11" || first.Items[1].ID != "10" || first.NextCursor != "10" {
		t.Fatalf("unexpected numeric first page: %#v", first)
	}
	second, err := repository.ListOwned(context.Background(), "1001", ObjectFilter{}, PageRequest{Limit: 2, Cursor: first.NextCursor})
	if err != nil {
		t.Fatalf("second page: %v", err)
	}
	if len(second.Items) != 1 || second.Items[0].ID != "9" || second.NextCursor != "" {
		t.Fatalf("unexpected numeric second page: %#v", second)
	}
	if _, err := repository.ListOwned(context.Background(), "1001", ObjectFilter{}, PageRequest{Cursor: "not-an-id"}); !errors.Is(err, ErrObjectPageCursor) {
		t.Fatalf("invalid cursor error = %v", err)
	}
}
