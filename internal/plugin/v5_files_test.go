package plugin

import (
	"bytes"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestV5ManagedFilesDurabilityAndBoundaries(t *testing.T) {
	root := t.TempDir()
	f, err := NewV5ManagedFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	raw, _, err := BuildPluginV5Package(testV5PackageSource(t, 2))
	if err != nil {
		t.Fatal(err)
	}
	digest := v5BytesDigest(raw)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := f.PutPackage(raw, digest); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	f, err = NewV5ManagedFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	got, err := f.Package(digest)
	if err != nil || !bytes.Equal(got, raw) {
		t.Fatal("package not durable", err)
	}
	if err := f.PutPackage(append(raw, 1), digest); err != ErrPluginV5PackageDigest {
		t.Fatal("wrong digest accepted")
	}
	for _, bad := range []string{"../escape", "sha256:../escape", "SHA256:bad"} {
		if _, err := f.Package(bad); err == nil {
			t.Fatal("unsafe digest")
		}
	}
	if _, err := f.Prepare("campusos", "demo", "101", 1, []byte(`{"safe":1}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Prepare("campusos", "demo", "101", 1, []byte(`{"safe":2}`)); err != ErrV5Conflict {
		t.Fatal("generation overwritten")
	}
	for _, owner := range []string{"../102", "01", "-1", "system"} {
		if _, err := f.Prepare("campusos", "demo", owner, 1, nil); err == nil {
			t.Fatal("unsafe owner")
		}
	}
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "preserve"), []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, owner := range []string{"101", "102"} {
		d := filepath.Join(root, "data/campusos/demo/user-"+owner)
		if err := os.MkdirAll(d, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, "keep"), []byte(owner), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(outside, filepath.Join(root, "data/campusos/demo/user-101/link")); err != nil {
		t.Fatal(err)
	}
	if err := f.PurgeOwner("campusos", "demo", "101"); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{filepath.Join(outside, "preserve"), filepath.Join(root, "data/campusos/demo/user-102/keep")} {
		if _, err := os.Stat(n); err != nil {
			t.Fatal("purge escaped owner", err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "packages", digest[7:]+".tar.gz"), []byte("tampered"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Package(digest); err != ErrPluginV5PackageDigest {
		t.Fatal("tampering accepted")
	}
	if err := f.RemovePackage(digest); err != nil {
		t.Fatal(err)
	}
	if err := f.PutPackage(raw, digest); err != nil {
		t.Fatal(err)
	}
}

func TestV5PurgeRejectsIntermediateOwnerRedirect(t *testing.T) {
	root := t.TempDir()
	f, err := NewV5ManagedFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := os.MkdirAll(filepath.Join(root, "data", "other", "demo", "user-101"), 0700); err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(root, "data", "other", "demo", "user-101", "keep")
	if err := os.WriteFile(keep, []byte("other owner"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("other", filepath.Join(root, "data", "campusos")); err != nil {
		t.Fatal(err)
	}
	if err := f.PurgeOwner("campusos", "demo", "101"); err != ErrV5PlatformUnavailable {
		t.Fatal("intermediate symlink accepted", err)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatal("another namespace removed", err)
	}
}
