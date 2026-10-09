package security

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"strings"
)

const AlgorithmAES256GCM = "aes-256-gcm"

var (
	ErrInvalidEnvelope = errors.New("invalid encrypted envelope")
	ErrKeyUnavailable  = errors.New("encryption key version is unavailable")
	ErrAuthentication  = errors.New("encrypted envelope authentication failed")
)

// Envelope contains only encrypted material and the key identifier needed to
// choose a read key. It must not contain the underlying key or plaintext.
type Envelope struct {
	KeyID      string
	Algorithm  string
	Nonce      []byte
	Ciphertext []byte
}

// Keyring encrypts with its active key and can decrypt records written under
// any configured key ID. The constructor builds AEADs immediately, so later
// mutations to the caller's keys or map cannot change the effective keys.
type Keyring struct {
	activeID string
	aeads    map[string]cipher.AEAD
}

// ActiveKeyID identifies the key used for new envelopes. It does not expose
// key material.
func (k *Keyring) ActiveKeyID() string {
	if k == nil {
		return ""
	}
	return k.activeID
}

func NewKeyring(activeID string, keys map[string][]byte) (*Keyring, error) {
	if !validKeyID(activeID) {
		return nil, errors.New("active encryption key ID is invalid")
	}
	if len(keys) == 0 {
		return nil, errors.New("encryption keys are required")
	}
	aeads := make(map[string]cipher.AEAD, len(keys))
	seenKeyMaterial := make(map[[32]byte]struct{}, len(keys))
	for id, key := range keys {
		if !validKeyID(id) {
			return nil, errors.New("encryption key ID is invalid")
		}
		if len(key) != 32 {
			return nil, errors.New("encryption key must be exactly 32 bytes")
		}
		var keyBytes [32]byte
		copy(keyBytes[:], key)
		if _, duplicate := seenKeyMaterial[keyBytes]; duplicate {
			return nil, errors.New("encryption key material must be unique per key ID")
		}
		seenKeyMaterial[keyBytes] = struct{}{}
		keyCopy := append([]byte(nil), key...)
		block, err := aes.NewCipher(keyCopy)
		clear(keyCopy)
		if err != nil {
			return nil, errors.New("encryption key is invalid")
		}
		aead, err := cipher.NewGCM(block)
		if err != nil {
			return nil, errors.New("encryption key is invalid")
		}
		aeads[id] = aead
	}
	if _, ok := aeads[activeID]; !ok {
		return nil, errors.New("active encryption key is unavailable")
	}
	return &Keyring{activeID: activeID, aeads: aeads}, nil
}

func validKeyID(id string) bool {
	return id != "" && len(id) <= 64 && id == strings.TrimSpace(id) &&
		!strings.ContainsAny(id, "\x00\r\n")
}

func (k *Keyring) Seal(plaintext, aad []byte) (Envelope, error) {
	if k == nil {
		return Envelope{}, ErrKeyUnavailable
	}
	aead, ok := k.aeads[k.activeID]
	if !ok {
		return Envelope{}, ErrKeyUnavailable
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return Envelope{}, errors.New("encryption nonce generation failed")
	}
	return Envelope{
		KeyID:      k.activeID,
		Algorithm:  AlgorithmAES256GCM,
		Nonce:      nonce,
		Ciphertext: aead.Seal(nil, nonce, plaintext, aad),
	}, nil
}

func (k *Keyring) Open(envelope Envelope, aad []byte) ([]byte, error) {
	if k == nil || envelope.Algorithm != AlgorithmAES256GCM || !validKeyID(envelope.KeyID) {
		return nil, ErrInvalidEnvelope
	}
	aead, ok := k.aeads[envelope.KeyID]
	if !ok {
		return nil, ErrKeyUnavailable
	}
	if len(envelope.Nonce) != aead.NonceSize() || len(envelope.Ciphertext) < aead.Overhead() {
		return nil, ErrInvalidEnvelope
	}
	plaintext, err := aead.Open(nil, envelope.Nonce, envelope.Ciphertext, aad)
	if err != nil {
		return nil, ErrAuthentication
	}
	return plaintext, nil
}
