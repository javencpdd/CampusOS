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

func adminLifecyclePostgres(t *testing.T, seed int, kind, operation string) takeDownPG {
	t.Helper()
	g := takeDownPostgres(t, seed, kind)
	ctx := context.Background()
	admins := identityrepo.NewPgAdminDomainRepository(g.pool)
	sessions := identityservice.NewAdminSessionService(admins, identityrepo.NewPgAdminSessionRepository(g.pool), nil)
	if _, err := g.pool.Exec(ctx, `DELETE FROM admin_role_permissions WHERE role_id=$1`, g.role); err != nil {
		t.Fatal(err)
	}
	if _, err := g.pool.Exec(ctx, `INSERT INTO admin_role_permissions(role_id,permission_code,created_by) VALUES($1,'community.thread.trash','test'),($1,'community.thread.restore','test'),($1,'community.thread.direct_restore','test')`, g.role); err != nil {
		t.Fatal(err)
	}
	g.s.SetAdminExecutionAuthority(identityservice.NewBoardGovernanceAuthorityService(nil, admins, sessions))
	row, _ := g.threads.GetByID(ctx, g.row.ID)
	row.ModerationStatus = "taken_down"
	row.SetModerationActor("user", row.AuthorID)
	if operation == "restore" {
		row.DeletionStatus = "trashed"
	}
	if err := g.threads.Update(ctx, row); err != nil {
		t.Fatal(err)
	}
	g.row = row
	return g
}

func TestThreadAdminLifecyclePostgreSQLAtomicDomainsCasesAndMandatoryRollback(t *testing.T) {
	for i, kind := range []string{"admin_numeric", "admin_opaque"} {
		for j, op := range []string{"trash", "restore", "direct_restore"} {
			for k, withCase := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/%t", kind, op, withCase), func(t *testing.T) {
					g := adminLifecyclePostgres(t, 144201+i*30+j*5+k, kind, op)
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
								t.Fatal("lifecycle dependency not locked", q.sql, err)
							}
						}
					}})
					before, _ := g.threads.GetByID(ctx, g.row.ID)
					notified := 0
					g.s.SetNotificationWriter(lifecycleNotification{after: func(_ context.Context, owner string) error {
						notified++
						if owner != before.AuthorID {
							t.Fatal(owner)
						}
						return nil
					}})
					result, err := g.s.AdminLifecycleThread(ctx, g.row.ID, op, "actual lifecycle", g.proof)
					if err != nil || result == nil {
						t.Fatal(result, err)
					}
					after, _ := g.threads.GetByID(ctx, g.row.ID)
					deletion, moderation := before.DeletionStatus, before.ModerationStatus
					switch op {
					case "trash":
						deletion = "trashed"
					case "restore":
						deletion = "active"
					case "direct_restore":
						moderation = "clear"
					}
					if after.DeletionStatus != deletion || after.ModerationStatus != moderation || after.PublicationStatus != before.PublicationStatus || after.Content != before.Content || after.CurrentRevision != before.CurrentRevision+1 || after.FactsVersion <= before.FactsVersion {
						t.Fatal(after)
					}
					if op == "direct_restore" {
						if after.ModerationActorKind != "admin" || after.ModerationActorID != g.claims.UserID || after.ModerationBy != "" {
							t.Fatal(after)
						}
					} else if after.ModerationActorKind != before.ModerationActorKind || after.ModerationActorID != before.ModerationActorID || after.ModerationBy != before.ModerationBy {
						t.Fatal("unrelated actor changed", after)
					}
					if (notified == 1) != (op == "trash") {
						t.Fatal("notification domain conflated", notified, op)
					}
					caseCount := 0
					if withCase {
						caseCount = 1
					}
					if counts := takeDownCounts(t, g, g.row.ID); !reflect.DeepEqual(counts, []int{1, caseCount, 1, 1, 1}) {
						t.Fatal(counts)
					}
					if withCase {
						var resolvedKind, resolvedID, resolvedLegacy string
						var unresolved bool
						if err := g.pool.QueryRow(ctx, `SELECT resolved_actor_kind,resolved_actor_id,COALESCE(resolved_by::text,''),resolved_at IS NULL FROM content_moderation_cases WHERE id=$1`, caseID).Scan(&resolvedKind, &resolvedID, &resolvedLegacy, &unresolved); err != nil {
							t.Fatal(err)
						}
						if op == "direct_restore" {
							if unresolved || resolvedKind != "admin" || resolvedID != g.claims.UserID || resolvedLegacy != "" {
								t.Fatal(resolvedKind, resolvedID, resolvedLegacy)
							}
						} else if !unresolved || resolvedKind != "legacy_unknown" || resolvedID != "" || resolvedLegacy != "" {
							t.Fatal("unrelated case changed", resolvedKind, resolvedID, resolvedLegacy)
						}
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
					if err := json.Unmarshal(payload, &fields); err != nil || len(fields) != 3 || strings.Contains(string(payload), before.Content) || strings.Contains(string(payload), before.Title) {
						t.Fatal("unsafe lifecycle event", string(payload), err)
					}
					modes := []string{"audit", "history"}
					if op == "direct_restore" && withCase {
						modes = append(modes, "case_resolution")
					}
					if op == "trash" {
						modes = append(modes, "notification")
					}
					for _, mode := range modes {
						row := *before
						row.ID = fmt.Sprint(idgen.New())
						row.FactsVersion = 0
						if err := g.threads.Create(ctx, &row); err != nil {
							t.Fatal(err)
						}
						failureCase := ""
						if withCase {
							failureCase = fmt.Sprint(idgen.New())
							if err := original.CreateModerationCase(ctx, &domain.ModerationCase{ID: failureCase, ThreadID: row.ID, Status: "taken_down", OpenedBy: row.AuthorID, OpenedAt: now}); err != nil {
								t.Fatal(err)
							}
						}
						base, _ := g.threads.GetByID(ctx, row.ID)
						counts := takeDownCounts(t, g, row.ID)
						var caseBefore []byte
						if failureCase != "" {
							if err := g.pool.QueryRow(ctx, `SELECT to_jsonb(c)::text FROM content_moderation_cases c WHERE id=$1`, failureCase).Scan(&caseBefore); err != nil {
								t.Fatal(err)
							}
						}
						g.s.SetGovernanceRepository(original)
						g.s.SetNotificationWriter(nil)
						g.s.SetReliability(reliability.NewService(transaction.NewPostgreSQL(g.pool), reliability.NewPostgreSQLStore(g.pool)))
						var notificationID string
						switch mode {
						case "audit":
							g.s.SetReliability(reliability.NewService(transaction.NewPostgreSQL(g.pool), authorFailAudit{Store: reliability.NewPostgreSQLStore(g.pool)}))
						case "history":
							g.s.SetGovernanceRepository(reviewHistoryHook{ContentGovernanceRepository: original, ModerationCaseCommandRepository: caseRepo, afterAction: func(context.Context) error { return errors.New("required lifecycle history failed") }})
						case "case_resolution":
							g.s.SetGovernanceRepository(reviewFailCase{ContentGovernanceRepository: original, ModerationCaseCommandRepository: caseRepo})
						case "notification":
							notificationID = fmt.Sprint(idgen.New())
							g.s.SetNotificationWriter(lifecycleNotification{after: func(ctx context.Context, owner string) error {
								_, err := transaction.ExecutorFor(ctx, g.pool).Exec(ctx, `INSERT INTO notifications(id,user_id,type,title,content) VALUES($1,$2,'thread_trashed','owned rollback fixture','metadata only')`, notificationID, owner)
								if err != nil {
									return err
								}
								return errors.New("required notification failed after actual insert")
							}})
						}
						if got, err := g.s.AdminLifecycleThread(ctx, row.ID, op, "required failure", g.proof); err == nil || got != nil {
							t.Fatal("required lifecycle failure accepted", mode, got, err)
						}
						actual, _ := g.threads.GetByID(ctx, row.ID)
						if !sameGovernanceThread(base, actual) || !reflect.DeepEqual(counts, takeDownCounts(t, g, row.ID)) {
							t.Fatal("partial lifecycle persisted", mode, actual)
						}
						if failureCase != "" {
							var actualCase []byte
							if err := g.pool.QueryRow(ctx, `SELECT to_jsonb(c)::text FROM content_moderation_cases c WHERE id=$1`, failureCase).Scan(&actualCase); err != nil || !reflect.DeepEqual(actualCase, caseBefore) {
								t.Fatal("case transition persisted", mode, err)
							}
						}
						if notificationID != "" {
							var count int
							if err := g.pool.QueryRow(ctx, `SELECT count(*) FROM notifications WHERE id=$1`, notificationID).Scan(&count); err != nil || count != 0 {
								t.Fatal("notification persisted", count, err)
							}
						}
					}
				})
			}
		}
	}
}

func TestThreadAdminLifecyclePostgreSQLCommittedChangesAndGuard(t *testing.T) {
	for i, mode := range []string{"permission_aba", "binding_revoke", "session_revoke", "session_version", "mfa_expire", "account_suspend", "board_change", "body_aba", "hard_delete_reinsert", "state_change", "trigger_disabled", "counters"} {
		t.Run(mode, func(t *testing.T) {
			g := adminLifecyclePostgres(t, 144301+i, "admin_opaque", "direct_restore")
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
						exec(`INSERT INTO admin_role_permissions(role_id,permission_code,created_by) VALUES($1,'community.thread.trash','test'),($1,'community.thread.restore','test'),($1,'community.thread.direct_restore','test')`, g.role)
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
						exec(`UPDATE threads SET moderation_status='clear' WHERE id=$1`, g.row.ID)

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
			result, err := g.s.AdminLifecycleThread(ctx, g.row.ID, "direct_restore", "actual lifecycle", proof)
			actual, getErr := g.threads.GetByID(ctx, g.row.ID)
			if getErr != nil {
				t.Fatal(getErr)
			}
			if mode == "counters" {
				if err != nil || result == nil || actual.ModerationStatus != "clear" || actual.ViewCount != 17 || actual.ReplyCount != 3 || actual.LikeCount != 2 {
					t.Fatal(result, actual, err)
				}
				return
			}
			if err == nil || result != nil || actual.ModerationActorID != g.row.AuthorID {
				t.Fatal(fmt.Sprintf("stale lifecycle %s accepted", mode), result, actual, err)
			}
			if counts := takeDownCounts(t, g, g.row.ID); !reflect.DeepEqual(counts, []int{0, 0, 0, 0, 0}) {
				t.Fatal("denied command history/audit", counts)
			}
		})
	}
}
