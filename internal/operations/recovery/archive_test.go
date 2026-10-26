package recovery

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

type archiveEntry struct {
	name string
	kind byte
	data string
}

func testTar(t *testing.T, entries []archiveEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tr := tar.NewWriter(gz)
	for _, e := range entries {
		h := &tar.Header{Name: e.name, Typeflag: e.kind, Mode: 0777, Size: int64(len(e.data))}
		if e.kind == tar.TypeSymlink || e.kind == tar.TypeLink {
			h.Linkname = "../../outside"
			h.Size = 0
		}
		if e.kind == tar.TypeDir {
			h.Size = 0
		}
		if err := tr.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if h.Size > 0 {
			if _, err := tr.Write([]byte(e.data)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tr.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
func testArchive(t *testing.T, inner []byte, mutation func(map[string]string)) string {
	t.Helper()
	files := map[string]string{"database.sql": "SELECT 1;\n", "files.tar.gz": string(inner), "metadata.txt": "format=campusos-single-node-v3\nroot_dotenv=excluded\ndeployment_keys=separate_custody_required\n"}
	var sums string
	for _, n := range []string{"database.sql", "files.tar.gz", "metadata.txt"} {
		sums += fmt.Sprintf("%x  %s\n", sha256.Sum256([]byte(files[n])), n)
	}
	files["SHA256SUMS"] = sums
	if mutation != nil {
		mutation(files)
	}
	var entries []archiveEntry
	for _, n := range []string{"database.sql", "files.tar.gz", "metadata.txt", "SHA256SUMS"} {
		entries = append(entries, archiveEntry{n, tar.TypeReg, files[n]})
	}
	name := filepath.Join(t.TempDir(), "backup.tar.gz")
	if err := os.WriteFile(name, testTar(t, entries), 0600); err != nil {
		t.Fatal(err)
	}
	return name
}
func TestArchiveRejectsAmbiguousOrEscapingPayloads(t *testing.T) {
	cases := map[string][]archiveEntry{
		"parent": {{"../outside", tar.TypeReg, "bad"}}, "absolute": {{"/outside", tar.TypeReg, "bad"}},
		"backslash": {{"data/config/..\\outside", tar.TypeReg, "bad"}}, "dot": {{"data/./config/x", tar.TypeReg, "bad"}},
		"symlink": {{"data/config/escape", tar.TypeSymlink, ""}}, "hardlink": {{"data/config/escape", tar.TypeLink, ""}},
		"duplicate": {{"data/config/x", tar.TypeReg, "one"}, {"./data/config/x", tar.TypeReg, "two"}},
		"dotenv":    {{".env", tar.TypeReg, "secret"}}, "unknown-root": {{"bin/code", tar.TypeReg, "bad"}},
		"fifo": {{"data/config/pipe", tar.TypeFifo, ""}}, "parent-file": {{"data/config", tar.TypeReg, "x"}, {"data/config/child", tar.TypeReg, "bad"}},
	}
	for name, entries := range cases {
		t.Run(name, func(t *testing.T) {
			file := testArchive(t, testTar(t, entries), nil)
			if a, err := Prepare(file, DefaultLimits); err == nil {
				a.Close()
				t.Fatal("unsafe archive accepted")
			}
		})
	}
}
func TestArchiveChecksExactManifestAndCompressedTail(t *testing.T) {
	inner := testTar(t, []archiveEntry{{"data/config/plain.json", tar.TypeReg, "{}"}})
	for name, change := range map[string]func(map[string]string){
		"mismatch":       func(f map[string]string) { f["database.sql"] = "changed" },
		"missing":        func(f map[string]string) { f["SHA256SUMS"] = "" },
		"extra-path":     func(f map[string]string) { f["SHA256SUMS"] += fmt.Sprintf("%x  ../outside\n", sha256.Sum256(nil)) },
		"duplicate-line": func(f map[string]string) { f["SHA256SUMS"] += f["SHA256SUMS"] },
	} {
		t.Run(name, func(t *testing.T) {
			a, e := Prepare(testArchive(t, inner, change), DefaultLimits)
			if e == nil {
				a.Close()
				t.Fatal("bad checksum manifest accepted")
			}
		})
	}
	file := testArchive(t, inner, nil)
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	for name, tail := range map[string][]byte{"gzip-member": inner, "nonzero": []byte("extra payload")} {
		t.Run(name, func(t *testing.T) {
			f := filepath.Join(t.TempDir(), "bad.tar.gz")
			if err := os.WriteFile(f, append(append([]byte(nil), raw...), tail...), 0600); err != nil {
				t.Fatal(err)
			}
			a, e := Prepare(f, DefaultLimits)
			if e == nil {
				a.Close()
				t.Fatal("appended payload accepted")
			}
		})
	}
}
func TestArchiveExtractPrivateAtomicAndBounded(t *testing.T) {
	file := testArchive(t, testTar(t, []archiveEntry{{"data/config/plain.json", tar.TypeReg, "private-data"}}), nil)
	for _, limits := range []Limits{{Bytes: 5, Entries: 20}, {Bytes: 1 << 20, Entries: 2}} {
		if a, e := Prepare(file, limits); e == nil {
			a.Close()
			t.Fatal("limit bypass")
		}
	}
	a, err := Prepare(file, DefaultLimits)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	dest := filepath.Join(t.TempDir(), "restored")
	if err = a.Extract(dest); err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(dest, "files/data/config/plain.json")
	raw, err := os.ReadFile(f)
	if err != nil || string(raw) != "private-data" {
		t.Fatal("lost restored data", err)
	}
	stat, err := os.Stat(f)
	if err != nil || stat.Mode().Perm() != 0600 {
		t.Fatal("unsafe restored file mode")
	}
	if err = a.Extract(dest); err == nil {
		t.Fatal("nonempty destination overwritten")
	}
	link := filepath.Join(t.TempDir(), "link")
	if err = os.Symlink(dest, link); err != nil {
		t.Fatal(err)
	}
	if err = a.Extract(link); err == nil {
		t.Fatal("destination symlink followed")
	}
}
