package plugin

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
)

func TestPluginDirectorySignatureVerifiesAndDetectsTampering(t *testing.T) {
	dir := t.TempDir()
	manifest := []byte("api_version: campusos.plugin/v2\nhost_api_version: v2\nname: signed-plugin\nversion: 1.0.0\nruntime: grpc\nscope: user\ntype: external\n")
	if err := os.WriteFile(filepath.Join(dir, "plugin.yaml"), manifest, 0o640); err != nil {
		t.Fatal(err)
	}
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := SignPluginDirectory(dir, "test-key", base64.StdEncoding.EncodeToString(privateKey)); err != nil {
		t.Fatalf("sign: %v", err)
	}
	status, err := VerifyPluginDirectorySignature(dir, &PluginTrustStore{Keys: []TrustedSigningKey{{ID: "test-key", Algorithm: "ed25519", PublicKey: base64.StdEncoding.EncodeToString(publicKey)}}})
	if err != nil || status != "verified" {
		t.Fatalf("verify = %q, %v", status, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "plugin.yaml"), append(manifest, []byte("description: changed\n")...), 0o640); err != nil {
		t.Fatal(err)
	}
	status, err = VerifyPluginDirectorySignature(dir, &PluginTrustStore{Keys: []TrustedSigningKey{{ID: "test-key", Algorithm: "ed25519", PublicKey: base64.StdEncoding.EncodeToString(publicKey)}}})
	if status != "invalid" || err == nil {
		t.Fatalf("tampered verify = %q, %v; want invalid error", status, err)
	}
}
