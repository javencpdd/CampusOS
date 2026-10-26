package recovery

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestStartupRejectsPartialOrTamperedRecoveryWithoutDatabase(t *testing.T) {
	root := t.TempDir()
	_, release, e := Startup(context.Background(), nil, root)
	if e != nil {
		t.Fatal(e)
	}
	release()
	epoch, e := NewEpoch()
	if e != nil {
		t.Fatal(e)
	}
	s := State{Epoch: epoch, ArchiveSHA: epoch, Phase: "quarantined", EgressPaused: true}
	if e = WriteMarker(root, s); e != nil {
		t.Fatal(e)
	}
	if _, _, e = Startup(context.Background(), nil, root); e == nil {
		t.Fatal("recovery silently fell back to memory")
	}
	s.Phase = "reviewed"
	if e = WriteMarker(root, s); e != nil {
		t.Fatal(e)
	}
	if _, _, e = Startup(context.Background(), nil, root); e == nil {
		t.Fatal("missing database trusted a file-only review")
	}
	if e = os.WriteFile(filepath.Join(root, markerName), []byte(`{"phase":"reviewed"}`), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e = ReadMarker(root); e == nil {
		t.Fatal("partial marker accepted")
	}
	if e = os.Remove(filepath.Join(root, markerName)); e != nil {
		t.Fatal(e)
	}
	if e = os.Symlink("outside", filepath.Join(root, markerName)); e != nil {
		t.Fatal(e)
	}
	if _, e = ReadMarker(root); e == nil {
		t.Fatal("symlink marker accepted")
	}
}
func TestAuthenticationEpochSeparatesRestores(t *testing.T) {
	a, e := NewEpoch()
	if e != nil {
		t.Fatal(e)
	}
	b, e := NewEpoch()
	if e != nil {
		t.Fatal(e)
	}
	if a == b || len(a) != 64 || len(b) != 64 {
		t.Fatal("reused or weak epoch")
	}
	key := "fixture-base-secret"
	if DeriveSecret(key, "") != key || DeriveSecret(key, a) == key || DeriveSecret(key, a) == DeriveSecret(key, b) || DeriveSecret(key, a) != DeriveSecret(key, a) {
		t.Fatal("authentication epoch key separation failed")
	}
}
