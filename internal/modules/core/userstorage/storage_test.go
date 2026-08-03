package storage

import (
	"context"
	"errors"
	"testing"
)

func TestLocalAdapterConfinesUserPaths(t *testing.T) {
	adapter, err := NewLocalAdapter(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.EnsureLayout("user-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.Path("user-1", ImageDir, "avatar.png"); err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.Path("user-1", ".."); err == nil {
		t.Fatal("expected unsafe path rejection")
	}
}

func TestLocalAdapterPersistsPerUserQuotaOverrides(t *testing.T) {
	repository := NewMemoryQuotaRepository()
	root := t.TempDir()
	adapter, err := NewLocalAdapterWithQuotaRepository(root, DefaultQuotaBytes, repository)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.SetQuota(context.Background(), "1001", 75*1024*1024, "9001"); err != nil {
		t.Fatal(err)
	}
	if got := adapter.QuotaBytes("1001"); got != 75*1024*1024 {
		t.Fatalf("override quota=%d", got)
	}
	if got := adapter.QuotaBytes("1002"); got != DefaultQuotaBytes {
		t.Fatalf("default quota=%d", got)
	}
	reloaded, err := NewLocalAdapterWithQuotaRepository(root, DefaultQuotaBytes, repository)
	if err != nil {
		t.Fatal(err)
	}
	if got := reloaded.QuotaBytes("1001"); got != 75*1024*1024 {
		t.Fatalf("persisted override quota=%d", got)
	}
}

func TestLocalAdapterEnforcesQuota(t *testing.T) {
	adapter, err := NewLocalAdapterWithQuota(t.TempDir(), 4)
	if err != nil {
		t.Fatal(err)
	}
	if err := adapter.CheckQuota("user-1", 4); err != nil {
		t.Fatal(err)
	}
	if err := adapter.CheckQuota("user-1", 5); !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("expected quota error, got %v", err)
	}
}
