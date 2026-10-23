package server

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	communitydomain "github.com/campusos/CampusOS/internal/modules/core/community/domain"
	communityrepo "github.com/campusos/CampusOS/internal/modules/core/community/repository"
	identityrepo "github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Seeds only the exact owned08b source DB. Password/key inputs stay in the
// caller's environment; this test prints counts, never credentials or bodies.
func TestV12RecoverySeedAccountsAndCapacity(t *testing.T) {
	dsn := os.Getenv("CAMPUSOS_V12_BACKUP_DSN")
	if dsn == "" {
		t.Skip("requires owned08b source database")
	}
	cfg, e := pgxpool.ParseConfig(dsn)
	if e != nil || cfg.ConnConfig.Database != "campusos_v12_08b_backup_seed" || cfg.ConnConfig.Host != "127.0.0.1" {
		t.Fatal("refuse non-owned source database")
	}
	password, machine := os.Getenv("CAMPUSOS_RECOVERY_PASSWORD"), os.Getenv("CAMPUSOS_RECOVERY_MACHINE_KEY")
	if len(password) < 24 || !strings.HasPrefix(machine, "cint1_0123456789abcdef.") {
		t.Fatal("requires fresh owned credentials")
	}
	ctx := context.Background()
	pool, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	hash, e := auth.HashPassword(password)
	if e != nil {
		t.Fatal(e)
	}
	users := identityrepo.NewPgUserRepository(pool)
	for _, owner := range []string{"2801001", "2801002"} {
		u, e := users.GetByID(ctx, owner)
		if e != nil {
			t.Fatal(e)
		}
		if e = users.CreateVerifiedAccount(ctx, owner, u.Email, hash); e != nil {
			t.Fatal(e)
		}
	}
	admins := identityrepo.NewPgAdminDomainRepository(pool)
	if e = admins.CreateAccount(ctx, identityrepo.AdminDomainAccount{ID: "adm.restore.operator", Username: "restore-operator", Email: "restore-operator@example.test", DisplayName: "恢复验收管理员"}); e != nil {
		t.Fatal(e)
	}
	if _, ok, e := admins.SetCredentialCAS(ctx, "adm.restore.operator", hash, 0, time.Now()); e != nil || !ok {
		t.Fatal(e)
	}
	if e = admins.BindRole(ctx, identityrepo.AdminDomainRoleBinding{ID: "restore.operator.role", AccountID: "adm.restore.operator", RoleID: "security_admin", GrantedBy: "adm.restore.operator"}); e != nil {
		t.Fatal(e)
	}
	now := time.Now().UTC()
	if e = communityrepo.NewPgCategoryRepository(pool).Create(ctx, &communitydomain.Category{ID: "2801010", Name: "恢复验收板块", Slug: "restore-owned", DefaultTags: []string{}, CreatedAt: now, UpdatedAt: now}); e != nil {
		t.Fatal(e)
	}
	if e = communityrepo.NewPgThreadTypePolicyRepository(pool).Replace(ctx, "2801010", []communitydomain.ThreadType{communitydomain.ThreadTypeDiscussion}); e != nil {
		t.Fatal(e)
	}
	_, e = pool.Exec(ctx, `INSERT INTO threads(id,title,content,author_id,author_name,category_id,status,thread_type,publication_status,moderation_status,deletion_status)
 SELECT 8800000+n,'Recovery capacity '||n,repeat('Owned recovery public content. ',30),2801001,'Recovery Alice',2801010,'published','discussion',CASE WHEN n=10000 THEN 'private' ELSE 'published' END,'clear','active' FROM generate_series(1,10000) n`)
	if e != nil {
		t.Fatal(e)
	}
	digest := sha256.Sum256([]byte("campusos.integration.credential/v1\x00" + machine))
	expires := time.Now().Add(time.Hour).UnixMilli()
	rules := fmt.Sprintf(`[{"id":"owned-rule","status":"active","channel":"integration_http","endpoint":"public_thread.read","action":"community.thread.read","resource_kind":"community.thread","resource_ids":["8800001"],"not_before_ms":0,"expires_at_ms":%d}]`, expires)
	_, e = pool.Exec(ctx, `INSERT INTO integration_principals(id,name,status,created_by_admin_id) VALUES('int.recovery','Owned recovery machine','active','adm.restore.operator')`)
	if e != nil {
		t.Fatal(e)
	}
	_, e = pool.Exec(ctx, `INSERT INTO integration_credentials(id,principal_id,prefix,digest,purpose,status,rules,not_before_ms,expires_at_ms,created_by_admin_id) VALUES('0123456789abcdef','int.recovery','cint1_0123456789abcdef',$1,'integration_api','active',$2::jsonb,0,$3,'adm.restore.operator')`, digest[:], rules, expires)
	if e != nil {
		t.Fatal(e)
	}
	_, e = pool.Exec(ctx, `INSERT INTO webhook_endpoints(id,name,url,enabled,created_by) VALUES(8808001,'Owned disabled-on-restore','https://example.invalid/recovery',true,'adm.restore.operator')`)
	if e != nil {
		t.Fatal(e)
	}
	proof, _ := json.Marshal(map[string]any{"status": "passed", "threads": 10000, "owners": 2, "independent_admin": true, "machine_credential": "seeded_active", "webhook": "seeded_enabled"})
	fmt.Printf("V12_RECOVERY_SEED %s\n", proof)
}
