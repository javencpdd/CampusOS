package plugin

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/campusos/CampusOS/pkg/idgen"
)

type SecretMetadata struct {
	ID          int64                  `json:"id"`
	PluginID    int64                  `json:"plugin_id"`
	OwnerUserID *int64                 `json:"owner_user_id,omitempty"`
	SecretName  string                 `json:"secret_name"`
	KeyVersion  string                 `json:"key_version"`
	Algorithm   string                 `json:"algorithm"`
	Status      string                 `json:"status"`
	Metadata    map[string]interface{} `json:"metadata"`
	CreatedAt   time.Time              `json:"created_at"`
	RotatedAt   *time.Time             `json:"rotated_at,omitempty"`
	RevokedAt   *time.Time             `json:"revoked_at,omitempty"`
	MaskedValue string                 `json:"masked_value"`
	Ciphertext  []byte                 `json:"-"`
	Nonce       []byte                 `json:"-"`
}

type SecretStore interface {
	PutSecret(context.Context, SecretMetadata) (SecretMetadata, error)
	ActiveSecret(context.Context, int64, *int64, string) (SecretMetadata, error)
	ListSecrets(context.Context, int64, *int64) ([]SecretMetadata, error)
	RevokeSecret(context.Context, int64, *int64, string) error
}

type SecretService struct {
	store      SecretStore
	aead       cipher.AEAD
	keyVersion string
	now        func() time.Time
}

func NewSecretService(store SecretStore, key []byte, keyVersion string) (*SecretService, error) {
	if store == nil {
		return nil, errors.New("plugin secret store is unavailable")
	}
	if len(key) != 32 {
		return nil, errors.New("plugin secret key must be exactly 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(keyVersion) == "" {
		keyVersion = "v1"
	}
	return &SecretService{store: store, aead: aead, keyVersion: keyVersion, now: time.Now}, nil
}

func NewSecretServiceFromEnv(store SecretStore) (*SecretService, error) {
	raw := strings.TrimSpace(os.Getenv("CAMPUSOS_PLUGIN_SECRET_KEY"))
	if raw == "" {
		return nil, errors.New("CAMPUSOS_PLUGIN_SECRET_KEY is not configured")
	}
	key, err := decodeSecretKey(raw)
	if err != nil {
		return nil, err
	}
	return NewSecretService(store, key, strings.TrimSpace(os.Getenv("CAMPUSOS_PLUGIN_SECRET_KEY_VERSION")))
}

func decodeSecretKey(raw string) ([]byte, error) {
	if value, err := hex.DecodeString(raw); err == nil && len(value) == 32 {
		return value, nil
	}
	if value, err := base64.StdEncoding.DecodeString(raw); err == nil && len(value) == 32 {
		return value, nil
	}
	return nil, errors.New("CAMPUSOS_PLUGIN_SECRET_KEY must be 64 hexadecimal characters or base64 for 32 bytes")
}

func (s *SecretService) Available() bool { return s != nil && s.store != nil && s.aead != nil }
func secretAAD(pluginID int64, owner *int64, name string) []byte {
	ownerText := "system"
	if owner != nil {
		ownerText = strconv.FormatInt(*owner, 10)
	}
	return []byte(fmt.Sprintf("campusos-secret/v1:%d:%s:%s", pluginID, ownerText, name))
}

func (s *SecretService) Put(ctx context.Context, pluginID int64, owner *int64, name, value string, actor *int64) (SecretMetadata, error) {
	if !s.Available() {
		return SecretMetadata{}, errors.New("plugin secret service is unavailable")
	}
	if !regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.-]{0,127}$`).MatchString(name) {
		return SecretMetadata{}, errors.New("secret name is invalid")
	}
	if value == "" || len(value) > 64*1024 {
		return SecretMetadata{}, errors.New("secret value must contain 1 to 65536 bytes")
	}
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return SecretMetadata{}, err
	}
	ciphertext := s.aead.Seal(nil, nonce, []byte(value), secretAAD(pluginID, owner, name))
	metadata := map[string]interface{}{}
	if actor != nil {
		metadata["created_by"] = strconv.FormatInt(*actor, 10)
	}
	return s.store.PutSecret(ctx, SecretMetadata{ID: idgen.New(), PluginID: pluginID, OwnerUserID: owner, SecretName: name, KeyVersion: s.keyVersion, Algorithm: "aes-256-gcm", Status: "active", Metadata: metadata, CreatedAt: s.now(), MaskedValue: "••••••••", Ciphertext: ciphertext, Nonce: nonce})
}

func (s *SecretService) Resolve(ctx context.Context, pluginID int64, owner *int64, name string) (string, error) {
	if !s.Available() {
		return "", errors.New("plugin secret service is unavailable")
	}
	record, err := s.store.ActiveSecret(ctx, pluginID, owner, name)
	if err != nil {
		return "", err
	}
	if record.Algorithm != "aes-256-gcm" || record.KeyVersion != s.keyVersion {
		return "", errors.New("plugin secret key version is unavailable; rotate the secret")
	}
	plaintext, err := s.aead.Open(nil, record.Nonce, record.Ciphertext, secretAAD(pluginID, owner, name))
	if err != nil {
		return "", errors.New("plugin secret cannot be decrypted")
	}
	return string(plaintext), nil
}

func (s *SecretService) List(ctx context.Context, pluginID int64, owner *int64) ([]SecretMetadata, error) {
	return s.store.ListSecrets(ctx, pluginID, owner)
}
func (s *SecretService) Revoke(ctx context.Context, pluginID int64, owner *int64, name string) error {
	return s.store.RevokeSecret(ctx, pluginID, owner, name)
}

type MemorySecretStore struct {
	mu    sync.RWMutex
	items map[string]SecretMetadata
}

func NewMemorySecretStore() *MemorySecretStore {
	return &MemorySecretStore{items: map[string]SecretMetadata{}}
}
func memorySecretKey(pluginID int64, owner *int64, name string) string {
	return string(secretAAD(pluginID, owner, name))
}
func (s *MemorySecretStore) PutSecret(_ context.Context, value SecretMetadata) (SecretMetadata, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := memorySecretKey(value.PluginID, value.OwnerUserID, value.SecretName)
	if old, ok := s.items[key]; ok {
		now := time.Now()
		old.Status = "rotated"
		old.RotatedAt = &now
	}
	s.items[key] = value
	return maskSecret(value), nil
}
func (s *MemorySecretStore) ActiveSecret(_ context.Context, pluginID int64, owner *int64, name string) (SecretMetadata, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	value, ok := s.items[memorySecretKey(pluginID, owner, name)]
	if !ok || value.Status != "active" {
		return SecretMetadata{}, ErrMarketNotFound
	}
	return value, nil
}
func (s *MemorySecretStore) ListSecrets(_ context.Context, pluginID int64, owner *int64) ([]SecretMetadata, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := []SecretMetadata{}
	for _, value := range s.items {
		if value.PluginID == pluginID && sameOptionalInt64(value.OwnerUserID, owner) && value.Status == "active" {
			result = append(result, maskSecret(value))
		}
	}
	return result, nil
}
func (s *MemorySecretStore) RevokeSecret(_ context.Context, pluginID int64, owner *int64, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := memorySecretKey(pluginID, owner, name)
	value, ok := s.items[key]
	if !ok {
		return ErrMarketNotFound
	}
	now := time.Now()
	value.Status = "revoked"
	value.RevokedAt = &now
	s.items[key] = value
	return nil
}
func sameOptionalInt64(left, right *int64) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}
func maskSecret(value SecretMetadata) SecretMetadata {
	value.Ciphertext = nil
	value.Nonce = nil
	value.MaskedValue = "••••••••"
	return value
}
