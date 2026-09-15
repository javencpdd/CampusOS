package plugin

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPreparePluginStorageIsPrivateAndAllowsManifestDots(t *testing.T) {
	layout, err := PreparePluginStorage(t.TempDir(), "builtin.pdf-viewer")
	if err != nil {
		t.Fatalf("prepare plugin storage: %v", err)
	}
	if filepath.Base(layout.DataDir) != "builtin.pdf-viewer" {
		t.Fatalf("data directory = %q", layout.DataDir)
	}
	if filepath.Base(layout.SQLitePath) != "plugin.db" || filepath.Base(layout.ConfigPath) != "config.json" {
		t.Fatalf("unexpected standard paths: %#v", layout)
	}
}

func TestPluginFileConfigIsAtomicAndRejectsUnsafeContent(t *testing.T) {
	root := t.TempDir()
	if err := WritePluginFileConfig(root, "example.process", []byte(`{"config_version":1,"language":"zh-CN"}`)); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := WritePluginFileConfig(root, "example.process", []byte(`{"config_version":2,"language":"en"}`)); err != nil {
		t.Fatalf("replace config: %v", err)
	}
	data, found, err := ReadPluginFileConfig(root, "example.process")
	if err != nil || !found || string(data) != `{"config_version":2,"language":"en"}` {
		t.Fatalf("read config data=%q found=%v err=%v", data, found, err)
	}
	if err := WritePluginFileConfig(root, "example.process", []byte(`not-json`)); err == nil {
		t.Fatal("invalid JSON config was accepted")
	}
	layout, err := PreparePluginStorage(root, "example.process")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(layout.ConfigPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "outside.json"), layout.ConfigPath); err == nil {
		if _, _, err := ReadPluginFileConfig(root, "example.process"); err == nil {
			t.Fatal("symlink config was accepted")
		}
	}
}

func TestPreparePluginStorageRejectsTraversal(t *testing.T) {
	for _, name := range []string{"../other", "nested/plugin", `nested\\plugin`, ".", ".."} {
		if _, err := PreparePluginStorage(t.TempDir(), name); err == nil {
			t.Fatalf("unsafe name %q was accepted", name)
		}
	}
}
