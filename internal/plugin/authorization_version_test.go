package plugin

import (
	"context"
	"strings"
	"testing"
)

func versionTestManifest(version string) *Manifest {
	return &Manifest{
		Name: "version-identity-fixture", DisplayName: "Version Identity Fixture", Version: version,
		APIVersion: ManifestAPIVersionV3, HostAPIVersion: HostAPIVersionV3,
		Runtime: "process", Scope: ScopeUser,
		CapabilityDeclarations: []CapabilityRequest{
			{Code: "schedule.self.read", Required: true, Purpose: "读取课表", Scope: "self"},
		},
	}
}

func newVersionTestService(t *testing.T) (*AuthorizationService, *MemoryAuthorizationStore) {
	t.Helper()
	ctx := context.Background()
	repo := NewMemoryPluginRepository()
	if err := repo.Save(ctx, &PluginRecord{ID: 101, Name: "version-identity-fixture", Status: string(StatusEnabled)}); err != nil {
		t.Fatal(err)
	}
	store := NewMemoryAuthorizationStore()
	return NewAuthorizationService(store, repo, func(string) bool { return true }), store
}

func TestMemoryVersionReactivationRetiresPreviousAndPreservesConsent(t *testing.T) {
	ctx := context.Background()
	service, store := newVersionTestService(t)
	manifestA := versionTestManifest("1.0.0")
	versionA, err := service.SyncInstalled(ctx, &Plugin{Manifest: manifestA, Checksum: strings.Repeat("a", 64)}, "1")
	if err != nil {
		t.Fatal(err)
	}
	originalDeclarations, err := store.ListDeclarations(ctx, versionA.ID)
	if err != nil || len(originalDeclarations) != 1 {
		t.Fatalf("declarations=%v err=%v", originalDeclarations, err)
	}
	code := "schedule.self.read"
	if _, err := service.SetAdminGrant(ctx, versionA.ID, code, "granted", "test", map[string]interface{}{"scope": "self"}, "1", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := service.SetUserConsent(ctx, "42", versionA.ID, code, "revoked", map[string]interface{}{"scope": "self"}); err != nil {
		t.Fatal(err)
	}

	manifestB := versionTestManifest("1.1.0")
	versionB, err := service.SyncInstalled(ctx, &Plugin{Manifest: manifestB, Checksum: strings.Repeat("b", 64)}, "1")
	if err != nil {
		t.Fatal(err)
	}
	if previous, err := store.VersionByID(ctx, versionA.ID); err != nil || previous.LifecycleStatus != "retired" {
		t.Fatalf("A after activating B=%+v err=%v", previous, err)
	}
	reactivatedA, err := service.SyncInstalled(ctx, &Plugin{Manifest: manifestA, Checksum: strings.Repeat("a", 64)}, "1")
	if err != nil {
		t.Fatal(err)
	}
	if reactivatedA.ID != versionA.ID {
		t.Fatalf("reactivation created a new ID: initial=%d current=%d", versionA.ID, reactivatedA.ID)
	}
	if active, err := store.ActiveVersion(ctx, manifestA.Name); err != nil || active.ID != versionA.ID {
		t.Fatalf("active version=%+v err=%v", active, err)
	}
	if retired, err := store.VersionByID(ctx, versionB.ID); err != nil || retired.LifecycleStatus != "retired" {
		t.Fatalf("B after reactivating A=%+v err=%v", retired, err)
	}
	currentDeclarations, err := store.ListDeclarations(ctx, versionA.ID)
	if err != nil || len(currentDeclarations) != 1 || currentDeclarations[0].ID != originalDeclarations[0].ID {
		t.Fatalf("declaration identity changed: initial=%v current=%v err=%v", originalDeclarations, currentDeclarations, err)
	}
	if consent, err := store.CurrentUserConsent(ctx, 42, versionA.ID, code); err != nil || consent.Status != "revoked" {
		t.Fatalf("revoked consent lost: %+v err=%v", consent, err)
	}
	input := AuthorizationInput{PluginName: manifestA.Name, CapabilityCode: code, OperationCode: "host.GetSchedule", ActorUserID: "42", ResourceOwnerID: "42", ResourceScope: map[string]interface{}{"scope": "self"}}
	if decision := service.Authorize(ctx, input); decision.ReasonCode != ReasonUserConsentMissing {
		t.Fatalf("revoked consent must still deny after reactivation: %+v", decision)
	}
}

func TestMemoryVersionIdentityRejectsManifestChangeAndDigestReuse(t *testing.T) {
	ctx := context.Background()
	service, store := newVersionTestService(t)
	manifestA := versionTestManifest("1.0.0")
	versionA, err := service.SyncInstalled(ctx, &Plugin{Manifest: manifestA, Checksum: strings.Repeat("a", 64)}, "1")
	if err != nil {
		t.Fatal(err)
	}
	changed := *manifestA
	changed.Description = "mutated after publication"
	if _, err := service.SyncInstalled(ctx, &Plugin{Manifest: &changed, Checksum: strings.Repeat("a", 64)}, "1"); err == nil {
		t.Fatal("same version accepted changed manifest")
	}
	if _, err := service.SyncInstalled(ctx, &Plugin{Manifest: manifestA, Checksum: strings.Repeat("c", 64)}, "1"); err == nil {
		t.Fatal("same version accepted changed package digest")
	}
	manifestB := versionTestManifest("1.1.0")
	if _, err := service.SyncInstalled(ctx, &Plugin{Manifest: manifestB, Checksum: strings.Repeat("a", 64)}, "1"); err == nil {
		t.Fatal("different version reused package digest")
	}
	if active, err := store.ActiveVersion(ctx, manifestA.Name); err != nil || active.ID != versionA.ID {
		t.Fatalf("failed write changed active version: %+v err=%v", active, err)
	}

	versionA.Manifest["description"] = "caller mutation"
	declarations, err := store.ListDeclarations(ctx, versionA.ID)
	if err != nil {
		t.Fatal(err)
	}
	declarations[0].ResourceScope["scope"] = "caller mutation"
	if _, err := service.SyncInstalled(ctx, &Plugin{Manifest: manifestA, Checksum: strings.Repeat("a", 64)}, "1"); err != nil {
		t.Fatalf("caller mutated returned data inside store: %v", err)
	}
	storedDeclarations, err := store.ListDeclarations(ctx, versionA.ID)
	if err != nil || storedDeclarations[0].ResourceScope["scope"] == "caller mutation" {
		t.Fatalf("caller mutated stored declaration: %+v err=%v", storedDeclarations, err)
	}
}
