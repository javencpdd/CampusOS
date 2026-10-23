package recovery

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	repo "github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	service "github.com/campusos/CampusOS/internal/modules/core/identity/service"
	"github.com/campusos/CampusOS/pkg/auth"
)

func TestRecoveryPostgresCurrentCredentialsAndBarrier(t *testing.T) {
	dsn := os.Getenv("CAMPUSOS_RECOVERY_TEST_DSN")
	if dsn == "" {
		t.Skip("requires exact owned recovery PostgreSQL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool, e := OpenPool(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	if pool.Config().ConnConfig.Database != "campusos_v12_08b_recovery" || pool.Config().ConnConfig.Host != "127.0.0.1" {
		t.Fatal("refuse non-owned recovery database")
	}
	root := t.TempDir()
	if _, e = pool.Exec(ctx, `INSERT INTO users(id,username,nickname,email) VALUES(9801001,'recovery-alice','Recovery Alice','recovery-alice@example.test')`); e != nil {
		t.Fatal(e)
	}
	users := repo.NewPgUserRepository(pool)
	sessionsRepo := repo.NewPgSessionRepository(pool)
	jwt := auth.NewJWTManager(auth.JWTConfig{Secret: "owned-test-base-key", AccessTTL: time.Hour})
	sessions, e := service.NewSessionService(sessionsRepo, users, jwt, service.SessionConfig{IPHashSecret: "owned-test-ip-key"})
	if e != nil {
		t.Fatal(e)
	}
	user, e := users.GetByID(ctx, "9801001")
	if e != nil {
		t.Fatal(e)
	}
	tokens, e := sessions.Issue(ctx, user, service.SessionMetadata{})
	if e != nil {
		t.Fatal(e)
	}
	claims, e := jwt.VerifyAccessToken(tokens.AccessToken)
	if e != nil || sessions.VerifyAccess(ctx, claims) != nil {
		t.Fatal("seed User session not live")
	}
	admins := repo.NewPgAdminDomainRepository(pool)
	ar := repo.NewPgAdminSessionRepository(pool)
	if e = admins.CreateAccount(ctx, repo.AdminDomainAccount{ID: "adm.recovery", Username: "recovery-admin", Email: "recovery-admin@example.test", DisplayName: "Recovery"}); e != nil {
		t.Fatal(e)
	}
	password := "owned-test-password-0123456789"
	hash, e := auth.HashPassword(password)
	if e != nil {
		t.Fatal(e)
	}
	if _, ok, e := admins.SetCredentialCAS(ctx, "adm.recovery", hash, 0, time.Now()); e != nil || !ok {
		t.Fatal("admin credential", e)
	}
	adminSessions := service.NewAdminSessionService(admins, ar, jwt)
	adminTokens, e := adminSessions.Login(ctx, "recovery-admin", password, service.AdminSessionMetadata{})
	if e != nil {
		t.Fatal(e)
	}
	adminClaims, e := jwt.VerifyAccessToken(adminTokens.AccessToken)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = adminSessions.VerifyAccess(ctx, adminClaims.SessionID); e != nil {
		t.Fatal(e)
	}
	// Shared startup lock excludes maintenance while the API is alive.
	_, release, e := Startup(ctx, pool, root)
	if e != nil {
		t.Fatal(e)
	}
	if c, unlock, e := exclusive(ctx, pool); e == nil {
		unlock()
		_ = c
		t.Fatal("live Server did not exclude maintenance")
	}
	release()
	epoch, e := NewEpoch()
	if e != nil {
		t.Fatal(e)
	}
	state := State{Epoch: epoch, ArchiveSHA: strings.Repeat("a", 64), Phase: "quarantined", EgressPaused: true}
	if e = WriteMarker(root, state); e != nil {
		t.Fatal(e)
	}
	c, unlock, e := exclusive(ctx, pool)
	if e != nil {
		t.Fatal(e)
	}
	// Required quarantine audit/storage failure rolls back every revocation.
	_, e = c.Exec(ctx, `CREATE FUNCTION fail_recovery_evidence() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'owned recovery fault'; END $$; CREATE TRIGGER fail_recovery_evidence BEFORE INSERT ON instance_recovery_epochs FOR EACH ROW EXECUTE FUNCTION fail_recovery_evidence()`)
	if e != nil {
		unlock()
		t.Fatal(e)
	}
	if e = Quarantine(ctx, c, state); e == nil {
		unlock()
		t.Fatal("required evidence failure ignored")
	}
	if e = sessions.VerifyAccess(ctx, claims); e != nil {
		unlock()
		t.Fatal("partial revocation escaped failed transaction")
	}
	if _, e = c.Exec(ctx, `DROP TRIGGER fail_recovery_evidence ON instance_recovery_epochs; DROP FUNCTION fail_recovery_evidence()`); e != nil {
		unlock()
		t.Fatal(e)
	}
	if e = Quarantine(ctx, c, state); e != nil {
		unlock()
		t.Fatal("quarantine", e)
	}
	unlock()
	if e = sessions.VerifyAccess(ctx, claims); e == nil {
		t.Fatal("restored User access revived")
	}
	if _, e = sessions.Refresh(ctx, tokens.RefreshToken, service.SessionMetadata{}); e == nil {
		t.Fatal("restored User refresh revived")
	}
	if _, e = adminSessions.VerifyAccess(ctx, adminClaims.SessionID); e == nil {
		t.Fatal("restored Admin access revived")
	}
	if _, e = adminSessions.Refresh(ctx, adminTokens.RefreshToken, service.AdminSessionMetadata{}); e == nil {
		t.Fatal("restored Admin refresh revived")
	}
	rotated := auth.NewJWTManager(auth.JWTConfig{Secret: DeriveSecret("owned-test-base-key", epoch)})
	if _, e = rotated.VerifyAccessToken(tokens.AccessToken); e == nil {
		t.Fatal("old JWT signing epoch accepted")
	}
	if _, _, e = Startup(ctx, pool, root); e == nil {
		t.Fatal("quarantined Server started")
	}
	snapshot, e := Snapshot(ctx, pool)
	if e != nil {
		t.Fatal("inventory", e)
	}
	sha := snapshot["inventory_sha256"].(string)
	if _, e = Review(ctx, pool, root, epoch, strings.Repeat("b", 64), "owned-operator", "owned review"); e == nil {
		t.Fatal("stale review accepted")
	}
	if _, e = Review(ctx, pool, root, epoch, sha, "owned-operator", "owned reviewed authority inventory"); e != nil {
		t.Fatal("review", e)
	}
	var auditBefore, auditAfter int
	if e = pool.QueryRow(ctx, `SELECT jsonb_array_length(audit) FROM instance_recovery_epochs`).Scan(&auditBefore); e != nil {
		t.Fatal(e)
	}
	if e = WriteMarker(root, state); e != nil {
		t.Fatal(e)
	}
	if _, e = Review(ctx, pool, root, epoch, sha, "owned-operator", "repair marker after committed review"); e != nil {
		t.Fatal("review marker retry", e)
	}
	if e = pool.QueryRow(ctx, `SELECT jsonb_array_length(audit) FROM instance_recovery_epochs`).Scan(&auditAfter); e != nil || auditBefore != auditAfter {
		t.Fatal("review retry duplicated evidence")
	}
	current, release, e := Startup(ctx, pool, root)
	if e != nil {
		t.Fatal(e)
	}
	release()
	if current.Epoch != epoch || !current.EgressPaused {
		t.Fatal("lost authentication epoch/egress quarantine")
	}
	// A missing reviewed file cannot bypass the database/file agreement.
	if e = os.Remove(root + "/" + markerName); e != nil {
		t.Fatal(e)
	}
	if _, _, e = Startup(ctx, pool, root); e == nil {
		t.Fatal("lost marker accepted")
	}
	proof, _ := json.Marshal(map[string]any{"status": "passed", "user_access": "denied", "user_refresh": "denied", "admin_access": "denied", "admin_refresh": "denied", "old_jwt_epoch": "denied", "concurrent_maintenance": "denied", "audit_failure_atomic": true, "stale_review": "denied", "unreviewed_startup": "denied", "reviewed_startup": "passed", "missing_marker": "denied", "egress_paused": true})
	fmt.Printf("V12_RECOVERY_PROOF %s\n", proof)
}
