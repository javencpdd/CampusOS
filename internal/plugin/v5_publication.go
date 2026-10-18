package plugin

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"strings"
	"sync"
)

func v5Marshal(v any) ([]byte, error) { return json.Marshal(v) }

// CurrentPluginV5Catalogs publishes host-owned package contracts, not runnable
// providers. No v5 Host capability is callable before the 03c Dispatcher.
// In particular this catalog contains no synthetic Knowledge collections.
func CurrentPluginV5Catalogs() ([]byte, []byte) {
	host, _ := json.Marshal(v5HostManifestCatalog{Contract: "campusos.host-capabilities/v1", HostContract: "campusos.host/v1", Items: []v5HostCapability{}})
	points := []v5ExtensionDescriptor{
		{Point: "preview.renderer", ContractVersion: "v1", InputSchema: "campusos.preview.renderer.request/v1", OutputSchema: "campusos.preview.renderer.response/v1", Capabilities: []string{"pdf"}, DataModes: []string{"authorized_resource"}},
		{Point: "ai.chat", ContractVersion: "v1", InputSchema: "campusos.ai.chat.request/v1", OutputSchema: "campusos.ai.chat.response/v1", Capabilities: []string{"text"}, DataModes: []string{"public_only", "no_host_private_data"}},
		{Point: "knowledge.source", ContractVersion: "v1", InputSchema: "campusos.knowledge.source.request/v1", OutputSchema: "campusos.knowledge.source.response/v1", Capabilities: []string{"public_text"}, DataModes: []string{"public_only"}},
	}
	ext, _ := json.Marshal(v5ExtensionManifestCatalog{Contract: "campusos.extension-catalog/v1", HostContract: "campusos.host/v1", Points: points})
	return host, ext
}

// ValidatePluginV5SourceDeclaration uses actual host metadata but does not
// pretend a source declaration is a signed or verified release.
func ValidatePluginV5SourceDeclaration(raw []byte) (*PluginV5Manifest, error) {
	m, err := ParsePluginV5Manifest(raw)
	if err != nil {
		return nil, err
	}
	host, ext := CurrentPluginV5Catalogs()
	r := v5ReleaseEnvelope{Publisher: m.Publisher, Key: m.Key, Version: m.Version, PackageDigest: "sha256:" + strings.Repeat("0", 64)}
	r.Signature.KeyID = "declaration-only"
	r.Signature.Value = "not-a-signature"
	wire, err := json.Marshal(r)
	if err != nil {
		return nil, ErrPluginV5PackageInvalid
	}
	return ValidatePluginV5Manifest(raw, host, ext, wire)
}

type PluginV5TrustKey struct {
	Publisher string `json:"publisher"`
	KeyID     string `json:"key_id"`
	PublicKey string `json:"public_key"`
	Status    string `json:"status"`
}
type PluginV5TrustStore struct {
	Contract string             `json:"contract"`
	Keys     []PluginV5TrustKey `json:"keys"`
}

func parseV5Trust(raw []byte) (PluginV5TrustStore, error) {
	v, ok := v5JSON(raw)
	if !ok || !v5Object("contract keys", map[string]v5Check{
		"contract": v5Enum("campusos.plugin-trust/v1"),
		"keys":     v5Array(0, 256, false, v5Object("publisher key_id public_key status", map[string]v5Check{"publisher": v5Name, "key_id": v5String(1, 128, `^[a-zA-Z0-9_.-]+$`), "public_key": v5String(44, 44, `^[a-zA-Z0-9+/]+={0,2}$`), "status": v5Enum("active", "revoked")})),
	})(v) {
		return PluginV5TrustStore{}, ErrPluginV5PackageSignature
	}
	wire, _ := json.Marshal(v)
	var trust PluginV5TrustStore
	if json.Unmarshal(wire, &trust) != nil {
		return trust, ErrPluginV5PackageSignature
	}
	seen := map[string]bool{}
	for _, key := range trust.Keys {
		id := key.Publisher + "/" + key.KeyID
		pub, err := base64.StdEncoding.Strict().DecodeString(key.PublicKey)
		if seen[id] || err != nil || len(pub) != ed25519.PublicKeySize {
			return PluginV5TrustStore{}, ErrPluginV5PackageSignature
		}
		seen[id] = true
	}
	return trust, nil
}

func v5SignatureMessage(r v5ReleaseEnvelope) []byte {
	identity := struct {
		Publisher string `json:"publisher"`
		Key       string `json:"key"`
		Version   string `json:"version"`
		Digest    string `json:"package_digest"`
		KeyID     string `json:"key_id"`
	}{r.Publisher, r.Key, r.Version, r.PackageDigest, r.Signature.KeyID}
	raw, _ := json.Marshal(identity)
	return append([]byte("campusos.plugin-release/v5\x00"), raw...)
}

// SignPluginV5Package signs the full compressed package digest plus immutable
// identity and key ID. The envelope is detached to avoid self-digest cycles.
func SignPluginV5Package(raw []byte, keyID string, private ed25519.PrivateKey) ([]byte, error) {
	p, err := InspectPluginV5Package(raw)
	if err != nil {
		return nil, err
	}
	if !v5String(1, 128, `^[a-zA-Z0-9_.-]+$`)(keyID) || len(private) != ed25519.PrivateKeySize {
		return nil, ErrPluginV5PackageSignature
	}
	// Reject inconsistent 64-byte private key encodings.
	if !bytes.Equal(ed25519.NewKeyFromSeed(private[:ed25519.SeedSize]), private) {
		return nil, ErrPluginV5PackageSignature
	}
	r := v5ReleaseEnvelope{Publisher: p.Manifest.Publisher, Key: p.Manifest.Key, Version: p.Manifest.Version, PackageDigest: p.Digest}
	r.Signature.KeyID = keyID
	r.Signature.Value = base64.StdEncoding.EncodeToString(ed25519.Sign(private, v5SignatureMessage(r)))
	return json.Marshal(r)
}

func VerifyPluginV5Release(raw, envelope, trustBytes []byte) (*PluginV5Package, error) {
	p, err := InspectPluginV5Package(raw)
	if err != nil {
		return nil, err
	}
	host, ext := CurrentPluginV5Catalogs()
	if _, err = ValidatePluginV5Manifest(p.files[PluginV5ManifestFile], host, ext, envelope); err != nil {
		return nil, err
	}
	var r v5ReleaseEnvelope
	if json.Unmarshal(envelope, &r) != nil {
		return nil, ErrPluginV5PackageSignature
	}
	if r.PackageDigest != p.Digest {
		return nil, ErrPluginV5PackageDigest
	}
	trust, err := parseV5Trust(trustBytes)
	if err != nil {
		return nil, err
	}
	sig, err := base64.StdEncoding.Strict().DecodeString(r.Signature.Value)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return nil, ErrPluginV5PackageSignature
	}
	for _, key := range trust.Keys {
		if key.Publisher == r.Publisher && key.KeyID == r.Signature.KeyID && key.Status == "active" {
			pub, err := base64.StdEncoding.Strict().DecodeString(key.PublicKey)
			if err == nil && ed25519.Verify(pub, v5SignatureMessage(r), sig) {
				return p, nil
			}
		}
	}
	return nil, ErrPluginV5PackageSignature
}

// PluginV5PackageCatalog is distinct from Manager and stores immutable package
// identities only. A recorded identity never replaces current signature/trust
// verification. Installation/operation state belongs to subsequent 03b slices.
type PluginV5PackageCatalog struct {
	mu    sync.Mutex
	trust []byte
	store PluginV5IdentityStore
}

func NewPluginV5PackageCatalog(trust []byte) (*PluginV5PackageCatalog, error) {
	return NewPluginV5PackageCatalogWithIdentityStore(trust, NewMemoryPluginV5IdentityStore())
}

// NewPluginV5PackageCatalogWithIdentityStore binds host-owned persistence. A
// missing store is an error; callers must explicitly choose a memory adapter.
func NewPluginV5PackageCatalogWithIdentityStore(trust []byte, store PluginV5IdentityStore) (*PluginV5PackageCatalog, error) {
	if _, err := parseV5Trust(trust); err != nil {
		return nil, err
	}
	if store == nil {
		return nil, ErrPluginV5IdentityUnavailable
	}
	return &PluginV5PackageCatalog{trust: bytes.Clone(trust), store: store}, nil
}
func (c *PluginV5PackageCatalog) SetTrust(trust []byte) error {
	if c == nil {
		return ErrPluginV5PackageSignature
	}
	if _, err := parseV5Trust(trust); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.trust = bytes.Clone(trust)
	return nil
}
func (c *PluginV5PackageCatalog) VerifyAndRecord(raw, envelope []byte) (*PluginV5Package, error) {
	return c.VerifyAndRecordContext(context.Background(), raw, envelope)
}

func (c *PluginV5PackageCatalog) VerifyAndRecordContext(ctx context.Context, raw, envelope []byte) (*PluginV5Package, error) {
	var result *PluginV5Package
	err := c.WithVerified(ctx, raw, envelope, func(p *PluginV5Package) error { result = p; return nil })
	return result, err
}

// WithVerified retains this host's current trust through the caller's durable
// command. A callback failure does not erase the write-once known identity.
// The callback is host-only; it must never call SetTrust or this catalog again.
func (c *PluginV5PackageCatalog) WithVerified(ctx context.Context, raw, envelope []byte, action func(*PluginV5Package) error) error {
	if c == nil {
		return ErrPluginV5PackageSignature
	}
	// Hold the current trust and immutable identity check through publication;
	// a committed revocation cannot be bypassed by a cached verification.
	c.mu.Lock()
	defer c.mu.Unlock()
	if ctx.Err() != nil || c.store == nil {
		return ErrPluginV5IdentityUnavailable
	}
	p, err := VerifyPluginV5Release(raw, envelope, c.trust)
	if err != nil {
		return err
	}
	id := PluginV5PackageIdentity{Publisher: p.Manifest.Publisher, Key: p.Manifest.Key, Version: p.Manifest.Version, Digest: p.Digest}
	if err = c.store.RecordPluginV5Identity(ctx, id); err != nil {
		return err
	}
	if action == nil {
		return ErrV5Invalid
	}
	return action(p)
}
