package plugin

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func testV5PackageSource(t *testing.T, index int) string {
	t.Helper()
	dir := t.TempDir()
	var m PluginV5Manifest
	if err := json.Unmarshal(v5ManifestFixtures(t)[index].Manifest, &m); err != nil {
		t.Fatal(err)
	}
	m.Consumes = []PluginV5Consume{}
	write := func(name string, raw []byte) {
		t.Helper()
		p := filepath.Join(dir, name)
		if os.MkdirAll(filepath.Dir(p), 0700) != nil || os.WriteFile(p, raw, 0600) != nil {
			t.Fatal("fixture write")
		}
	}
	if m.Backend != nil && m.Backend.Artifact.Kind == "wasm" {
		raw := []byte{0, 'a', 's', 'm', 1, 0, 0, 0}
		m.Backend.Artifact.Path = "dist/module.wasm"
		m.Backend.Artifact.Digest = v5BytesDigest(raw)
		write(m.Backend.Artifact.Path, raw)
	}
	for i := range m.UI {
		raw := []byte("<!doctype html><title>Isolated UI</title>")
		m.UI[i].Entrypoint = "dist/" + m.UI[i].Audience + "/index.html"
		m.UI[i].Digest = v5BytesDigest(raw)
		write(m.UI[i].Entrypoint, raw)
	}
	wire, _ := json.Marshal(m)
	write(PluginV5ManifestFile, wire)
	return dir
}
func testV5Trust(t *testing.T) (ed25519.PrivateKey, []byte) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(PluginV5TrustStore{Contract: "campusos.plugin-trust/v1", Keys: []PluginV5TrustKey{{Publisher: "campusos", KeyID: "test-key", PublicKey: base64.StdEncoding.EncodeToString(pub), Status: "active"}}})
	return priv, raw
}
func TestPluginV5PackageByteSnapshot(t *testing.T) {
	for index, name := range []string{"wasm-headless", "container-headless", "ui-only", "combined"} {
		t.Run(name, func(t *testing.T) {
			dir := testV5PackageSource(t, index)
			if os.MkdirAll(filepath.Join(dir, "frontend", "node_modules"), 0700) != nil {
				t.Fatal("mkdir")
			}
			if os.WriteFile(filepath.Join(dir, ".env"), []byte("NOT-SHIPPED"), 0600) != nil {
				t.Fatal("write")
			}
			raw, p, err := BuildPluginV5Package(dir)
			if err != nil {
				t.Fatal(err)
			}
			again, _, err := BuildPluginV5Package(dir)
			if err != nil || !bytes.Equal(raw, again) {
				t.Fatal("package bytes are not deterministic")
			}
			inspected, err := InspectPluginV5Package(raw)
			if err != nil || inspected.Digest != p.Digest || len(inspected.files) != p.FileCount {
				t.Fatalf("inspect: %v", err)
			}
			if _, leak := inspected.files[".env"]; leak {
				t.Fatal("shipped local configuration")
			}
			priv, trust := testV5Trust(t)
			envelope, err := SignPluginV5Package(raw, "test-key", priv)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = VerifyPluginV5Release(raw, envelope, trust); err != nil {
				t.Fatal(err)
			}
		})
	}
	t.Run("source-symlink", func(t *testing.T) {
		dir := testV5PackageSource(t, 0)
		if os.Symlink(filepath.Join(dir, PluginV5ManifestFile), filepath.Join(dir, "dist", "leak")) != nil {
			t.Fatal("symlink")
		}
		if _, _, err := BuildPluginV5Package(dir); !errors.Is(err, ErrPluginV5PackageInvalid) {
			t.Fatal("symlink accepted")
		}
	})
	t.Run("artifact-digest", func(t *testing.T) {
		dir := testV5PackageSource(t, 0)
		if os.WriteFile(filepath.Join(dir, "dist/module.wasm"), []byte("wrong bytes"), 0600) != nil {
			t.Fatal("write")
		}
		if _, _, err := BuildPluginV5Package(dir); !errors.Is(err, ErrPluginV5PackageDigest) {
			t.Fatalf("digest: %v", err)
		}
	})
}

func TestPluginV5PackageArchiveDenials(t *testing.T) {
	archive := func(headers ...*tar.Header) []byte {
		t.Helper()
		var out bytes.Buffer
		gz := gzip.NewWriter(&out)
		tw := tar.NewWriter(gz)
		for _, h := range headers {
			if err := tw.WriteHeader(h); err != nil {
				t.Fatal(err)
			}
		}
		_ = tw.Close()
		_ = gz.Close()
		return out.Bytes()
	}
	for name, h := range map[string]*tar.Header{
		"traversal": {Name: "../escaped", Typeflag: tar.TypeReg}, "absolute": {Name: "/host", Typeflag: tar.TypeReg},
		"noncanonical": {Name: "dist/../host", Typeflag: tar.TypeReg}, "backslash": {Name: `dist\host`, Typeflag: tar.TypeReg},
		"link": {Name: "dist/link", Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd"}, "hardlink": {Name: "dist/link", Typeflag: tar.TypeLink, Linkname: "plugin.json"},
		"directory": {Name: "dist/", Typeflag: tar.TypeDir}, "device": {Name: "dist/device", Typeflag: tar.TypeChar},
		"oversized":      {Name: "dist/large", Typeflag: tar.TypeReg, Size: PluginV5MaxFileBytes + 1},
		"not-in-release": {Name: ".env", Typeflag: tar.TypeReg},
	} {
		t.Run(name, func(t *testing.T) {
			if p, err := InspectPluginV5Package(archive(h)); err == nil || p != nil {
				t.Fatal("unsafe archive accepted")
			}
		})
	}
	t.Run("duplicate", func(t *testing.T) {
		h := &tar.Header{Name: PluginV5ManifestFile, Typeflag: tar.TypeReg}
		if _, err := InspectPluginV5Package(archive(h, h)); err == nil {
			t.Fatal("duplicate accepted")
		}
	})
	dir := testV5PackageSource(t, 0)
	raw, _, err := BuildPluginV5Package(dir)
	if err != nil {
		t.Fatal(err)
	}
	for name, bad := range map[string][]byte{"trailing": append(bytes.Clone(raw), 1), "concatenated": append(bytes.Clone(raw), raw...), "truncated": raw[:len(raw)-3]} {
		t.Run(name, func(t *testing.T) {
			if _, err := InspectPluginV5Package(bad); err == nil {
				t.Fatal("invalid stream accepted")
			}
		})
	}
}

func TestPluginV5PublicationCurrentTrustAndConflict(t *testing.T) {
	dir := testV5PackageSource(t, 0)
	raw, _, err := BuildPluginV5Package(dir)
	if err != nil {
		t.Fatal(err)
	}
	priv, trust := testV5Trust(t)
	envelope, err := SignPluginV5Package(raw, "test-key", priv)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := NewPluginV5PackageCatalog(trust)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err = catalog.VerifyAndRecord(raw, envelope); err != nil {
			t.Fatal(err)
		}
	}
	if os.WriteFile(filepath.Join(dir, "README.md"), []byte("new bytes, same identity"), 0600) != nil {
		t.Fatal("write")
	}
	other, _, err := BuildPluginV5Package(dir)
	if err != nil {
		t.Fatal(err)
	}
	otherEnvelope, err := SignPluginV5Package(other, "test-key", priv)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = catalog.VerifyAndRecord(other, otherEnvelope); !errors.Is(err, ErrPluginV5PackageConflict) {
		t.Fatalf("conflict: %v", err)
	}
	if _, err = VerifyPluginV5Release(other, envelope, trust); !errors.Is(err, ErrPluginV5PackageDigest) {
		t.Fatal("wrong byte digest accepted")
	}
	_, untrusted := testV5Trust(t)
	if _, err = VerifyPluginV5Release(raw, envelope, untrusted); !errors.Is(err, ErrPluginV5PackageSignature) {
		t.Fatal("untrusted signer accepted")
	}
	revoked := bytes.Replace(trust, []byte(`"active"`), []byte(`"revoked"`), 1)
	if err = catalog.SetTrust(revoked); err != nil {
		t.Fatal(err)
	}
	if _, err = catalog.VerifyAndRecord(raw, envelope); !errors.Is(err, ErrPluginV5PackageSignature) {
		t.Fatal("cached trust bypassed revocation")
	}
	otherPublisher := bytes.Replace(trust, []byte(`"campusos"`), []byte(`"someone"`), 1)
	if _, err = VerifyPluginV5Release(raw, envelope, otherPublisher); !errors.Is(err, ErrPluginV5PackageSignature) {
		t.Fatal("cross publisher key accepted")
	}
	concurrent, err := NewPluginV5PackageCatalog(trust)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	accepted := map[string]bool{}
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			data, env := raw, envelope
			if i%2 == 1 {
				data, env = other, otherEnvelope
			}
			p, err := concurrent.VerifyAndRecord(data, env)
			if err != nil && !errors.Is(err, ErrPluginV5PackageConflict) {
				t.Errorf("publish: %v", err)
			}
			if p != nil {
				mu.Lock()
				accepted[p.Digest] = true
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()
	if len(accepted) != 1 {
		t.Fatal("same version has multiple byte identities")
	}
}

func TestPluginV5PackageReferencedSchemaAndInputBudget(t *testing.T) {
	for name, schema := range map[string]string{
		"valid-required-field": `{"type":"object","additionalProperties":false,"properties":{"name":{"type":"string","title":"Name"}},"required":["name"]}`,
		"wrong-field-range":    `{"type":"object","additionalProperties":false,"properties":{"age":{"type":"integer","title":"Age","minimum":9,"maximum":1}},"required":[]}`,
		"external-ref":         `{"type":"object","additionalProperties":false,"properties":{"name":{"$ref":"https://untrusted.invalid/schema"}},"required":[]}`,
	} {
		t.Run(name, func(t *testing.T) {
			dir := testV5PackageSource(t, 0)
			wire, err := os.ReadFile(filepath.Join(dir, PluginV5ManifestFile))
			if err != nil {
				t.Fatal(err)
			}
			var m PluginV5Manifest
			if json.Unmarshal(wire, &m) != nil {
				t.Fatal("manifest")
			}
			m.Provides[0].ConfigSchema = &PluginV5SchemaFile{Path: "schemas/provider.json", Digest: v5BytesDigest([]byte(schema))}
			if os.MkdirAll(filepath.Join(dir, "schemas"), 0700) != nil || os.WriteFile(filepath.Join(dir, "schemas/provider.json"), []byte(schema), 0600) != nil {
				t.Fatal("schema")
			}
			wire, _ = json.Marshal(m)
			if os.WriteFile(filepath.Join(dir, PluginV5ManifestFile), wire, 0600) != nil {
				t.Fatal("write")
			}
			_, _, err = BuildPluginV5Package(dir)
			if (err == nil) != (name == "valid-required-field") {
				t.Fatalf("schema policy: %v", err)
			}
		})
	}
	t.Run("document-file-budget", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "document")
		if os.WriteFile(path, bytes.Repeat([]byte("x"), PluginV5ManifestMaxBytes+1), 0600) != nil {
			t.Fatal("write")
		}
		if _, err := ReadPluginV5DocumentFile(path); err == nil {
			t.Fatal("oversized metadata file accepted")
		}
		if _, err := ReadPluginV5DocumentFile(filepath.Dir(path)); err == nil {
			t.Fatal("directory accepted as document")
		}
	})
}
