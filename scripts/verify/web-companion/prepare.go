package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"github.com/campusos/CampusOS/internal/plugin"
	"github.com/campusos/CampusOS/pkg/auth"
	"io/fs"
	"os"
	"path/filepath"
)

func must(err error) {
	if err != nil {
		panic(err)
	}
}
func main() {
	out := os.Args[1]
	public, private, err := ed25519.GenerateKey(rand.Reader)
	must(err)
	keys := []plugin.PluginV5TrustKey{}
	for _, publisher := range []string{"campusos", "example"} {
		keys = append(keys, plugin.PluginV5TrustKey{Publisher: publisher, KeyID: "owned-rss-fixture", PublicKey: base64.StdEncoding.EncodeToString(public), Status: "active"})
	}
	trust, _ := json.Marshal(plugin.PluginV5TrustStore{Contract: "campusos.plugin-trust/v1", Keys: keys})
	must(os.WriteFile(filepath.Join(out, "trust.json"), trust, 0600))
	raw, _, err := plugin.BuildPluginV5Package("plugins/campusos.rss-publisher")
	must(err)
	release, err := plugin.SignPluginV5Package(raw, "owned-rss-fixture", private)
	must(err)
	must(os.WriteFile(filepath.Join(out, "rss.tar.gz"), raw, 0600))
	must(os.WriteFile(filepath.Join(out, "rss.release.json"), release, 0600))
	clone := filepath.Join(out, "renamed")
	must(os.MkdirAll(clone, 0700))
	for _, name := range []string{"plugin.json", "README.md", "LICENSE"} {
		b, e := os.ReadFile(filepath.Join("plugins/campusos.rss-publisher", name))
		must(e)
		must(os.WriteFile(filepath.Join(clone, name), b, 0600))
	}
	must(filepath.WalkDir("plugins/campusos.rss-publisher/dist", func(path string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		rel, e := filepath.Rel("plugins/campusos.rss-publisher", path)
		if e != nil {
			return e
		}
		dest := filepath.Join(clone, rel)
		if d.IsDir() {
			return os.MkdirAll(dest, 0700)
		}
		b, e := os.ReadFile(path)
		if e != nil {
			return e
		}
		return os.WriteFile(dest, b, 0600)
	}))
	manifest, err := os.ReadFile(filepath.Join(clone, "plugin.json"))
	must(err)
	var m map[string]any
	must(json.Unmarshal(manifest, &m))
	m["publisher"] = "example"
	m["key"] = "news-reader"
	manifest, _ = json.Marshal(m)
	must(os.WriteFile(filepath.Join(clone, "plugin.json"), manifest, 0600))
	must(os.WriteFile(filepath.Join(clone, "dist/user/identity.js"), []byte(`export const identity={plugin_key:"example.news-reader",plugin_version:"2.1.0",surface_id:"ui",audience:"user"};`), 0600))
	raw, _, err = plugin.BuildPluginV5Package(clone)
	must(err)
	release, err = plugin.SignPluginV5Package(raw, "owned-rss-fixture", private)
	must(err)
	must(os.WriteFile(filepath.Join(out, "renamed.tar.gz"), raw, 0600))
	must(os.WriteFile(filepath.Join(out, "renamed.release.json"), release, 0600))
	hash, err := auth.HashPassword("owned-rss-fixture-password")
	must(err)
	must(os.WriteFile(filepath.Join(out, "credential"), []byte(hash), 0600))
}
