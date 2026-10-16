package service

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	"github.com/campusos/CampusOS/internal/modules/core/community/repository"
)

func TestAdminThreadMetadataPostgreSQLActualStateMatrixActorsSafeHistoryAndNoWrites(t *testing.T) {
	ctx := context.Background()
	for i, kind := range []string{"admin_numeric", "admin_opaque"} {
		t.Run(kind, func(t *testing.T) {
			g := adminFlagPostgres(t, 2580201+i*10, kind)
			if _, err := g.pool.Exec(ctx, "DELETE FROM admin_role_permissions WHERE role_id=$1", g.role); err != nil {
				t.Fatal(err)
			}
			if _, err := g.pool.Exec(ctx, "INSERT INTO admin_role_permissions(role_id,permission_code,created_by)VALUES($1,'community.thread.read','metadata')", g.role); err != nil {
				t.Fatal(err)
			}
			// Current password sessions may read metadata without write MFA.
			if _, err := g.pool.Exec(ctx, "UPDATE admin_sessions SET authentication_strength='password',mfa_authenticated_at=NULL,version=version+1 WHERE id=$1", g.claims.SessionID); err != nil {
				t.Fatal(err)
			}
			if err := g.s.governance.CreateModerationAction(ctx, &domain.ModerationAction{ID: fmt.Sprint(25802090 + i), ThreadID: g.row.ID, Action: "pin", ActorKind: "admin", ActorRefID: g.claims.UserID, Reason: "PRIVATE_PG_REASON", BeforeState: "PRIVATE_PG_BEFORE", AfterState: "PRIVATE_PG_AFTER", CreatedAt: time.Now()}); err != nil {
				t.Fatal(err)
			}
			for _, pub := range []string{"draft", "published", "private"} {
				for _, mod := range []string{"clear", "pending", "rejected", "taken_down"} {
					for _, del := range []string{"active", "trashed", "purged"} {
						if _, err := g.pool.Exec(ctx, `UPDATE threads SET publication_status=$2,moderation_status=$3,deletion_status=$4::text,status='archived',deleted_at=CASE WHEN $4::text='purged' THEN NOW() ELSE NULL END WHERE id=$1`, g.row.ID, pub, mod, del); err != nil {
							t.Fatal(err)
						}
						var before, after string
						if err := g.pool.QueryRow(ctx, "SELECT to_jsonb(t)::text FROM threads t WHERE id=$1", g.row.ID).Scan(&before); err != nil {
							t.Fatal(err)
						}
						counts := takeDownCounts(t, g, g.row.ID)
						for _, trash := range []bool{false, true} {
							rows, total, err := g.s.ListAdminThreadMetadata(ctx, g.proof, domain.ThreadListFilter{AuthorID: g.row.AuthorID, Page: 1, PageSize: 1}, trash)
							want := int64(0)
							if del == "active" && !trash || del == "trashed" && trash {
								want = 1
							}
							if err != nil || total != want || int64(len(rows)) != want {
								t.Fatal(pub, mod, del, trash, rows, total, err)
							}
							raw, _ := json.Marshal(rows)
							if strings.Contains(string(raw), g.row.Title) || strings.Contains(string(raw), g.row.Content) || strings.Contains(string(raw), "facts_version") {
								t.Fatal("private row", string(raw))
							}
						}
						history, total, err := g.s.AdminModerationMetadata(ctx, g.row.ID, g.proof, 1, 20)
						if err != nil || total != 1 || len(history) != 1 || history[0].ActorKind != "admin" || history[0].ActorRefID != g.claims.UserID {
							t.Fatal(history, total, err)
						}
						raw, _ := json.Marshal(history)
						if strings.Contains(string(raw), "PRIVATE_PG_") || strings.Contains(string(raw), "reason") {
							t.Fatal(string(raw))
						}
						if err := g.pool.QueryRow(ctx, "SELECT to_jsonb(t)::text FROM threads t WHERE id=$1", g.row.ID).Scan(&after); err != nil || before != after || !reflect.DeepEqual(counts, takeDownCounts(t, g, g.row.ID)) {
							t.Fatal("read wrote actual SQL", err)
						}
					}
				}
			}
		})
	}
}

type metadataPGInventoryHook struct {
	*repository.PgThreadRepository
	calls, at int
	after     func()
}

func (r *metadataPGInventoryHook) ListAdminThreadMetadata(ctx context.Context) ([]repository.AdminThreadMetadata, error) {
	rows, err := r.PgThreadRepository.ListAdminThreadMetadata(ctx)
	r.calls++
	if r.calls == r.at && r.after != nil {
		r.after()
	}
	return rows, err
}

type metadataPGHistoryHook struct {
	*repository.PgContentGovernanceRepository
	after func()
}

func (r *metadataPGHistoryHook) ReadAdminModerationMetadata(ctx context.Context, id string) ([]repository.AdminModerationMetadata, error) {
	rows, e := r.PgContentGovernanceRepository.ReadAdminModerationMetadata(ctx, id)
	if r.after != nil {
		f := r.after
		r.after = nil
		f()
	}
	return rows, e
}
func TestAdminThreadMetadataPostgreSQLCommittedAuthorityWholeEmptyHistoryChangesAndGuards(t *testing.T) {
	ctx := context.Background()
	index := 0
	for _, op := range []string{"list", "empty", "history"} {
		for _, mode := range []string{"permission_aba", "session", "row_aba", "final_row", "new_row", "guard", "views"} {
			t.Run(op+"/"+mode, func(t *testing.T) {
				index++
				g := adminFlagPostgres(t, 2580301+index*10, "admin_opaque")
				exec := func(sql string, args ...any) {
					t.Helper()
					if _, err := g.pool.Exec(ctx, sql, args...); err != nil {
						t.Fatal(err)
					}
				}
				exec("DELETE FROM admin_role_permissions WHERE role_id=$1", g.role)
				exec("INSERT INTO admin_role_permissions(role_id,permission_code,created_by)VALUES($1,'community.thread.read','metadata')", g.role)
				hook := &metadataPGInventoryHook{PgThreadRepository: g.threads, at: 1}
				if mode == "final_row" {
					hook.at = 5
				}
				mutated := false
				hook.after = func() {
					mutated = true
					switch mode {
					case "permission_aba":
						exec("DELETE FROM admin_role_permissions WHERE role_id=$1", g.role)
						exec("INSERT INTO admin_role_permissions(role_id,permission_code,created_by)VALUES($1,'community.thread.read','restore')", g.role)
					case "session":
						exec("UPDATE admin_sessions SET revoked_at=NOW(),revoke_reason='metadata',version=version+1 WHERE id=$1", g.claims.SessionID)
					case "row_aba", "final_row":
						exec("UPDATE threads SET content='changed' WHERE id=$1", g.row.ID)
						exec("UPDATE threads SET content=$2 WHERE id=$1", g.row.ID, g.row.Content)
					case "new_row":
						copy := *g.row
						copy.ID = fmt.Sprint(25890001 + index)
						copy.FactsVersion = 0
						if err := g.threads.Create(ctx, &copy); err != nil {
							t.Fatal(err)
						}
					case "guard":
						exec("ALTER TABLE threads DISABLE TRIGGER trg_threads_facts_version")
						t.Cleanup(func() { exec("ALTER TABLE threads ENABLE TRIGGER trg_threads_facts_version") })
					case "views":
						exec("UPDATE threads SET view_count=view_count+1 WHERE id=$1", g.row.ID)
					}
				}
				g.s.repo = hook
				var err error
				if op == "history" {
					_, _, err = g.s.AdminModerationMetadata(ctx, g.row.ID, g.proof, 1, 20)
				} else {
					f := domain.ThreadListFilter{AuthorID: g.row.AuthorID}
					if op == "empty" {
						f.Page = 100
					}
					_, _, err = g.s.ListAdminThreadMetadata(ctx, g.proof, f, false)
				}
				if !mutated || (err == nil) != (mode == "views") {
					t.Fatal("current binding", mode, mutated, err)
				}
				counts := takeDownCounts(t, g, g.row.ID)
				if counts[3] != 0 || counts[4] != 0 {
					t.Fatal("read audit/outbox", counts)
				}
			})
		}
	}
	t.Run("history_append_and_parent_change", func(t *testing.T) {
		for i, mode := range []string{"history_append", "parent_aba"} {
			g := adminFlagPostgres(t, 2580801+i*10, "admin_opaque")
			if _, err := g.pool.Exec(ctx, "INSERT INTO admin_role_permissions(role_id,permission_code,created_by)VALUES($1,'community.thread.read','metadata')", g.role); err != nil {
				t.Fatal(err)
			}
			history := repository.NewPgContentGovernanceRepository(g.pool)
			g.s.governance = &metadataPGHistoryHook{PgContentGovernanceRepository: history, after: func() {
				if mode == "history_append" {
					if err := history.CreateModerationAction(ctx, &domain.ModerationAction{ID: fmt.Sprint(25808090 + i), ThreadID: g.row.ID, Action: "pin", ActorKind: "admin", ActorRefID: g.claims.UserID, CreatedAt: time.Now()}); err != nil {
						t.Fatal(err)
					}
				} else {
					for _, value := range []string{"changed", g.row.Content} {
						if _, err := g.pool.Exec(ctx, "UPDATE threads SET content=$2 WHERE id=$1", g.row.ID, value); err != nil {
							t.Fatal(err)
						}
					}
				}
			}}
			if rows, _, err := g.s.AdminModerationMetadata(ctx, g.row.ID, g.proof, 1, 20); err == nil || rows != nil {
				t.Fatal("late history/earlier parent accepted", mode, rows, err)
			}
		}
	})
}
