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

func adminReviewPostgres(t *testing.T, seed int, kind string) takeDownPG {
	t.Helper()
	g := takeDownPostgres(t, seed, kind)
	ctx := context.Background()
	admins := identityrepo.NewPgAdminDomainRepository(g.pool)
	sessions := identityservice.NewAdminSessionService(admins, identityrepo.NewPgAdminSessionRepository(g.pool), nil)
	if _, err := g.pool.Exec(ctx, `DELETE FROM admin_role_permissions WHERE role_id=$1`, g.role); err != nil {
		t.Fatal(err)
	}
	if _, err := g.pool.Exec(ctx, `INSERT INTO admin_role_permissions(role_id,permission_code,created_by) VALUES($1,'community.thread.review','test')`, g.role); err != nil {
		t.Fatal(err)
	}
	g.s.SetAdminExecutionAuthority(identityservice.NewBoardGovernanceAuthorityService(nil, admins, sessions))
	row, _ := g.threads.GetByID(ctx, g.row.ID)
	row.ModerationStatus = "pending"
	if err := g.threads.Update(ctx, row); err != nil {
		t.Fatal(err)
	}
	g.row = row
	return g
}

func TestThreadAdminReviewPostgreSQLAtomicDomainsCasesLocksAndRollback(t *testing.T) {
	for i, kind := range []string{"admin_numeric", "admin_opaque"} {
		for j, op := range []string{"approve", "reject"} {
			for k, withCase := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/%t", kind, op, withCase), func(t *testing.T) {
					g := adminReviewPostgres(t, 142201+i*20+j*5+k, kind)
					ctx := context.Background()
					now := time.Now().UTC()
					original := g.s.governance
					caseRepo := original.(repository.ModerationCaseCommandRepository)
					caseID := ""
					if withCase {
						caseID = fmt.Sprint(idgen.New())
						if err := original.CreateModerationCase(ctx, &domain.ModerationCase{ID: caseID, ThreadID: g.row.ID, Status: "pending", Reason: "actual existing case", OpenedBy: g.row.AuthorID, OpenedAt: now}); err != nil {
							t.Fatal(err)
						}
					}
					g.s.SetGovernanceRepository(reviewLockHistory{ContentGovernanceRepository: original, ModerationCaseCommandRepository: caseRepo, afterRevision: func(context.Context) {
						queries := []struct{ sql, id string }{{`SELECT id FROM threads WHERE id=$1 FOR UPDATE NOWAIT`, g.row.ID}, {`SELECT id FROM admin_accounts WHERE id=$1 FOR UPDATE NOWAIT`, g.claims.UserID}, {`SELECT id FROM admin_sessions WHERE id=$1 FOR UPDATE NOWAIT`, g.claims.SessionID}, {`SELECT id FROM admin_roles WHERE id=$1 FOR UPDATE NOWAIT`, g.role}, {`SELECT id FROM admin_role_bindings WHERE role_id=$1 FOR UPDATE NOWAIT`, g.role}, {`SELECT role_id FROM admin_role_permissions WHERE role_id=$1 FOR UPDATE NOWAIT`, g.role}}
						if caseID != "" {
							queries = append(queries, struct{ sql, id string }{`SELECT id FROM content_moderation_cases WHERE id=$1 FOR UPDATE NOWAIT`, caseID})
						}
						for _, q := range queries {
							_, err := g.pool.Exec(ctx, q.sql, q.id)
							var pe *pgconn.PgError
							if !errors.As(err, &pe) || pe.Code != "55P03" {
								t.Fatal("review dependency row not locked", q.sql, err)
							}
						}
					}})
					before, _ := g.threads.GetByID(ctx, g.row.ID)
					result, err := g.s.AdminReviewThread(ctx, g.row.ID, op, "actual typed review", g.proof)
					if err != nil || result == nil {
						t.Fatal(result, err)
					}
					after, _ := g.threads.GetByID(ctx, g.row.ID)
					status := "clear"
					if op == "reject" {
						status = "rejected"
					}
					if string(after.ModerationStatus) != status || after.ModerationActorKind != "admin" || after.ModerationActorID != g.claims.UserID || after.ModerationBy != "" || after.Content != before.Content || after.PublicationStatus != before.PublicationStatus || after.FactsVersion <= before.FactsVersion || after.CurrentRevision != before.CurrentRevision+1 {
						t.Fatal(after)
					}
					caseCount := 0
					if withCase || op == "reject" {
						caseCount = 1
					}
					if counts := takeDownCounts(t, g, g.row.ID); !reflect.DeepEqual(counts, []int{1, caseCount, 1, 1, 1}) {
						t.Fatal(counts)
					}
					if caseCount != 0 {
						var openedKind, openedID, openedLegacy, resolvedKind, resolvedID, resolvedLegacy string
						var unresolved bool
						if err := g.pool.QueryRow(ctx, `SELECT opened_actor_kind,opened_actor_id,COALESCE(opened_by::text,''),resolved_actor_kind,resolved_actor_id,COALESCE(resolved_by::text,''),resolved_at IS NULL FROM content_moderation_cases WHERE thread_id=$1`, g.row.ID).Scan(&openedKind, &openedID, &openedLegacy, &resolvedKind, &resolvedID, &resolvedLegacy, &unresolved); err != nil {
							t.Fatal(err)
						}
						if withCase {
							if openedKind != "user" || openedID != g.row.AuthorID || openedLegacy != g.row.AuthorID {
								t.Fatal(openedKind, openedID, openedLegacy)
							}
						} else if openedKind != "admin" || openedID != g.claims.UserID || openedLegacy != "" {
							t.Fatal(openedKind, openedID, openedLegacy)
						}
						if op == "approve" && (unresolved || resolvedKind != "admin" || resolvedID != g.claims.UserID || resolvedLegacy != "") {
							t.Fatal(resolvedKind, resolvedID, resolvedLegacy)
						}
						if op == "reject" && (!unresolved || resolvedKind != "legacy_unknown" || resolvedID != "" || resolvedLegacy != "") {
							t.Fatal("reject closed case", resolvedKind, resolvedID, resolvedLegacy)
						}
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
					if err := json.Unmarshal(payload, &fields); err != nil || len(fields) != 2 || strings.Contains(string(payload), before.Content) || strings.Contains(string(payload), before.Title) {
						t.Fatal("unsafe review event", string(payload), err)
					}
					modes := []string{"audit", "history"}
					if op == "approve" && withCase {
						modes = append(modes, "case_resolution")
					}
					if op == "reject" && !withCase {
						modes = append(modes, "case_creation")
					}
					for _, mode := range modes {
						row := *before
						row.ID = fmt.Sprint(idgen.New())
						row.FactsVersion = 0
						row.ModerationStatus = "pending"
						if err := g.threads.Create(ctx, &row); err != nil {
							t.Fatal(err)
						}
						failCase := ""
						if withCase {
							failCase = fmt.Sprint(idgen.New())
							if err := original.CreateModerationCase(ctx, &domain.ModerationCase{ID: failCase, ThreadID: row.ID, Status: "pending", OpenedBy: row.AuthorID, OpenedAt: now}); err != nil {
								t.Fatal(err)
							}
						}
						base, _ := g.threads.GetByID(ctx, row.ID)
						counts := takeDownCounts(t, g, row.ID)
						var caseBefore []byte
						if failCase != "" {
							if err := g.pool.QueryRow(ctx, `SELECT to_jsonb(c)::text FROM content_moderation_cases c WHERE id=$1`, failCase).Scan(&caseBefore); err != nil {
								t.Fatal(err)
							}
						}
						g.s.SetGovernanceRepository(original)
						g.s.SetReliability(reliability.NewService(transaction.NewPostgreSQL(g.pool), reliability.NewPostgreSQLStore(g.pool)))
						switch mode {
						case "audit":
							g.s.SetReliability(reliability.NewService(transaction.NewPostgreSQL(g.pool), authorFailAudit{Store: reliability.NewPostgreSQLStore(g.pool)}))
						case "history":
							g.s.SetGovernanceRepository(reviewHistoryHook{ContentGovernanceRepository: original, ModerationCaseCommandRepository: caseRepo, afterAction: func(context.Context) error { return errors.New("forced required history error") }})
						default:
							g.s.SetGovernanceRepository(reviewFailCase{ContentGovernanceRepository: original, ModerationCaseCommandRepository: caseRepo})
						}
						if got, err := g.s.AdminReviewThread(ctx, row.ID, op, "required failure", g.proof); err == nil || got != nil {
							t.Fatal("required review failure accepted", mode, got, err)
						}
						actual, _ := g.threads.GetByID(ctx, row.ID)
						if !sameGovernanceThread(base, actual) || !reflect.DeepEqual(counts, takeDownCounts(t, g, row.ID)) {
							t.Fatal("partial review persisted", mode, actual)
						}
						if failCase != "" {
							var afterCase []byte
							if err := g.pool.QueryRow(ctx, `SELECT to_jsonb(c)::text FROM content_moderation_cases c WHERE id=$1`, failCase).Scan(&afterCase); err != nil || !reflect.DeepEqual(caseBefore, afterCase) {
								t.Fatal("case resolution persisted", mode, string(afterCase), err)
							}
						}
					}
				})
			}
		}
	}
}

type reviewLockHistory struct {
	repository.ContentGovernanceRepository
	repository.ModerationCaseCommandRepository
	afterRevision func(context.Context)
}

func (r reviewLockHistory) CreateRevision(ctx context.Context, value *domain.ContentRevision) error {
	if err := r.ContentGovernanceRepository.CreateRevision(ctx, value); err != nil {
		return err
	}
	r.afterRevision(ctx)
	return nil
}

func TestThreadAdminReviewPostgreSQLCommittedChangesAndGuard(t *testing.T) {
	for i, mode := range []string{"permission_aba", "binding_revoke", "session_revoke", "session_version", "mfa_expire", "account_suspend", "board_change", "body_aba", "hard_delete_reinsert", "state_change", "trigger_disabled", "counters"} {
		t.Run(mode, func(t *testing.T) {
			g := adminReviewPostgres(t, 142301+i, "admin_opaque")
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
						exec(`INSERT INTO admin_role_permissions(role_id,permission_code,created_by) VALUES($1,'community.thread.review','test')`, g.role)
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
			result, err := g.s.AdminReviewThread(ctx, g.row.ID, "approve", "actual review", proof)
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
			if err == nil || result != nil || actual.ModerationActorID != "" {
				t.Fatal(fmt.Sprintf("stale review %s accepted", mode), result, actual, err)
			}
			if counts := takeDownCounts(t, g, g.row.ID); !reflect.DeepEqual(counts, []int{0, 0, 0, 0, 0}) {
				t.Fatal("denied command history/audit", counts)
			}
		})
	}
}
