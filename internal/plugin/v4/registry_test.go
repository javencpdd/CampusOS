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

func TestDiscardDevelopmentReleasesOnlyRemovesRequestedPlugin(t *testing.T) {
	root := t.TempDir()
	for _, key := range []string{"campusos.pdf-viewer", "campusos.other-plugin"} {
		if err := os.MkdirAll(filepath.Join(root, ".installed", key, "2.0.0-dev.1-test"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := DiscardDevelopmentReleases(root, "campusos.pdf-viewer"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, ".installed", "campusos.pdf-viewer")); !os.IsNotExist(err) {
		t.Fatalf("requested development release was not removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".installed", "campusos.other-plugin")); err != nil {
		t.Fatalf("unrelated plugin release was removed: %v", err)
	}
	if err := DiscardDevelopmentReleases(root, "../escape"); err == nil {
		t.Fatal("invalid plugin key was accepted")
	}
}
