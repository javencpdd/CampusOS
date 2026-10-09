package plugin

import (
	"bytes"
	"context"
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

	"github.com/campusos/CampusOS/internal/platform/security"
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

// ErrSecretChanged means an active secret was replaced, revoked, or rewrapped
// after it was read for a conditional key-material update.
var ErrSecretChanged = errors.New("plugin secret changed during update")

type SecretStore interface {
	PutSecret(context.Context, SecretMetadata) (SecretMetadata, error)
	ActiveSecret(context.Context, int64, *int64, string) (SecretMetadata, error)
	// ReplaceSecretIfCurrent conditionally updates only the encrypted material
	// of the same active row and observed envelope; all other fields remain unchanged.
	ReplaceSecretIfCurrent(context.Context, SecretMetadata, SecretMetadata) (SecretMetadata, error)
	ListSecrets(context.Context, int64, *int64) ([]SecretMetadata, error)
	RevokeSecret(context.Context, int64, *int64, string) error
}

type SecretService struct {
	store   SecretStore
	keyring *security.Keyring
	now     func() time.Time
}

// NewSecretService preserves the existing single-key constructor and envelope
// format. All encryption now passes through the shared platform keyring.
func NewSecretService(store SecretStore, key []byte, keyVersion string) (*SecretService, error) {
	if len(key) != 32 {
		return nil, errors.New("plugin secret key must be exactly 32 bytes")
	}
	if strings.TrimSpace(keyVersion) == "" {
		keyVersion = "v1"
	}
	keyring, err := security.NewKeyring(keyVersion, map[string][]byte{keyVersion: key})
	if err != nil {
		return nil, err
	}
	return NewSecretServiceWithKeyring(store, keyring)
}

func NewSecretServiceWithKeyring(store SecretStore, keyring *security.Keyring) (*SecretService, error) {
	if store == nil {
		return nil, errors.New("plugin secret store is unavailable")
	}
	if keyring == nil {
		return nil, errors.New("plugin secret keyring is unavailable")
	}
	return &SecretService{store: store, keyring: keyring, now: time.Now}, nil
}

func NewSecretServiceFromEnv(store SecretStore) (*SecretService, error) {
	activeID := strings.TrimSpace(os.Getenv("CAMPUSOS_SECRET_ACTIVE_KEY_ID"))
	encodedKeys := strings.TrimSpace(os.Getenv("CAMPUSOS_SECRET_ENCRYPTION_KEYS"))
	if activeID != "" || encodedKeys != "" {
		if activeID == "" || encodedKeys == "" {
			return nil, errors.New("CAMPUSOS_SECRET_ACTIVE_KEY_ID and CAMPUSOS_SECRET_ENCRYPTION_KEYS must be configured together")
		}
		keys, err := parseSecretEncryptionKeys(encodedKeys)
		if err != nil {
			return nil, err
		}
		keyring, err := security.NewKeyring(activeID, keys)
		if err != nil {
			return nil, err
		}
		return NewSecretServiceWithKeyring(store, keyring)
	}
	raw := strings.TrimSpace(os.Getenv("CAMPUSOS_PLUGIN_SECRET_KEY"))
	if raw == "" {
		return nil, errors.New("CAMPUSOS_SECRET_ENCRYPTION_KEYS or CAMPUSOS_PLUGIN_SECRET_KEY is not configured")
	}
	key, err := decodeSecretKey(raw)
	if err != nil {
		return nil, err
	}
	return NewSecretService(store, key, strings.TrimSpace(os.Getenv("CAMPUSOS_PLUGIN_SECRET_KEY_VERSION")))
}

func parseSecretEncryptionKeys(raw string) (map[string][]byte, error) {
	if len(raw) > 16*1024 {
		return nil, errors.New("secret encryption keyring is too large")
	}
	entries := strings.Split(raw, ",")
	if len(entries) == 0 || len(entries) > 16 {
		return nil, errors.New("secret encryption keyring must contain 1 to 16 keys")
	}
	keys := make(map[string][]byte, len(entries))
	for _, entry := range entries {
		id, encoded, ok := strings.Cut(strings.TrimSpace(entry), ":")
		id = strings.TrimSpace(id)
		if !ok || id == "" || encoded == "" || strings.Contains(encoded, ":") {
			return nil, errors.New("secret encryption keyring entry is invalid")
		}
		if _, duplicate := keys[id]; duplicate {
			return nil, errors.New("secret encryption keyring has a duplicate ID")
		}
		key, err := decodeSecretKey(strings.TrimSpace(encoded))
		if err != nil {
			return nil, err
		}
		keys[id] = key
	}
	return keys, nil
}

func decodeSecretKey(raw string) ([]byte, error) {
	if value, err := hex.DecodeString(raw); err == nil && len(value) == 32 {
		return value, nil
	}
	if value, err := base64.StdEncoding.DecodeString(raw); err == nil && len(value) == 32 {
		return value, nil
	}
	return nil, errors.New("secret encryption key must be 64 hexadecimal characters or base64 for 32 bytes")
}

func (s *SecretService) Available() bool { return s != nil && s.store != nil && s.keyring != nil }
func (s *SecretService) ActiveKeyID() string {
	if !s.Available() {
		return ""
	}
	return s.keyring.ActiveKeyID()
}

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
	envelope, err := s.keyring.Seal([]byte(value), secretAAD(pluginID, owner, name))
	if err != nil {
		return SecretMetadata{}, err
	}
	metadata := map[string]interface{}{}
	if actor != nil {
		metadata["created_by"] = strconv.FormatInt(*actor, 10)
	}
	return s.store.PutSecret(ctx, SecretMetadata{ID: idgen.New(), PluginID: pluginID, OwnerUserID: owner, SecretName: name, KeyVersion: envelope.KeyID, Algorithm: envelope.Algorithm, Status: "active", Metadata: metadata, CreatedAt: s.now(), MaskedValue: "••••••••", Ciphertext: envelope.Ciphertext, Nonce: envelope.Nonce})
}

func (s *SecretService) Resolve(ctx context.Context, pluginID int64, owner *int64, name string) (string, error) {
	if !s.Available() {
		return "", errors.New("plugin secret service is unavailable")
	}
	record, err := s.store.ActiveSecret(ctx, pluginID, owner, name)
	if err != nil {
		return "", err
	}
	plaintext, err := s.keyring.Open(security.Envelope{
		KeyID: record.KeyVersion, Algorithm: record.Algorithm,
		Nonce: record.Nonce, Ciphertext: record.Ciphertext,
	}, secretAAD(pluginID, owner, name))
	if err != nil {
		return "", errors.New("plugin secret cannot be decrypted")
	}
	return string(plaintext), nil
}

// RewrapActive changes only the encrypted material of the current active row.
// Decrypting before the active-key check also detects tampering on no-op calls.
func (s *SecretService) RewrapActive(ctx context.Context, pluginID int64, owner *int64, name string) (SecretMetadata, bool, error) {
	return s.RewrapActiveIfCurrent(ctx, pluginID, owner, name, 0)
}

// RewrapActiveIfCurrent also requires the active row to retain its observed ID.
// A zero expectedID allows callers without a previously observed row to rewrap.
func (s *SecretService) RewrapActiveIfCurrent(ctx context.Context, pluginID int64, owner *int64, name string, expectedID int64) (SecretMetadata, bool, error) {
	if !s.Available() {
		return SecretMetadata{}, false, errors.New("plugin secret service is unavailable")
	}
	record, err := s.store.ActiveSecret(ctx, pluginID, owner, name)
	if err != nil {
		return SecretMetadata{}, false, err
	}
	if expectedID != 0 && record.ID != expectedID {
		return SecretMetadata{}, false, ErrSecretChanged
	}
	aad := secretAAD(pluginID, owner, name)
	plaintext, err := s.keyring.Open(security.Envelope{
		KeyID: record.KeyVersion, Algorithm: record.Algorithm,
		Nonce: record.Nonce, Ciphertext: record.Ciphertext,
	}, aad)
	if err != nil {
		return SecretMetadata{}, false, errors.New("plugin secret cannot be decrypted")
	}
	defer clear(plaintext)
	if record.KeyVersion == s.keyring.ActiveKeyID() {
		return maskSecret(record), false, nil
	}
	envelope, err := s.keyring.Seal(plaintext, aad)
	if err != nil {
		return SecretMetadata{}, false, err
	}
	replacement := record
	replacement.KeyVersion = envelope.KeyID
	replacement.Algorithm = envelope.Algorithm
	replacement.Nonce = envelope.Nonce
	replacement.Ciphertext = envelope.Ciphertext
	updated, err := s.store.ReplaceSecretIfCurrent(ctx, record, replacement)
	if err != nil {
		return SecretMetadata{}, false, err
	}
	return maskSecret(updated), true, nil
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
func sameActiveSecretIdentity(expected, replacement SecretMetadata) bool {
	return expected.ID > 0 && expected.ID == replacement.ID &&
		expected.PluginID == replacement.PluginID &&
		sameOptionalInt64(expected.OwnerUserID, replacement.OwnerUserID) &&
		expected.SecretName == replacement.SecretName &&
		expected.Status == "active" && replacement.Status == "active"
}

func (s *MemorySecretStore) ReplaceSecretIfCurrent(_ context.Context, expected, replacement SecretMetadata) (SecretMetadata, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !sameActiveSecretIdentity(expected, replacement) {
		return SecretMetadata{}, ErrSecretChanged
	}
	key := memorySecretKey(expected.PluginID, expected.OwnerUserID, expected.SecretName)
	current, ok := s.items[key]
	if !ok || current.ID != expected.ID || current.PluginID != expected.PluginID ||
		!sameOptionalInt64(current.OwnerUserID, expected.OwnerUserID) ||
		current.SecretName != expected.SecretName || current.Status != expected.Status || current.RevokedAt != nil ||
		current.KeyVersion != expected.KeyVersion || current.Algorithm != expected.Algorithm ||
		!bytes.Equal(current.Nonce, expected.Nonce) || !bytes.Equal(current.Ciphertext, expected.Ciphertext) {
		return SecretMetadata{}, ErrSecretChanged
	}
	current.KeyVersion = replacement.KeyVersion
	current.Algorithm = replacement.Algorithm
	current.Nonce = append([]byte(nil), replacement.Nonce...)
	current.Ciphertext = append([]byte(nil), replacement.Ciphertext...)
	s.items[key] = current
	return maskSecret(current), nil
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
