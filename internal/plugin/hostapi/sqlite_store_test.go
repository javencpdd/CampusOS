package hostapi

import "testing"

func TestSQLiteKVStorePersistsValues(t *testing.T) {
	root := t.TempDir()
	store, err := NewSQLiteKVStore(root)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}

	if err := store.Set(t.Context(), "hello-wasm", "greeting", "world"); err != nil {
		t.Fatalf("set value: %v", err)
	}

	reopened, err := NewSQLiteKVStore(root)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	value, found, err := reopened.Get(t.Context(), "hello-wasm", "greeting")
	if err != nil {
		t.Fatalf("get value: %v", err)
	}
	if !found || value != "world" {
		t.Fatalf("expected persisted value, got found=%v value=%q", found, value)
	}
}

func TestSQLiteKVStoreDelete(t *testing.T) {
	store, err := NewSQLiteKVStore(t.TempDir())
	if err != nil {
		t.Fatalf("create store: %v", err)
	}

	if err := store.Set(t.Context(), "hello-wasm", "greeting", "world"); err != nil {
		t.Fatalf("set value: %v", err)
	}
	if err := store.Delete(t.Context(), "hello-wasm", "greeting"); err != nil {
		t.Fatalf("delete value: %v", err)
	}

	_, found, err := store.Get(t.Context(), "hello-wasm", "greeting")
	if err != nil {
		t.Fatalf("get value: %v", err)
	}
	if found {
		t.Fatalf("expected value to be deleted")
	}
}

func TestSQLiteKVStoreRejectsUnsafePluginName(t *testing.T) {
	store, err := NewSQLiteKVStore(t.TempDir())
	if err != nil {
		t.Fatalf("create store: %v", err)
	}

	if err := store.Set(t.Context(), "../other", "key", "value"); err == nil {
		t.Fatalf("expected unsafe plugin name to be rejected")
	}
	if err := store.Set(t.Context(), "nested/plugin", "key", "value"); err == nil {
		t.Fatalf("expected plugin path separator to be rejected")
	}
}
