package plugin

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/campusos/CampusOS/pkg/idgen"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPostgresPublicationSealAndAuthorizationWrites(t *testing.T) {
	dsn := os.Getenv("CAMPUSOS_PG_INTEGRATION_DSN")
	if dsn == "" {
		t.Skip("set CAMPUSOS_PG_INTEGRATION_DSN to run PostgreSQL publication assertions")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	userID, pluginID := idgen.New(), idgen.New()
	userText := strconv.FormatInt(userID, 10)
	pluginName := fmt.Sprintf("v12-publication-%d", pluginID)
	if _, err := pool.Exec(ctx, "INSERT INTO users(id,username,nickname,email) VALUES($1,$2,$3,$4)",
		userID, "v12p_"+userText, "Publication Test", "v12-publication-"+userText+"@example.invalid"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM plugin_authorization_decisions WHERE plugin_version_id IN (SELECT id FROM plugin_versions WHERE plugin_id=$1)", pluginID)
		_, _ = pool.Exec(context.Background(), "DELETE FROM plugins WHERE id=$1", pluginID)
		_, _ = pool.Exec(context.Background(), "DELETE FROM users WHERE id=$1", userID)
	})
	repo := NewPgPluginRepository(pool)
	if err := repo.Save(ctx, &PluginRecord{
		ID: pluginID, Name: pluginName, DisplayName: pluginName, Version: "1.0.0",
		Runtime: "process", Status: string(StatusRunning),
		BackendState: string(BackendRunning), FrontendState: string(FrontendUnloaded),
		HealthState: string(HealthHealthy), Config: "{}", Checksum: strings.Repeat("a", 64),
		InstalledBy: "test", InstalledAt: time.Now(), UpdatedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	store := NewPgAuthorizationStore(pool)
	service := NewAuthorizationService(store, repo, func(string) bool { return true })
	manifest := func(version string) *Manifest {
		value := versionTestManifest(version)
		value.Name, value.DisplayName = pluginName, pluginName
		return value
	}
	syncVersion := func(version, digest string) PluginVersion {
		t.Helper()
		result, err := service.SyncInstalled(ctx, &Plugin{Manifest: manifest(version), Checksum: strings.Repeat(digest, 64)}, userText)
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	versionA := syncVersion("1.0.0", "a")
	declarations, err := store.ListDeclarations(ctx, versionA.ID)
	if err != nil || len(declarations) != 1 {
		t.Fatalf("staged publication did not create declarations: rows=%v err=%v", declarations, err)
	}
	var activatedAt *time.Time
	if err := pool.QueryRow(ctx, "SELECT activated_at FROM plugin_versions WHERE id=$1", versionA.ID).Scan(&activatedAt); err != nil || activatedAt == nil {
		t.Fatalf("published release has no activation stamp: value=%v err=%v", activatedAt, err)
	}

	const code = "schedule.self.read"
	grant, err := service.SetAdminGrant(ctx, versionA.ID, code, "granted", "publication test",
		map[string]interface{}{"scope": "self"}, userText, nil)
	if err != nil {
		t.Fatal(err)
	}
	consent, err := service.SetUserConsent(ctx, userText, versionA.ID, code, "granted",
		map[string]interface{}{"scope": "self"})
	if err != nil {
		t.Fatal(err)
	}
	delegation, token, err := service.IssueDelegation(ctx, pluginName, userText, versionA.ID,
		[]string{code}, map[string]interface{}{"scope": "self"}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	input := AuthorizationInput{
		PluginName: pluginName, PluginVersion: strconv.FormatInt(versionA.ID, 10),
		CapabilityCode: code, OperationCode: "host.GetSchedule", ActorUserID: userText,
		ResourceOwnerID: userText, ResourceScope: map[string]interface{}{"scope": "self"},
		DelegationToken: token,
	}
	if decision := service.Authorize(ctx, input); !decision.Allow {
		t.Fatalf("fresh delegation rejected: %+v", decision)
	}

	expectCheck := func(label string, err error) {
		t.Helper()
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
			t.Fatalf("%s: wanted SQLSTATE 23514, got %v", label, err)
		}
	}
	insertDeclaration := func(versionID int64) error {
		_, err := pool.Exec(ctx,
			"INSERT INTO plugin_capability_declarations(id,plugin_version_id,capability_code,purpose,resource_scope) VALUES($1,$2,'schedule.extra.read','late addition','{}'::jsonb)",
			idgen.New(), versionID)
		return err
	}
	expectCheck("active declaration insert", insertDeclaration(versionA.ID))
	_, err = pool.Exec(ctx, "DELETE FROM plugin_capability_declarations WHERE id=$1", declarations[0].ID)
	expectCheck("active declaration delete", err)
	_, err = pool.Exec(ctx, "UPDATE plugin_capability_declarations SET purpose='rewritten' WHERE id=$1", declarations[0].ID)
	expectCheck("active declaration update", err)
	_, err = pool.Exec(ctx, "UPDATE plugin_versions SET lifecycle_status='staged' WHERE id=$1", versionA.ID)
	expectCheck("published version restaging", err)
	_, err = pool.Exec(ctx, "UPDATE plugin_versions SET activated_at=NULL WHERE id=$1", versionA.ID)
	expectCheck("published activation reset", err)

	// Draft declarations can be built and removed before publication.
	stagedID := idgen.New()
	_, err = pool.Exec(ctx,
		"INSERT INTO plugin_versions(id,plugin_id,version,package_digest,manifest_api_version,host_api_version,permission_fingerprint,manifest,lifecycle_status) VALUES($1,$2,'0.5.0',$3,'v3','v3',$4,'{}'::jsonb,'staged')",
		stagedID, pluginID, strings.Repeat("e", 64), strings.Repeat("f", 64))
	if err != nil {
		t.Fatal(err)
	}
	if err := insertDeclaration(stagedID); err != nil {
		t.Fatalf("staged declaration insertion rejected: %v", err)
	}
	if _, err := pool.Exec(ctx, "DELETE FROM plugin_capability_declarations WHERE plugin_version_id=$1", stagedID); err != nil {
		t.Fatalf("staged declaration deletion rejected: %v", err)
	}
	if _, err := pool.Exec(ctx, "DELETE FROM plugin_versions WHERE id=$1", stagedID); err != nil {
		t.Fatalf("staged release cleanup rejected: %v", err)
	}

	versionB := syncVersion("1.1.0", "b")
	if decision := service.Authorize(ctx, input); decision.ReasonCode != ReasonVersionMismatch {
		t.Fatalf("retired A token accepted while B active: %+v", decision)
	}
	expectCheck("retired declaration insert", insertDeclaration(versionA.ID))
	_, err = pool.Exec(ctx, "DELETE FROM plugin_capability_declarations WHERE id=$1", declarations[0].ID)
	expectCheck("retired declaration delete", err)
	var status string
	var revokedAt *time.Time
	if err := pool.QueryRow(ctx, "SELECT status,revoked_at FROM plugin_delegations WHERE id=$1", delegation.ID).Scan(&status, &revokedAt); err != nil || status != "revoked" || revokedAt == nil {
		t.Fatalf("retired delegation was not durably revoked: status=%q revoked_at=%v err=%v", status, revokedAt, err)
	}
	if _, err := store.SetAdminGrant(ctx, AdminGrant{ID: idgen.New(), PluginVersionID: versionA.ID, CapabilityCode: code}); err == nil || !strings.Contains(err.Error(), "version mismatch") {
		t.Fatalf("retired version accepted admin grant: %v", err)
	}
	if _, err := store.SetUserConsent(ctx, UserConsent{ID: idgen.New(), UserID: userID, PluginVersionID: versionA.ID, CapabilityCode: code}); err == nil || !strings.Contains(err.Error(), "version mismatch") {
		t.Fatalf("retired version accepted user consent: %v", err)
	}
	if _, err := store.CreateDelegation(ctx, Delegation{ID: idgen.New(), PluginVersionID: versionA.ID, SubjectUserID: userID}); err == nil || !strings.Contains(err.Error(), "version mismatch") {
		t.Fatalf("retired version accepted delegation: %v", err)
	}

	reactivatedA := syncVersion("1.0.0", "a")
	if reactivatedA.ID != versionA.ID {
		t.Fatalf("A reactivation changed release identity: %d -> %d", versionA.ID, reactivatedA.ID)
	}
	if decision := service.Authorize(ctx, input); decision.ReasonCode != ReasonDelegationInvalid {
		t.Fatalf("old A token revived after A-B-A: %+v", decision)
	}
	if current, err := store.CurrentAdminGrant(ctx, versionA.ID, code); err != nil || current.ID != grant.ID {
		t.Fatalf("A grant not retained: %+v err=%v", current, err)
	}
	if current, err := store.CurrentUserConsent(ctx, userID, versionA.ID, code); err != nil || current.ID != consent.ID {
		t.Fatalf("A consent not retained: %+v err=%v", current, err)
	}

	// Hold the version update open, then start all three writes. Each must
	// wait for the lifecycle commit and reject the now-retired release.
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, "UPDATE plugin_versions SET lifecycle_status='retired',retired_at=NOW() WHERE id=$1", versionA.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, "UPDATE plugin_versions SET lifecycle_status='active',retired_at=NULL WHERE id=$1", versionB.ID); err != nil {
		t.Fatal(err)
	}
	writeResults := make(chan error, 3)
	go func() {
		_, err := store.SetAdminGrant(ctx, AdminGrant{ID: idgen.New(), PluginVersionID: versionA.ID, CapabilityCode: code})
		writeResults <- err
	}()
	go func() {
		_, err := store.SetUserConsent(ctx, UserConsent{ID: idgen.New(), UserID: userID, PluginVersionID: versionA.ID, CapabilityCode: code})
		writeResults <- err
	}()
	go func() {
		_, err := store.CreateDelegation(ctx, Delegation{ID: idgen.New(), PluginVersionID: versionA.ID, SubjectUserID: userID})
		writeResults <- err
	}()
	select {
	case err := <-writeResults:
		t.Fatalf("version-bound write returned before lifecycle commit: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 3; index++ {
		select {
		case err := <-writeResults:
			if err == nil || !strings.Contains(err.Error(), "version mismatch") {
				t.Fatalf("write %d after retirement: %v", index, err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("version-bound write did not unblock after lifecycle commit")
		}
	}
	var grantCount, consentCount, delegationCount int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM plugin_admin_grants WHERE plugin_version_id=$1", versionA.ID).Scan(&grantCount); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM plugin_user_consents WHERE plugin_version_id=$1", versionA.ID).Scan(&consentCount); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM plugin_delegations WHERE plugin_version_id=$1", versionA.ID).Scan(&delegationCount); err != nil {
		t.Fatal(err)
	}
	if grantCount != 1 || consentCount != 1 || delegationCount != 1 {
		t.Fatalf("retired release gained authorization facts: grants=%d consents=%d delegations=%d", grantCount, consentCount, delegationCount)
	}

	// The publication seal must not turn plugin uninstall into a blocked
	// migration: deleting the parent still cascades its declarations.
	if _, err := pool.Exec(ctx, "DELETE FROM plugin_authorization_decisions WHERE plugin_version_id IN (SELECT id FROM plugin_versions WHERE plugin_id=$1)", pluginID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "DELETE FROM plugins WHERE id=$1", pluginID); err != nil {
		t.Fatalf("published declaration guard blocked plugin uninstall cascade: %v", err)
	}
}
