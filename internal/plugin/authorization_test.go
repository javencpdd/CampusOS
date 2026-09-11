package plugin

import (
	"context"
	"testing"
	"time"
)

func newAuthorizationFixture(t testing.TB) (*AuthorizationService, PluginVersion) {
	t.Helper()
	ctx := context.Background()
	repo := NewMemoryPluginRepository()
	if err := repo.Save(ctx, &PluginRecord{ID: 101, Name: "authorization-fixture", Status: string(StatusEnabled)}); err != nil {
		t.Fatal(err)
	}
	manifest := &Manifest{
		Name: "authorization-fixture", DisplayName: "Authorization Fixture", Version: "1.0.0",
		APIVersion: ManifestAPIVersionV3, HostAPIVersion: HostAPIVersionV3,
		Runtime: "process", Scope: ScopeUser,
		CapabilityDeclarations: []CapabilityRequest{
			{Code: "schedule.self.read", Required: true, Purpose: "生成当前用户的课表提醒", Scope: "self"},
			{Code: "config.system.read", Required: true, Purpose: "读取插件非敏感配置", Scope: "system"},
		},
	}
	store := NewMemoryAuthorizationStore()
	service := NewAuthorizationService(store, repo, func(string) bool { return true })
	version, err := service.SyncInstalled(ctx, &Plugin{Manifest: manifest, Checksum: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}, "1")
	if err != nil {
		t.Fatal(err)
	}
	return service, version
}

func TestAuthorizationThreeLayerAndImmediateRevocation(t *testing.T) {
	ctx := context.Background()
	service, version := newAuthorizationFixture(t)
	input := AuthorizationInput{PluginName: version.PluginName, PluginVersion: version.Version, CapabilityCode: "schedule.self.read", OperationCode: "host.GetSchedule", ActorUserID: "42", ResourceOwnerID: "42", ResourceScope: map[string]interface{}{"scope": "self"}}
	if got := service.Authorize(ctx, input); got.ReasonCode != ReasonAdminGrantMissing {
		t.Fatalf("without admin grant: got %s", got.ReasonCode)
	}
	if _, err := service.SetAdminGrant(ctx, version.ID, input.CapabilityCode, "granted", "允许课表提醒", map[string]interface{}{"scope": "self"}, "1", nil); err != nil {
		t.Fatal(err)
	}
	if got := service.Authorize(ctx, input); got.ReasonCode != ReasonUserConsentMissing {
		t.Fatalf("without consent: got %s", got.ReasonCode)
	}
	if _, err := service.SetUserConsent(ctx, "42", version.ID, input.CapabilityCode, "granted", map[string]interface{}{"scope": "self"}); err != nil {
		t.Fatal(err)
	}
	if got := service.Authorize(ctx, input); !got.Allow || got.ReasonCode != ReasonAllow {
		t.Fatalf("fully granted: %+v", got)
	}
	if _, err := service.SetUserConsent(ctx, "42", version.ID, input.CapabilityCode, "revoked", map[string]interface{}{"scope": "self"}); err != nil {
		t.Fatal(err)
	}
	if got := service.Authorize(ctx, input); got.Allow || got.ReasonCode != ReasonUserConsentMissing {
		t.Fatalf("next call after revoke must deny: %+v", got)
	}
}

func TestAuthorizationScopeInactiveUnknownAndDelegation(t *testing.T) {
	ctx := context.Background()
	service, version := newAuthorizationFixture(t)
	code := "schedule.self.read"
	if _, err := service.SetAdminGrant(ctx, version.ID, code, "granted", "允许课表提醒", map[string]interface{}{"scope": "self"}, "1", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := service.SetUserConsent(ctx, "42", version.ID, code, "granted", map[string]interface{}{"scope": "self"}); err != nil {
		t.Fatal(err)
	}
	if got := service.Authorize(ctx, AuthorizationInput{PluginName: version.PluginName, CapabilityCode: code, OperationCode: "host.GetSchedule", ActorUserID: "42", ResourceOwnerID: "7"}); got.ReasonCode != ReasonScopeMismatch {
		t.Fatalf("cross-user request: got %s", got.ReasonCode)
	}
	if got := service.Authorize(ctx, AuthorizationInput{PluginName: version.PluginName, CapabilityCode: "missing.capability", OperationCode: "unknown"}); got.ReasonCode != ReasonUnknownOperation {
		t.Fatalf("unknown operation: got %s", got.ReasonCode)
	}
	delegation, token, err := service.IssueDelegation(ctx, "42", version.ID, []string{code}, map[string]interface{}{"scope": "self"}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	background := AuthorizationInput{PluginName: version.PluginName, CapabilityCode: code, OperationCode: "job.remind", ResourceOwnerID: "42", ResourceScope: map[string]interface{}{"scope": "self"}, DelegationToken: token, Background: true}
	if got := service.Authorize(ctx, background); !got.Allow {
		t.Fatalf("delegated request: %+v", got)
	}
	if err := service.RevokeDelegation(ctx, "42", delegation.ID); err != nil {
		t.Fatal(err)
	}
	if got := service.Authorize(ctx, background); got.ReasonCode != ReasonDelegationInvalid {
		t.Fatalf("revoked delegation: got %s", got.ReasonCode)
	}
}

func BenchmarkAuthorizationHotPath(b *testing.B) {
	service, version := newAuthorizationFixture(b)
	ctx := context.Background()
	code := "config.system.read"
	_, _ = service.SetAdminGrant(ctx, version.ID, code, "granted", "读取配置", map[string]interface{}{"scope": "system"}, "1", nil)
	input := AuthorizationInput{PluginName: version.PluginName, CapabilityCode: code, OperationCode: "host.GetConfig", ResourceScope: map[string]interface{}{"scope": "system"}}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if !service.Authorize(ctx, input).Allow {
			b.Fatal("authorization unexpectedly denied")
		}
	}
}
