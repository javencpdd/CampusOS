package space

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestSpaceAvatarSelectPostgreSQLActualLocksAndRequiredSQLRollback(t *testing.T) {
	ctx := context.Background()
	index := 0
	for _, existing := range []bool{false, true} {
		for _, failure := range []string{"none", "platform_command_audits", "platform_outbox"} {
			t.Run(fmt.Sprint(existing)+"/"+failure, func(t *testing.T) {
				index++
				owner := fmt.Sprint(232201 + index*10)
				s, repo, pool, proof, claims := spaceOwnerPG(t, owner, existing)
				files, err := s.fileStore.avatarFiles(owner)
				if err != nil || len(files) < 2 {
					t.Fatal(files, err)
				}
				name := files[len(files)-1].name
				before := spaceOwnerPGState(t, pool, owner)
				ran := false
				s.repo = &spaceOwnerPGHook{PgRepository: repo, after: func(tx context.Context, space *Space) {
					ran = true
					for _, row := range []struct{ q, id string }{{"SELECT id FROM users WHERE id=$1 FOR UPDATE NOWAIT", owner}, {"SELECT id FROM sessions WHERE id=$1 FOR UPDATE NOWAIT", claims.SessionID}} {
						_, err := pool.Exec(ctx, row.q, row.id)
						var p *pgconn.PgError
						if !errors.As(err, &p) || p.Code != "55P03" {
							t.Fatal("actual User/session lock absent", row, err)
						}
					}
					var acquired bool
					if err := pool.QueryRow(ctx, "SELECT pg_try_advisory_xact_lock(hashtextextended('personal-space:' || $1::text,0))", owner).Scan(&acquired); err != nil || acquired {
						t.Fatal("owner admission lock absent", err)
					}
					if existing {
						_, err := pool.Exec(ctx, "SELECT id FROM user_spaces WHERE user_id=$1 FOR UPDATE NOWAIT", owner)
						var p *pgconn.PgError
						if !errors.As(err, &p) || p.Code != "55P03" {
							t.Fatal("Space row lock absent", err)
						}
					}
				}}
				if failure != "none" {
					_, err := pool.Exec(ctx, "CREATE FUNCTION avatar_select_reject() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'avatar required write rejected'; END $$; CREATE TRIGGER avatar_select_reject BEFORE INSERT ON "+failure+" FOR EACH ROW EXECUTE FUNCTION avatar_select_reject()")
					if err != nil {
						t.Fatal(err)
					}
					defer func() {
						if _, err := pool.Exec(context.Background(), "DROP TRIGGER avatar_select_reject ON "+failure+"; DROP FUNCTION avatar_select_reject()"); err != nil {
							t.Fatal(err)
						}
					}()
				}
				ack, err := s.SelectAvatarForPrincipal(ctx, name, proof)
				if !ran || (err == nil) != (failure == "none") {
					t.Fatal("actual selection not executed", ack, err, ran)
				}
				if failure != "none" {
					var p *pgconn.PgError
					if !errors.As(err, &p) || p.Code != "P0001" || ack != nil || before != spaceOwnerPGState(t, pool, owner) {
						t.Fatal("required SQL not rolled back", ack, err)
					}
				} else {
					stored, err := repo.GetByUserID(ctx, owner)
					if err != nil || stored.Avatar != s.fileStore.AvatarURL(owner, name) || ack == nil || ack.OwnerID != owner {
						t.Fatal(stored, ack, err)
					}
					var audits, events int
					if err := pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM platform_command_audits WHERE actor_id=$1::text AND command_code='feature.personal_space.avatar_select'),(SELECT count(*) FROM platform_outbox WHERE aggregate_id=$1::text)", owner).Scan(&audits, &events); err != nil || audits != 1 || events != 1 {
						t.Fatal(audits, events, err)
					}
				}
				after, err := s.fileStore.avatarFiles(owner)
				if err != nil || !reflect.DeepEqual(files, after) {
					t.Fatal("selection changed bytes", err)
				}
			})
		}
	}
}

func TestSpaceAvatarSelectPostgreSQLHeldFactsAndPostWriteChangesClose(t *testing.T) {
	ctx := context.Background()
	for index, mode := range []string{"space_aba", "session", "bytes", "inode", "inventory", "extra_space", "extra_session"} {
		t.Run(mode, func(t *testing.T) {
			owner := fmt.Sprint(233201 + index*10)
			s, repo, pool, proof, claims := spaceOwnerPG(t, owner, true)
			files, err := s.fileStore.avatarFiles(owner)
			if err != nil || len(files) < 2 {
				t.Fatal(files, err)
			}
			name := files[len(files)-1].name
			before := spaceOwnerPGState(t, pool, owner)
			ran := false
			if mode == "space_aba" {
				s.repo = &spaceOwnerPGHook{PgRepository: repo, afterRead: func() {
					ran = true
					var old string
					if err := pool.QueryRow(ctx, "SELECT bio FROM user_spaces WHERE user_id=$1", owner).Scan(&old); err != nil {
						t.Fatal(err)
					}
					if _, err := pool.Exec(ctx, "UPDATE user_spaces SET bio='ABA' WHERE user_id=$1", owner); err != nil {
						t.Fatal(err)
					}
					if _, err := pool.Exec(ctx, "UPDATE user_spaces SET bio=$2 WHERE user_id=$1", owner, old); err != nil {
						t.Fatal(err)
					}
				}}
			} else if mode == "session" {
				base := proof
				calls := 0
				proof = func(ctx context.Context) (identityport.PrincipalContext, error) {
					p, e := base(ctx)
					calls++
					if calls == 2 {
						ran = true
						if _, err := pool.Exec(ctx, "UPDATE sessions SET revoked_at=NOW(),revoke_reason='avatar test' WHERE id=$1", claims.SessionID); err != nil {
							t.Fatal(err)
						}
					}
					return p, e
				}
			} else {
				s.repo = &spaceOwnerPGHook{PgRepository: repo, after: func(tx context.Context, space *Space) {
					ran = true
					switch mode {
					case "extra_space":
						_, err = transaction.ExecutorFor(tx, pool).Exec(tx, "UPDATE user_spaces SET cover_image='extra' WHERE user_id=$1", owner)
					case "extra_session":
						_, err = transaction.ExecutorFor(tx, pool).Exec(tx, "UPDATE sessions SET revoked_at=NOW(),revoke_reason='avatar test' WHERE id=$1", claims.SessionID)
					default:
						mutateSelectedAvatar(t, s, owner, name, mode)
					}
					if err != nil {
						t.Fatal("actual mutation failed", err)
					}
				}}
			}
			ack, err := s.SelectAvatarForPrincipal(ctx, name, proof)
			if !ran || err == nil || ack != nil {
				t.Fatal("current changed facts accepted", ack, err, ran)
			}
			if mode != "space_aba" && mode != "session" && before != spaceOwnerPGState(t, pool, owner) {
				t.Fatal("post-write metadata escaped rollback")
			}
		})
	}
}
