package plugin

import (
	"context"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPostgresAuthorizationRequiresActiveVersion(t *testing.T) {
	dsn := os.Getenv("CAMPUSOS_PG_INTEGRATION_DSN")
	if dsn == "" {
		t.Skip("set CAMPUSOS_PG_INTEGRATION_DSN to run PostgreSQL active-version integration")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	fixture := newVersionedHandlerFixture(t, pool)
	const code = "schedule.self.read"
	input := AuthorizationInput{
		PluginName: fixture.pluginName, PluginVersion: strconv.FormatInt(fixture.retiredVersion.ID, 10),
		CapabilityCode: code, OperationCode: "host.GetSchedule", ActorUserID: fixture.userID,
		ResourceOwnerID: fixture.userID, ResourceScope: map[string]interface{}{"scope": "self"},
	}
	if decision := fixture.service.Authorize(ctx, input); decision.ReasonCode != ReasonVersionMismatch {
		t.Fatalf("retired A with valid grant and consent allowed: %+v", decision)
	}
	if _, err := fixture.service.SetAdminGrant(ctx, fixture.retiredVersion.ID, code, "revoked", "retired version", map[string]interface{}{"scope": "self"}, fixture.userID, nil); err == nil {
		t.Fatal("direct service call changed retired PostgreSQL grant")
	}
	if _, err := fixture.service.SetUserConsent(ctx, fixture.userID, fixture.retiredVersion.ID, code, "revoked", map[string]interface{}{"scope": "self"}); err == nil {
		t.Fatal("direct service call changed retired PostgreSQL consent")
	}
	grant, err := fixture.store.CurrentAdminGrant(ctx, fixture.retiredVersion.ID, code)
	if err != nil || grant.ID != fixture.originalGrant[fixture.retiredVersion.ID] || grant.Status != "granted" {
		t.Fatalf("retired PostgreSQL grant mutated: %+v err=%v", grant, err)
	}
	userID, _ := strconv.ParseInt(fixture.userID, 10, 64)
	consent, err := fixture.store.CurrentUserConsent(ctx, userID, fixture.retiredVersion.ID, code)
	if err != nil || consent.ID != fixture.originalConsent[fixture.retiredVersion.ID] || consent.Status != "granted" {
		t.Fatalf("retired PostgreSQL consent mutated: %+v err=%v", consent, err)
	}
	if _, err := fixture.store.VersionByID(ctx, fixture.retiredVersion.ID); err != nil {
		t.Fatalf("historical version cannot be read: %v", err)
	}
	manifestA := versionTestManifest("1.0.0")
	manifestA.Name, manifestA.DisplayName = fixture.pluginName, fixture.pluginName
	if _, err := fixture.service.SyncInstalled(ctx, &Plugin{Manifest: manifestA, Checksum: strings.Repeat("a", 64)}, fixture.userID); err != nil {
		t.Fatal(err)
	}
	if decision := fixture.service.Authorize(ctx, input); !decision.Allow {
		t.Fatalf("reactivated A with current grant and consent denied: %+v", decision)
	}
	_, token, err := fixture.service.IssueDelegation(ctx, fixture.pluginName, fixture.userID, fixture.retiredVersion.ID, []string{code}, map[string]interface{}{"scope": "self"}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	before := fixture.delegationCount(t)

	manifestB := versionTestManifest("1.1.0")
	manifestB.Name, manifestB.DisplayName = fixture.pluginName, fixture.pluginName
	switching := &switchOnConsentStore{AuthorizationStore: fixture.store}
	switching.switchToB = func() error {
		_, err := fixture.service.SyncInstalled(ctx, &Plugin{Manifest: manifestB, Checksum: strings.Repeat("c", 64)}, fixture.userID)
		return err
	}
	fixture.service.store = switching
	if decision := fixture.service.Authorize(ctx, input); decision.ReasonCode != ReasonVersionMismatch {
		t.Fatalf("A allowed after B activated during PostgreSQL decision: %+v", decision)
	}
	if switching.switchErr != nil {
		t.Fatal(switching.switchErr)
	}
	if decision := fixture.service.Authorize(ctx, input); decision.ReasonCode != ReasonVersionMismatch {
		t.Fatalf("retired numeric A allowed: %+v", decision)
	}
	input.PluginVersion = manifestA.Version
	if decision := fixture.service.Authorize(ctx, input); decision.ReasonCode != ReasonVersionMismatch {
		t.Fatalf("retired semantic A allowed: %+v", decision)
	}
	if _, _, err := fixture.service.IssueDelegation(ctx, fixture.pluginName, fixture.userID, fixture.retiredVersion.ID, []string{code}, map[string]interface{}{"scope": "self"}, time.Minute); err == nil {
		t.Fatal("retired A issued a delegation")
	}
	if after := fixture.delegationCount(t); after != before {
		t.Fatalf("retired delegation persisted: before=%d after=%d", before, after)
	}
	if err := fixture.service.RequireActiveVersion(ctx, fixture.pluginName, fixture.otherVersion.ID); err == nil {
		t.Fatal("other plugin version accepted under first plugin name")
	}
	if _, err := fixture.service.SetAdminGrant(ctx, fixture.activeVersion.ID, code, "granted", "current B", map[string]interface{}{"scope": "self"}, fixture.userID, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.service.SetUserConsent(ctx, fixture.userID, fixture.activeVersion.ID, code, "granted", map[string]interface{}{"scope": "self"}); err != nil {
		t.Fatal(err)
	}
	input.PluginVersion = strconv.FormatInt(fixture.activeVersion.ID, 10)
	if decision := fixture.service.Authorize(ctx, input); !decision.Allow {
		t.Fatalf("current B denied: %+v", decision)
	}
	input.Background, input.DelegationToken, input.ActorUserID = true, token, ""
	if decision := fixture.service.Authorize(ctx, input); decision.ReasonCode != ReasonDelegationInvalid {
		t.Fatalf("old A delegation accepted by B: %+v", decision)
	}
}
