package plugin

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestPluginV5IdentityMemoryContract(t *testing.T) {
	store := NewMemoryPluginV5IdentityStore()
	id := PluginV5PackageIdentity{"campusos", "demo", "1.0.0", "sha256:" + strings.Repeat("a", 64)}
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		if err := store.RecordPluginV5Identity(ctx, id); err != nil {
			t.Fatal(err)
		}
	}
	other := id
	other.Digest = "sha256:" + strings.Repeat("b", 64)
	if err := store.RecordPluginV5Identity(ctx, other); !errors.Is(err, ErrPluginV5PackageConflict) {
		t.Fatal("identity overwritten")
	}
	for _, change := range []func(*PluginV5PackageIdentity){
		func(i *PluginV5PackageIdentity) { i.Publisher = "another" },
		func(i *PluginV5PackageIdentity) { i.Key = "another" },
		func(i *PluginV5PackageIdentity) { i.Version = "1.0.1" },
	} {
		i := other
		change(&i)
		if err := store.RecordPluginV5Identity(ctx, i); err != nil {
			t.Fatal("coordinate isolation failed", err)
		}
	}
	for _, bad := range []PluginV5PackageIdentity{
		{"x", "demo", "1.0.0", id.Digest},
		{"campusos", "../demo", "1.0.0", id.Digest},
		{"campusos", "demo", "01.0.0", id.Digest},
		{"campusos", "demo", "1.0.0", strings.Repeat("a", 64)},
	} {
		if err := store.RecordPluginV5Identity(ctx, bad); !errors.Is(err, ErrPluginV5PackageInvalid) {
			t.Fatal("invalid identity accepted")
		}
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if err := store.RecordPluginV5Identity(canceled, id); !errors.Is(err, ErrPluginV5IdentityUnavailable) {
		t.Fatal("canceled operation accepted")
	}
	for _, unavailable := range []PluginV5IdentityStore{(*MemoryPluginV5IdentityStore)(nil), &MemoryPluginV5IdentityStore{}, (*PgPluginV5IdentityStore)(nil), NewPgPluginV5IdentityStore(nil)} {
		if err := unavailable.RecordPluginV5Identity(ctx, id); err != ErrPluginV5IdentityUnavailable {
			t.Fatal("unavailable store did not fail closed", err)
		}
	}
}

type v5IdentityRejectingStore struct{ calls int }

func (s *v5IdentityRejectingStore) RecordPluginV5Identity(context.Context, PluginV5PackageIdentity) error {
	s.calls++
	return ErrPluginV5IdentityUnavailable
}

func TestPluginV5IdentityCatalogFailureAndTrust(t *testing.T) {
	raw, _, err := BuildPluginV5Package(testV5PackageSource(t, 0))
	if err != nil {
		t.Fatal(err)
	}
	priv, trust := testV5Trust(t)
	envelope, err := SignPluginV5Package(raw, "test-key", priv)
	if err != nil {
		t.Fatal(err)
	}
	store := &v5IdentityRejectingStore{}
	c, err := NewPluginV5PackageCatalogWithIdentityStore(trust, store)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if p, err := c.VerifyAndRecord(raw, envelope); p != nil || err != ErrPluginV5IdentityUnavailable {
			t.Fatal("failed persistence returned an accepted package")
		}
	}
	if store.calls != 2 {
		t.Fatal("verification cached after a persistence failure")
	}
	if err := c.SetTrust(bytes.Replace(trust, []byte(`"active"`), []byte(`"revoked"`), 1)); err != nil {
		t.Fatal(err)
	}
	if p, err := c.VerifyAndRecord(raw, envelope); p != nil || err != ErrPluginV5PackageSignature || store.calls != 2 {
		t.Fatal("revoked signature reached persistence")
	}
	if _, err := NewPluginV5PackageCatalogWithIdentityStore(trust, nil); err != ErrPluginV5IdentityUnavailable {
		t.Fatal("missing store silently became memory")
	}
}

func TestPluginV5IdentitySharedCatalogConcurrency(t *testing.T) {
	dir := testV5PackageSource(t, 0)
	first, _, err := BuildPluginV5Package(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("different bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	second, _, err := BuildPluginV5Package(dir)
	if err != nil {
		t.Fatal(err)
	}
	priv, trust := testV5Trust(t)
	store := NewMemoryPluginV5IdentityStore()
	var wg sync.WaitGroup
	var mu sync.Mutex
	accepted := map[string]int{}
	for i := 0; i < 16; i++ {
		raw := first
		if i%2 == 1 {
			raw = second
		}
		envelope, err := SignPluginV5Package(raw, "test-key", priv)
		if err != nil {
			t.Fatal(err)
		}
		c, err := NewPluginV5PackageCatalogWithIdentityStore(trust, store)
		if err != nil {
			t.Fatal(err)
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			p, err := c.VerifyAndRecord(raw, envelope)
			if err != nil && err != ErrPluginV5PackageConflict {
				t.Error(err)
			}
			if p != nil {
				mu.Lock()
				accepted[p.Digest]++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if len(accepted) != 1 {
		t.Fatal("separate catalog locks allowed two digests")
	}
}
