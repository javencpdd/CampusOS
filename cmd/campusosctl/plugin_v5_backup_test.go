package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/campusos/CampusOS/internal/plugin"
)

func TestPluginBackupPreservesOriginalPackageAndSignature(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	var out, errout bytes.Buffer
	if run([]string{"plugin", "init", "backup-demo", "--dir", source}, &out, &errout) != 0 {
		t.Fatal(errout.String())
	}
	raw, p, err := plugin.BuildPluginV5Package(source)
	if err != nil {
		t.Fatal(err)
	}
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := plugin.SignPluginV5Package(raw, "backup-test", private)
	if err != nil {
		t.Fatal(err)
	}
	manifest, _ := json.Marshal(p.Manifest)
	r := plugin.V5PackageRelease{V5Coordinate: plugin.V5Coordinate{Publisher: p.Manifest.Publisher, Key: p.Manifest.Key, Version: p.Manifest.Version}, Digest: p.Digest, Size: len(raw), Manifest: manifest, Release: envelope, Storage: "available", Revision: 1}
	directory := filepath.Join(root, "managed")
	files, err := plugin.NewV5ManagedFiles(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer files.Close()
	if err := files.PutPackage(raw, p.Digest); err != nil {
		t.Fatal(err)
	}
	inventory := filepath.Join(root, "inventory.json")
	input, _ := json.Marshal(map[string]any{"releases": []plugin.V5PackageRelease{r}})
	if err := os.WriteFile(inventory, input, 0600); err != nil {
		t.Fatal(err)
	}
	args := []string{"plugin", "backup", "--inventory", inventory, "--data-dir", directory}
	for attempt := 0; attempt < 2; attempt++ {
		out.Reset()
		errout.Reset()
		if run(args, &out, &errout) != 0 {
			t.Fatal(errout.String())
		}
		var report struct {
			Verified bool `json:"backup_verified"`
		}
		if json.Unmarshal(out.Bytes(), &report) != nil || !report.Verified {
			t.Fatal("backup not verified")
		}
	}
	if got, err := files.RepositoryPackage(r); err != nil || !bytes.Equal(got, raw) {
		t.Fatal("published package changed", err)
	}
	if got, err := files.Package(r.Digest); err != nil || !bytes.Equal(got, raw) {
		t.Fatal("backup modified hot package", err)
	}
	if err := files.RemovePackage(r.Digest); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	errout.Reset()
	if run(args, &out, &errout) != 0 {
		t.Fatal("verified archive could not be backed up again", errout.String())
	}
}
