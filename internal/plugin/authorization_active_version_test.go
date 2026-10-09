package plugin

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestAuthorizationRequiresCurrentActiveVersion(t *testing.T) {
	ctx := context.Background()
	service, store := newVersionTestService(t)
	manifestA := versionTestManifest("1.0.0")
	versionA, err := service.SyncInstalled(ctx, &Plugin{Manifest: manifestA, Checksum: strings.Repeat("a", 64)}, "1")
	if err != nil {
		t.Fatal(err)
	}
	const code = "schedule.self.read"
	if _, err := service.SetAdminGrant(ctx, versionA.ID, code, "granted", "test", map[string]interface{}{"scope": "self"}, "1", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := service.SetUserConsent(ctx, "42", versionA.ID, code, "granted", map[string]interface{}{"scope": "self"}); err != nil {
		t.Fatal(err)
	}
	input := AuthorizationInput{
		PluginName: manifestA.Name, PluginVersion: strconv.FormatInt(versionA.ID, 10),
		CapabilityCode: code, OperationCode: "host.GetSchedule", ActorUserID: "42",
		ResourceOwnerID: "42", ResourceScope: map[string]interface{}{"scope": "self"},
	}
	if decision := service.Authorize(ctx, input); !decision.Allow {
		t.Fatalf("current A denied: %+v", decision)
	}
	_, oldToken, err := service.IssueDelegation(ctx, manifestA.Name, "42", versionA.ID, []string{code}, map[string]interface{}{"scope": "self"}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	manifestB := versionTestManifest("1.1.0")
	versionB, err := service.SyncInstalled(ctx, &Plugin{Manifest: manifestB, Checksum: strings.Repeat("b", 64)}, "1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.SetAdminGrant(ctx, versionB.ID, code, "granted", "test", map[string]interface{}{"scope": "self"}, "1", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := service.SetUserConsent(ctx, "42", versionB.ID, code, "granted", map[string]interface{}{"scope": "self"}); err != nil {
		t.Fatal(err)
	}
	oldDelegation, err := store.DelegationByDigest(ctx, tokenDigest(oldToken))
	if err != nil || oldDelegation.Status != "revoked" || oldDelegation.RevokedAt == nil {
		t.Fatalf("retired A delegation not revoked: %+v err=%v", oldDelegation, err)
	}
	if _, err := store.SetAdminGrant(ctx, AdminGrant{PluginVersionID: versionA.ID, CapabilityCode: code, Status: "granted"}); err == nil {
		t.Fatal("memory store wrote grant to retired A")
	}
	if _, err := store.SetUserConsent(ctx, UserConsent{UserID: 42, PluginVersionID: versionA.ID, CapabilityCode: code, Status: "granted"}); err == nil {
		t.Fatal("memory store wrote consent to retired A")
	}
	if _, err := store.CreateDelegation(ctx, Delegation{PluginVersionID: versionA.ID, TokenDigest: tokenDigest("stale")}); err == nil {
		t.Fatal("memory store wrote delegation to retired A")
	}
	if decision := service.Authorize(ctx, input); decision.ReasonCode != ReasonVersionMismatch {
		t.Fatalf("retired numeric A allowed: %+v", decision)
	}
	oldGrant, err := store.CurrentAdminGrant(ctx, versionA.ID, code)
	if err != nil {
		t.Fatal(err)
	}
	oldConsent, err := store.CurrentUserConsent(ctx, 42, versionA.ID, code)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.SetAdminGrant(ctx, versionA.ID, code, "revoked", "retired version", map[string]interface{}{"scope": "self"}, "1", nil); err == nil {
		t.Fatal("direct service call changed retired A grant")
	}
	if _, err := service.SetUserConsent(ctx, "42", versionA.ID, code, "revoked", map[string]interface{}{"scope": "self"}); err == nil {
		t.Fatal("direct service call changed retired A consent")
	}
	currentGrant, err := store.CurrentAdminGrant(ctx, versionA.ID, code)
	if err != nil || currentGrant.ID != oldGrant.ID || currentGrant.Status != "granted" {
		t.Fatalf("retired grant mutated: %+v err=%v", currentGrant, err)
	}
	currentConsent, err := store.CurrentUserConsent(ctx, 42, versionA.ID, code)
	if err != nil || currentConsent.ID != oldConsent.ID || currentConsent.Status != "granted" {
		t.Fatalf("retired consent mutated: %+v err=%v", currentConsent, err)
	}
	input.PluginVersion = manifestA.Version
	if decision := service.Authorize(ctx, input); decision.ReasonCode != ReasonVersionMismatch {
		t.Fatalf("retired semantic A allowed: %+v", decision)
	}
	input.PluginVersion = strconv.FormatInt(versionB.ID, 10)
	if decision := service.Authorize(ctx, input); !decision.Allow {
		t.Fatalf("current B denied: %+v", decision)
	}
	input.Background, input.DelegationToken = true, oldToken
	input.ActorUserID = ""
	if decision := service.Authorize(ctx, input); decision.ReasonCode != ReasonDelegationInvalid {
		t.Fatalf("A delegation accepted by B: %+v", decision)
	}
	store.mu.RLock()
	before := len(store.delegations)
	store.mu.RUnlock()
	if _, _, err := service.IssueDelegation(ctx, manifestA.Name, "42", versionA.ID, []string{code}, map[string]interface{}{"scope": "self"}, time.Minute); err == nil {
		t.Fatal("retired A issued a new delegation")
	}
	store.mu.RLock()
	after := len(store.delegations)
	store.mu.RUnlock()
	if after != before {
		t.Fatalf("retired delegation persisted: before=%d after=%d", before, after)
	}
	if err := service.RequireActiveVersion(ctx, "another-plugin", versionB.ID); err == nil {
		t.Fatal("wrong plugin name accepted current B version ID")
	}
	if _, err := store.VersionByID(ctx, versionA.ID); err != nil {
		t.Fatalf("historical A disappeared: %v", err)
	}

	if _, err := service.SyncInstalled(ctx, &Plugin{Manifest: manifestA, Checksum: strings.Repeat("a", 64)}, "1"); err != nil {
		t.Fatal(err)
	}
	input.Background, input.DelegationToken, input.ActorUserID = false, "", "42"
	input.PluginVersion = strconv.FormatInt(versionA.ID, 10)
	if decision := service.Authorize(ctx, input); !decision.Allow {
		t.Fatalf("reactivated A with valid current facts denied: %+v", decision)
	}
	input.Background, input.DelegationToken, input.ActorUserID = true, oldToken, ""
	if decision := service.Authorize(ctx, input); decision.ReasonCode != ReasonDelegationInvalid {
		t.Fatalf("retired A delegation revived after A reactivation: %+v", decision)
	}
	input.Background, input.DelegationToken, input.ActorUserID = false, "", "42"
	if _, err := service.SetUserConsent(ctx, "42", versionA.ID, code, "revoked", map[string]interface{}{"scope": "self"}); err != nil {
		t.Fatal(err)
	}
	if decision := service.Authorize(ctx, input); decision.ReasonCode != ReasonUserConsentMissing {
		t.Fatalf("revoked reactivated A allowed: %+v", decision)
	}
}

type switchOnConsentStore struct {
	AuthorizationStore
	once      sync.Once
	switchToB func() error
	switchErr error
}

func (s *switchOnConsentStore) CurrentUserConsent(ctx context.Context, userID, versionID int64, code string) (UserConsent, error) {
	consent, err := s.AuthorizationStore.CurrentUserConsent(ctx, userID, versionID, code)
	s.once.Do(func() { s.switchErr = s.switchToB() })
	return consent, err
}

func TestAuthorizationRechecksActiveVersionBeforeAllow(t *testing.T) {
	ctx := context.Background()
	service, memory := newVersionTestService(t)
	manifestA := versionTestManifest("1.0.0")
	versionA, err := service.SyncInstalled(ctx, &Plugin{Manifest: manifestA, Checksum: strings.Repeat("a", 64)}, "1")
	if err != nil {
		t.Fatal(err)
	}
	const code = "schedule.self.read"
	if _, err := service.SetAdminGrant(ctx, versionA.ID, code, "granted", "test", map[string]interface{}{"scope": "self"}, "1", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := service.SetUserConsent(ctx, "42", versionA.ID, code, "granted", map[string]interface{}{"scope": "self"}); err != nil {
		t.Fatal(err)
	}
	manifestB := versionTestManifest("1.1.0")
	switching := &switchOnConsentStore{AuthorizationStore: memory}
	switching.switchToB = func() error {
		_, err := service.SyncInstalled(ctx, &Plugin{Manifest: manifestB, Checksum: strings.Repeat("b", 64)}, "1")
		return err
	}
	service.store = switching
	decision := service.Authorize(ctx, AuthorizationInput{
		PluginName: manifestA.Name, PluginVersion: strconv.FormatInt(versionA.ID, 10),
		CapabilityCode: code, OperationCode: "host.GetSchedule", ActorUserID: "42",
		ResourceOwnerID: "42", ResourceScope: map[string]interface{}{"scope": "self"},
	})
	if switching.switchErr != nil {
		t.Fatal(switching.switchErr)
	}
	if decision.Allow || decision.ReasonCode != ReasonVersionMismatch {
		t.Fatalf("version switched during decision but was allowed: %+v", decision)
	}
}

type failingDecisionStore struct{ AuthorizationStore }

func (f failingDecisionStore) SaveAuthorizationDecision(context.Context, AuthorizationDecision) error {
	return errors.New("decision storage unavailable")
}

func TestAuthorizationFailsClosedWhenDecisionAuditFails(t *testing.T) {
	ctx := context.Background()
	service, store := newVersionTestService(t)
	manifest := versionTestManifest("1.0.0")
	version, err := service.SyncInstalled(ctx, &Plugin{Manifest: manifest, Checksum: strings.Repeat("a", 64)}, "1")
	if err != nil {
		t.Fatal(err)
	}
	const code = "schedule.self.read"
	if _, err := service.SetAdminGrant(ctx, version.ID, code, "granted", "test", map[string]interface{}{"scope": "self"}, "1", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := service.SetUserConsent(ctx, "42", version.ID, code, "granted", map[string]interface{}{"scope": "self"}); err != nil {
		t.Fatal(err)
	}
	service.store = failingDecisionStore{AuthorizationStore: store}
	decision := service.Authorize(ctx, AuthorizationInput{
		PluginName: manifest.Name, PluginVersion: strconv.FormatInt(version.ID, 10),
		CapabilityCode: code, OperationCode: "host.GetSchedule", ActorUserID: "42",
		ResourceOwnerID: "42", ResourceScope: map[string]interface{}{"scope": "self"},
	})
	if decision.Allow || decision.ReasonCode != ReasonSystemPolicy {
		t.Fatalf("audit persistence failed but authorization did not fail closed: %+v", decision)
	}
}
