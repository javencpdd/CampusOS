package server

import (
	"context"
	"os"
	"testing"
	"time"

	identityrepo "github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/jackc/pgx/v5/pgxpool"
)

// This fixture only creates owned initial accounts. Challenge codes and Tickets
// are subsequently issued and verified by the normal HTTP/SMTP application.
func TestFinalIdentityRecoverySeedPostgres(t *testing.T) {
	raw := os.Getenv("CAMPUSOS_FINAL_RECOVERY_DSN")
	if raw == "" {
		t.Skip("owned Final recovery fixture is not configured")
	}
	cfg, err := pgxpool.ParseConfig(raw)
	if err != nil || cfg.ConnConfig.Database != "campusos_v12_final_admin_recovery" || cfg.ConnConfig.Host != "127.0.0.1" {
		t.Fatal("requires owned loopback Final recovery database")
	}
	ctx := context.Background()
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal("owned fixture database unavailable")
	}
	defer pool.Close()
	password := func(name string) string {
		value := os.Getenv(name)
		if len(value) < 24 || len(value) > 64 {
			t.Fatal("fresh fixture credentials required")
		}
		return value
	}
	bootstrap := password("AUTH_BOOTSTRAP_ADMIN_SECRET")
	if err := SeedAdmin(pool, adminSeedOptions{Environment: "test", PasswordHashEnabled: true, BootstrapAdminSecret: bootstrap}); err != nil {
		t.Fatal("normal bootstrap fixture failed")
	}
	userHash, err := auth.HashPassword(password("CAMPUSOS_FINAL_RECOVERY_USER_PASSWORD"))
	if err != nil {
		t.Fatal("fixture credential hashing failed")
	}
	_, err = pool.Exec(ctx, `INSERT INTO users(id,username,nickname,email,status) VALUES
 (870901,'final-recovery-user','Final recovery user','owned-final-recovery@example.test','active'),
 (870903,'final-recovery-user-only','Final user only','owned-final-user-only@example.test','active');
 INSERT INTO admin_roles(id,name,description,is_system) VALUES('final_recovery_readonly','Final readonly observer','owned recovery fixture',false);
 INSERT INTO admin_role_permissions(role_id,permission_code,created_by) VALUES('final_recovery_readonly','plugin.plugin.read','owned-fixture');`)
	if err != nil {
		t.Fatal("initial owned account facts failed")
	}
	_, err = pool.Exec(ctx, `INSERT INTO accounts(id,user_id,type,identifier,identifier_normalized,credential,verified,verification_state,verified_at,verification_source,password_changed_at,credential_version)
 VALUES(870902,870901,'email','owned-final-recovery@example.test','owned-final-recovery@example.test',$1,true,'verified',now(),'owned-fixture',now(),1)`, userHash)
	if err != nil {
		t.Fatal("owned verified email fixture failed")
	}
	admins := identityrepo.NewPgAdminDomainRepository(pool)
	for _, input := range []struct{ id, username, email, role, secret string }{
		{"870901", "final-recovery-security", "owned-final-recovery@example.test", "security_admin", password("CAMPUSOS_FINAL_RECOVERY_ADMIN_A_PASSWORD")},
		{"1000000000000000001", "final-recovery-system", "admin@campusos.local", "final_recovery_readonly", password("CAMPUSOS_FINAL_RECOVERY_ADMIN_B_PASSWORD")},
	} {
		if err := admins.CreateAccount(ctx, identityrepo.AdminDomainAccount{ID: input.id, Username: input.username, Email: input.email, DisplayName: input.username}); err != nil {
			t.Fatal("independent Admin fixture failed")
		}
		hash, err := auth.HashPassword(input.secret)
		if err != nil {
			t.Fatal("Admin fixture credential hashing failed")
		}
		if _, ok, err := admins.SetCredentialCAS(ctx, input.id, hash, 0, time.Now().UTC()); err != nil || !ok {
			t.Fatal("independent Admin credential fixture failed")
		}
		if err := admins.BindRole(ctx, identityrepo.AdminDomainRoleBinding{ID: "final-recovery-binding-" + input.id, AccountID: input.id, RoleID: input.role, GrantedBy: "owned-fixture"}); err != nil {
			t.Fatal("independent Admin binding fixture failed")
		}
	}
	t.Log("owned initial same-ID/email User/Admin fixtures created; no code or Ticket inserted")
}
