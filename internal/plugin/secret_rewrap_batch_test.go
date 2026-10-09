package plugin

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/campusos/CampusOS/internal/platform/security"
)

func testSecretRewrapServices(t *testing.T, store *MemorySecretStore) (oldOnly, rotating, newOnly *SecretService) {
	t.Helper()
	oldKey := []byte("0123456789abcdef0123456789abcdef")
	newKey := []byte("abcdef0123456789abcdef0123456789")
	var err error
	oldOnly, err = NewSecretService(store, oldKey, "old-v1")
	if err != nil {
		t.Fatal(err)
	}
	newOnly, err = NewSecretService(store, newKey, "new-v2")
	if err != nil {
		t.Fatal(err)
	}
	keyring, err := security.NewKeyring("new-v2", map[string][]byte{"old-v1": oldKey, "new-v2": newKey})
	if err != nil {
		t.Fatal(err)
	}
	rotating, err = NewSecretServiceWithKeyring(store, keyring)
	if err != nil {
		t.Fatal(err)
	}
	return oldOnly, rotating, newOnly
}

func TestRewrapActiveSecretBatchMultiBatchAndIdempotence(t *testing.T) {
	ctx := t.Context()
	store := NewMemorySecretStore()
	oldOnly, rotating, newOnly := testSecretRewrapServices(t, store)
	const pluginID = int64(19)
	owner := int64(42)
	fixtures := []struct {
		owner *int64
		name  string
		value string
	}{
		{nil, "system.alpha", "value-alpha"},
		{&owner, "user.beta", "value-beta"},
		{&owner, "user.gamma", "value-gamma"},
	}
	for _, fixture := range fixtures {
		if _, err := oldOnly.Put(ctx, pluginID, fixture.owner, fixture.name, fixture.value, nil); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := newOnly.Put(ctx, pluginID, nil, "already.current", "current-value", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := oldOnly.Put(ctx, pluginID+1, nil, "other.plugin", "other-value", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := oldOnly.Put(ctx, pluginID, nil, "revoked.old", "revoked-value", nil); err != nil {
		t.Fatal(err)
	}
	if err := oldOnly.Revoke(ctx, pluginID, nil, "revoked.old"); err != nil {
		t.Fatal(err)
	}
	selected, err := store.ListActiveSecretRewrapCandidates(ctx, pluginID, "new-v2", 500)
	if err != nil || len(selected) != 3 {
		t.Fatalf("candidate selection: count=%d err=%v", len(selected), err)
	}
	for i := 1; i < len(selected); i++ {
		if selected[i-1].ID >= selected[i].ID {
			t.Fatal("candidate IDs were not strictly ascending")
		}
	}
	for _, want := range []SecretRewrapBatchResult{
		{Selected: 2, Rewrapped: 2, Remaining: 1},
		{Selected: 1, Rewrapped: 1, Remaining: 0},
		{Selected: 0, Rewrapped: 0, Remaining: 0},
	} {
		got, err := RewrapActiveSecretBatch(ctx, rotating, store, pluginID, 2)
		if err != nil || got != want {
			t.Fatalf("batch result=%+v, want %+v, err=%v", got, want, err)
		}
	}
	for _, fixture := range fixtures {
		value, err := newOnly.Resolve(ctx, pluginID, fixture.owner, fixture.name)
		if err != nil || value != fixture.value {
			t.Fatalf("new key could not read rewrapped value: err=%v", err)
		}
	}
	other, err := store.ActiveSecret(ctx, pluginID+1, nil, "other.plugin")
	if err != nil || other.KeyVersion != "old-v1" {
		t.Fatalf("another plugin's key was changed: key=%q err=%v", other.KeyVersion, err)
	}
	current, err := store.ActiveSecret(ctx, pluginID, nil, "already.current")
	if err != nil || current.KeyVersion != "new-v2" {
		t.Fatalf("active-key row was changed: key=%q err=%v", current.KeyVersion, err)
	}
}

func TestRewrapActiveSecretBatchFailsClosedOnUnreadableCandidate(t *testing.T) {
	for _, tamper := range []bool{false, true} {
		name := "missing old key"
		if tamper {
			name = "tampered envelope"
		}
		t.Run(name, func(t *testing.T) {
			ctx := t.Context()
			store := NewMemorySecretStore()
			oldOnly, rotating, newOnly := testSecretRewrapServices(t, store)
			const secretName = "never.emit.name"
			const secretValue = "never-emit-value"
			if _, err := oldOnly.Put(ctx, 19, nil, secretName, secretValue, nil); err != nil {
				t.Fatal(err)
			}
			if tamper {
				store.mu.Lock()
				key := memorySecretKey(19, nil, secretName)
				record := store.items[key]
				record.Ciphertext = append([]byte(nil), record.Ciphertext...)
				record.Ciphertext[0] ^= 0xff
				store.items[key] = record
				store.mu.Unlock()
			}
			before, err := store.ActiveSecret(ctx, 19, nil, secretName)
			if err != nil {
				t.Fatal(err)
			}
			service := newOnly
			if tamper {
				service = rotating
			}
			result, err := RewrapActiveSecretBatch(ctx, service, store, 19, 1)
			if !errors.Is(err, ErrSecretRewrapFailed) || result.Selected != 1 || result.Rewrapped != 0 ||
				strings.Contains(err.Error(), secretName) || strings.Contains(err.Error(), secretValue) {
				t.Fatalf("unsafe failed batch: result=%+v err=%v", result, err)
			}
			after, err := store.ActiveSecret(ctx, 19, nil, secretName)
			if err != nil || !reflect.DeepEqual(before, after) {
				t.Fatalf("failed batch changed the row: err=%v", err)
			}
		})
	}
}

type mutatingSecretRewrapInventory struct {
	*MemorySecretStore
	mutate func() error
}

func (s mutatingSecretRewrapInventory) ListActiveSecretRewrapCandidates(ctx context.Context, pluginID int64, activeKeyID string, limit int) ([]SecretRewrapCandidate, error) {
	candidates, err := s.MemorySecretStore.ListActiveSecretRewrapCandidates(ctx, pluginID, activeKeyID, limit)
	if err != nil {
		return nil, err
	}
	if err := s.mutate(); err != nil {
		return nil, err
	}
	return candidates, nil
}

func TestRewrapActiveSecretBatchRejectsChangedRow(t *testing.T) {
	ctx := t.Context()
	store := NewMemorySecretStore()
	oldOnly, rotating, _ := testSecretRewrapServices(t, store)
	const secretName = "never.emit.name"
	if _, err := oldOnly.Put(ctx, 19, nil, secretName, "old-value", nil); err != nil {
		t.Fatal(err)
	}
	inventory := mutatingSecretRewrapInventory{MemorySecretStore: store, mutate: func() error {
		_, err := oldOnly.Put(ctx, 19, nil, secretName, "newer-value", nil)
		return err
	}}
	result, err := RewrapActiveSecretBatch(ctx, rotating, inventory, 19, 1)
	if !errors.Is(err, ErrSecretChanged) || result.Selected != 1 || result.Rewrapped != 0 || strings.Contains(err.Error(), secretName) {
		t.Fatalf("changed candidate was not rejected safely: result=%+v err=%v", result, err)
	}
	current, err := store.ActiveSecret(ctx, 19, nil, secretName)
	if err != nil || current.KeyVersion != "old-v1" {
		t.Fatalf("newer row was rewrapped: key=%q err=%v", current.KeyVersion, err)
	}
	value, err := oldOnly.Resolve(ctx, 19, nil, secretName)
	if err != nil || value != "newer-value" {
		t.Fatalf("newer row was changed: err=%v", err)
	}
}

func TestRewrapActiveSecretBatchRejectsInvalidLimitWithoutMutation(t *testing.T) {
	ctx := t.Context()
	store := NewMemorySecretStore()
	oldOnly, rotating, _ := testSecretRewrapServices(t, store)
	if _, err := oldOnly.Put(ctx, 19, nil, "valid.name", "old-value", nil); err != nil {
		t.Fatal(err)
	}
	for _, limit := range []int{0, -1, 501} {
		if result, err := RewrapActiveSecretBatch(ctx, rotating, store, 19, limit); err == nil || result != (SecretRewrapBatchResult{}) {
			t.Fatalf("invalid limit %d was accepted: result=%+v err=%v", limit, result, err)
		}
	}
	current, err := store.ActiveSecret(ctx, 19, nil, "valid.name")
	if err != nil || current.KeyVersion != "old-v1" {
		t.Fatalf("invalid limit changed row: key=%q err=%v", current.KeyVersion, err)
	}
}

type failingSecretRewrapInventory struct {
	*MemorySecretStore
	failList  bool
	failCount bool
	err       error
}

func (s failingSecretRewrapInventory) ListActiveSecretRewrapCandidates(ctx context.Context, pluginID int64, activeKeyID string, limit int) ([]SecretRewrapCandidate, error) {
	if s.failList {
		return nil, s.err
	}
	return s.MemorySecretStore.ListActiveSecretRewrapCandidates(ctx, pluginID, activeKeyID, limit)
}

func (s failingSecretRewrapInventory) CountActiveSecretRewrapCandidates(ctx context.Context, pluginID int64, activeKeyID string) (int64, error) {
	if s.failCount {
		return 0, s.err
	}
	return s.MemorySecretStore.CountActiveSecretRewrapCandidates(ctx, pluginID, activeKeyID)
}

func TestRewrapActiveSecretBatchSanitizesInventoryErrors(t *testing.T) {
	ctx := t.Context()
	store := NewMemorySecretStore()
	oldOnly, rotating, _ := testSecretRewrapServices(t, store)
	const secretName = "never.emit.name"
	if _, err := oldOnly.Put(ctx, 19, nil, secretName, "value", nil); err != nil {
		t.Fatal(err)
	}
	for _, failList := range []bool{true, false} {
		inventory := failingSecretRewrapInventory{
			MemorySecretStore: store,
			failList:          failList,
			failCount:         !failList,
			err:               errors.New("storage error for " + secretName),
		}
		result, err := RewrapActiveSecretBatch(ctx, rotating, inventory, 19, 1)
		if !errors.Is(err, ErrSecretRewrapInventory) || strings.Contains(err.Error(), secretName) {
			t.Fatalf("inventory error leaked a secret name: result=%+v err=%v", result, err)
		}
		if failList && result != (SecretRewrapBatchResult{}) {
			t.Fatalf("list failure reported progress: %+v", result)
		}
		if !failList && (result.Selected != 1 || result.Rewrapped != 1) {
			t.Fatalf("count failure lost partial progress: %+v", result)
		}
	}
}
