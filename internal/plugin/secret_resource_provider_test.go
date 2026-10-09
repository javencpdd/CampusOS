package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/campusos/CampusOS/internal/platform/security"
)

type secretResourceFactsFixture struct {
	version PluginVersion
	grant   AdminGrant
}

func (f *secretResourceFactsFixture) VersionByID(_ context.Context, id int64) (PluginVersion, error) {
	if id != f.version.ID {
		return PluginVersion{}, errors.New("not found")
	}
	return f.version, nil
}
func (f *secretResourceFactsFixture) ActiveVersion(_ context.Context, name string) (PluginVersion, error) {
	if name != f.version.PluginName {
		return PluginVersion{}, errors.New("not found")
	}
	return f.version, nil
}
func (f *secretResourceFactsFixture) CurrentAdminGrant(_ context.Context, id int64, code string) (AdminGrant, error) {
	if id != f.version.ID || code != f.grant.CapabilityCode {
		return AdminGrant{}, errors.New("not found")
	}
	return f.grant, nil
}

func resourceSetMap(t *testing.T, value security.ResourceSet) map[string]interface{} {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var mapped map[string]interface{}
	if err := json.Unmarshal(raw, &mapped); err != nil {
		t.Fatal(err)
	}
	return mapped
}

func TestSecretGrantResourceProviderRechecksActiveGrantAndCeilings(t *testing.T) {
	now := time.Now().UTC()
	target := "https://mailer.example.edu"
	other := "https://other.example.edu"
	requested := security.ResourceSet{
		NetworkTargets: []string{target, other}, StorageBytes: 1000,
		CPUMillis: 1000, MemoryMB: 256, MaxConcurrency: 8, TimeoutMS: 5000,
	}
	published := security.ResourceSet{
		NetworkTargets: []string{target}, StorageBytes: 800,
		CPUMillis: 800, MemoryMB: 200, MaxConcurrency: 4, TimeoutMS: 4000,
	}
	granted := security.ResourceSet{
		NetworkTargets: []string{target, other}, StorageBytes: 600,
		CPUMillis: 600, MemoryMB: 128, MaxConcurrency: 3, TimeoutMS: 3000,
	}
	deployed := security.ResourceSet{
		NetworkTargets: []string{target}, StorageBytes: 500,
		CPUMillis: 500, MemoryMB: 96, MaxConcurrency: 2, TimeoutMS: 2000,
	}
	expiry := now.Add(time.Minute)
	facts := &secretResourceFactsFixture{
		version: PluginVersion{ID: 91, PluginID: 19, PluginName: "mailer", LifecycleStatus: "active",
			Manifest: map[string]interface{}{"host_resources": resourceSetMap(t, requested)}},
		grant: AdminGrant{PluginVersionID: 91, CapabilityCode: "secret.system.read", Status: "granted",
			GrantedScope: map[string]interface{}{"host_resources": resourceSetMap(t, granted)}, ExpiresAt: &expiry},
	}
	provider, err := NewSecretGrantResourceProvider(facts, published, deployed)
	if err != nil {
		t.Fatal(err)
	}
	provider.now = func() time.Time { return now }
	decision, err := provider.CurrentSecretResources(context.Background(), 91, "secret.system.read", "notify-course")
	if err != nil || !decision.AllowsNetworkTarget(target) || decision.AllowsNetworkTarget(other) {
		t.Fatalf("unexpected resource decision: %+v %v", decision, err)
	}
	if got := decision.Effective; got.StorageBytes != 500 || got.CPUMillis != 500 ||
		got.MemoryMB != 96 || got.MaxConcurrency != 2 || got.TimeoutMS != 2000 {
		t.Fatalf("numeric ceilings did not intersect: %+v", got)
	}
	facts.grant.Status = "revoked"
	if _, err := provider.CurrentSecretResources(context.Background(), 91, "secret.system.read", "notify-course"); !errors.Is(err, ErrSecretUseDenied) {
		t.Fatalf("revoked grant not denied: %v", err)
	}
	facts.grant.Status = "granted"
	provider.now = func() time.Time { return expiry }
	if _, err := provider.CurrentSecretResources(context.Background(), 91, "secret.system.read", "notify-course"); !errors.Is(err, ErrSecretUseDenied) {
		t.Fatalf("expired grant not denied: %v", err)
	}
	provider.now = func() time.Time { return now }
	delete(facts.version.Manifest, "host_resources")
	if _, err := provider.CurrentSecretResources(context.Background(), 91, "secret.system.read", "notify-course"); !errors.Is(err, ErrSecretUseDenied) {
		t.Fatalf("missing immutable request not denied: %v", err)
	}
}
