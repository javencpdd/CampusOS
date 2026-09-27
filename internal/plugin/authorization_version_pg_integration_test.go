package plugin

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/campusos/CampusOS/pkg/idgen"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPostgresVersionIdentityAndConcurrentActivation(t *testing.T) {
	dsn := os.Getenv("CAMPUSOS_PG_INTEGRATION_DSN")
	if dsn == "" {
		t.Skip("set CAMPUSOS_PG_INTEGRATION_DSN to run PostgreSQL plugin version integration")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	userID := idgen.New()
	userIDText := strconv.FormatInt(userID, 10)
	if _, err := pool.Exec(ctx, `INSERT INTO users(id,username,nickname,email) VALUES($1,$2,$3,$4)`,
		userID, "v12_version_"+userIDText, "Version Test", "v12-version-"+userIDText+"@example.invalid"); err != nil {
		t.Fatal(err)
	}
	record := &PluginRecord{
		ID: idgen.New(), DisplayName: "Version Identity Fixture", Version: "1.0.0",
		Runtime: "process", Status: string(StatusRunning),
		BackendState: string(BackendRunning), FrontendState: string(FrontendUnloaded), HealthState: string(HealthHealthy),
		Config: `{}`, Checksum: strings.Repeat("a", 64), InstalledBy: "test",
		InstalledAt: time.Now(), UpdatedAt: time.Now(),
	}
	record.Name = fmt.Sprintf("v12-version-%d", record.ID)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM plugins WHERE id=$1`, record.ID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id=$1`, userID)
	})
	repo := NewPgPluginRepository(pool)
	if err := repo.Save(ctx, record); err != nil {
		t.Fatal(err)
	}
	store := NewPgAuthorizationStore(pool)
	service := NewAuthorizationService(store, repo, func(string) bool { return true })
	manifest := func(version string) *Manifest {
		value := versionTestManifest(version)
		value.Name, value.DisplayName = record.Name, record.DisplayName
		return value
	}
	manifestA := manifest("1.0.0")
	versionA, err := service.SyncInstalled(ctx, &Plugin{Manifest: manifestA, Checksum: strings.Repeat("a", 64)}, userIDText)
	if err != nil {
		t.Fatal(err)
	}
	originalDeclarations, err := store.ListDeclarations(ctx, versionA.ID)
	if err != nil || len(originalDeclarations) != 1 {
		t.Fatalf("declarations=%v err=%v", originalDeclarations, err)
	}
	const code = "schedule.self.read"
	if _, err := service.SetAdminGrant(ctx, versionA.ID, code, "granted", "test", map[string]interface{}{"scope": "self"}, userIDText, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := service.SetUserConsent(ctx, userIDText, versionA.ID, code, "revoked", map[string]interface{}{"scope": "self"}); err != nil {
		t.Fatal(err)
	}
	manifestB := manifest("1.1.0")
	versionB, err := service.SyncInstalled(ctx, &Plugin{Manifest: manifestB, Checksum: strings.Repeat("b", 64)}, userIDText)
	if err != nil {
		t.Fatal(err)
	}
	reactivatedA, err := service.SyncInstalled(ctx, &Plugin{Manifest: manifestA, Checksum: strings.Repeat("a", 64)}, userIDText)
	if err != nil {
		t.Fatal(err)
	}
	if reactivatedA.ID != versionA.ID {
		t.Fatalf("reactivation created a new ID: initial=%d current=%d", versionA.ID, reactivatedA.ID)
	}
	if retired, err := store.VersionByID(ctx, versionB.ID); err != nil || retired.LifecycleStatus != "retired" {
		t.Fatalf("B after reactivating A=%+v err=%v", retired, err)
	}
	if active, err := store.ActiveVersion(ctx, record.Name); err != nil || active.ID != versionA.ID {
		t.Fatalf("active version=%+v err=%v", active, err)
	}
	currentDeclarations, err := store.ListDeclarations(ctx, versionA.ID)
	if err != nil || len(currentDeclarations) != 1 || currentDeclarations[0].ID != originalDeclarations[0].ID {
		t.Fatalf("declaration identity changed: initial=%v current=%v err=%v", originalDeclarations, currentDeclarations, err)
	}
	if consent, err := store.CurrentUserConsent(ctx, userID, versionA.ID, code); err != nil || consent.Status != "revoked" {
		t.Fatalf("revoked consent lost: %+v err=%v", consent, err)
	}
	input := AuthorizationInput{PluginName: record.Name, CapabilityCode: code, OperationCode: "host.GetSchedule", ActorUserID: userIDText, ResourceOwnerID: userIDText, ResourceScope: map[string]interface{}{"scope": "self"}}
	if decision := service.Authorize(ctx, input); decision.ReasonCode != ReasonUserConsentMissing {
		t.Fatalf("revoked consent must still deny after reactivation: %+v", decision)
	}

	changed := *manifestA
	changed.Description = "mutated after publication"
	if _, err := service.SyncInstalled(ctx, &Plugin{Manifest: &changed, Checksum: strings.Repeat("a", 64)}, userIDText); err == nil {
		t.Fatal("same version accepted changed manifest")
	}
	if _, err := service.SyncInstalled(ctx, &Plugin{Manifest: manifestA, Checksum: strings.Repeat("c", 64)}, userIDText); err == nil {
		t.Fatal("same version accepted changed package digest")
	}
	manifestReused := manifest("1.2.0")
	if _, err := service.SyncInstalled(ctx, &Plugin{Manifest: manifestReused, Checksum: strings.Repeat("a", 64)}, userIDText); err == nil {
		t.Fatal("different version reused package digest")
	}
	if _, err := pool.Exec(ctx, `UPDATE plugin_versions SET package_digest=$2 WHERE id=$1`, versionA.ID, strings.Repeat("f", 64)); err == nil {
		t.Fatal("database accepted direct package digest mutation")
	}
	if _, err := pool.Exec(ctx, `UPDATE plugin_versions SET manifest=jsonb_set(manifest,'{description}','"mutated"'::jsonb) WHERE id=$1`, versionA.ID); err == nil {
		t.Fatal("database accepted direct manifest mutation")
	}
	if _, err := pool.Exec(ctx, `UPDATE plugin_capability_declarations SET purpose='mutated' WHERE id=$1`, originalDeclarations[0].ID); err == nil {
		t.Fatal("database accepted direct declaration mutation")
	}

	const concurrentVersions = 8
	start := make(chan struct{})
	var wg sync.WaitGroup
	errs := make(chan error, concurrentVersions)
	for index := 0; index < concurrentVersions; index++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			<-start
			candidate := manifest(fmt.Sprintf("2.0.%d", index))
			digest := fmt.Sprintf("%064x", index+1)
			_, err := service.SyncInstalled(ctx, &Plugin{Manifest: candidate, Checksum: digest}, userIDText)
			errs <- err
		}(index)
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent activation: %v", err)
		}
	}
	var activeCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM plugin_versions WHERE plugin_id=$1 AND lifecycle_status='active'`, record.ID).Scan(&activeCount); err != nil || activeCount != 1 {
		t.Fatalf("active version count=%d err=%v", activeCount, err)
	}
}
