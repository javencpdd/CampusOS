package plugin

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/campusos/CampusOS/internal/platform/security"
)

func newSecretRegistryBudget(t *testing.T, purpose string) *security.PurposeBudget {
	t.Helper()
	budget, err := security.NewPurposeBudget(purpose, security.PurposeBudgetLimits{
		MaxUnits: 2, MaxConcurrency: 1, MaxDuration: time.Second,
		ExpiresAt: time.Now().Add(time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	return budget
}

func TestSecretPurposeBudgetRegistryVersionPurposeAndRevocation(t *testing.T) {
	registry := NewSecretPurposeBudgetRegistry()
	first := newSecretRegistryBudget(t, "notify-course")
	if err := registry.Register(41, "different-purpose", first); !errors.Is(err, ErrSecretUseDenied) {
		t.Fatalf("accepted mismatched purpose: %v", err)
	}
	if err := registry.Register(41, "notify-course", first); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.CurrentSecretPurposeBudget(context.Background(), 42, "notify-course"); !errors.Is(err, ErrSecretUseDenied) {
		t.Fatalf("other version received budget: %v", err)
	}
	if _, err := registry.CurrentSecretPurposeBudget(context.Background(), 41, "other-purpose"); !errors.Is(err, ErrSecretUseDenied) {
		t.Fatalf("other purpose received budget: %v", err)
	}
	lease, err := first.Reserve(context.Background(), 1, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	second := newSecretRegistryBudget(t, "notify-course")
	if err := registry.Register(41, "notify-course", second); err != nil {
		t.Fatal(err)
	}
	if !first.Snapshot().Revoked || lease.Context().Err() == nil {
		t.Fatal("replaced budget did not cancel in-flight work")
	}
	if err := lease.CommitUnknown(); err != nil {
		t.Fatal(err)
	}
	if current, err := registry.CurrentSecretPurposeBudget(context.Background(), 41, "notify-course"); err != nil || current != second {
		t.Fatalf("new budget unavailable: %p %v", current, err)
	}
	registry.Revoke(41, "notify-course")
	if !second.Snapshot().Revoked {
		t.Fatal("budget was not revoked")
	}
	if _, err := registry.CurrentSecretPurposeBudget(context.Background(), 41, "notify-course"); !errors.Is(err, ErrSecretUseDenied) {
		t.Fatalf("revoked budget was returned: %v", err)
	}
}
