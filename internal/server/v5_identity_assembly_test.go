package server

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	platformfeature "github.com/campusos/CampusOS/internal/platform/feature"
	platformmodule "github.com/campusos/CampusOS/internal/platform/module"
	"github.com/campusos/CampusOS/internal/plugin"
)

type v5IdentityAssemblyStore struct{ calls int }

func (s *v5IdentityAssemblyStore) RecordPluginV5Identity(context.Context, plugin.PluginV5PackageIdentity) error {
	s.calls++
	return plugin.ErrPluginV5IdentityUnavailable
}

func TestPluginV5IdentityAssemblyUsesHostStore(t *testing.T) {
	t.Setenv("CAMPUSOS_PLUGIN_V5_TRUST_STORE", "")
	owner := &Server{}
	app := platformmodule.NewAppContext()
	features := newFeatureRegistryModule(owner, platformfeature.NewMemoryStore())
	if err := features.Register(app); err != nil {
		t.Fatal(err)
	}
	store := &v5IdentityAssemblyStore{}
	m := newPluginPlatformModule(owner, nil, features, nil, nil, nil, nil, store)
	if err := m.Register(app); err != nil {
		t.Fatal(err)
	}
	v, ok := app.Lookup("plugin.v5.package-catalog")
	catalog, compatible := v.(*plugin.PluginV5PackageCatalog)
	if !ok || !compatible || catalog == nil {
		t.Fatal("host catalog not registered")
	}
	// Use a tracked declaration and small real UI bytes, without depending on a
	// developer's ignored Vite build outputs.
	manifest, err := os.ReadFile("../../plugins/campusos.pdf-viewer/plugin.json")
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := plugin.ParsePluginV5Manifest(manifest)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	html := []byte("<!doctype html><title>test</title>")
	digest := sha256.Sum256(html)
	for i := range parsed.UI {
		parsed.UI[i].Digest = "sha256:" + hex.EncodeToString(digest[:])
		p := filepath.Join(dir, parsed.UI[i].Entrypoint)
		if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, html, 0600); err != nil {
			t.Fatal(err)
		}
	}
	manifest, err = json.Marshal(parsed)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "plugin.json"), manifest, 0600); err != nil {
		t.Fatal(err)
	}
	raw, _, err := plugin.BuildPluginV5Package(dir)
	if err != nil {
		t.Fatal(err)
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	trust, _ := json.Marshal(plugin.PluginV5TrustStore{Contract: "campusos.plugin-trust/v1", Keys: []plugin.PluginV5TrustKey{{Publisher: parsed.Publisher, KeyID: "assembly", PublicKey: base64.StdEncoding.EncodeToString(pub), Status: "active"}}})
	if err := catalog.SetTrust(trust); err != nil {
		t.Fatal(err)
	}
	envelope, err := plugin.SignPluginV5Package(raw, "assembly", priv)
	if err != nil {
		t.Fatal(err)
	}
	if p, err := catalog.VerifyAndRecordContext(context.Background(), raw, envelope); p != nil || err != plugin.ErrPluginV5IdentityUnavailable || store.calls != 1 {
		t.Fatal("server assembly bypassed the supplied persistence port")
	}
}
