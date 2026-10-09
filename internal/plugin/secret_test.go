package plugin

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/campusos/CampusOS/internal/platform/security"
)

func TestSecretServiceEncryptsMasksRotatesAndRevokes(t *testing.T) {
	ctx := context.Background()
	store := NewMemorySecretStore()
	service, err := NewSecretService(store, []byte("0123456789abcdef0123456789abcdef"), "test-v1")
	if err != nil {
		t.Fatal(err)
	}
	owner := int64(42)
	metadata, err := service.Put(ctx, 10, &owner, "smtp.password", "first-secret", &owner)
	if err != nil {
		t.Fatal(err)
	}
	if len(metadata.Ciphertext) != 0 || metadata.MaskedValue == "" {
		t.Fatalf("metadata leaked ciphertext or was not masked: %+v", metadata)
	}
	stored, err := store.ActiveSecret(ctx, 10, &owner, "smtp.password")
	if err != nil {
		t.Fatal(err)
	}
	if string(stored.Ciphertext) == "first-secret" || len(stored.Nonce) == 0 {
		t.Fatal("secret was not encrypted with an authenticated nonce")
	}
	if value, err := service.Resolve(ctx, 10, &owner, "smtp.password"); err != nil || value != "first-secret" {
		t.Fatalf("resolve: value=%q err=%v", value, err)
	}
	if _, err := service.Put(ctx, 10, &owner, "smtp.password", "second-secret", &owner); err != nil {
		t.Fatal(err)
	}
	if value, _ := service.Resolve(ctx, 10, &owner, "smtp.password"); value != "second-secret" {
		t.Fatalf("rotation did not replace current value: %q", value)
	}
	if err := service.Revoke(ctx, 10, &owner, "smtp.password"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Resolve(ctx, 10, &owner, "smtp.password"); err == nil {
		t.Fatal("revoked secret remained readable")
	}
}

func TestSecretServiceReadsOldKeyAfterRotationAndFailsClosedWithoutIt(t *testing.T) {
	ctx := t.Context()
	store := NewMemorySecretStore()
	oldKey := []byte("0123456789abcdef0123456789abcdef")
	newKey := []byte("abcdef0123456789abcdef0123456789")
	oldService, err := NewSecretService(store, oldKey, "old-v1")
	if err != nil {
		t.Fatal(err)
	}
	owner := int64(42)
	if _, err := oldService.Put(ctx, 19, &owner, "mail.password", "old-mail-token", &owner); err != nil {
		t.Fatal(err)
	}
	ring, err := security.NewKeyring("new-v2", map[string][]byte{"old-v1": oldKey, "new-v2": newKey})
	if err != nil {
		t.Fatal(err)
	}
	rotated, err := NewSecretServiceWithKeyring(store, ring)
	if err != nil {
		t.Fatal(err)
	}
	if value, err := rotated.Resolve(ctx, 19, &owner, "mail.password"); err != nil || value != "old-mail-token" {
		t.Fatalf("old version could not be read after key rotation: value=%q err=%v", value, err)
	}
	metadata, err := rotated.Put(ctx, 19, &owner, "api.token", "new-api-token", &owner)
	if err != nil {
		t.Fatal(err)
	}
	if metadata.KeyVersion != "new-v2" || len(metadata.Ciphertext) != 0 || len(metadata.Nonce) != 0 {
		t.Fatalf("new write did not use active key or leaked encrypted bytes: %+v", metadata)
	}
	encoded, err := json.Marshal(metadata)
	if err != nil || strings.Contains(string(encoded), "new-api-token") || strings.Contains(string(encoded), "ciphertext") {
		t.Fatalf("metadata leaked payload: %s err=%v", encoded, err)
	}
	if _, err := rotated.Resolve(ctx, 19, nil, "mail.password"); err == nil {
		t.Fatal("owner change opened another user's secret")
	}
	if _, err := rotated.Resolve(ctx, 20, &owner, "mail.password"); err == nil {
		t.Fatal("plugin change opened another plugin's secret")
	}
	newOnlyRing, err := security.NewKeyring("new-v2", map[string][]byte{"new-v2": newKey})
	if err != nil {
		t.Fatal(err)
	}
	newOnly, err := NewSecretServiceWithKeyring(store, newOnlyRing)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := newOnly.Resolve(ctx, 19, &owner, "mail.password"); err == nil {
		t.Fatal("old ciphertext opened without its read key")
	}
	if value, err := newOnly.Resolve(ctx, 19, &owner, "api.token"); err != nil || value != "new-api-token" {
		t.Fatalf("new ciphertext unavailable with active key: value=%q err=%v", value, err)
	}
}

func TestSecretServiceEnvKeyringTakesPrecedenceAndRejectsPartialConfig(t *testing.T) {
	oldKey := []byte("0123456789abcdef0123456789abcdef")
	newKey := []byte("abcdef0123456789abcdef0123456789")
	store := NewMemorySecretStore()
	t.Setenv("CAMPUSOS_PLUGIN_SECRET_KEY", hex.EncodeToString(oldKey))
	t.Setenv("CAMPUSOS_PLUGIN_SECRET_KEY_VERSION", "old-v1")
	t.Setenv("CAMPUSOS_SECRET_ACTIVE_KEY_ID", "new-v2")
	t.Setenv("CAMPUSOS_SECRET_ENCRYPTION_KEYS", "old-v1:"+hex.EncodeToString(oldKey)+",new-v2:"+hex.EncodeToString(newKey))
	service, err := NewSecretServiceFromEnv(store)
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := service.Put(t.Context(), 19, nil, "mail.password", "env-secret", nil)
	if err != nil || metadata.KeyVersion != "new-v2" {
		t.Fatalf("generic active key was not selected: metadata=%+v err=%v", metadata, err)
	}
	t.Setenv("CAMPUSOS_SECRET_ACTIVE_KEY_ID", "")
	if _, err := NewSecretServiceFromEnv(store); err == nil {
		t.Fatal("incomplete generic keyring silently fell back to plugin-only key")
	}
	t.Setenv("CAMPUSOS_SECRET_ACTIVE_KEY_ID", "new-v2")
	t.Setenv("CAMPUSOS_SECRET_ENCRYPTION_KEYS", "new-v2:"+hex.EncodeToString(newKey)+",new-v2:"+hex.EncodeToString(oldKey))
	if _, err := NewSecretServiceFromEnv(store); err == nil {
		t.Fatal("duplicate key ID was accepted")
	}
	t.Setenv("CAMPUSOS_SECRET_ENCRYPTION_KEYS", "")
	t.Setenv("CAMPUSOS_SECRET_ACTIVE_KEY_ID", "")
	legacy, err := NewSecretServiceFromEnv(store)
	if err != nil {
		t.Fatal(err)
	}
	if value, err := legacy.Resolve(t.Context(), 19, nil, "mail.password"); err == nil || value != "" {
		t.Fatalf("legacy key unexpectedly opened new ciphertext: value=%q err=%v", value, err)
	}
}

func TestSecretServiceRewrapActivePreservesRowAndAllowsOldKeyRetirement(t *testing.T) {
	ctx := t.Context()
	store := NewMemorySecretStore()
	oldKey := []byte("0123456789abcdef0123456789abcdef")
	newKey := []byte("abcdef0123456789abcdef0123456789")
	owner := int64(42)
	oldService, err := NewSecretService(store, oldKey, "old-v1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := oldService.Put(ctx, 19, &owner, "mail.password", "keep-this-value", &owner); err != nil {
		t.Fatal(err)
	}
	before, err := store.ActiveSecret(ctx, 19, &owner, "mail.password")
	if err != nil {
		t.Fatal(err)
	}
	ring, err := security.NewKeyring("new-v2", map[string][]byte{"old-v1": oldKey, "new-v2": newKey})
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewSecretServiceWithKeyring(store, ring)
	if err != nil {
		t.Fatal(err)
	}
	masked, changed, err := service.RewrapActive(ctx, 19, &owner, "mail.password")
	if err != nil || !changed {
		t.Fatalf("rewrap: changed=%v err=%v", changed, err)
	}
	if len(masked.Ciphertext) != 0 || len(masked.Nonce) != 0 || masked.MaskedValue == "" {
		t.Fatal("rewrap response exposed encrypted material or omitted its mask")
	}
	after, err := store.ActiveSecret(ctx, 19, &owner, "mail.password")
	if err != nil {
		t.Fatal(err)
	}
	if after.ID != before.ID || after.PluginID != before.PluginID || !sameOptionalInt64(after.OwnerUserID, before.OwnerUserID) ||
		after.SecretName != before.SecretName || after.Status != before.Status || !after.CreatedAt.Equal(before.CreatedAt) ||
		!reflect.DeepEqual(after.RotatedAt, before.RotatedAt) || !reflect.DeepEqual(after.RevokedAt, before.RevokedAt) ||
		!reflect.DeepEqual(after.Metadata, before.Metadata) {
		t.Fatal("key-material rewrap changed the secret's identity or metadata")
	}
	if after.KeyVersion != "new-v2" || bytes.Equal(after.Ciphertext, before.Ciphertext) || bytes.Equal(after.Nonce, before.Nonce) {
		t.Fatal("rewrap did not replace the encrypted envelope under the active key")
	}
	newOnlyRing, err := security.NewKeyring("new-v2", map[string][]byte{"new-v2": newKey})
	if err != nil {
		t.Fatal(err)
	}
	newOnly, err := NewSecretServiceWithKeyring(store, newOnlyRing)
	if err != nil {
		t.Fatal(err)
	}
	if value, err := newOnly.Resolve(ctx, 19, &owner, "mail.password"); err != nil || value != "keep-this-value" {
		t.Fatalf("new key could not read rewrapped value: value=%q err=%v", value, err)
	}
	if value, err := oldService.Resolve(ctx, 19, &owner, "mail.password"); err == nil || value != "" {
		t.Fatalf("retired key read rewrapped value: value=%q err=%v", value, err)
	}
	if _, changed, err := newOnly.RewrapActive(ctx, 19, &owner, "mail.password"); err != nil || changed {
		t.Fatalf("active-key no-op: changed=%v err=%v", changed, err)
	}
	unchanged, err := store.ActiveSecret(ctx, 19, &owner, "mail.password")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(unchanged.Ciphertext, after.Ciphertext) || !bytes.Equal(unchanged.Nonce, after.Nonce) {
		t.Fatal("active-key no-op rewrote encrypted material")
	}
}

func TestSecretServiceRewrapActiveFailsClosed(t *testing.T) {
	ctx := t.Context()
	oldKey := []byte("0123456789abcdef0123456789abcdef")
	newKey := []byte("abcdef0123456789abcdef0123456789")
	owner := int64(42)
	for _, tc := range []struct {
		name       string
		withOldKey bool
		tamper     bool
	}{
		{name: "missing old key"},
		{name: "tampered old ciphertext", withOldKey: true, tamper: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := NewMemorySecretStore()
			oldService, err := NewSecretService(store, oldKey, "old-v1")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := oldService.Put(ctx, 19, &owner, "mail.password", "never-expose-this", nil); err != nil {
				t.Fatal(err)
			}
			if tc.tamper {
				tamperMemorySecret(store, 19, &owner, "mail.password")
			}
			before, err := store.ActiveSecret(ctx, 19, &owner, "mail.password")
			if err != nil {
				t.Fatal(err)
			}
			keys := map[string][]byte{"new-v2": newKey}
			if tc.withOldKey {
				keys["old-v1"] = oldKey
			}
			ring, err := security.NewKeyring("new-v2", keys)
			if err != nil {
				t.Fatal(err)
			}
			service, err := NewSecretServiceWithKeyring(store, ring)
			if err != nil {
				t.Fatal(err)
			}
			if _, changed, err := service.RewrapActive(ctx, 19, &owner, "mail.password"); err == nil || changed || strings.Contains(err.Error(), "never-expose-this") {
				t.Fatalf("unreadable secret was rewritten or leaked: changed=%v err=%v", changed, err)
			}
			after, err := store.ActiveSecret(ctx, 19, &owner, "mail.password")
			if err != nil {
				t.Fatal(err)
			}
			if after.KeyVersion != before.KeyVersion || !bytes.Equal(after.Ciphertext, before.Ciphertext) || !bytes.Equal(after.Nonce, before.Nonce) {
				t.Fatal("unreadable secret was modified")
			}
		})
	}

	store := NewMemorySecretStore()
	service, err := NewSecretService(store, newKey, "new-v2")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Put(ctx, 19, &owner, "mail.password", "active-value", nil); err != nil {
		t.Fatal(err)
	}
	tamperMemorySecret(store, 19, &owner, "mail.password")
	if _, changed, err := service.RewrapActive(ctx, 19, &owner, "mail.password"); err == nil || changed {
		t.Fatalf("tampered active-key record passed no-op: changed=%v err=%v", changed, err)
	}
	wrongOwner := int64(43)
	if _, changed, err := service.RewrapActive(ctx, 19, &wrongOwner, "mail.password"); !errors.Is(err, ErrMarketNotFound) || changed {
		t.Fatalf("wrong owner accessed secret: changed=%v err=%v", changed, err)
	}
}

func TestMemorySecretStoreReplaceSecretIfCurrentPreservesNonCryptoFields(t *testing.T) {
	ctx := t.Context()
	store := NewMemorySecretStore()
	owner := int64(42)
	createdAt := time.Unix(123, 0)
	original := SecretMetadata{
		ID: 1, PluginID: 19, OwnerUserID: &owner, SecretName: "mail.password",
		Status: "active", Metadata: map[string]interface{}{"created_by": "42"},
		CreatedAt: createdAt, KeyVersion: "old-v1", Algorithm: security.AlgorithmAES256GCM,
		Nonce: []byte("old-nonce"), Ciphertext: []byte("old-ciphertext"),
	}
	if _, err := store.PutSecret(ctx, original); err != nil {
		t.Fatal(err)
	}
	replacement := original
	replacement.KeyVersion = "new-v2"
	replacement.Nonce = []byte("new-nonce")
	replacement.Ciphertext = []byte("new-ciphertext")
	replacement.Metadata = map[string]interface{}{"wrong": true}
	replacement.CreatedAt = createdAt.Add(time.Hour)
	replacement.RotatedAt = &replacement.CreatedAt
	replacement.RevokedAt = &replacement.CreatedAt
	masked, err := store.ReplaceSecretIfCurrent(ctx, original, replacement)
	if err != nil {
		t.Fatal(err)
	}
	if len(masked.Ciphertext) != 0 || len(masked.Nonce) != 0 {
		t.Fatal("conditional rewrap returned encrypted material")
	}
	current, err := store.ActiveSecret(ctx, 19, &owner, "mail.password")
	if err != nil {
		t.Fatal(err)
	}
	if current.ID != original.ID || current.Status != "active" || !current.CreatedAt.Equal(createdAt) ||
		current.RotatedAt != nil || current.RevokedAt != nil || !reflect.DeepEqual(current.Metadata, original.Metadata) ||
		current.KeyVersion != "new-v2" || !bytes.Equal(current.Nonce, replacement.Nonce) || !bytes.Equal(current.Ciphertext, replacement.Ciphertext) {
		t.Fatal("conditional rewrap changed non-crypto fields or failed to update encrypted material")
	}
}

func TestMemorySecretStoreReplaceSecretIfCurrentRejectsStaleAndRevoked(t *testing.T) {
	ctx := t.Context()
	owner := int64(42)
	for _, tc := range []struct {
		name   string
		change func(*MemorySecretStore) error
	}{
		{name: "new value", change: func(store *MemorySecretStore) error {
			_, err := store.PutSecret(ctx, SecretMetadata{ID: 2, PluginID: 19, OwnerUserID: &owner, SecretName: "mail.password", Status: "active", KeyVersion: "other"})
			return err
		}},
		{name: "revoked", change: func(store *MemorySecretStore) error {
			return store.RevokeSecret(ctx, 19, &owner, "mail.password")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := NewMemorySecretStore()
			original := SecretMetadata{ID: 1, PluginID: 19, OwnerUserID: &owner, SecretName: "mail.password", Status: "active", KeyVersion: "old-v1"}
			if _, err := store.PutSecret(ctx, original); err != nil {
				t.Fatal(err)
			}
			if err := tc.change(store); err != nil {
				t.Fatal(err)
			}
			replacement := original
			replacement.KeyVersion = "new-v2"
			if _, err := store.ReplaceSecretIfCurrent(ctx, original, replacement); !errors.Is(err, ErrSecretChanged) {
				t.Fatalf("stale rewrap was accepted: %v", err)
			}
			store.mu.RLock()
			current := store.items[memorySecretKey(19, &owner, "mail.password")]
			store.mu.RUnlock()
			if current.KeyVersion != "other" && tc.name == "new value" || current.Status != "revoked" && tc.name == "revoked" {
				t.Fatalf("stale rewrap modified current record: %+v", maskSecret(current))
			}
		})
	}
}

func TestMemorySecretStoreReplaceSecretIfCurrentRejectsChangedIdentityAndEnvelope(t *testing.T) {
	ctx := t.Context()
	store := NewMemorySecretStore()
	owner := int64(42)
	original := SecretMetadata{
		ID: 1, PluginID: 19, OwnerUserID: &owner, SecretName: "mail.password", Status: "active",
		KeyVersion: "old-v1", Algorithm: security.AlgorithmAES256GCM,
		Nonce: []byte("old-nonce"), Ciphertext: []byte("old-ciphertext"),
	}
	if _, err := store.PutSecret(ctx, original); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		change func(*SecretMetadata, *SecretMetadata)
	}{
		{"ID", func(_, replacement *SecretMetadata) { replacement.ID++ }},
		{"plugin", func(_, replacement *SecretMetadata) { replacement.PluginID++ }},
		{"owner", func(_, replacement *SecretMetadata) { other := int64(43); replacement.OwnerUserID = &other }},
		{"name", func(_, replacement *SecretMetadata) { replacement.SecretName = "other.password" }},
		{"status", func(_, replacement *SecretMetadata) { replacement.Status = "revoked" }},
		{"old key", func(expected, _ *SecretMetadata) { expected.KeyVersion = "other" }},
		{"old algorithm", func(expected, _ *SecretMetadata) { expected.Algorithm = "other" }},
		{"old nonce", func(expected, _ *SecretMetadata) { expected.Nonce = []byte("other") }},
		{"old ciphertext", func(expected, _ *SecretMetadata) { expected.Ciphertext = []byte("other") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			expected := original
			replacement := original
			replacement.KeyVersion = "new-v2"
			tc.change(&expected, &replacement)
			if _, err := store.ReplaceSecretIfCurrent(ctx, expected, replacement); !errors.Is(err, ErrSecretChanged) {
				t.Fatalf("changed identity or old envelope was accepted: %v", err)
			}
			current, err := store.ActiveSecret(ctx, 19, &owner, "mail.password")
			if err != nil || current.KeyVersion != original.KeyVersion || !bytes.Equal(current.Nonce, original.Nonce) || !bytes.Equal(current.Ciphertext, original.Ciphertext) {
				t.Fatalf("rejected rewrap modified current record: err=%v", err)
			}
		})
	}
}

type delayedRewrapStore struct {
	SecretStore
	blockKey string
	blocked  chan struct{}
	release  chan struct{}
}

func (s *delayedRewrapStore) ReplaceSecretIfCurrent(ctx context.Context, expected, replacement SecretMetadata) (SecretMetadata, error) {
	if replacement.KeyVersion == s.blockKey {
		close(s.blocked)
		<-s.release
	}
	return s.SecretStore.ReplaceSecretIfCurrent(ctx, expected, replacement)
}

func TestSecretServiceConcurrentRewrapRejectsStaleEnvelope(t *testing.T) {
	ctx := t.Context()
	base := NewMemorySecretStore()
	owner := int64(42)
	oldKey := []byte("0123456789abcdef0123456789abcdef")
	keyA := []byte("abcdef0123456789abcdef0123456789")
	keyB := []byte("1234567890abcdef1234567890abcdef")
	oldService, err := NewSecretService(base, oldKey, "old-v1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := oldService.Put(ctx, 19, &owner, "mail.password", "keep-this-value", nil); err != nil {
		t.Fatal(err)
	}
	before, err := base.ActiveSecret(ctx, 19, &owner, "mail.password")
	if err != nil {
		t.Fatal(err)
	}
	store := &delayedRewrapStore{SecretStore: base, blockKey: "key-b", blocked: make(chan struct{}), release: make(chan struct{})}
	defer func() {
		select {
		case <-store.release:
		default:
			close(store.release)
		}
	}()
	keys := map[string][]byte{"old-v1": oldKey, "key-a": keyA, "key-b": keyB}
	newOperator := func(id string) *SecretService {
		ring, err := security.NewKeyring(id, keys)
		if err != nil {
			t.Fatal(err)
		}
		service, err := NewSecretServiceWithKeyring(store, ring)
		if err != nil {
			t.Fatal(err)
		}
		return service
	}
	operatorA, operatorB := newOperator("key-a"), newOperator("key-b")
	if operatorA.ActiveKeyID() != "key-a" || operatorB.ActiveKeyID() != "key-b" {
		t.Fatal("operator active key ID mismatch")
	}
	if _, changed, err := operatorA.RewrapActiveIfCurrent(ctx, 19, &owner, "mail.password", before.ID+1); !errors.Is(err, ErrSecretChanged) || changed {
		t.Fatalf("wrong expected row ID was accepted: changed=%v err=%v", changed, err)
	}
	type result struct {
		changed bool
		err     error
	}
	second := make(chan result, 1)
	go func() {
		_, changed, err := operatorB.RewrapActiveIfCurrent(ctx, 19, &owner, "mail.password", before.ID)
		second <- result{changed, err}
	}()
	select {
	case <-store.blocked:
	case <-time.After(5 * time.Second):
		t.Fatal("second operator did not reach the conditional write")
	}
	updated, changed, err := operatorA.RewrapActiveIfCurrent(ctx, 19, &owner, "mail.password", before.ID)
	if err != nil || !changed || updated.ID != before.ID {
		t.Fatalf("first operator rewrap failed: changed=%v err=%v", changed, err)
	}
	close(store.release)
	select {
	case got := <-second:
		if !errors.Is(got.err, ErrSecretChanged) || got.changed {
			t.Fatalf("stale second operator overwrote first: changed=%v err=%v", got.changed, got.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("second operator did not return from stale CAS")
	}
	after, err := base.ActiveSecret(ctx, 19, &owner, "mail.password")
	if err != nil || after.ID != before.ID || after.KeyVersion != "key-a" || bytes.Equal(after.Nonce, before.Nonce) || bytes.Equal(after.Ciphertext, before.Ciphertext) {
		t.Fatalf("first operator envelope was not retained: err=%v", err)
	}
	if value, err := operatorA.Resolve(ctx, 19, &owner, "mail.password"); err != nil || value != "keep-this-value" {
		t.Fatalf("first operator envelope cannot be opened: value=%q err=%v", value, err)
	}
}

func tamperMemorySecret(store *MemorySecretStore, pluginID int64, owner *int64, name string) {
	store.mu.Lock()
	defer store.mu.Unlock()
	key := memorySecretKey(pluginID, owner, name)
	value := store.items[key]
	value.Ciphertext = append([]byte(nil), value.Ciphertext...)
	value.Ciphertext[0] ^= 1
	store.items[key] = value
}
