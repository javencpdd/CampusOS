package v4

import (
	"crypto/ed25519"
	"crypto/rand"
	"os"
	"path/filepath"
	"testing"
)

func TestBuildSignAndAtomicallyInstallRelease(t *testing.T) {
	release := makeReleaseFixture(t)
	unsignedPath := filepath.Join(t.TempDir(), "pdf.tar.gz")
	unsigned, err := BuildReleaseArchive(release, unsignedPath)
	if err != nil {
		t.Fatal(err)
	}
	if unsigned.Digest == "" || len(unsigned.Files) == 0 {
		t.Fatal("package metadata was not generated")
	}
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signedPath := filepath.Join(t.TempDir(), "pdf.signed.tar.gz")
	signed, err := SignReleaseArchive(unsignedPath, signedPath, "campusos-test", privateKey)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	installed, err := InstallReleaseArchive(signed.ArchivePath, InstallOptions{RootDir: root, RequireSignature: true, TrustedKeys: map[string]ed25519.PublicKey{"campusos-test": publicKey}})
	if err != nil {
		t.Fatal(err)
	}
	if installed.InstallPath == "" {
		t.Fatal("install path was not returned")
	}
	if _, err := os.Stat(filepath.Join(installed.InstallPath, "ui", "user", "index.html")); err != nil {
		t.Fatalf("installed UI artifact is absent: %v", err)
	}
	again, err := InstallReleaseArchive(signed.ArchivePath, InstallOptions{RootDir: root, RequireSignature: true, TrustedKeys: map[string]ed25519.PublicKey{"campusos-test": publicKey}})
	if err != nil || again.InstallPath != installed.InstallPath {
		t.Fatalf("same release must install idempotently: %v, %#v", err, again)
	}
}

func TestInstallRejectsUnsignedReleaseWhenPolicyRequiresTrust(t *testing.T) {
	release := makeReleaseFixture(t)
	archive, err := BuildReleaseArchive(release, filepath.Join(t.TempDir(), "pdf.tar.gz"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := InstallReleaseArchive(archive.ArchivePath, InstallOptions{RootDir: t.TempDir(), RequireSignature: true}); err == nil {
		t.Fatal("unsigned release bypassed signature policy")
	}
}

func TestPrepareReleaseDirectoryCopiesOnlyReleaseInputs(t *testing.T) {
	source := t.TempDir()
	for _, directory := range []string{"frontend/user", "frontend/admin", "dist/user", "dist/admin", "config"} {
		if err := os.MkdirAll(filepath.Join(source, directory), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{
		"plugin.yaml":                 validManifest,
		"README.md":                   "fixture\n",
		"LICENSE":                     "MIT\n",
		"frontend/user/main.ts":       "throw new Error('source must not ship')",
		"dist/user/index.html":        "<!doctype html><title>user</title>",
		"dist/admin/index.html":       "<!doctype html><title>admin</title>",
		"config/system.schema.json":   `{"type":"object","additionalProperties":false}`,
		"config/system.defaults.json": `{}`,
		"config/user.schema.json":     `{"type":"object","additionalProperties":false}`,
		"config/user.defaults.json":   `{}`,
	}
	for filename, data := range files {
		if err := os.WriteFile(filepath.Join(source, filepath.FromSlash(filename)), []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	release := filepath.Join(t.TempDir(), "release")
	manifest, err := PrepareReleaseDirectory(source, release)
	if err != nil || manifest.Key != "campusos.pdf-viewer" {
		t.Fatalf("prepare release failed: %v, %#v", err, manifest)
	}
	if _, err := os.Stat(filepath.Join(release, "ui", "user", "index.html")); err != nil {
		t.Fatalf("user release entry missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(release, "frontend", "user", "main.ts")); !os.IsNotExist(err) {
		t.Fatalf("source was copied into release: %v", err)
	}
}

func makeReleaseFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, directory := range []string{"ui/user", "ui/admin", "config"} {
		if err := os.MkdirAll(filepath.Join(root, directory), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{
		"plugin.yaml":                 validManifest,
		"README.md":                   "fixture\n",
		"LICENSE":                     "MIT\n",
		"ui/user/index.html":          "<!doctype html><title>user</title>",
		"ui/admin/index.html":         "<!doctype html><title>admin</title>",
		"config/system.schema.json":   `{"type":"object","additionalProperties":false}`,
		"config/system.defaults.json": `{}`,
		"config/user.schema.json":     `{"type":"object","additionalProperties":false}`,
		"config/user.defaults.json":   `{}`,
	}
	for name, contents := range files {
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(name)), []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}
