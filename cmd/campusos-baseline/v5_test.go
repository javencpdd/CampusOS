package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestV5BaselineKeepsSourceDeclarationsSeparate(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "plugins", "campusos.chat-echo")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile("../../plugins/campusos.chat-echo/plugin.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "plugin.json"), raw, 0644); err != nil {
		t.Fatal(err)
	}
	v4, err := collectV4PluginSources(root)
	if err != nil || v4.Count != 0 {
		t.Fatal("v5 declaration became a v4 source", v4, err)
	}
	v5, err := collectV5PluginSources(root)
	if err != nil || v5.Count != 1 || v5.Packages[0].Publisher != "campusos" || v5.Packages[0].Key != "chat-echo" || v5.Packages[0].Runtime != "container" || v5.SHA256 == "" {
		t.Fatal("current v5 declaration missing", v5, err)
	}
	installed, err := collectV4InstalledReleases(root)
	if err != nil || installed.Count != 0 {
		t.Fatal("declaration treated as installed", installed, err)
	}
}

func TestV5BaselineRejectsMalformedAndSymlinkDeclarations(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "plugins", "broken")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "plugin.json")
	if err := os.WriteFile(file, []byte(`{"api_version":"campusos.plugin/v5"}`), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := collectV5PluginSources(root); err == nil {
		t.Fatal("invalid v5 source was accepted")
	}
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../../../../plugins/campusos.chat-echo/plugin.json", file); err != nil {
		t.Fatal(err)
	}
	if _, err := collectV5PluginSources(root); err == nil {
		t.Fatal("symlink v5 declaration was accepted")
	}
}
