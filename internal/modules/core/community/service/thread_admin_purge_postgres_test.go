package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	"github.com/campusos/CampusOS/internal/modules/core/community/repository"

	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	identityrepo "github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	identityservice "github.com/campusos/CampusOS/internal/modules/core/identity/service"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/campusos/CampusOS/pkg/idgen"
	"github.com/jackc/pgx/v5/pgconn"
)

func adminPurgePostgres(t *testing.T, seed int, kind string) takeDownPG {
	t.Helper()
	g := takeDownPostgres(t, seed, kind)
	ctx := context.Background()
	admins := identityrepo.NewPgAdminDomainRepository(g.pool)
	sessions := identityservice.NewAdminSessionService(admins, identityrepo.NewPgAdminSessionRepository(g.pool), nil)
	if _, err := g.pool.Exec(ctx, `DELETE FROM admin_role_permissions WHERE role_id=$1`, g.role); err != nil {
		t.Fatal(err)
	}
	if _, err := g.pool.Exec(ctx, `INSERT INTO admin_role_permissions(role_id,permission_code,created_by) VALUES($1,'community.thread.purge','test')`, g.role); err != nil {
		t.Fatal(err)
	}
	g.s.SetAdminExecutionAuthority(identityservice.NewBoardGovernanceAuthorityService(nil, admins, sessions))
	row, _ := g.threads.GetByID(ctx, g.row.ID)
	row.ModerationStatus = "taken_down"
	row.SetModerationActor("user", row.AuthorID)
	row.DeletionStatus = "trashed"
	if err := g.threads.Update(ctx, row); err != nil {
		t.Fatal(err)
	}
	g.row = row
	return g
}

func TestThreadAdminPurgePostgreSQLTombstoneDomainsLocksAndAtomicRollback(t *testing.T) {
	for i, kind := range []string{"admin_numeric", "admin_opaque"} {
		for j, withCase := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/%t", kind, withCase), func(t *testing.T) {
				g := adminPurgePostgres(t, 146201+i*10+j, kind)
				ctx := context.Background()
				now := time.Now().UTC()
				original := g.s.governance
				caseRepo := original.(repository.ModerationCaseCommandRepository)
				caseID := ""
				if withCase {
					caseID = fmt.Sprint(idgen.New())
					if err := original.CreateModerationCase(ctx, &domain.ModerationCase{ID: caseID, ThreadID: g.row.ID, Status: "taken_down", OpenedBy: g.row.AuthorID, OpenedAt: now}); err != nil {
						t.Fatal(err)
					}
				}
				g.s.SetGovernanceRepository(reviewLockHistory{ContentGovernanceRepository: original, ModerationCaseCommandRepository: caseRepo, afterRevision: func(context.Context) {
					queries := []struct{ sql, id string }{{`SELECT id FROM threads WHERE id=$1 FOR UPDATE NOWAIT`, g.row.ID}, {`SELECT id FROM admin_accounts WHERE id=$1 FOR UPDATE NOWAIT`, g.claims.UserID}, {`SELECT id FROM admin_sessions WHERE id=$1 FOR UPDATE NOWAIT`, g.claims.SessionID}, {`SELECT id FROM admin_roles WHERE id=$1 FOR UPDATE NOWAIT`, g.role}, {`SELECT id FROM admin_role_bindings WHERE role_id=$1 FOR UPDATE NOWAIT`, g.role}, {`SELECT role_id FROM admin_role_permissions WHERE role_id=$1 FOR UPDATE NOWAIT`, g.role}}
					if withCase {
						queries = append(queries, struct{ sql, id string }{`SELECT id FROM content_moderation_cases WHERE id=$1 FOR UPDATE NOWAIT`, caseID})
					}
					for _, q := range queries {
						_, err := g.pool.Exec(ctx, q.sql, q.id)
						var pe *pgconn.PgError
						if !errors.As(err, &pe) || pe.Code != "55P03" {
							t.Fatal("purge dependency not locked", q.sql, err)
						}
					}
				}})
				before, _ := g.threads.GetByID(ctx, g.row.ID)
				result, err := g.s.AdminPurgeThread(ctx, g.row.ID, "purge", "actual purge", g.proof)
				if err != nil || result == nil || result.DeletionStatus != "purged" {
					t.Fatal(result, err)
				}
				if _, err := g.threads.GetByID(ctx, g.row.ID); !errors.Is(err, repository.ErrThreadNotFound) {
					t.Fatal("normal purge read open", err)
				}
				if err := transaction.NewPostgreSQL(g.pool).Within(ctx, func(tx context.Context) error {
					actual, err := g.threads.LockThreadGovernanceSnapshot(tx, g.row.ID)
					if err != nil {
						return err
					}
					if actual.DeletedAt == nil || actual.DeletionStatus != "purged" || actual.FactsVersion <= before.FactsVersion || actual.CurrentRevision != before.CurrentRevision+1 || actual.Content != before.Content || actual.PublicationStatus != before.PublicationStatus || actual.ModerationStatus != before.ModerationStatus || actual.ModerationActorID != before.ModerationActorID {
						t.Fatal(actual)
					}
					if _, err := g.threads.PurgeThreadIfFacts(tx, g.row.ID, actual.FactsVersion, actual.CurrentRevision); !errors.Is(err, repository.ErrThreadRevisionConflict) {
						t.Fatal("repeat tombstone mutation", err)
					}
					return nil
				}); err != nil {
					t.Fatal(err)
				}
				count := 0
				if withCase {
					count = 1
				}
				if counts := takeDownCounts(t, g, g.row.ID); !reflect.DeepEqual(counts, []int{1, count, 1, 1, 1}) {
					t.Fatal(counts)
				}
				for _, q := range []string{`SELECT created_actor_kind,created_actor_id,COALESCE(created_by::text,'') FROM content_revisions WHERE thread_id=$1`, `SELECT actor_kind,actor_ref_id,COALESCE(actor_id::text,'') FROM content_moderation_actions WHERE thread_id=$1`, `SELECT actor_type,actor_id,'' FROM platform_command_audits WHERE resource_id=$1`} {
					var actor, id, legacy string
					if err := g.pool.QueryRow(ctx, q, g.row.ID).Scan(&actor, &id, &legacy); err != nil || actor != "admin" || id != g.claims.UserID || legacy != "" {
						t.Fatal(actor, id, legacy, err)
					}
				}
				var payload []byte
				if err := g.pool.QueryRow(ctx, `SELECT payload FROM platform_outbox WHERE aggregate_id=$1`, g.row.ID).Scan(&payload); err != nil {
					t.Fatal(err)
				}
				var fields map[string]any
				if err := json.Unmarshal(payload, &fields); err != nil || len(fields) != 2 || strings.Contains(string(payload), before.Content) || strings.Contains(string(payload), before.Title) || strings.Contains(string(payload), "deleted_at") {
					t.Fatal("unsafe purge event", string(payload), err)
				}
				for _, mode := range []string{"audit", "history"} {
					row := *before
					row.ID = fmt.Sprint(idgen.New())
					row.FactsVersion = 0
					if err := g.threads.Create(ctx, &row); err != nil {
						t.Fatal(err)
					}
					base, _ := g.threads.GetByID(ctx, row.ID)
					counts := takeDownCounts(t, g, row.ID)
					g.s.SetGovernanceRepository(original)
					g.s.SetReliability(reliability.NewService(transaction.NewPostgreSQL(g.pool), reliability.NewPostgreSQLStore(g.pool)))
					if mode == "audit" {
						g.s.SetReliability(reliability.NewService(transaction.NewPostgreSQL(g.pool), authorFailAudit{Store: reliability.NewPostgreSQLStore(g.pool)}))
					} else {
						g.s.SetGovernanceRepository(reviewHistoryHook{ContentGovernanceRepository: original, ModerationCaseCommandRepository: caseRepo, afterAction: func(context.Context) error { return errors.New("required purge history failed") }})
					}
					if got, err := g.s.AdminPurgeThread(ctx, row.ID, "purge", "required failure", g.proof); err == nil || got != nil {
						t.Fatal("failed purge accepted", mode, got, err)
					}
					actual, err := g.threads.GetByID(ctx, row.ID)
					if err != nil || actual.DeletedAt != nil || !sameGovernanceThread(base, actual) || !reflect.DeepEqual(counts, takeDownCounts(t, g, row.ID)) {
						t.Fatal("partial tombstone persisted", mode, actual, err)
					}
				}
			})
		}
	}
}

func TestThreadAdminPurgePostgreSQLCommittedChangesAndGuard(t *testing.T) {
	for i, mode := range []string{"permission_aba", "binding_revoke", "session_revoke", "session_version", "mfa_expire", "account_suspend", "board_change", "body_aba", "hard_delete_reinsert", "state_change", "date_aba", "trigger_disabled", "counters"} {
		t.Run(mode, func(t *testing.T) {
			g := adminPurgePostgres(t, 146301+i, "admin_opaque")
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
						exec(`INSERT INTO admin_role_permissions(role_id,permission_code,created_by) VALUES($1,'community.thread.purge','test')`, g.role)
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
					case "state_change":
						exec(`UPDATE threads SET deletion_status='active' WHERE id=$1`, g.row.ID)
					case "date_aba":
						exec(`UPDATE threads SET deleted_at=NOW() WHERE id=$1`, g.row.ID)
						exec(`UPDATE threads SET deleted_at=NULL WHERE id=$1`, g.row.ID)

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
			result, err := g.s.AdminPurgeThread(ctx, g.row.ID, "purge", "actual purge", proof)
			if mode == "trigger_disabled" {
				if _, e := g.pool.Exec(ctx, `ALTER TABLE threads ENABLE TRIGGER trg_threads_facts_version`); e != nil {
					t.Fatal(e)
				}
			}
			var actual *domain.Thread
			if e := transaction.NewPostgreSQL(g.pool).Within(ctx, func(tx context.Context) error {
				var getErr error
				actual, getErr = g.threads.LockThreadGovernanceSnapshot(tx, g.row.ID)
				return getErr
			}); e != nil {
				t.Fatal(e)
			}
			if mode == "counters" {
				if err != nil || result == nil || actual.DeletedAt == nil || actual.DeletionStatus != "purged" || actual.ViewCount != 17 || actual.ReplyCount != 3 || actual.LikeCount != 2 {
					t.Fatal(result, actual, err)
				}
				return
			}
			if err == nil || result != nil || actual.DeletedAt != nil || actual.DeletionStatus != "trashed" && mode != "state_change" {
				t.Fatal(fmt.Sprintf("stale purge %s accepted", mode), result, actual, err)
			}
			if counts := takeDownCounts(t, g, g.row.ID); !reflect.DeepEqual(counts, []int{0, 0, 0, 0, 0}) {
				t.Fatal("denied command history/audit", counts)
			}
		})
	}
}
