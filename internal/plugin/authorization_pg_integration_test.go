package plugin

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPostgresAuthorizationAndSecretRoundTrip(t *testing.T) {
	dsn := os.Getenv("CAMPUSOS_PG_INTEGRATION_DSN")
	if dsn == "" {
		t.Skip("set CAMPUSOS_PG_INTEGRATION_DSN to run PostgreSQL authorization integration")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil { t.Fatal(err) }
	defer pool.Close()
	if _, err := pool.Exec(ctx, `INSERT INTO users(id,username,nickname,email) VALUES(42,'plugin_test_user','插件测试','plugin-test@example.invalid') ON CONFLICT(id) DO NOTHING`); err != nil { t.Fatal(err) }
	repo := NewPgPluginRepository(pool)
	record := &PluginRecord{ID: 101, Name: "pg-authorization-fixture", DisplayName: "PG Fixture", Version: "1.0.0", Runtime: "process", Status: string(StatusRunning), BackendState: string(BackendRunning), FrontendState: string(FrontendUnloaded), HealthState: string(HealthHealthy), Config: `{}`, Checksum: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", InstalledBy: "test", InstalledAt: time.Now(), UpdatedAt: time.Now()}
	if err := repo.Save(ctx, record); err != nil { t.Fatal(err) }
	manifest := &Manifest{Name: record.Name, DisplayName: record.DisplayName, Version: record.Version, APIVersion: ManifestAPIVersionV3, HostAPIVersion: HostAPIVersionV3, Runtime: "process", Scope: ScopeUser, CapabilityDeclarations: []CapabilityRequest{{Code: "schedule.self.read", Required: true, Purpose: "集成测试课表读取", Scope: "self"}}}
	store := NewPgAuthorizationStore(pool)
	service := NewAuthorizationService(store, repo, func(string) bool { return true })
	version, err := service.SyncInstalled(ctx, &Plugin{Manifest: manifest, Checksum: record.Checksum}, "42")
	if err != nil { t.Fatal(err) }
	if _, err := service.SetAdminGrant(ctx, version.ID, "schedule.self.read", "granted", "integration", map[string]interface{}{"scope": "self"}, "42", nil); err != nil { t.Fatal(err) }
	if _, err := service.SetUserConsent(ctx, "42", version.ID, "schedule.self.read", "granted", map[string]interface{}{"scope": "self"}); err != nil { t.Fatal(err) }
	if result := service.Authorize(ctx, AuthorizationInput{PluginName: record.Name, CapabilityCode: "schedule.self.read", OperationCode: "integration.read", ActorUserID: "42", ResourceOwnerID: "42", ResourceScope: map[string]interface{}{"scope": "self"}}); !result.Allow { t.Fatalf("authorization denied: %+v", result) }
	if result := service.Authorize(ctx, AuthorizationInput{PluginName: record.Name, CapabilityCode: "user.contact.read", OperationCode: "integration.undeclared", ActorUserID: "42", ResourceOwnerID: "42"}); result.ReasonCode != ReasonCapabilityNotDeclared { t.Fatalf("undeclared reason=%s", result.ReasonCode) }
	decisions, err := service.Decisions(ctx, record.Name, 10)
	if err != nil || len(decisions) < 2 { t.Fatalf("decision audit len=%d err=%v", len(decisions), err) }
	secrets, err := NewSecretService(store, []byte("0123456789abcdef0123456789abcdef"), "integration-v1")
	if err != nil { t.Fatal(err) }
	owner := int64(42)
	if _, err := secrets.Put(ctx, record.ID, &owner, "mail.password", "first", &owner); err != nil { t.Fatal(err) }
	if _, err := secrets.Put(ctx, record.ID, &owner, "mail.password", "second", &owner); err != nil { t.Fatalf("secret rotation: %v", err) }
	if value, err := secrets.Resolve(ctx, record.ID, &owner, "mail.password"); err != nil || value != "second" { t.Fatalf("secret resolve value=%q err=%v", value, err) }
}
