package storage

import (
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
