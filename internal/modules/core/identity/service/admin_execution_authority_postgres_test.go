package service

import (
	"context"
	"errors"
	"testing"

	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestAdminExecutionAuthorityPostgreSQLCurrentLocks(t *testing.T) {
	for i, code := range []string{"community.thread.review", "platform.reliability.read", "community.thread.pin", "personal_space.asset.manage", "personal_space.asset.read_audit"} {
		t.Run(code, func(t *testing.T) {
			g := governancePostgres(t, 138201+i, "admin")
			ctx := context.Background()
			q := namedAdminQuery(code)
			if q.Scope.Kind == "community.board" {
				q.Scope.ID = g.query.BoardID
			}
			if _, err := g.pool.Exec(ctx, `UPDATE admin_role_permissions SET permission_code=$2 WHERE role_id=$1`, g.roleID, code); err != nil {
				t.Fatal(err)
			}
			if !q.RequiresCurrentMFA() {
				if _, err := g.pool.Exec(ctx, `UPDATE admin_sessions SET authentication_strength='password',mfa_authenticated_at=NULL,version=version+1 WHERE id=$1`, g.claims.SessionID); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := g.service.LoadAdminExecutionAuthority(ctx, q, g.proof, true); !errors.Is(err, identityport.ErrGovernanceAuthorityUnavailable) {
				t.Fatal("lock without transaction accepted", err)
			}
			err := transaction.NewPostgreSQL(g.pool).Within(ctx, func(tx context.Context) error {
				a, err := g.service.LoadAdminExecutionAuthority(tx, q, g.proof, true)
				if err != nil {
					return err
				}
				if !a.Allowed {
					t.Fatal(a)
				}
				for _, query := range []struct{ sql, id string }{{`SELECT id FROM admin_accounts WHERE id=$1 FOR UPDATE NOWAIT`, g.claims.UserID}, {`SELECT id FROM admin_sessions WHERE id=$1 FOR UPDATE NOWAIT`, g.claims.SessionID}, {`SELECT id FROM admin_roles WHERE id=$1 FOR UPDATE NOWAIT`, g.roleID}, {`SELECT id FROM admin_role_bindings WHERE role_id=$1 FOR UPDATE NOWAIT`, g.roleID}, {`SELECT role_id FROM admin_role_permissions WHERE role_id=$1 FOR UPDATE NOWAIT`, g.roleID}} {
					_, err := g.pool.Exec(ctx, query.sql, query.id)
					var pe *pgconn.PgError
					if !errors.As(err, &pe) || pe.Code != "55P03" {
						t.Fatal("actual Admin authority row not locked", query.sql, err)
					}
				}
				current, err := g.service.LoadAdminExecutionAuthority(tx, q, g.proof, true)
				if err != nil {
					return err
				}
				return identityport.CheckAdminExecutionAuthority(a, current)
			})
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestAdminExecutionAuthorityPostgreSQLCommittedChanges(t *testing.T) {
	for i, mode := range []string{"permission_aba", "permission_metadata_aba", "binding_revoke", "role_metadata", "session_version", "session_revoke", "mfa_expire", "account_aba", "code_change", "scope_replay"} {
		t.Run(mode, func(t *testing.T) {
			g := governancePostgres(t, 138301+i, "admin")
			ctx := context.Background()
			q := namedAdminQuery("community.thread.review")
			q.Scope.ID = g.query.BoardID
			if _, err := g.pool.Exec(ctx, `UPDATE admin_role_permissions SET permission_code=$2 WHERE role_id=$1`, g.roleID, q.PermissionCode); err != nil {
				t.Fatal(err)
			}
			if mode == "scope_replay" {
				before, err := g.service.LoadAdminExecutionAuthority(ctx, q, g.proof, false)
				if err != nil || !before.Allowed {
					t.Fatal(before, err)
				}
				q.Scope.ID = "118112"
				after, err := g.service.LoadAdminExecutionAuthority(ctx, q, g.proof, false)
				if err != nil || !after.Allowed {
					t.Fatal(after, err)
				}
				if !errors.Is(identityport.CheckAdminExecutionAuthority(before, after), identityport.ErrPolicyFactsChanged) {
					t.Fatal("other resource scope replay accepted")
				}
				return
			}
			calls := 0
			proof := func(ctx context.Context) (identityport.PrincipalContext, error) {
				calls++
				if calls == 2 {
					tx, err := g.pool.Begin(ctx)
					if err != nil {
						t.Fatal(err)
					}
					defer tx.Rollback(ctx)
					exec := func(query string, args ...any) {
						if _, err := tx.Exec(ctx, query, args...); err != nil {
							t.Fatal(mode, err)
						}
					}
					switch mode {
					case "permission_aba":
						exec(`DELETE FROM admin_role_permissions WHERE role_id=$1`, g.roleID)
						exec(`INSERT INTO admin_role_permissions(role_id,permission_code,created_by) VALUES($1,$2,'gov-test')`, g.roleID, q.PermissionCode)
					case "permission_metadata_aba":
						exec(`UPDATE admin_role_permissions SET created_by='temporary' WHERE role_id=$1`, g.roleID)
						exec(`UPDATE admin_role_permissions SET created_by='gov-test' WHERE role_id=$1`, g.roleID)
					case "binding_revoke":
						exec(`UPDATE admin_role_bindings SET status='revoked',revoked_at=NOW(),version=version+1 WHERE role_id=$1`, g.roleID)
					case "role_metadata":
						exec(`UPDATE admin_roles SET description='changed' WHERE id=$1`, g.roleID)
					case "session_version":
						exec(`UPDATE admin_sessions SET version=version+1 WHERE id=$1`, g.claims.SessionID)
					case "session_revoke":
						exec(`UPDATE admin_sessions SET revoked_at=NOW(),revoke_reason='test',version=version+1 WHERE id=$1`, g.claims.SessionID)
					case "mfa_expire":
						exec(`UPDATE admin_sessions SET mfa_authenticated_at=NOW()-INTERVAL '30 minutes',version=version+1 WHERE id=$1`, g.claims.SessionID)
					case "account_aba":
						exec(`UPDATE admin_accounts SET display_name='temporary',version=version+1 WHERE id=$1`, g.claims.UserID)
						exec(`UPDATE admin_accounts SET display_name='Proof Admin',version=version+1 WHERE id=$1`, g.claims.UserID)
					case "code_change":
						exec(`UPDATE admin_role_permissions SET permission_code='community.thread.take_down' WHERE role_id=$1`, g.roleID)
					}
					if err := tx.Commit(ctx); err != nil {
						t.Fatal(err)
					}
				}
				return g.proof(ctx)
			}
			a, err := g.service.LoadAdminExecutionAuthority(ctx, q, proof, false)
			if err == nil {
				t.Fatal("changed authority accepted", a)
			}
		})
	}
}
