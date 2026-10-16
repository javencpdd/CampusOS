package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func userScopeFactsPostgres(t *testing.T, seed int) (*PgRoleRepository, *pgxpool.Pool, string, int64, map[string]int64) {
	t.Helper()
	url := strings.TrimSpace(os.Getenv("CAMPUSOS_AUTHOR_WRITE_TEST_DATABASE_URL"))
	if url == "" {
		t.Skip("CAMPUSOS_AUTHOR_WRITE_TEST_DATABASE_URL is not configured")
	}
	config, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	if config.ConnConfig.Database != "campusos_v12_02b_author_write" {
		t.Fatal("User scoped facts require owned author_write database")
	}
	ctx := context.Background()
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	user, role, assignment, link, definition := int64(seed), int64(seed+1), int64(seed+2), int64(seed+3), int64(seed+4)
	_, err = pool.Exec(ctx, `INSERT INTO users(id,username,nickname,email,status,auth_version) VALUES($1,$2,'Scope facts',$3,'active',1)`, user, fmt.Sprintf("scope-facts-%d", seed), fmt.Sprintf("scope-facts-%d@example.test", seed))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO roles(id,name,description,is_system) VALUES($1,$2,'Original role metadata',FALSE)`, role, fmt.Sprintf("scope_role_%d", seed)); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO permission_definitions(id,code,domain,resource,action,description,risk_level,allowed_scope_types,audit_level) VALUES($1,$2,'community','scopefacts','pin','Original definition','medium','["global","category"]','required')`, definition, fmt.Sprintf("community.scopefacts%d.pin", seed)); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO role_permissions(id,role_id,permission_id,created_by) VALUES($1,$2,$3,'scope-test')`, link, role, definition); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO categories(id,name,slug,node_kind,lifecycle_status) VALUES(151110,'Scope facts group','scope-facts-group','group','active') ON CONFLICT(id) DO NOTHING;
INSERT INTO categories(id,name,slug,parent_id,node_kind,lifecycle_status) VALUES(151111,'Scope facts board','scope-facts-board',151110,'board','active'),(151112,'Other scope board','scope-facts-other',151110,'board','active') ON CONFLICT(id) DO NOTHING;
INSERT INTO category_thread_type_policies(category_id,thread_type,enabled) VALUES(151111,'discussion',TRUE),(151112,'discussion',TRUE) ON CONFLICT(category_id,thread_type) DO NOTHING;`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO user_roles(id,user_id,role_id,scope_type,scope_id) VALUES($1,$2,$3,'category',151111)`, assignment, user, role); err != nil {
		t.Fatal(err)
	}
	return NewPgRoleRepository(pool), pool, fmt.Sprint(user), 151111, map[string]int64{"roles": role, "user_roles": assignment, "role_permissions": link, "permission_definitions": definition}
}

func TestUserScopedFactsPostgreSQLActualScopeLocksAndDetachedSnapshots(t *testing.T) {
	r, pool, user, board, ids := userScopeFactsPostgres(t, 151501)
	ctx := context.Background()
	facts, err := r.LoadUserScopedExecutionFacts(ctx, user, board, false)
	if err != nil || len(facts) != 1 {
		t.Fatal(facts, err)
	}
	if rows, err := r.LoadUserScopedExecutionFacts(ctx, user, board+1, false); err != nil || len(rows) != 0 {
		t.Fatal("cross-scope rows", rows, err)
	}
	if _, err := r.LoadUserScopedExecutionFacts(ctx, user, board, true); !errors.Is(err, ErrUserScopeFactsUnavailable) {
		t.Fatal("lock outside transaction accepted", err)
	}
	for _, raw := range []json.RawMessage{facts[0].Assignment, facts[0].Role, facts[0].Link, facts[0].Definition} {
		var row struct {
			Version int64 `json:"facts_version"`
		}
		if err := json.Unmarshal(raw, &row); err != nil || row.Version < 1 {
			t.Fatal(string(raw), err)
		}
	}
	before := append(json.RawMessage(nil), facts[0].Role...)
	facts[0].Role[0] = 'x'
	fresh, err := r.LoadUserScopedExecutionFacts(ctx, user, board, false)
	if err != nil || !reflect.DeepEqual(before, fresh[0].Role) {
		t.Fatal("snapshot alias", fresh, err)
	}
	err = transaction.NewPostgreSQL(pool).Within(ctx, func(tx context.Context) error {
		if _, err := r.LoadUserScopedExecutionFacts(tx, user, board, true); err != nil {
			return err
		}
		for table, id := range ids {
			_, err := pool.Exec(ctx, `SELECT id FROM public.`+table+` WHERE id=$1 FOR UPDATE NOWAIT`, id)
			var pe *pgconn.PgError
			if !errors.As(err, &pe) || pe.Code != "55P03" {
				t.Fatal("actual User authority row not held", table, err)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestUserScopedFactsPostgreSQLNonceABAReinsertAndManagedGuard(t *testing.T) {
	r, pool, user, board, ids := userScopeFactsPostgres(t, 151601)
	ctx := context.Background()
	for _, table := range []string{"roles", "user_roles", "role_permissions", "permission_definitions"} {
		t.Run(table, func(t *testing.T) {
			id := ids[table]
			var before, after int64
			if err := pool.QueryRow(ctx, `SELECT facts_version FROM public.`+table+` WHERE id=$1`, id).Scan(&before); err != nil {
				t.Fatal(err)
			}
			var column, original, changed string
			switch table {
			case "roles":
				column = "description"
				original = "Original role metadata"
				changed = "Changed role"
			case "user_roles":
				column = "scope_id"
				original = "151111"
				changed = "151112"
			case "role_permissions":
				column = "created_by"
				original = "scope-test"
				changed = "Changed grant"
			case "permission_definitions":
				column = "description"
				original = "Original definition"
				changed = "Changed definition"
			}
			for _, value := range []string{changed, original} {
				if _, err := pool.Exec(ctx, `UPDATE public.`+table+` SET `+column+`=$2 WHERE id=$1`, id, value); err != nil {
					t.Fatal(err)
				}
			}
			if err := pool.QueryRow(ctx, `SELECT facts_version FROM public.`+table+` WHERE id=$1`, id).Scan(&after); err != nil || after <= before {
				t.Fatal("actual ABA allocation reused", table, before, after, err)
			}
			// DELETE/INSERT restores a complete same-valued row while allocating a
			// fresh nonce. The row's references must survive the actual rollback.
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(ctx)
			// Existing RESTRICT foreign keys remain intact. Remove references
			// within this rollback-only probe before deleting their parent.
			if table == "roles" {
				if _, err := tx.Exec(ctx, `DELETE FROM user_roles WHERE role_id=$1`, id); err != nil {
					t.Fatal(err)
				}
				if _, err := tx.Exec(ctx, `DELETE FROM role_permissions WHERE role_id=$1`, id); err != nil {
					t.Fatal(err)
				}
			}
			if table == "permission_definitions" {
				if _, err := tx.Exec(ctx, `DELETE FROM role_permissions WHERE permission_id=$1`, id); err != nil {
					t.Fatal(err)
				}
			}
			for _, q := range []string{`CREATE TEMP TABLE saved_scope_row ON COMMIT DROP AS SELECT * FROM public.` + table + ` WHERE id=$1`, `DELETE FROM public.` + table + ` WHERE id=$1`, `INSERT INTO public.` + table + ` SELECT * FROM saved_scope_row`} {
				var e error
				if strings.Contains(q, "$1") {
					_, e = tx.Exec(ctx, q, id)
				} else {
					_, e = tx.Exec(ctx, q)
				}
				if e != nil {
					t.Fatal(table, e)
				}
			}
			var rolledVersion int64
			if err := tx.QueryRow(ctx, `SELECT facts_version FROM public.`+table+` WHERE id=$1`, id).Scan(&rolledVersion); err != nil || rolledVersion <= after {
				t.Fatal(rolledVersion, err)
			}
			if err := tx.Rollback(ctx); err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, `UPDATE public.`+table+` SET `+column+`=$2 WHERE id=$1`, id, changed); err != nil {
				t.Fatal(err)
			}
			if err := pool.QueryRow(ctx, `SELECT facts_version FROM public.`+table+` WHERE id=$1`, id).Scan(&after); err != nil || after <= rolledVersion {
				t.Fatal("rollback high water reused", after, rolledVersion, err)
			}
			if _, err := pool.Exec(ctx, `UPDATE public.`+table+` SET `+column+`=$2 WHERE id=$1`, id, original); err != nil {
				t.Fatal(err)
			}
			trigger := "trg_" + table + "_user_scope_facts_version"
			if _, err := pool.Exec(ctx, `ALTER TABLE public.`+table+` DISABLE TRIGGER `+trigger); err != nil {
				t.Fatal(err)
			}
			_, readErr := r.LoadUserScopedExecutionFacts(ctx, user, board, false)
			if _, err := pool.Exec(ctx, `ALTER TABLE public.`+table+` ENABLE TRIGGER `+trigger); err != nil {
				t.Fatal(err)
			}
			if !errors.Is(readErr, ErrUserScopeFactsUnavailable) {
				t.Fatal("missing actual guard accepted", table, readErr)
			}
		})
	}
	if _, err := pool.Exec(ctx, `UPDATE roles SET updated_at=NOW(),facts_version=999 WHERE id=$1`, ids["roles"]); err != nil {
		t.Fatal(err)
	}
	facts, err := r.LoadUserScopedExecutionFacts(ctx, user, board, false)
	if err != nil || len(facts) != 1 {
		t.Fatal(facts, err)
	}
	var row struct {
		Version int64 `json:"facts_version"`
	}
	json.Unmarshal(facts[0].Role, &row)
	if row.Version == 999 {
		t.Fatal("caller-owned nonce persisted")
	}
}
