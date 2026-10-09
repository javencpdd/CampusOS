package security

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"errors"
	"testing"
)

func TestKeyringRotationAndAuthentication(t *testing.T) {
	oldKey := []byte("0123456789abcdef0123456789abcdef")
	newKey := []byte("abcdef0123456789abcdef0123456789")
	oldRing, err := NewKeyring("v1", map[string][]byte{"v1": oldKey})
	if err != nil {
		t.Fatal(err)
	}
	const secret = "only-host-reads-this-secret"
	aad := []byte("plugin:42:user:7:mail.password")
	oldEnvelope, err := oldRing.Seal([]byte(secret), aad)
	if err != nil {
		t.Fatal(err)
	}
	if oldEnvelope.KeyID != "v1" || oldEnvelope.Algorithm != AlgorithmAES256GCM ||
		bytes.Contains(oldEnvelope.Ciphertext, []byte(secret)) || len(oldEnvelope.Nonce) != 12 {
		t.Fatalf("invalid encrypted envelope: %+v", oldEnvelope)
	}

	rotatedRing, err := NewKeyring("v2", map[string][]byte{"v1": oldKey, "v2": newKey})
	if err != nil {
		t.Fatal(err)
	}
	opened, err := rotatedRing.Open(oldEnvelope, aad)
	if err != nil || string(opened) != secret {
		t.Fatalf("old secret unavailable during rotation: value=%q err=%v", opened, err)
	}
	newEnvelope, err := rotatedRing.Seal([]byte("new-secret"), aad)
	if err != nil || newEnvelope.KeyID != "v2" {
		t.Fatalf("new writes must use active key: %+v err=%v", newEnvelope, err)
	}
	if opened, err := rotatedRing.Open(newEnvelope, aad); err != nil || string(opened) != "new-secret" {
		t.Fatalf("new secret unavailable: value=%q err=%v", opened, err)
	}
	if _, err := oldRing.Open(newEnvelope, aad); !errors.Is(err, ErrKeyUnavailable) {
		t.Fatalf("missing new key was accepted: %v", err)
	}
	newOnly, err := NewKeyring("v2", map[string][]byte{"v2": newKey})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := newOnly.Open(oldEnvelope, aad); !errors.Is(err, ErrKeyUnavailable) {
		t.Fatalf("retired read key silently opened old record: %v", err)
	}

	wrongAAD := []byte("plugin:42:user:8:mail.password")
	if _, err := rotatedRing.Open(oldEnvelope, wrongAAD); !errors.Is(err, ErrAuthentication) {
		t.Fatalf("wrong AAD was accepted: %v", err)
	}
	changed := oldEnvelope
	changed.Ciphertext = bytes.Clone(oldEnvelope.Ciphertext)
	changed.Ciphertext[0] ^= 0xff
	if _, err := rotatedRing.Open(changed, aad); !errors.Is(err, ErrAuthentication) {
		t.Fatalf("tampered ciphertext was accepted: %v", err)
	}
	changed = oldEnvelope
	changed.Nonce = bytes.Clone(oldEnvelope.Nonce)
	changed.Nonce[0] ^= 0xff
	if _, err := rotatedRing.Open(changed, aad); !errors.Is(err, ErrAuthentication) {
		t.Fatalf("tampered nonce was accepted: %v", err)
	}
	changed = oldEnvelope
	changed.Algorithm = "aes-128-gcm"
	if _, err := rotatedRing.Open(changed, aad); !errors.Is(err, ErrInvalidEnvelope) {
		t.Fatalf("unknown algorithm was accepted: %v", err)
	}
}

func TestKeyringReadsLegacyPluginAESGCMRows(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	aad := []byte("campusos-secret/v1:42:7:mail.password")
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	legacyAEAD, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	nonce := []byte("0123456789ab")
	legacyEnvelope := Envelope{
		KeyID: "v1", Algorithm: AlgorithmAES256GCM, Nonce: nonce,
		Ciphertext: legacyAEAD.Seal(nil, nonce, []byte("legacy-value"), aad),
	}
	ring, err := NewKeyring("v1", map[string][]byte{"v1": key})
	if err != nil {
		t.Fatal(err)
	}
	if opened, err := ring.Open(legacyEnvelope, aad); err != nil || string(opened) != "legacy-value" {
		t.Fatalf("existing row AAD is incompatible: value=%q err=%v", opened, err)
	}
	newEnvelope, err := ring.Seal([]byte("new-value"), aad)
	if err != nil {
		t.Fatal(err)
	}
	if opened, err := legacyAEAD.Open(nil, newEnvelope.Nonce, newEnvelope.Ciphertext, aad); err != nil || string(opened) != "new-value" {
		t.Fatalf("new row AAD is incompatible: value=%q err=%v", opened, err)
	}
}

func TestKeyringCopiesCallerKeyMaterial(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	keys := map[string][]byte{"v1": key}
	ring, err := NewKeyring("v1", keys)
	if err != nil {
		t.Fatal(err)
	}
	for i := range key {
		key[i] = 0
	}
	delete(keys, "v1")
	envelope, err := ring.Seal([]byte("kept"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if opened, err := ring.Open(envelope, nil); err != nil || string(opened) != "kept" {
		t.Fatalf("caller mutated effective key: value=%q err=%v", opened, err)
	}
}

func TestKeyringRejectsInvalidSetupAndEnvelope(t *testing.T) {
	valid := []byte("0123456789abcdef0123456789abcdef")
	for _, tc := range []struct {
		name   string
		active string
		keys   map[string][]byte
	}{
		{"no active ID", "", map[string][]byte{"v1": valid}},
		{"missing active key", "v2", map[string][]byte{"v1": valid}},
		{"short key", "v1", map[string][]byte{"v1": []byte("short")}},
		{"duplicate material", "v1", map[string][]byte{"v1": valid, "v2": bytes.Clone(valid)}},
		{"newline ID", "v1\n", map[string][]byte{"v1\n": valid}},
		{"oversized ID", "v1", map[string][]byte{"v1": valid, "xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx": valid}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewKeyring(tc.active, tc.keys); err == nil {
				t.Fatal("invalid keyring configuration was accepted")
			}
		})
	}
	ring, err := NewKeyring("v1", map[string][]byte{"v1": valid})
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := ring.Seal([]byte("secret"), []byte("scope"))
	if err != nil {
		t.Fatal(err)
	}
	envelope.Nonce = envelope.Nonce[:len(envelope.Nonce)-1]
	if _, err := ring.Open(envelope, []byte("scope")); !errors.Is(err, ErrInvalidEnvelope) {
		t.Fatalf("short nonce was accepted: %v", err)
	}
}
