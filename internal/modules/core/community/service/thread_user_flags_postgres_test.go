package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	identityrepo "github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	identityservice "github.com/campusos/CampusOS/internal/modules/core/identity/service"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/jackc/pgx/v5/pgconn"
)

func userFlagPostgres(t *testing.T, seed int) takeDownPG {
	t.Helper()
	s, threads, pool, row, current, claims := authorPostgres(t, fmt.Sprint(seed))
	ctx := context.Background()
	role := fmt.Sprint(seed + 10000)
	if _, err := pool.Exec(ctx, `UPDATE users SET nickname='User flag' WHERE id=$1`, claims.UserID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO roles(id,name,description,is_system) VALUES($1,$2,'User flag',FALSE)`, role, "user_flag_"+claims.UserID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO role_permissions(id,role_id,permission_id,created_by)
SELECT v.id,$2,pd.id,'user-flag' FROM (VALUES($1::bigint,'community.thread.pin'),($3::bigint,'community.thread.lock')) v(id,code)
JOIN permission_definitions pd ON pd.code=v.code`, seed+30000, role, seed+30001); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO user_roles(id,user_id,role_id,scope_type,scope_id) VALUES($1,$2,$3,'category',$4)`, seed+20000, claims.UserID, role, row.CategoryID); err != nil {
		t.Fatal(err)
	}
	s.SetUserScopedExecutionAuthority(identityservice.NewUserScopedExecutionAuthorityService(identityrepo.NewPgRoleRepository(pool), current, identityrepo.NewPgAdminAccountRepository(pool)))
	return takeDownPG{s: s, pool: pool, threads: threads, row: row, proof: authorPgProof(current, claims), claims: claims, role: role, actor: "user"}
}

func TestThreadUserFlagPostgreSQLAtomicLocksTypedActorAndSQLRollback(t *testing.T) {
	for i, operation := range []string{"pin", "unpin", "lock", "unlock"} {
		t.Run(operation, func(t *testing.T) {
			g := userFlagPostgres(t, 153201+i*10)
			ctx := context.Background()
			g.row.IsPinned, g.row.IsLocked = operation == "unpin", operation == "unlock"
			if err := g.threads.Update(ctx, g.row); err != nil {
				t.Fatal(err)
			}
			part := userFlagParticipant{record: func(tx context.Context, p identityport.PrincipalContext, _ string, _, _ *domain.Thread) error {
				if !transaction.Active(tx) || p.Actor.Kind != "user" || p.Actor.ID != g.claims.UserID {
					t.Fatal("wrong transaction or actor")
				}
				for _, row := range []struct{ sql, id string }{
					{`SELECT id FROM threads WHERE id=$1 FOR UPDATE NOWAIT`, g.row.ID},
					{`SELECT id FROM users WHERE id=$1 FOR UPDATE NOWAIT`, g.claims.UserID},
					{`SELECT id FROM sessions WHERE id=$1 FOR UPDATE NOWAIT`, g.claims.SessionID},
					{`SELECT id FROM roles WHERE id=$1 FOR UPDATE NOWAIT`, g.role},
					{`SELECT id FROM user_roles WHERE role_id=$1 FOR UPDATE NOWAIT`, g.role},
					{`SELECT id FROM role_permissions WHERE role_id=$1 FOR UPDATE NOWAIT`, g.role},
					{`SELECT id FROM permission_definitions WHERE id IN(SELECT permission_id FROM role_permissions WHERE role_id=$1) FOR UPDATE NOWAIT`, g.role},
				} {
					_, err := g.pool.Exec(ctx, row.sql, row.id)
					var pe *pgconn.PgError
					if !errors.As(err, &pe) || pe.Code != "55P03" {
						t.Fatal("actual User resource/authority row not held", row.sql, err)
					}
				}
				return nil
			}}
			before, _ := g.threads.GetByID(ctx, g.row.ID)
			result, err := g.s.UserSetThreadFlag(ctx, g.row.ID, operation, g.proof, part)
			if err != nil || result == nil {
				t.Fatal(result, err)
			}
			after, _ := g.threads.GetByID(ctx, g.row.ID)
			if after.CurrentRevision != before.CurrentRevision+1 || after.FactsVersion <= before.FactsVersion || after.Content != before.Content || after.Title != before.Title || after.PublicationStatus != before.PublicationStatus || after.ModerationStatus != before.ModerationStatus || (operation == "pin" || operation == "unpin") && after.IsPinned != (operation == "pin") || (operation == "lock" || operation == "unlock") && after.IsLocked != (operation == "lock") {
				t.Fatal("incorrect actual User flags", after)
			}
			if counts := takeDownCounts(t, g, g.row.ID); !reflect.DeepEqual(counts, []int{1, 0, 1, 1, 1}) {
				t.Fatal(counts)
			}
			for _, query := range []string{`SELECT created_actor_kind,created_actor_id,COALESCE(created_by::text,'') FROM content_revisions WHERE thread_id=$1`, `SELECT actor_kind,actor_ref_id,COALESCE(actor_id::text,'') FROM content_moderation_actions WHERE thread_id=$1`} {
				var kind, id, legacy string
				if err := g.pool.QueryRow(ctx, query, g.row.ID).Scan(&kind, &id, &legacy); err != nil || kind != "user" || id != g.claims.UserID || legacy != id {
					t.Fatal("lost typed/legacy User actor", kind, id, legacy, err)
				}
			}
			var payload []byte
			if err := g.pool.QueryRow(ctx, `SELECT payload FROM platform_outbox WHERE aggregate_id=$1`, g.row.ID).Scan(&payload); err != nil {
				t.Fatal(err)
			}
			var fields map[string]any
			if json.Unmarshal(payload, &fields) != nil || len(fields) != 3 || strings.Contains(string(payload), before.Content) || strings.Contains(string(payload), before.Title) {
				t.Fatal("unsafe outbox", string(payload))
			}
			for _, table := range []string{"threads", "content_revisions", "content_moderation_actions", "platform_command_audits", "platform_outbox", "participant"} {
				t.Run("required_"+table, func(t *testing.T) {
					base, _ := g.threads.GetByID(ctx, g.row.ID)
					counts := takeDownCounts(t, g, g.row.ID)
					part := userFlagParticipant{}
					if table == "participant" {
						part.record = func(tx context.Context, _ identityport.PrincipalContext, _ string, _, _ *domain.Thread) error {
							db, _ := transaction.FromContext(tx)
							_, err := db.Exec(tx, `SELECT 1/0`)
							return err
						}
					} else {
						if _, err := g.pool.Exec(ctx, `CREATE OR REPLACE FUNCTION v12_user_flag_reject() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'required User flag write rejected'; END $$`); err != nil {
							t.Fatal(err)
						}
						if _, err := g.pool.Exec(ctx, `CREATE TRIGGER v12_user_flag_reject BEFORE INSERT OR UPDATE ON `+table+` FOR EACH ROW EXECUTE FUNCTION v12_user_flag_reject()`); err != nil {
							t.Fatal(err)
						}
						t.Cleanup(func() {
							if _, err := g.pool.Exec(context.Background(), `DROP TRIGGER v12_user_flag_reject ON `+table+`; DROP FUNCTION v12_user_flag_reject()`); err != nil {
								t.Error(err)
							}
						})
					}
					if got, err := g.s.UserSetThreadFlag(ctx, g.row.ID, operation, g.proof, part); err == nil || got != nil {
						t.Fatal("required SQL failure accepted", table, got, err)
					}
					actual, _ := g.threads.GetByID(ctx, g.row.ID)
					if !sameGovernanceThread(base, actual) || !reflect.DeepEqual(counts, takeDownCounts(t, g, g.row.ID)) {
						t.Fatal("SQL rollback retained partial command", table, actual)
					}
				})
			}
		})
	}
}

func TestThreadUserFlagPostgreSQLCommittedAuthorityResourceABAAndCounters(t *testing.T) {
	for i, mode := range []string{"permission_aba", "scope_aba", "role_metadata_aba", "session_revoke", "user_version", "board_change", "owner_change", "body_aba", "hard_delete_reinsert", "flag_aba", "guard_disabled", "counters"} {
		t.Run(mode, func(t *testing.T) {
			g := userFlagPostgres(t, 153301+i*10)
			ctx := context.Background()
			calls := 0
			proof := func(txCtx context.Context) (identityport.PrincipalContext, error) {
				calls++
				if calls == 3 {
					tx, err := g.pool.Begin(ctx)
					if err != nil {
						t.Fatal(err)
					}
					defer tx.Rollback(ctx)
					update := func(query string, args ...any) {
						if _, err := tx.Exec(ctx, query, args...); err != nil {
							t.Fatal(mode, err)
						}
					}
					switch mode {
					case "permission_aba":
						update(`UPDATE role_permissions SET deleted_at=NOW() WHERE role_id=$1`, g.role)
						update(`UPDATE role_permissions SET deleted_at=NULL WHERE role_id=$1`, g.role)
					case "scope_aba":
						update(`UPDATE user_roles SET scope_id=113112 WHERE role_id=$1`, g.role)
						update(`UPDATE user_roles SET scope_id=113111 WHERE role_id=$1`, g.role)
					case "role_metadata_aba":
						update(`UPDATE roles SET description='Temporary' WHERE id=$1`, g.role)
						update(`UPDATE roles SET description='User flag' WHERE id=$1`, g.role)
					case "session_revoke":
						update(`UPDATE sessions SET revoked_at=NOW(),revoke_reason='test' WHERE id=$1`, g.claims.SessionID)
					case "user_version":
						update(`UPDATE users SET auth_version=auth_version+1 WHERE id=$1`, g.claims.UserID)
					case "board_change":
						update(`UPDATE threads SET category_id=113112 WHERE id=$1`, g.row.ID)
					case "owner_change":
						other := 153900 + i
						update(`INSERT INTO users(id,username,nickname,email,status,auth_version) VALUES($1,$2,'Other flag owner',$3,'active',1)`, other, fmt.Sprintf("other-flag-owner-%d", other), fmt.Sprintf("other-flag-owner-%d@example.test", other))
						update(`UPDATE threads SET author_id=$2 WHERE id=$1`, g.row.ID, other)
					case "body_aba":
						update(`UPDATE threads SET content='Temporary' WHERE id=$1`, g.row.ID)
						update(`UPDATE threads SET content=$2 WHERE id=$1`, g.row.ID, g.row.Content)
					case "hard_delete_reinsert":
						update(`DELETE FROM threads WHERE id=$1`, g.row.ID)
					case "flag_aba":
						update(`UPDATE threads SET is_pinned=TRUE WHERE id=$1`, g.row.ID)
						update(`UPDATE threads SET is_pinned=FALSE WHERE id=$1`, g.row.ID)
					case "guard_disabled":
						update(`ALTER TABLE threads DISABLE TRIGGER trg_threads_facts_version`)
						t.Cleanup(func() {
							g.pool.Exec(context.Background(), `ALTER TABLE threads ENABLE TRIGGER trg_threads_facts_version`)
						})
					case "counters":
						update(`UPDATE threads SET view_count=17,reply_count=3,like_count=2 WHERE id=$1`, g.row.ID)
					}
					if err := tx.Commit(ctx); err != nil {
						t.Fatal(err)
					}
					if mode == "hard_delete_reinsert" {
						if err := g.threads.Create(ctx, g.row); err != nil {
							t.Fatal(err)
						}
					}
				}
				return g.proof(txCtx)
			}
			result, err := g.s.UserSetThreadFlag(ctx, g.row.ID, "pin", proof, userFlagParticipant{})
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
				t.Fatal("committed stale User flags accepted", mode, result, actual, err)
			}
			if counts := takeDownCounts(t, g, g.row.ID); !reflect.DeepEqual(counts, []int{0, 0, 0, 0, 0}) {
				t.Fatal("stale flags retained command evidence", counts)
			}
		})
	}
}
