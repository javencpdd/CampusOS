package service

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"
)

var ErrMFASecretUnavailable = errors.New("identity MFA encryption key is unavailable")

const mfaSecretAAD = "campusos.identity.mfa.totp.v1"

// MFASecretProtector is a compact key-ring AEAD envelope. Key material is
// derived in memory from deployment-injected secret strings; it is never
// serialized into a database row, response, log, resource package or plugin
// configuration.
type MFASecretProtector struct {
	activeKeyID string
	keys        map[string][]byte
}

func NewMFASecretProtector(activeKeyID string, source map[string]string) (*MFASecretProtector, error) {
	activeKeyID = strings.TrimSpace(activeKeyID)
	if activeKeyID == "" {
		return nil, ErrMFASecretUnavailable
	}
	keys := make(map[string][]byte, len(source))
	for keyID, secret := range source {
		keyID = strings.TrimSpace(keyID)
		secret = strings.TrimSpace(secret)
		if keyID == "" || len(secret) < 16 {
			continue
		}
		digest := sha256.Sum256([]byte(secret))
		keys[keyID] = digest[:]
	}
	if len(keys[activeKeyID]) == 0 {
		return nil, ErrMFASecretUnavailable
	}
	return &MFASecretProtector{activeKeyID: activeKeyID, keys: keys}, nil
}

func (p *MFASecretProtector) ActiveKeyID() string {
	if p == nil {
		return ""
	}
	return p.activeKeyID
}

func (p *MFASecretProtector) Seal(plaintext []byte) (keyID, nonce, ciphertext string, err error) {
	if p == nil || len(plaintext) == 0 || len(p.keys[p.activeKeyID]) == 0 {
		return "", "", "", ErrMFASecretUnavailable
	}
	block, err := aes.NewCipher(p.keys[p.activeKeyID])
	if err != nil {
		return "", "", "", fmt.Errorf("initialize MFA secret cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return "", "", "", fmt.Errorf("initialize MFA secret envelope: %w", err)
	}
	nonceBytes := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonceBytes); err != nil {
		return "", "", "", fmt.Errorf("read MFA envelope nonce: %w", err)
	}
	sealed := aead.Seal(nil, nonceBytes, plaintext, []byte(mfaSecretAAD))
	return p.activeKeyID, base64.RawURLEncoding.EncodeToString(nonceBytes), base64.RawURLEncoding.EncodeToString(sealed), nil
}

func (p *MFASecretProtector) Open(keyID, nonce, ciphertext string) ([]byte, error) {
	if p == nil || strings.TrimSpace(keyID) == "" {
		return nil, ErrMFASecretUnavailable
	}
	key := p.keys[strings.TrimSpace(keyID)]
	if len(key) == 0 {
		return nil, ErrMFASecretUnavailable
	}
	nonceBytes, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(nonce))
	if err != nil {
		return nil, ErrMFASecretUnavailable
	}
	ciphertextBytes, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(ciphertext))
	if err != nil {
		return nil, ErrMFASecretUnavailable
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, ErrMFASecretUnavailable
	}
	aead, err := cipher.NewGCM(block)
	if err != nil || len(nonceBytes) != aead.NonceSize() {
		return nil, ErrMFASecretUnavailable
	}
	plaintext, err := aead.Open(nil, nonceBytes, ciphertextBytes, []byte(mfaSecretAAD))
	if err != nil {
		return nil, ErrMFASecretUnavailable
	}
	return plaintext, nil
}
