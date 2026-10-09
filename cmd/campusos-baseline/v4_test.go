package main

import (
	"os"
	"path/filepath"
	"testing"

	pluginv4 "github.com/campusos/CampusOS/internal/plugin/v4"
)

const baselineV4Manifest = `api_version: campusos.plugin/v4
key: campusos.demo
version: 1.0.0
publisher:
  id: campusos
  name: CampusOS
display_name: Demo
compatibility:
  host: ">=1.1"
  ui: campusos.ui/v3
  bridge: campusos.bridge/v1
backend:
  runtime: none
artifacts:
  user_ui:
    source: frontend/user
    entry: ui/user/index.html
ui:
  user:
    surfaces:
      - id: view
        route: view
        title: View
        presentations: [page]
`

func TestV4BaselineSeparatesSourceFromInstalledRelease(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "plugins", "campusos.demo")
	for _, path := range []string{filepath.Join(source, "frontend", "user"), filepath.Join(root, "plugins", ".staging", "invalid")} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for name, body := range map[string]string{"plugin.yaml": baselineV4Manifest, "README.md": "demo\n", "LICENSE": "test\n"} {
		if err := os.WriteFile(filepath.Join(source, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	sources, err := collectV4PluginSources(root)
	if err != nil {
		t.Fatal(err)
	}
	if sources.Count != 1 || sources.Packages[0].Key != "campusos.demo" || sources.Packages[0].Digest != "" || sources.SHA256 == "" {
		t.Fatalf("unexpected source inventory: %#v", sources)
	}
	installed, err := collectV4InstalledReleases(root)
	if err != nil {
		t.Fatal(err)
	}
	if installed.Count != 0 || len(installed.Packages) != 0 {
		t.Fatalf("source/staging was treated as installed: %#v", installed)
	}

	if err := os.MkdirAll(filepath.Join(source, "dist", "user"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "dist", "user", "index.html"), []byte("<html></html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	releaseRoot := filepath.Join(t.TempDir(), "release")
	if _, err := pluginv4.PrepareReleaseDirectory(source, releaseRoot); err != nil {
		t.Fatal(err)
	}
	archive, err := pluginv4.BuildReleaseArchive(releaseRoot, filepath.Join(t.TempDir(), "release.tar.gz"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pluginv4.InstallReleaseArchive(archive.ArchivePath, pluginv4.InstallOptions{RootDir: filepath.Join(root, "plugins")}); err != nil {
		t.Fatal(err)
	}
	installed, err = collectV4InstalledReleases(root)
	if err != nil {
		t.Fatal(err)
	}
	if installed.Count != 1 || installed.Packages[0].Key != "campusos.demo" || installed.Packages[0].Digest == "" {
		t.Fatalf("verified release is missing from inventory: %#v", installed)
	}

	invalidRelease := filepath.Join(root, "plugins", ".installed", "campusos.invalid", "1.0.0")
	if err := os.MkdirAll(invalidRelease, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := collectV4InstalledReleases(root); err == nil {
		t.Fatal("unverified installed release was accepted")
	}
}

func TestV4BaselineRejectsInvalidSource(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "plugins", "broken"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := collectV4PluginSources(root); err == nil {
		t.Fatal("invalid source package was accepted")
	}
}
