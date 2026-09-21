package v4

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverInstalledReleasesIgnoresSourceAndDetectsTampering(t *testing.T) {
	release := makeReleaseFixture(t)
	archive, err := BuildReleaseArchive(release, filepath.Join(t.TempDir(), "pdf.tar.gz"))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	installed, err := InstallReleaseArchive(archive.ArchivePath, InstallOptions{RootDir: root})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "campusos.untrusted"), 0o700); err != nil {
		t.Fatal(err)
	}
	releases, err := DiscoverInstalledReleases(root)
	if err != nil || len(releases) != 1 || releases[0].Path != installed.InstallPath {
		t.Fatalf("unexpected release discovery: %v %#v", err, releases)
	}
	if err := os.WriteFile(filepath.Join(installed.InstallPath, "ui", "user", "index.html"), []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := DiscoverInstalledReleases(root); err == nil {
		t.Fatal("tampered installed release was accepted")
	}
}
