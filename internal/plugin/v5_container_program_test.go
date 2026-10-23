package plugin

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func containerSignedFixture(t *testing.T, ref string, transport string) ([]byte, []byte, ed25519.PrivateKey, []byte, string) {
	t.Helper()
	dir := testV5PackageSource(t, 1)
	raw, err := os.ReadFile(filepath.Join(dir, PluginV5ManifestFile))
	if err != nil {
		t.Fatal(err)
	}
	var m PluginV5Manifest
	if json.Unmarshal(raw, &m) != nil {
		t.Fatal("manifest")
	}
	m.Backend.Artifact.Image = ref
	m.Backend.Transport = transport
	m.HostResources.NetworkTargets = []string{"https://api.example"}
	m.Consumes = []PluginV5Consume{{Capability: "community.thread.public.read", Scope: PluginV5ConsumeScope{Kind: "public"}, Purpose: "Read current public thread metadata", Required: true}}
	raw, _ = json.Marshal(m)
	if err := os.WriteFile(filepath.Join(dir, PluginV5ManifestFile), raw, 0600); err != nil {
		t.Fatal(err)
	}
	archive, _, err := BuildPluginV5Package(dir)
	if err != nil {
		t.Fatal(err)
	}
	priv, trust := testV5Trust(t)
	release, err := SignPluginV5Package(archive, "test-key", priv)
	if err != nil {
		t.Fatal(err)
	}
	return archive, release, priv, trust, dir
}

func TestV5ContainerProgramCaptureTrustIdentityAndCopies(t *testing.T) {
	ctx := context.Background()
	ref := "registry.example/team/plugin@sha256:" + strings.Repeat("a", 64)
	archive, release, priv, trust, dir := containerSignedFixture(t, ref, "grpc")
	c, err := NewPluginV5PackageCatalog(trust)
	if err != nil {
		t.Fatal(err)
	}
	p, err := c.CaptureContainerProgram(ctx, archive, release)
	if err != nil {
		t.Fatal(err)
	}
	if p.ImageReference() != ref || p.Transport() != "grpc" || p.PackageDigest() != v5BytesDigest(archive) || p.Coordinate().Publisher != "campusos" {
		t.Fatal("lost signed identity")
	}
	r := p.Resources()
	before := p.Resources()
	r.NetworkTargets[0] = "https://changed.example"
	caps := p.Consumes()
	caps[0].Capability = "secret.read"
	if !reflect.DeepEqual(p.Resources(), before) || p.Consumes()[0].Capability != "community.thread.public.read" {
		t.Fatal("mutable declaration snapshot")
	}
	raw, _ := os.ReadFile(filepath.Join(dir, PluginV5ManifestFile))
	raw = []byte(strings.Replace(string(raw), ref, "registry.example/team/plugin@sha256:"+strings.Repeat("b", 64), 1))
	if err := os.WriteFile(filepath.Join(dir, PluginV5ManifestFile), raw, 0600); err != nil {
		t.Fatal(err)
	}
	changed, _, err := BuildPluginV5Package(dir)
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := SignPluginV5Package(changed, "test-key", priv)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.CaptureContainerProgram(ctx, changed, envelope); err != ErrPluginV5PackageConflict {
		t.Fatal("same version changed image accepted", err)
	}
	if p.ImageReference() != ref {
		t.Fatal("source directory changed snapshot")
	}
	if err := c.SetTrust([]byte(strings.ReplaceAll(string(trust), `"active"`, `"revoked"`))); err != nil {
		t.Fatal(err)
	}
	if got, err := c.CaptureContainerProgram(ctx, archive, release); err != ErrPluginV5PackageSignature || got.PackageDigest() != "" {
		t.Fatal("revoked capture returned program", err)
	}
}

func TestV5ContainerProgramReferenceSubset(t *testing.T) {
	digest := "@sha256:" + strings.Repeat("a", 64)
	for _, ref := range []string{"registry.example/team/plugin" + digest, "localhost:5000/team/plugin_name.v2" + digest, "127.0.0.1:65535/plugin--name" + digest} {
		if !v5ContainerImageReference(ref) {
			t.Fatal("canonical ref denied", ref)
		}
	}
	for _, ref := range []string{"", "plugin" + digest, "registry/plugin" + digest, "registry.example/plugin:latest", "registry.example/plugin:tag" + digest, "https://registry.example/plugin" + digest, "registry.example//plugin" + digest, "registry.example/../plugin" + digest, "REGISTRY.example/plugin" + digest, "registry.example/Plugin" + digest, "registry.example/plugin" + strings.ToUpper(digest), "localhost:0/plugin" + digest, "localhost:01/plugin" + digest, "localhost:65536/plugin" + digest, "registry.example/plugin" + digest + "\n", "-option/plugin" + digest, "registry.example/_plugin" + digest} {
		if v5ContainerImageReference(ref) {
			t.Fatal("unsafe ref accepted", ref)
		}
	}
}

func TestV5ContainerProgramRejectsUnsupportedAndUntrusted(t *testing.T) {
	ref := "registry.example/plugin@sha256:" + strings.Repeat("a", 64)
	for _, transport := range []string{"http", "grpc", "host-call"} {
		t.Run(transport, func(t *testing.T) {
			a, r, _, trust, _ := containerSignedFixture(t, ref, transport)
			c, _ := NewPluginV5PackageCatalog(trust)
			p, err := c.CaptureContainerProgram(context.Background(), a, r)
			if transport == "host-call" {
				if err != ErrPluginV5ManifestShape || p.PackageDigest() != "" {
					t.Fatal(err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if got, err := c.CaptureContainerProgram(context.Background(), a, []byte(`{}`)); err == nil || got.PackageDigest() != "" {
				t.Fatal("unsigned image descriptor accepted")
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if got, err := c.CaptureContainerProgram(ctx, a, r); err == nil || got.PackageDigest() != "" {
				t.Fatal("canceled capture accepted")
			}
		})
	}
	for _, index := range []int{0, 2} {
		dir := testV5PackageSource(t, index)
		a, _, err := BuildPluginV5Package(dir)
		if err != nil {
			t.Fatal(err)
		}
		priv, trust := testV5Trust(t)
		r, _ := SignPluginV5Package(a, "test-key", priv)
		c, _ := NewPluginV5PackageCatalog(trust)
		if got, err := c.CaptureContainerProgram(context.Background(), a, r); err != ErrPluginV5ManifestShape || got.ImageReference() != "" {
			t.Fatal("non-container accepted", err)
		}
	}
	a, r, _, trust, _ := containerSignedFixture(t, "registry.example//plugin@sha256:"+strings.Repeat("a", 64), "http")
	c, _ := NewPluginV5PackageCatalog(trust)
	if _, err := c.CaptureContainerProgram(context.Background(), a, r); err != ErrPluginV5ManifestShape {
		t.Fatal("noncanonical signed reference accepted", err)
	}
}

// Test-only export for the external test combining real catalog and Docker.
func NewV5ContainerTestProgram(t *testing.T, ref string) V5ContainerProgram {
	return NewV5ContainerTestProgramTransport(t, ref, "http")
}
func NewV5ContainerTestProgramTransport(t *testing.T, ref, transport string) V5ContainerProgram {
	t.Helper()
	a, r, _, trust, _ := containerSignedFixture(t, ref, transport)
	c, err := NewPluginV5PackageCatalog(trust)
	if err != nil {
		t.Fatal(err)
	}
	p, err := c.CaptureContainerProgram(context.Background(), a, r)
	if err != nil {
		t.Fatal(err)
	}
	return p
}
