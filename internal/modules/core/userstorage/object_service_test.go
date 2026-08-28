package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
)

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
