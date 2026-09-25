package v4

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestUserConfigProvisioningCASAndOwnerLayout(t *testing.T) {
	release := makeReleaseFixture(t)
	root := t.TempDir()
	options := UserConfigOptions{
		PersonalSpaceRoot: root,
		PluginVersion:     "2.0.0-dev.1",
		Generation:        "1",
		Now:               func() time.Time { return time.Date(2026, 9, 21, 0, 0, 0, 0, time.UTC) },
	}
	meta, created, err := InitializeUserConfig(release, "1001", options)
	if err != nil || !created || meta.Revision != 1 {
		t.Fatalf("initial provisioning failed: %#v, %t, %v", meta, created, err)
	}
	data, readMeta, err := ReadUserConfig(root, "1001", "campusos.pdf-viewer")
	if err != nil || string(data) != "{}\n" || readMeta.Revision != 1 {
		t.Fatalf("initial configuration read failed: %q %#v %v", data, readMeta, err)
	}
	next, err := UpdateUserConfig(release, "1001", 1, []byte(`{}`), options)
	if err != nil || next.Revision != 2 {
		t.Fatalf("compare-and-swap update failed: %#v %v", next, err)
	}
	if _, err := UpdateUserConfig(release, "1001", 1, []byte(`{}`), options); err == nil {
		t.Fatal("stale configuration revision was accepted")
	}
	layout, err := PrepareUserConfigLayout(root, "1001", "campusos.pdf-viewer")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(layout.PluginDir, ".snapshots", "00000000000000000001.json")); err != nil {
		t.Fatalf("prior value snapshot is missing: %v", err)
	}
	if _, _, err := InitializeUserConfig(release, "../1002", options); err == nil {
		t.Fatal("unsafe user ID was accepted")
	}
}

func TestUserConfigRecoversPendingPair(t *testing.T) {
	root := t.TempDir()
	layout, err := PrepareUserConfigLayout(root, "1001", "campusos.pdf-viewer")
	if err != nil {
		t.Fatal(err)
	}
	userData := []byte("{}\n")
	meta := UserConfigMeta{PluginKey: "campusos.pdf-viewer", PluginVersion: "2.0.0", SchemaVersion: "v1", Generation: "1", Revision: 1, ContentSHA256: jsonDigest(userData), UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	metaData, err := canonicalJSON(meta)
	if err != nil {
		t.Fatal(err)
	}
	if err := writePendingJSON(layout.PendingDir, "user-00000000000000000001.json", userData); err != nil {
		t.Fatal(err)
	}
	if err := writePendingJSON(layout.PendingDir, "meta-00000000000000000001.json", metaData); err != nil {
		t.Fatal(err)
	}
	operation, _ := canonicalJSON(pendingUserConfigWrite{UserFile: "user-00000000000000000001.json", MetaFile: "meta-00000000000000000001.json"})
	if err := writePendingJSON(layout.PendingDir, "operation.json", operation); err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareUserConfigLayout(root, "1001", "campusos.pdf-viewer"); err != nil {
		t.Fatalf("pending recovery failed: %v", err)
	}
	if _, recovered, err := ReadUserConfig(root, "1001", "campusos.pdf-viewer"); err != nil || recovered.Revision != 1 {
		t.Fatalf("recovered configuration was unavailable: %#v %v", recovered, err)
	}
}
