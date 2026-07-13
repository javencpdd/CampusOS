package plugin

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const packageSignaturePath = ".campusos/signature.json"

var ErrPackageSignature = errors.New("plugin package signature is invalid")

// PackageSignature is signed over a deterministic digest of every package
// file except this envelope. That avoids a self-referential archive checksum.
type PackageSignature struct {
	Version       string `json:"version"`
	Algorithm     string `json:"algorithm"`
	KeyID         string `json:"key_id"`
	ContentDigest string `json:"content_digest"`
	Signature     string `json:"signature"`
}

type TrustedSigningKey struct {
	ID        string `json:"id"`
	Algorithm string `json:"algorithm"`
	PublicKey string `json:"public_key"`
}

type PluginTrustStore struct {
	Keys []TrustedSigningKey `json:"keys"`
}

func LoadPluginTrustStore(path string) (*PluginTrustStore, error) {
	if strings.TrimSpace(path) == "" {
		return &PluginTrustStore{}, nil
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &PluginTrustStore{}, nil
	}
	if err != nil {
		return nil, err
	}
	store := &PluginTrustStore{}
	if err := json.Unmarshal(data, store); err != nil {
		return nil, fmt.Errorf("parse plugin trust store: %w", err)
	}
	return store, nil
}

func PackageContentDigest(pluginDir string) (string, error) {
	files, err := listPackageFiles(pluginDir)
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	for _, rel := range files {
		if filepath.ToSlash(rel) == packageSignaturePath {
			continue
		}
		path := filepath.Join(pluginDir, filepath.FromSlash(rel))
		data, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		_, _ = hash.Write([]byte(filepath.ToSlash(rel)))
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write(data)
		_, _ = hash.Write([]byte{0})
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil)), nil
}

// SignPluginDirectory writes a signature envelope that can safely be packed
// with the plugin. The supplied key is the base64 encoding of an Ed25519
// private key; it is never stored by CampusOS.
func SignPluginDirectory(pluginDir, keyID, privateKeyBase64 string) (*PackageSignature, error) {
	keyBytes, err := base64.StdEncoding.DecodeString(strings.TrimSpace(privateKeyBase64))
	if err != nil || len(keyBytes) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("%w: expected base64 Ed25519 private key", ErrPackageSignature)
	}
	if strings.TrimSpace(keyID) == "" {
		return nil, fmt.Errorf("%w: signing key id is required", ErrPackageSignature)
	}
	digest, err := PackageContentDigest(pluginDir)
	if err != nil {
		return nil, err
	}
	signature := ed25519.Sign(ed25519.PrivateKey(keyBytes), []byte(digest))
	envelope := &PackageSignature{Version: "v1", Algorithm: "ed25519", KeyID: keyID, ContentDigest: digest, Signature: base64.StdEncoding.EncodeToString(signature)}
	data, err := json.MarshalIndent(envelope, "", "  ")
	if err != nil {
		return nil, err
	}
	path := filepath.Join(pluginDir, filepath.FromSlash(packageSignaturePath))
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o640); err != nil {
		return nil, err
	}
	return envelope, nil
}

func VerifyPluginDirectorySignature(pluginDir string, trust *PluginTrustStore) (string, error) {
	path := filepath.Join(pluginDir, filepath.FromSlash(packageSignaturePath))
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "unsigned", nil
	}
	if err != nil {
		return "invalid", err
	}
	envelope := PackageSignature{}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return "invalid", fmt.Errorf("%w: parse signature envelope: %v", ErrPackageSignature, err)
	}
	if envelope.Version != "v1" || envelope.Algorithm != "ed25519" || envelope.KeyID == "" || envelope.ContentDigest == "" || envelope.Signature == "" {
		return "invalid", fmt.Errorf("%w: unsupported or incomplete signature envelope", ErrPackageSignature)
	}
	digest, err := PackageContentDigest(pluginDir)
	if err != nil {
		return "invalid", err
	}
	if digest != envelope.ContentDigest {
		return "invalid", fmt.Errorf("%w: package content digest mismatch", ErrPackageSignature)
	}
	if trust == nil {
		return "untrusted", nil
	}
	for _, key := range trust.Keys {
		if key.ID != envelope.KeyID || key.Algorithm != "ed25519" {
			continue
		}
		publicKey, decodeErr := base64.StdEncoding.DecodeString(key.PublicKey)
		signature, signatureErr := base64.StdEncoding.DecodeString(envelope.Signature)
		if decodeErr != nil || signatureErr != nil || len(publicKey) != ed25519.PublicKeySize || len(signature) != ed25519.SignatureSize {
			return "invalid", fmt.Errorf("%w: malformed trusted key or signature", ErrPackageSignature)
		}
		if !ed25519.Verify(ed25519.PublicKey(publicKey), []byte(digest), signature) {
			return "invalid", fmt.Errorf("%w: verification failed", ErrPackageSignature)
		}
		return "verified", nil
	}
	return "untrusted", nil
}
