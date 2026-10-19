package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestV5WasmProgramImmutableSignedCapture(t *testing.T) {
	ctx := context.Background()
	dir := testV5PackageSource(t, 0)
	archive, p, err := BuildPluginV5Package(dir)
	if err != nil {
		t.Fatal(err)
	}
	priv, trust := testV5Trust(t)
	release, err := SignPluginV5Package(archive, "test-key", priv)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := NewPluginV5PackageCatalog(trust)
	if err != nil {
		t.Fatal(err)
	}
	program, err := catalog.CaptureWasmProgram(ctx, archive, release)
	if err != nil || program.Digest() != p.Manifest.Backend.Artifact.Digest || program.Coordinate().Version != p.Manifest.Version {
		t.Fatal("signed capture", err)
	}
	original := program.Bytes()
	copy := program.Bytes()
	copy[0]++
	r := program.Resources()
	before := program.Resources()
	if len(r.NetworkTargets) > 0 {
		r.NetworkTargets[0] = "https://mutated.example"
	}
	if !bytes.Equal(program.Bytes(), original) || !reflect.DeepEqual(program.Resources(), before) {
		t.Fatal("mutable captured program")
	}
	if err := os.WriteFile(filepath.Join(dir, "dist/module.wasm"), []byte("source changed after capture"), 0600); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(program.Bytes(), original) {
		t.Fatal("source path reused instead of archive bytes")
	}
	if err := catalog.SetTrust([]byte(strings.ReplaceAll(string(trust), `"active"`, `"revoked"`))); err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.CaptureWasmProgram(ctx, archive, release); err != ErrPluginV5PackageSignature {
		t.Fatal("revoked signing key captured", err)
	}
}

func TestV5WasmProgramRejectsWrongRuntimeTransportAndSignature(t *testing.T) {
	for _, index := range []int{0, 1, 2} {
		t.Run([]string{"wrong-transport", "container", "none"}[index], func(t *testing.T) {
			dir := testV5PackageSource(t, index)
			if index == 0 {
				name := filepath.Join(dir, PluginV5ManifestFile)
				raw, err := os.ReadFile(name)
				if err != nil {
					t.Fatal(err)
				}
				var m PluginV5Manifest
				if json.Unmarshal(raw, &m) != nil {
					t.Fatal("manifest")
				}
				m.Backend.Transport = "http"
				raw, _ = json.Marshal(m)
				if err := os.WriteFile(name, raw, 0600); err != nil {
					t.Fatal(err)
				}
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
			catalog, err := NewPluginV5PackageCatalog(trust)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := catalog.CaptureWasmProgram(context.Background(), archive, release); err != ErrPluginV5ManifestShape {
				t.Fatal("unsupported artifact captured", err)
			}
			if _, err := catalog.CaptureWasmProgram(context.Background(), archive, []byte(`{}`)); err == nil {
				t.Fatal("unsigned artifact captured")
			}
		})
	}
	if len((V5WasmProgram{}).Bytes()) != 0 {
		t.Fatal("zero program")
	}
}
