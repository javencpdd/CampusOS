package storage

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
)

type currentQuotaChangeReader struct {
	reader io.Reader
	before func()
}

func (r *currentQuotaChangeReader) Read(p []byte) (int, error) {
	if r.before != nil {
		f := r.before
		r.before = nil
		f()
	}
	return r.reader.Read(p)
}

func TestObjectCurrentQuotaChangesDuringIOCloseAndRelease(t *testing.T) {
	for _, mode := range []string{"lower", "missing_override", "unavailable"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			quotas := NewMemoryQuotaRepository()
			initial := int64(100)
			if mode == "missing_override" {
				initial = 10
			}
			if _, err := quotas.Upsert(ctx, QuotaRecord{UserID: "19002", QuotaBytes: initial}); err != nil {
				t.Fatal(err)
			}
			adapter, err := NewLocalAdapterWithQuotaRepository(t.TempDir(), 100, quotas)
			if err != nil {
				t.Fatal(err)
			}
			repo := NewMemoryObjectRepository()
			objects, err := NewObjectService(adapter, adapter, repo, 100)
			if err != nil {
				t.Fatal(err)
			}
			reader := &currentQuotaChangeReader{reader: bytes.NewReader(make([]byte, 20))}
			if mode == "lower" {
				reader.before = func() {
					if _, err := quotas.Upsert(ctx, QuotaRecord{UserID: "19002", QuotaBytes: 10}); err != nil {
						t.Fatal(err)
					}
				}
			}
			if mode == "missing_override" {
				quotas.mu.Lock()
				delete(quotas.items, "19002")
				quotas.mu.Unlock()
			}
			if mode == "unavailable" {
				adapter.quotaRepository = nil
			}
			object, err := objects.Put(ctx, "19002", PutRequest{Namespace: "richtext", Purpose: "article_attachment", OriginalName: "quota.txt", MimeType: "text/plain", SizeHint: 20, Reader: reader})
			if mode == "missing_override" {
				if err != nil || object.ID == "" {
					t.Fatal("configured default not used", object, err)
				}
				return
			}
			want := ErrObjectQuota
			if mode == "unavailable" {
				want = ErrObjectUnavailable
			}
			if !errors.Is(err, want) || object.ID != "" {
				t.Fatal(mode, object, err)
			}
			account, exists, err := repo.Account(ctx, "19002")
			if err != nil || !exists || account.UsedBytes != 0 || account.ReservedBytes != 0 {
				t.Fatal("failed I/O left capacity reserved", account, exists, err)
			}
			usage, err := adapter.Usage("19002")
			if err != nil || usage != 0 {
				t.Fatal("failed I/O left file bytes", usage, err)
			}
		})
	}
}

func TestObjectCurrentQuotaIgnoresStaleStartupCache(t *testing.T) {
	ctx := context.Background()
	quotas := NewMemoryQuotaRepository()
	if _, err := quotas.Upsert(ctx, QuotaRecord{UserID: "19001", QuotaBytes: 1024}); err != nil {
		t.Fatal(err)
	}
	adapter, err := NewLocalAdapterWithQuotaRepository(t.TempDir(), 1024, quotas)
	if err != nil {
		t.Fatal(err)
	}
	objects, err := NewObjectService(adapter, adapter, NewMemoryObjectRepository(), 1024)
	if err != nil {
		t.Fatal(err)
	}
	// A persisted update made by another process must affect this writer.
	if _, err := quotas.Upsert(ctx, QuotaRecord{UserID: "19001", QuotaBytes: 10}); err != nil {
		t.Fatal(err)
	}
	if adapter.QuotaBytes("19001") != 1024 {
		t.Fatal("fixture did not preserve stale startup cache")
	}
	if object, err := objects.Put(ctx, "19001", PutRequest{Namespace: "richtext", Purpose: "article_attachment", OriginalName: "quota.txt", MimeType: "text/plain", SizeHint: 20, Reader: bytes.NewReader(make([]byte, 20))}); !errors.Is(err, ErrObjectQuota) || object.ID != "" {
		t.Fatal("persisted current quota did not reject write", object.ID, err)
	}
}
