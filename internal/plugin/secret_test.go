package plugin

import (
	"context"
	"testing"
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
