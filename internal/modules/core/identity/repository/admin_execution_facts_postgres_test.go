package repository

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func adminPermissionFactsPG(t *testing.T, seed int) (*PgAdminDomainRepository, *pgxpool.Pool, string, string) {
	t.Helper()
	url := strings.TrimSpace(os.Getenv("CAMPUSOS_AUTHOR_WRITE_TEST_DATABASE_URL"))
	if url == "" {
		t.Skip("CAMPUSOS_AUTHOR_WRITE_TEST_DATABASE_URL is not configured")
	}
	c, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	if c.ConnConfig.Database != "campusos_v12_02b_author_write" {
		t.Fatal("permission facts require owned campusos_v12_02b_author_write database")
	}
	ctx := context.Background()
	pool, err := pgxpool.NewWithConfig(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	r := NewPgAdminDomainRepository(pool)
	account, role := fmt.Sprintf("adm.permission.%d", seed), fmt.Sprintf("permission_role_%d", seed)
	if err := r.CreateAccount(ctx, AdminDomainAccount{ID: account, Username: fmt.Sprintf("permission-admin-%d", seed), Email: fmt.Sprintf("permission-%d@example.test", seed)}); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO admin_roles(id,name,description,is_system) VALUES($1,$1,'Permission facts drill',FALSE)`, role); err != nil {
		t.Fatal(err)
	}
	if err := r.BindRole(ctx, AdminDomainRoleBinding{ID: fmt.Sprintf("binding.permission.%d", seed), AccountID: account, RoleID: role, GrantedBy: account}); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO admin_role_permissions(role_id,permission_code,created_by) VALUES($1,'community.thread.take_down','test-seed')`, role); err != nil {
		t.Fatal(err)
	}
	return r, pool, account, role
}

func TestAdminPermissionFactsPostgreSQLVersions(t *testing.T) {
	r, pool, account, role := adminPermissionFactsPG(t, 117801)
	ctx := context.Background()
	get := func() AdminRoleExecutionFact {
		f, e := r.LoadAdminRoleExecutionFacts(ctx, account, false)
		if e != nil || len(f) != 1 {
			t.Fatal(f, e)
		}
		return f[0]
	}
	first := get()
	if _, err := pool.Exec(ctx, `UPDATE admin_role_permissions SET facts_version=-1 WHERE role_id=$1`, role); err != nil {
		t.Fatal(err)
	}
	if get().PermissionFactsVersion != first.PermissionFactsVersion {
		t.Fatal("caller assigned facts version")
	}
	if _, err := pool.Exec(ctx, `UPDATE admin_role_permissions SET created_by='changed' WHERE role_id=$1`, role); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE admin_role_permissions SET created_by='test-seed' WHERE role_id=$1`, role); err != nil {
		t.Fatal(err)
	}
	second := get()
	if second.PermissionFactsVersion <= first.PermissionFactsVersion {
		t.Fatal("metadata ABA reused version")
	}
	if _, err := pool.Exec(ctx, `DELETE FROM admin_role_permissions WHERE role_id=$1`, role); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO admin_role_permissions(role_id,permission_code,created_by,facts_version) VALUES($1,'community.thread.take_down','test-seed',$2)`, role, first.PermissionFactsVersion); err != nil {
		t.Fatal(err)
	}
	third := get()
	if third.PermissionFactsVersion <= second.PermissionFactsVersion {
		t.Fatal("delete/reinsert reused version")
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var rolledBack int64
	if err := tx.QueryRow(ctx, `UPDATE admin_role_permissions SET created_by='rollback' WHERE role_id=$1 RETURNING facts_version`, role).Scan(&rolledBack); err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if get().PermissionFactsVersion != third.PermissionFactsVersion {
		t.Fatal("rollback lost old version")
	}
	if _, err := pool.Exec(ctx, `UPDATE admin_role_permissions SET created_by='next' WHERE role_id=$1`, role); err != nil {
		t.Fatal(err)
	}
	if get().PermissionFactsVersion <= rolledBack {
		t.Fatal("rollback allocation reused")
	}
	if _, err := pool.Exec(ctx, `ALTER TABLE admin_role_permissions DISABLE TRIGGER trg_admin_permission_facts_version`); err != nil {
		t.Fatal(err)
	}
	_, readErr := r.LoadAdminRoleExecutionFacts(ctx, account, false)
	if _, err := pool.Exec(ctx, `ALTER TABLE admin_role_permissions ENABLE TRIGGER trg_admin_permission_facts_version`); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(readErr, ErrAdminExecutionFactsUnavailable) {
		t.Fatal("disabled trigger did not fail closed", readErr)
	}
}

func TestAdminPermissionFactsPostgreSQLWitnessLocks(t *testing.T) {
	r, pool, account, role := adminPermissionFactsPG(t, 117802)
	ctx := context.Background()
	if _, err := r.LoadAdminRoleExecutionFacts(ctx, account, true); !errors.Is(err, ErrAdminAuthorityLockUnavailable) {
		t.Fatal("lock outside SQL accepted", err)
	}
	err := transaction.NewPostgreSQL(pool).Within(ctx, func(txCtx context.Context) error {
		f, e := r.LoadAdminRoleExecutionFacts(txCtx, account, true)
		if e != nil {
			return e
		}
		if len(f) != 1 || f[0].Binding.AccountID != account || f[0].PermissionFactsVersion < 1 {
			t.Fatal(f)
		}
		for _, q := range []struct{ sql, id string }{{`SELECT id FROM admin_role_bindings WHERE role_id=$1 FOR UPDATE NOWAIT`, role}, {`SELECT id FROM admin_roles WHERE id=$1 FOR UPDATE NOWAIT`, role}, {`SELECT role_id FROM admin_role_permissions WHERE role_id=$1 FOR UPDATE NOWAIT`, role}} {
			_, e := pool.Exec(context.Background(), q.sql, q.id)
			var pe *pgconn.PgError
			if !errors.As(e, &pe) || pe.Code != "55P03" {
				t.Fatal("actual role execution witness not locked", q.sql, e)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
