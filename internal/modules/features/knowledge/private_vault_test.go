package knowledge

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestPrivateVaultRetirementSurvivesOldCiphertext(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("private vault native ACL contract is Linux-only")
	}
	dir := t.TempDir()
	if e := os.Chmod(dir, 0700); e != nil {
		t.Fatal(e)
	}
	v, e := NewPrivateVault(dir)
	if e != nil {
		t.Fatal(e)
	}
	id := privateID()
	plain := []byte("PRIVATE_DERIVED_TEXT")
	aad := []byte("owner1/source/revision1")
	sealed, e := v.SealNew(id, plain, aad)
	if e != nil {
		t.Fatal(e)
	}
	if bytes.Contains(sealed.Ciphertext, plain) {
		t.Fatal("plaintext at rest")
	}
	got, e := v.Open(id, sealed, aad)
	if e != nil || !bytes.Equal(got, plain) {
		t.Fatal(e)
	}
	if _, e = v.Open(id, sealed, []byte("owner2/source/revision1")); e == nil {
		t.Fatal("cross owner envelope")
	}
	oldKey, e := os.ReadFile(filepath.Join(dir, id+".key"))
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(dir, id+".key"), bytes.Repeat([]byte{0x7a}, 32), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e = v.Open(id, sealed, aad); e == nil {
		t.Fatal("wrong external key accepted or read key cached")
	}
	if e = os.WriteFile(filepath.Join(dir, id+".key"), oldKey, 0600); e != nil {
		t.Fatal(e)
	}
	clear(oldKey)
	if _, e = v.SealNew(id, plain, aad); e == nil {
		t.Fatal("key identity overwritten")
	}
	if e = v.Retire(id); e != nil {
		t.Fatal(e)
	}
	v, e = NewPrivateVault(dir)
	if e != nil {
		t.Fatal(e)
	}
	if v.Has(id) {
		t.Fatal("revived key")
	}
	if _, e = v.Open(id, sealed, aad); e == nil {
		t.Fatal("old database backup resurrected withdrawn text")
	}
	if _, e = v.SealNew(id, plain, aad); e == nil {
		t.Fatal("retired identity reused")
	}
	if e = v.Retire(id); e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(filepath.Join(dir, id+".key")); !os.IsNotExist(e) {
		t.Fatal("withdrawn key retained")
	}
}
func TestPrivateVaultProtectionFailureCloses(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("private vault native ACL contract is Linux-only")
	}
	dir := t.TempDir()
	_ = os.Chmod(dir, 0755)
	if _, e := NewPrivateVault(dir); e == nil {
		t.Fatal("world accessible vault")
	}
	_ = os.Chmod(dir, 0700)
	if _, e := NewPrivateVault(dir, dir); e == nil {
		t.Fatal("vault in backup root")
	}
	v, e := NewPrivateVault(dir)
	if e != nil {
		t.Fatal(e)
	}
	id := privateID()
	sealed, e := v.SealNew(id, []byte("secret"), []byte("bound"))
	if e != nil {
		t.Fatal(e)
	}
	_ = os.Chmod(filepath.Join(dir, id+".key"), 0644)
	if _, e = v.Open(id, sealed, []byte("bound")); e == nil {
		t.Fatal("unsafe key permission")
	}
	_ = os.Chmod(filepath.Join(dir, id+".key"), 0600)
	_ = os.Remove(filepath.Join(dir, id+".key"))
	_ = os.Symlink("/etc/passwd", filepath.Join(dir, id+".key"))
	if v.Has(id) {
		t.Fatal("symlink key accepted")
	}
	_ = os.Chmod(dir, 0755)
	if v.Available() {
		t.Fatal("permission revocation ignored")
	}
}

func TestPrivateVaultPrunesOnlyOldUnreferencedKeys(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux vault contract")
	}
	dir := t.TempDir()
	_ = os.Chmod(dir, 0700)
	v, e := NewPrivateVault(dir)
	if e != nil {
		t.Fatal(e)
	}
	orphan, active, recent := privateID(), privateID(), privateID()
	for _, id := range []string{orphan, active, recent} {
		if _, e = v.SealNew(id, []byte("synthetic"), []byte("binding")); e != nil {
			t.Fatal(e)
		}
	}
	old := time.Now().Add(-2 * time.Hour)
	for _, id := range []string{orphan, active} {
		if e = os.Chtimes(filepath.Join(dir, id+".key"), old, old); e != nil {
			t.Fatal(e)
		}
	}
	if e = v.PruneOrphans(func(id string) (bool, error) { return id == active, nil }); e != nil {
		t.Fatal(e)
	}
	v.closeScan()
	if v.Has(orphan) || !v.Has(active) || !v.Has(recent) {
		t.Fatal("orphan retirement crossed active/in-flight key boundary")
	}
}
