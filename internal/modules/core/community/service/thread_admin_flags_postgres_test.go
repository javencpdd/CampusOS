package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	identityrepo "github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	identityservice "github.com/campusos/CampusOS/internal/modules/core/identity/service"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/jackc/pgx/v5/pgconn"
)

func adminFlagPostgres(t *testing.T, seed int, kind string) takeDownPG {
	t.Helper()
	g := takeDownPostgres(t, seed, kind)
	ctx := context.Background()
	admins := identityrepo.NewPgAdminDomainRepository(g.pool)
	sessions := identityservice.NewAdminSessionService(admins, identityrepo.NewPgAdminSessionRepository(g.pool), nil)
	if _, err := g.pool.Exec(ctx, `DELETE FROM admin_role_permissions WHERE role_id=$1`, g.role); err != nil {
		t.Fatal(err)
	}
	if _, err := g.pool.Exec(ctx, `INSERT INTO admin_role_permissions(role_id,permission_code,created_by) VALUES($1,'community.thread.pin','test'),($1,'community.thread.lock','test')`, g.role); err != nil {
		t.Fatal(err)
	}
	g.s.SetAdminExecutionAuthority(identityservice.NewBoardGovernanceAuthorityService(nil, admins, sessions))
	return g
}

func TestThreadAdminFlagPostgreSQLAtomicDomainsLocksAndAuditRollback(t *testing.T) {
	for i, kind := range []string{"admin_numeric", "admin_opaque"} {
		for j, op := range []string{"pin", "unpin", "lock", "unlock"} {
			t.Run(kind+"/"+op, func(t *testing.T) {
				g := adminFlagPostgres(t, 139201+i*10+j, kind)
				ctx := context.Background()
				g.row.IsPinned, g.row.IsLocked = op == "unpin", op == "unlock"
				if err := g.threads.Update(ctx, g.row); err != nil {
					t.Fatal(err)
				}
				original := g.s.governance
				g.s.SetGovernanceRepository(takeDownLockHistory{ContentGovernanceRepository: original, afterRevision: func(context.Context) {
					for _, q := range []struct{ sql, id string }{{`SELECT id FROM threads WHERE id=$1 FOR UPDATE NOWAIT`, g.row.ID}, {`SELECT id FROM admin_accounts WHERE id=$1 FOR UPDATE NOWAIT`, g.claims.UserID}, {`SELECT id FROM admin_sessions WHERE id=$1 FOR UPDATE NOWAIT`, g.claims.SessionID}, {`SELECT id FROM admin_roles WHERE id=$1 FOR UPDATE NOWAIT`, g.role}, {`SELECT id FROM admin_role_bindings WHERE role_id=$1 FOR UPDATE NOWAIT`, g.role}, {`SELECT role_id FROM admin_role_permissions WHERE role_id=$1 FOR UPDATE NOWAIT`, g.role}} {
						_, err := g.pool.Exec(ctx, q.sql, q.id)
						var pe *pgconn.PgError
						if !errors.As(err, &pe) || pe.Code != "55P03" {
							t.Fatal("actual flag authority not locked", q.sql, err)
						}
					}
				}})
				before, _ := g.threads.GetByID(ctx, g.row.ID)
				result, err := g.s.AdminSetThreadFlag(ctx, g.row.ID, op, g.proof)
				if err != nil || result == nil {
					t.Fatal(op, result, err)
				}
				after, _ := g.threads.GetByID(ctx, g.row.ID)
				if after.FactsVersion <= before.FactsVersion || after.CurrentRevision != before.CurrentRevision+1 || after.Content != before.Content || after.ModerationStatus != before.ModerationStatus || after.PublicationStatus != before.PublicationStatus {
					t.Fatal(after)
				}
				if after.IsPinned != (op == "pin") && (op == "pin" || op == "unpin") {
					t.Fatal(after)
				}
				if after.IsLocked != (op == "lock") && (op == "lock" || op == "unlock") {
					t.Fatal(after)
				}
				if counts := takeDownCounts(t, g, g.row.ID); !reflect.DeepEqual(counts, []int{1, 0, 1, 1, 1}) {
					t.Fatal(counts)
				}
				for _, q := range []string{`SELECT created_actor_kind,created_actor_id,COALESCE(created_by::text,'') FROM content_revisions WHERE thread_id=$1`, `SELECT actor_kind,actor_ref_id,COALESCE(actor_id::text,'') FROM content_moderation_actions WHERE thread_id=$1`, `SELECT actor_type,actor_id,'' FROM platform_command_audits WHERE resource_id=$1`} {
					var kind, id, legacy string
					if err := g.pool.QueryRow(ctx, q, g.row.ID).Scan(&kind, &id, &legacy); err != nil || kind != "admin" || id != g.claims.UserID || legacy != "" {
						t.Fatal(kind, id, legacy, err)
					}
				}
				var payload []byte
				if err := g.pool.QueryRow(ctx, `SELECT payload FROM platform_outbox WHERE aggregate_id=$1`, g.row.ID).Scan(&payload); err != nil {
					t.Fatal(err)
				}
				var fields map[string]any
				if err := json.Unmarshal(payload, &fields); err != nil || len(fields) != 3 || strings.Contains(string(payload), before.Content) || strings.Contains(string(payload), before.Title) {
					t.Fatal("unsafe event", string(payload), err)
				}
				for _, mode := range []string{"audit", "history"} {
					base, _ := g.threads.GetByID(ctx, g.row.ID)
					counts := takeDownCounts(t, g, g.row.ID)
					g.s.SetGovernanceRepository(original)
					g.s.SetReliability(reliability.NewService(transaction.NewPostgreSQL(g.pool), reliability.NewPostgreSQLStore(g.pool)))
					if mode == "audit" {
						g.s.SetReliability(reliability.NewService(transaction.NewPostgreSQL(g.pool), authorFailAudit{Store: reliability.NewPostgreSQLStore(g.pool)}))
					} else {
						g.s.SetGovernanceRepository(takeDownFailHistory{ContentGovernanceRepository: original})
					}
					if got, err := g.s.AdminSetThreadFlag(ctx, g.row.ID, op, g.proof); err == nil || got != nil {
						t.Fatal("required failure accepted", mode, got, err)
					}
					actual, _ := g.threads.GetByID(ctx, g.row.ID)
					if !sameGovernanceThread(base, actual) || !reflect.DeepEqual(counts, takeDownCounts(t, g, g.row.ID)) {
						t.Fatal("partial command persisted", mode, actual)
					}
				}
			})
		}
	}
}

func TestThreadAdminFlagPostgreSQLCommittedChangesAndGuard(t *testing.T) {
	for i, mode := range []string{"permission_aba", "binding_revoke", "session_revoke", "session_version", "mfa_expire", "account_suspend", "board_change", "body_aba", "hard_delete_reinsert", "flag_aba", "trigger_disabled", "counters"} {
		t.Run(mode, func(t *testing.T) {
			g := adminFlagPostgres(t, 139301+i, "admin_opaque")
			ctx := context.Background()
			calls := 0
			proof := func(ctx context.Context) (identityport.PrincipalContext, error) {
				calls++
				if calls == 3 {
					tx, err := g.pool.Begin(context.Background())
					if err != nil {
						t.Fatal(err)
					}
					defer tx.Rollback(context.Background())
					exec := func(q string, args ...any) {
						if _, err := tx.Exec(context.Background(), q, args...); err != nil {
							t.Fatal(mode, err)
						}
					}
					switch mode {
					case "permission_aba":
						exec(`DELETE FROM admin_role_permissions WHERE role_id=$1`, g.role)
						exec(`INSERT INTO admin_role_permissions(role_id,permission_code,created_by) VALUES($1,'community.thread.pin','test'),($1,'community.thread.lock','test')`, g.role)
					case "binding_revoke":
						exec(`UPDATE admin_role_bindings SET status='revoked',revoked_at=NOW(),version=version+1 WHERE role_id=$1`, g.role)
					case "session_revoke":
						exec(`UPDATE admin_sessions SET revoked_at=NOW(),revoke_reason='test',version=version+1 WHERE id=$1`, g.claims.SessionID)
					case "session_version":
						exec(`UPDATE admin_sessions SET version=version+1 WHERE id=$1`, g.claims.SessionID)
					case "mfa_expire":
						exec(`UPDATE admin_sessions SET mfa_authenticated_at=NOW()-INTERVAL '30 minutes',version=version+1 WHERE id=$1`, g.claims.SessionID)
					case "account_suspend":
						exec(`UPDATE admin_accounts SET status='suspended',version=version+1 WHERE id=$1`, g.claims.UserID)
					case "board_change":
						exec(`UPDATE threads SET category_id=113112 WHERE id=$1`, g.row.ID)
					case "body_aba":
						exec(`UPDATE threads SET content='changed' WHERE id=$1`, g.row.ID)
						exec(`UPDATE threads SET content=$2 WHERE id=$1`, g.row.ID, g.row.Content)
					case "flag_aba":
						exec(`UPDATE threads SET is_locked=TRUE WHERE id=$1`, g.row.ID)
						exec(`UPDATE threads SET is_locked=FALSE WHERE id=$1`, g.row.ID)
					case "hard_delete_reinsert":
						exec(`DELETE FROM threads WHERE id=$1`, g.row.ID)
					case "trigger_disabled":
						exec(`ALTER TABLE threads DISABLE TRIGGER trg_threads_facts_version`)
					case "counters":
						exec(`UPDATE threads SET view_count=17,reply_count=3,like_count=2 WHERE id=$1`, g.row.ID)
					}
					if err := tx.Commit(context.Background()); err != nil {
						t.Fatal(err)
					}
					if mode == "hard_delete_reinsert" {
						if err := g.threads.Create(context.Background(), g.row); err != nil {
							t.Fatal(err)
						}
					}
				}
				return g.proof(ctx)
			}
			if mode == "trigger_disabled" {
				defer func() {
					if _, err := g.pool.Exec(context.Background(), `ALTER TABLE threads ENABLE TRIGGER trg_threads_facts_version`); err != nil {
						t.Fatal(err)
					}
				}()
			}
			result, err := g.s.AdminSetThreadFlag(ctx, g.row.ID, "pin", proof)
			actual, getErr := g.threads.GetByID(ctx, g.row.ID)
			if getErr != nil {
				t.Fatal(getErr)
			}
			if mode == "counters" {
				if err != nil || result == nil || !actual.IsPinned || actual.ViewCount != 17 || actual.ReplyCount != 3 || actual.LikeCount != 2 {
					t.Fatal(result, actual, err)
				}
				return
			}
			if err == nil || result != nil || actual.IsPinned {
				t.Fatal(fmt.Sprintf("stale %s accepted", mode), result, actual, err)
			}
			if counts := takeDownCounts(t, g, g.row.ID); !reflect.DeepEqual(counts, []int{0, 0, 0, 0, 0}) {
				t.Fatal("denied command history/audit", counts)
			}
		})
	}
}
