package service

import (
	"context"
	"fmt"
	"reflect"
	"testing"
)

func TestAdminThreadPreviewPostgreSQLActualDomainsSamePoliciesWholeChangesAndNoSideEffects(t *testing.T) {
	ctx := context.Background()
	index := 0
	for _, kind := range []string{"admin_numeric", "admin_opaque"} {
		for _, mode := range []string{"current", "password", "permission_aba", "session", "row_aba", "new_row", "guard", "views", "caller_input"} {
			t.Run(kind+"/"+mode, func(t *testing.T) {
				index++
				g := adminFlagPostgres(t, 2600201+index*10, kind)
				replaceCodes := func(codes []string, reason string) error {
					if _, err := g.pool.Exec(ctx, "DELETE FROM admin_role_permissions WHERE role_id=$1", g.role); err != nil {
						return err
					}
					_, err := g.pool.Exec(ctx, "INSERT INTO admin_role_permissions(role_id,permission_code,created_by) SELECT $1,unnest($2::text[]),$3", g.role, codes, reason)
					return err
				}
				if err := replaceCodes(previewTestCodes, "preview"); err != nil {
					t.Fatal(err)
				}
				exec := func(sql string, args ...any) {
					t.Helper()
					if _, err := g.pool.Exec(ctx, sql, args...); err != nil {
						t.Fatal(err)
					}
				}
				if mode == "password" {
					exec("UPDATE admin_sessions SET authentication_strength='password',mfa_authenticated_at=NULL,version=version+1 WHERE id=$1", g.claims.SessionID)
				}
				var before, after string
				if err := g.pool.QueryRow(ctx, "SELECT to_jsonb(t)::text FROM threads t WHERE id=$1", g.row.ID).Scan(&before); err != nil {
					t.Fatal(err)
				}
				counts := takeDownCounts(t, g, g.row.ID)
				ids := []string{g.row.ID}
				changed := false
				if mode != "current" && mode != "password" {
					source := g.s.adminExecution
					g.s.adminExecution = &previewExecutionHook{AdminExecutionAuthorityReader: source, after: func() {
						changed = true
						switch mode {
						case "permission_aba":
							if err := replaceCodes([]string{"community.thread.read"}, "revoke"); err != nil {
								t.Fatal(err)
							}
							if err := replaceCodes(previewTestCodes, "restore"); err != nil {
								t.Fatal(err)
							}
						case "session":
							exec("UPDATE admin_sessions SET revoked_at=NOW(),revoke_reason='preview',version=version+1 WHERE id=$1", g.claims.SessionID)
						case "row_aba":
							exec("UPDATE threads SET content='changed' WHERE id=$1", g.row.ID)
							exec("UPDATE threads SET content=$2 WHERE id=$1", g.row.ID, g.row.Content)
						case "new_row":
							copy := *g.row
							copy.ID = fmt.Sprint(26090001 + index)
							copy.FactsVersion = 0
							if err := g.threads.Create(ctx, &copy); err != nil {
								t.Fatal(err)
							}
						case "guard":
							exec("ALTER TABLE threads DISABLE TRIGGER trg_threads_facts_version")
							t.Cleanup(func() { exec("ALTER TABLE threads ENABLE TRIGGER trg_threads_facts_version") })
						case "views":
							exec("UPDATE threads SET view_count=view_count+1 WHERE id=$1", g.row.ID)
						case "caller_input":
							ids[0] = "missing"
						}
					}}
				}
				rows, err := g.s.PreviewAdminThreadPermissions(ctx, ids, g.proof)
				allow := mode == "current" || mode == "password" || mode == "views" || mode == "caller_input"
				if (err == nil) != allow {
					t.Fatal(mode, rows, err)
				}
				if mode != "current" && mode != "password" && !changed {
					t.Fatal("actual mutation not reached")
				}
				if allow {
					if len(rows) != 1 || rows[0].ID != g.row.ID {
						t.Fatal(rows)
					}
					if mode == "password" {
						if rows[0].Permissions != (AdminThreadPermissionFlags{}) {
							t.Fatal("password became write MFA", rows)
						}
					} else if !rows[0].Permissions.Pin || !rows[0].Permissions.Trash || !rows[0].Permissions.TakeDown {
						t.Fatal("actual command policies not applied", rows)
					}
				}
				if !reflect.DeepEqual(counts, takeDownCounts(t, g, g.row.ID)) {
					t.Fatal("preview wrote SQL evidence")
				}
				if err := g.pool.QueryRow(ctx, "SELECT to_jsonb(t)::text FROM threads t WHERE id=$1", g.row.ID).Scan(&after); err != nil {
					t.Fatal(err)
				}
				if (mode == "current" || mode == "password" || mode == "caller_input") && before != after {
					t.Fatal("read changed actual resource")
				}
				if mode == "current" {
					if err := replaceCodes([]string{"community.thread.read"}, "revoke before execution"); err != nil {
						t.Fatal(err)
					}
					if result, err := g.s.AdminSetThreadFlag(ctx, g.row.ID, "pin", g.proof); err == nil || result != nil {
						t.Fatal("preview replay became execution authority", result, err)
					}
					if !reflect.DeepEqual(counts, takeDownCounts(t, g, g.row.ID)) {
						t.Fatal("denied replay wrote SQL")
					}
				}
			})
		}
	}
}
